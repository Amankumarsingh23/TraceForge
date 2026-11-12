$ErrorActionPreference='Stop'
$savedPassword=$env:CLICKHOUSE_PASSWORD
try {
    foreach ($line in Get-Content .env) { if ($line -match '^CLICKHOUSE_PASSWORD=(.+)$') { $env:CLICKHOUSE_PASSWORD=$Matches[1] } }
    & docker compose stop clickhouse
    if ($LASTEXITCODE -ne 0) { throw 'Controlled stop failed' }
    & .\bin\otel-loadgen.exe --requests 2 --spans-per-request 10 --concurrency 2 --connections 2 --output artifacts/session04-outage-load.json
    $loadExit=$LASTEXITCODE
    $report=Get-Content artifacts/session04-outage-load.json -Raw | ConvertFrom-Json
    if ($loadExit -ne 1 -or $report.load.acknowledged_spans -ne 0 -or $report.load.retryable_failures -ne 2 -or $report.storage_verified) { throw 'Outage reporting contract failed' }
    $report.load | Select-Object attempted_requests,generated_spans,acknowledged_spans,retryable_failures,unknown_outcomes
} finally {
    & docker compose start clickhouse
    $deadline=[DateTime]::UtcNow.AddSeconds(90)
    do {
        try { $ready=(Invoke-WebRequest http://localhost:8082/readyz -TimeoutSec 3).StatusCode -eq 200 } catch { $ready=$false }
        if ($ready) { break }
        Start-Sleep -Milliseconds 500
    } while ([DateTime]::UtcNow -lt $deadline)
    $env:CLICKHOUSE_PASSWORD=$savedPassword
    if (-not $ready) { throw 'Database did not recover' }
}
$prefix=$report.load.config.run_id
if ($prefix -notmatch '^[a-f0-9]{16}$') { throw 'Bad fixture prefix' }
$sql="SELECT count() FROM traceforge.spans WHERE startsWith(trace_id,'$prefix');"
$sql | docker compose exec -T clickhouse sh -c 'clickhouse-client --user traceforge --password "$CLICKHOUSE_PASSWORD" --multiquery' | Tee-Object -FilePath artifacts/session04-outage-sql.txt
if ($LASTEXITCODE -ne 0) { throw 'Post-recovery accounting failed' }
