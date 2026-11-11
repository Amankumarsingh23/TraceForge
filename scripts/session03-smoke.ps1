param([switch]$SkipDatabaseFailure)
$ErrorActionPreference = 'Stop'
$savedPassword = $env:CLICKHOUSE_PASSWORD
$savedFailureFlag = $env:TRACEFORGE_EXPECT_DB_DOWN
try {
    if (-not $env:CLICKHOUSE_PASSWORD) {
        foreach ($line in Get-Content .env) {
            if ($line -match '^CLICKHOUSE_PASSWORD=(.+)$') { $env:CLICKHOUSE_PASSWORD = $Matches[1] }
        }
    }
    if (-not $env:CLICKHOUSE_PASSWORD) { throw 'Existing ClickHouse password is missing' }
    & go test -count=1 -tags integration ./tests/integration -run 'TestKnownSession2TraceAPI|TestTraceQueryReplay|TestSyntheticGraphAPI|TestTraceSearchPaging|TestTraceDetailLimitAPI|TestOTLPStorageContract|TestDemoTraceStorage' -v
    if ($LASTEXITCODE -ne 0) { throw 'Session 3 integration checks failed' }
    if (-not $SkipDatabaseFailure) {
        try {
            & docker compose stop clickhouse
            if ($LASTEXITCODE -ne 0) { throw 'ClickHouse stop failed' }
            $env:TRACEFORGE_EXPECT_DB_DOWN = '1'
            & go test -count=1 -tags integration ./tests/integration -run 'TestQueryDatabaseUnavailable|TestDatabaseUnavailable' -v
            if ($LASTEXITCODE -ne 0) { throw 'Query/ingestion outage checks failed' }
        } finally {
            $env:TRACEFORGE_EXPECT_DB_DOWN = $savedFailureFlag
            & docker compose start clickhouse
            if ($LASTEXITCODE -ne 0) { throw 'ClickHouse restore failed' }
            $deadline = [DateTime]::UtcNow.AddSeconds(90)
            $ready = $false
            do {
                try { $ready = (Invoke-WebRequest 'http://localhost:8082/readyz' -TimeoutSec 3).StatusCode -eq 200 } catch { $ready = $false }
                if ($ready) { break }
                Start-Sleep -Milliseconds 500
            } while ([DateTime]::UtcNow -lt $deadline)
            if (-not $ready) { throw 'Readiness failed to recover' }
        }
    }
    & go test -count=1 -tags integration ./tests/integration -run 'TestFreshExportAfterRecovery|TestKnownSession2TraceAPI' -v
    if ($LASTEXITCODE -ne 0) { throw 'Ingestion/query recovery checks failed' }
} finally {
    $env:CLICKHOUSE_PASSWORD = $savedPassword
    $env:TRACEFORGE_EXPECT_DB_DOWN = $savedFailureFlag
}
