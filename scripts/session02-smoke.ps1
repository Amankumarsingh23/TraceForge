param([switch]$SkipDatabaseFailure)
$ErrorActionPreference = 'Stop'
# Load just the existing database password without printing it or rewriting .env.
$savedPassword = $env:CLICKHOUSE_PASSWORD
$savedFailureFlag = $env:TRACEFORGE_EXPECT_DB_DOWN
try {
    if (-not $env:CLICKHOUSE_PASSWORD) {
        foreach ($line in Get-Content -LiteralPath .env) {
            if ($line -match '^CLICKHOUSE_PASSWORD=(.+)$') { $env:CLICKHOUSE_PASSWORD = $Matches[1] }
        }
    }
    if (-not $env:CLICKHOUSE_PASSWORD) { throw 'CLICKHOUSE_PASSWORD is missing' }
    & go test -count=1 -tags integration ./tests/integration -run 'TestOTLPStorageContract|TestDemoTraceStorage' -v
    if ($LASTEXITCODE -ne 0) { throw 'Storage integration checks failed' }
    if (-not $SkipDatabaseFailure) {
        try {
            & docker compose stop clickhouse
            if ($LASTEXITCODE -ne 0) { throw 'Could not stop ClickHouse' }
            $env:TRACEFORGE_EXPECT_DB_DOWN = '1'
            & go test -count=1 -tags integration ./tests/integration -run '^TestDatabaseUnavailable$' -v
            if ($LASTEXITCODE -ne 0) { throw 'Database failure checks failed' }
        } finally {
            $env:TRACEFORGE_EXPECT_DB_DOWN = $savedFailureFlag
            & docker compose start clickhouse
            if ($LASTEXITCODE -ne 0) { throw 'ClickHouse restart failed; restore manually' }
            $deadline = [DateTime]::UtcNow.AddSeconds(90)
            $ready = $false
            do {
                try { $ready = (Invoke-WebRequest 'http://localhost:8082/readyz' -TimeoutSec 3).StatusCode -eq 200 } catch { $ready = $false }
                if ($ready) { break }
                Start-Sleep -Milliseconds 500
            } while ([DateTime]::UtcNow -lt $deadline)
            if (-not $ready) { throw 'Readiness did not recover after database restart' }
        }
    }
    & go test -count=1 -tags integration ./tests/integration -run '^TestFreshExportAfterRecovery$' -v
    if ($LASTEXITCODE -ne 0) { throw 'Fresh ingestion after recovery failed' }
} finally {
    $env:CLICKHOUSE_PASSWORD = $savedPassword
    $env:TRACEFORGE_EXPECT_DB_DOWN = $savedFailureFlag
}
