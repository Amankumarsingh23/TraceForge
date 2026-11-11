param([string]$Gateway = 'http://localhost:8080')
$ErrorActionPreference = 'Stop'

function Assert-Trace([string]$TraceId, [bool]$Failed) {
    $deadline = [DateTime]::UtcNow.AddSeconds(20)
    do {
        $lines = & docker compose logs --no-color --no-log-prefix otel-collector 2>&1
        if ($LASTEXITCODE -ne 0) { throw 'Cannot read Collector logs' }
        $text = $lines -join "`n"
        $spans = @()
        foreach ($match in [regex]::Matches($text, '(?ms)^Span #\d+\s*\r?\n(.*?)(?=^Span #\d+|^ResourceSpans #|\z)')) {
            $block = $match.Groups[1].Value
            if ($block -notmatch "Trace ID\s*:\s*$TraceId\b") { continue }
            $id = [regex]::Match($block, '(?m)^\s*ID\s*:\s*([a-f0-9]{16})').Groups[1].Value
            $parent = [regex]::Match($block, '(?m)^\s*Parent ID\s*:\s*([a-f0-9]*)').Groups[1].Value
            $name = [regex]::Match($block, '(?m)^\s*Name\s*:\s*(.+)').Groups[1].Value.Trim()
            $kind = [regex]::Match($block, '(?m)^\s*Kind\s*:\s*(.+)').Groups[1].Value.Trim()
            $status = [regex]::Match($block, '(?m)^\s*Status code\s*:\s*(.+)').Groups[1].Value.Trim()
            $spans += [pscustomobject]@{ Id=$id; Parent=$parent; Name=$name; Kind=$kind; Status=$status }
        }
        $spans = @($spans | Sort-Object Id -Unique)
        if ($spans.Count -eq 5) { break }
        Start-Sleep -Milliseconds 250
    } while ([DateTime]::UtcNow -lt $deadline)
    if ($spans.Count -ne 5) { throw "Expected 5 exported spans for $TraceId; found $($spans.Count)" }
    $g = @($spans | Where-Object Name -eq 'GET /checkout')
    $p = @($spans | Where-Object Name -eq 'GET /charge')
    $c = @($spans | Where-Object Kind -eq 'Client')
    $v = @($spans | Where-Object Name -eq 'checkout.validate')
    $i = @($spans | Where-Object Name -eq 'payment.process')
    foreach ($operation in @($g,$p,$c,$v,$i)) { if ($operation.Count -ne 1) { throw 'Missing or duplicate operation' } }
    if ($g[0].Kind -ne 'Server' -or $p[0].Kind -ne 'Server' -or
        $c[0].Parent -ne $g[0].Id -or $v[0].Parent -ne $g[0].Id -or
        $p[0].Parent -ne $c[0].Id -or $i[0].Parent -ne $p[0].Id -or
        $g[0].Parent -notin @('', '0000000000000000')) { throw 'Incorrect trace hierarchy' }
    if ($Failed) {
        foreach ($operation in @($g[0],$p[0],$c[0],$i[0])) {
            if ($operation.Status -ne 'Error') { throw "Missing error status on $($operation.Name)" }
        }
    }
    "Verified Collector trace $TraceId"
    $spans | Format-Table
}

foreach ($scenario in @(
    @{ Name='normal'; Query=''; Code=200 },
    @{ Name='delayed'; Query='?delay_ms=500'; Code=200 },
    @{ Name='failed'; Query='?fail=true'; Code=502 }
)) {
    $watch = [Diagnostics.Stopwatch]::StartNew()
    $response = Invoke-WebRequest -Uri "$Gateway/checkout$($scenario.Query)" -SkipHttpErrorCheck -TimeoutSec 10
    $watch.Stop()
    if ([int]$response.StatusCode -ne $scenario.Code) { throw "Wrong status for $($scenario.Name)" }
    if ($scenario.Name -eq 'delayed' -and $watch.ElapsedMilliseconds -lt 500) { throw 'Delay was not applied' }
    $body = $response.Content | ConvertFrom-Json
    if ($body.trace_id -notmatch '^[a-f0-9]{32}$' -or $body.trace_id -eq ('0'*32)) { throw 'Invalid trace ID' }
    "$($scenario.Name): HTTP $($response.StatusCode), $($watch.ElapsedMilliseconds)ms"
    Assert-Trace $body.trace_id ($scenario.Code -eq 502)
}

& docker compose exec -T clickhouse sh -c 'clickhouse-client --user traceforge --password "$CLICKHOUSE_PASSWORD" --query "SELECT 1"'
if ($LASTEXITCODE -ne 0) { throw 'ClickHouse SQL check failed' }
& docker compose exec -T postgres sh -c 'PGPASSWORD="$POSTGRES_PASSWORD" psql -h 127.0.0.1 -U traceforge -d traceforge -v ON_ERROR_STOP=1 -c "SELECT 1"'
if ($LASTEXITCODE -ne 0) { throw 'PostgreSQL SQL check failed' }
'All smoke checks passed. Database connectivity does not imply telemetry persistence.'
