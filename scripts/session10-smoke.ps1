# Run against rebuilt local Docker backend and the rebuilt gateway/payment demo.
# Start the demo with TRACEFORGE_SERVICE_NAMESPACE set to a unique run identity.
param([string]$Namespace = (Get-Content artifacts/session10/namespace.txt -Raw).Trim())
$ErrorActionPreference = 'Stop'
New-Item -ItemType Directory -Force artifacts/session10 | Out-Null
if ($Namespace -notmatch '^session10-[0-9]{14}$') { throw 'Use an isolated session10-YYYYMMDDHHMMSS demo namespace' }
$from = [DateTime]::UtcNow
$requests = @()
for ($i=0; $i -lt 100; $i++) {
    $kind = if ($i -lt 80) { 'normal' } elseif ($i -lt 95) { 'delayed' } else { 'failed' }
    $query = if ($kind -eq 'delayed') { '?delay_ms=100' } elseif ($kind -eq 'failed') { '?fail=true' } else { '' }
    $response = Invoke-WebRequest "http://127.0.0.1:8080/checkout$query" -SkipHttpErrorCheck -TimeoutSec 6
    $body = $response.Content | ConvertFrom-Json
    $expected = if ($kind -eq 'failed') { 502 } else { 200 }
    if ($response.StatusCode -ne $expected -or $body.trace_id -notmatch '^[a-f0-9]{32}$') { throw "Request $i did not complete as expected" }
    $requests += @{ kind=$kind; trace_id=$body.trace_id; http_status=$response.StatusCode }
}
$to = [DateTime]::UtcNow
$parameters = 'from='+[Uri]::EscapeDataString($from.ToString('o'))+'&to='+[Uri]::EscapeDataString($to.ToString('o'))+'&namespace='+$Namespace+'&workload=synthetic&bucket_seconds=60'
$url = 'http://127.0.0.1:8082/api/v1/service-graph?'+$parameters
$deadline = [DateTime]::UtcNow.AddSeconds(40)
do {
    $before = Invoke-RestMethod $url -TimeoutSec 6
    $gateway = @($before.services | Where-Object { $_.identity.name -eq 'gateway' })
    $payment = @($before.services | Where-Object { $_.identity.name -eq 'payment' })
    if ($gateway.Count -eq 1 -and $payment.Count -eq 1 -and $gateway[0].stats.count -eq 100 -and $payment[0].stats.count -eq 100 -and $before.edges.Count -eq 1 -and $before.edges[0].client.count -eq 100) { break }
    Start-Sleep -Milliseconds 500
} while ([DateTime]::UtcNow -lt $deadline)
if ($gateway.Count -ne 1 -or $payment.Count -ne 1 -or $gateway[0].stats.count -ne 100 -or $payment[0].stats.count -ne 100 -or $before.edges.Count -ne 1 -or $before.edges[0].client.count -ne 100) { throw 'Finite telemetry polling did not reach expected SERVER and pair counts' }
foreach ($stats in @($gateway[0].stats,$payment[0].stats,$before.edges[0].client,$before.edges[0].server)) {
    if ($stats.errors -ne 5 -or [Math]::Abs($stats.error_fraction-.05) -gt .0000001 -or $stats.duration_samples -ne 100 -or $stats.p95_ms -lt 90) { throw 'Error or delayed duration evidence mismatch' }
}
if ($payment[0].stats.http_status.'503' -ne 5 -or $gateway[0].stats.http_status.'502' -ne 5) { throw 'HTTP response status evidence mismatch' }
$before | ConvertTo-Json -Depth 35 | Set-Content artifacts/session10/before-replay.json
# Duplicate actual physical rows for one isolated request. No fixture or trace ID is invented.
$trace = $requests[0].trace_id
$replaySql = 'INSERT INTO traceforge.spans SELECT * REPLACE (now64(3) AS ingested_at) FROM traceforge.spans WHERE trace_id = {tid:String}'
& docker compose exec -T clickhouse sh -c 'exec clickhouse-client --user traceforge --password "$CLICKHOUSE_PASSWORD" --param_tid "$1" --query "$2"' sh $trace $replaySql
if ($LASTEXITCODE -ne 0) { throw 'Isolated replay insertion failed' }
$after = Invoke-RestMethod $url -TimeoutSec 6
if (($before.services | ConvertTo-Json -Depth 35 -Compress) -ne ($after.services | ConvertTo-Json -Depth 35 -Compress) -or ($before.edges | ConvertTo-Json -Depth 35 -Compress) -ne ($after.edges | ConvertTo-Json -Depth 35 -Compress) -or $after.diagnostics.physical_rows -ne ($before.diagnostics.physical_rows+5) -or $after.diagnostics.duplicate_rows -ne ($before.diagnostics.duplicate_rows+5)) { throw 'Replay inflated counts or failed to expose physical duplication' }
$after | ConvertTo-Json -Depth 35 | Set-Content artifacts/session10/after-replay.json
@{ namespace=$Namespace; from=$from.ToString('o'); to=$to.ToString('o'); query=$parameters; requests=$requests; replay_trace_id=$trace; services=$after.services; edges=$after.edges; diagnostics=$after.diagnostics } | ConvertTo-Json -Depth 35 | Set-Content artifacts/session10/evidence.json
Write-Output "PASS: 80 normal, 15 delayed, 5 failed actual requests; gateway/payment each 100 SERVER, 5 ERROR; 100 confirmed pairs. Replay stable. Window $($from.ToString('o')) to $($to.ToString('o'))."
