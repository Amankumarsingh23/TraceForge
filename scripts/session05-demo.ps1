param([switch]$ReuseFixtures)
$ErrorActionPreference = 'Stop'
$savedPassword = $env:CLICKHOUSE_PASSWORD
try {
    New-Item -ItemType Directory -Force artifacts/session05 | Out-Null
    if (-not $env:CLICKHOUSE_PASSWORD) {
        foreach ($line in Get-Content .env) {
            if ($line -match '^CLICKHOUSE_PASSWORD=(.+)$') { $env:CLICKHOUSE_PASSWORD = $Matches[1] }
        }
    }
    $fixtureLog = 'artifacts/session05/fixture-setup.log'
    if (-not $ReuseFixtures) {
        & go test -count=1 -tags integration ./tests/integration -run '^(TestTraceQueryReplay|TestSyntheticGraphAPI|TestTraceDetailLimitAPI)$' -v *> $fixtureLog
        if ($LASTEXITCODE -ne 0) { throw 'Fixture exports/verification failed' }
    }
    $log = Get-Content $fixtureLog -Raw
    $replay = [regex]::Match($log, 'replayed real trace=([a-f0-9]{32})').Groups[1].Value
    $anomaly = [regex]::Match($log, 'synthetic trace=([a-f0-9]{32})').Groups[1].Value
    $oversized = [regex]::Match($log, 'trace=([a-f0-9]{32}) physical_rows=1001').Groups[1].Value
    if (-not $replay -or -not $anomaly -or -not $oversized) { throw 'Missing verified fixture IDs' }
    $scenarios = @{}
    foreach ($scenario in @(@{name='normal';query='';status=200},@{name='delayed';query='?delay_ms=500';status=200},@{name='failed';query='?fail=true';status=502})) {
        $response = Invoke-WebRequest "http://localhost:8080/checkout$($scenario.query)" -SkipHttpErrorCheck -TimeoutSec 10
        if ([int]$response.StatusCode -ne $scenario.status) { throw "Unexpected $($scenario.name) HTTP status" }
        $id = ($response.Content | ConvertFrom-Json).trace_id
        $deadline = [DateTime]::UtcNow.AddSeconds(20)
        do {
            try { $detail = Invoke-RestMethod "http://localhost:8082/api/v1/traces/$id" -TimeoutSec 5 } catch { $detail = $null }
            if ($detail -and $detail.diagnostics.unique_spans -eq 5) { break }
            Start-Sleep -Milliseconds 250
        } while ([DateTime]::UtcNow -lt $deadline)
        if (-not $detail -or $detail.diagnostics.unique_spans -ne 5) { throw 'Demo trace did not persist five spans' }
        $detail | ConvertTo-Json -Depth 80 | Set-Content "artifacts/session05/$($scenario.name).json"
        $scenarios[$scenario.name] = @{id=$id;http_status=[int]$response.StatusCode}
    }
    & go run ./cmd/otel-loadgen --requests 1 --spans-per-request 1000 --concurrency 1 --connections 1 --seed 42 --verify-storage=false --output artifacts/session05/large-load.json
    if ($LASTEXITCODE -ne 0) { throw 'Large trace export failed' }
    $largeReport = Get-Content artifacts/session05/large-load.json -Raw | ConvertFrom-Json
    # First request: index 0, default seed 42 -> suffix 1 XOR 42 = 43.
    $largeID = $largeReport.load.config.run_id + '000000000000002b'
    $largeDetail = Invoke-RestMethod "http://localhost:8082/api/v1/traces/$largeID" -TimeoutSec 5
    if ($largeDetail.diagnostics.unique_spans -ne 1000) { throw 'Expected 1000 persisted unique spans' }
    # Supply enough real trace identities for pagination on an otherwise new DB.
    & go run ./cmd/otel-loadgen --requests 12 --spans-per-request 1 --concurrency 1 --connections 1 --seed 42 --verify-storage=false --output artifacts/session05/paging-load.json
    if ($LASTEXITCODE -ne 0) { throw 'Pagination trace export failed' }
    @{ historical='656f80607b005aa65927df202991a837'; replay=$replay; anomaly=$anomaly; oversized=$oversized; large=$largeID; scenarios=$scenarios } | ConvertTo-Json -Depth 8 | Set-Content artifacts/session05/demo-evidence.json
    Get-Content artifacts/session05/demo-evidence.json
} finally { $env:CLICKHOUSE_PASSWORD = $savedPassword }
