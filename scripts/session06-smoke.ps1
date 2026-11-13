param([switch]$SkipDatabaseFailure)
$ErrorActionPreference = 'Stop'
New-Item -ItemType Directory -Force artifacts/session06 | Out-Null
$savedPassword = $env:CLICKHOUSE_PASSWORD
$savedFailureFlag = $env:TRACEFORGE_EXPECT_DB_DOWN
try {
    if (-not $env:CLICKHOUSE_PASSWORD) {
        foreach ($line in Get-Content .env) { if ($line -match '^CLICKHOUSE_PASSWORD=(.+)$') { $env:CLICKHOUSE_PASSWORD = $Matches[1] } }
    }
    & go test -count=1 -tags integration ./tests/integration -run '^(TestLogStorageQueryPrivacyAndReplay|TestRealDemoLogCorrelation|TestMixedSignalLogAndTraceBudgets)$' -v
    if ($LASTEXITCODE -ne 0) { throw 'Log integration checks failed' }
    & .\scripts\session03-smoke.ps1 -SkipDatabaseFailure
    if (-not $SkipDatabaseFailure) {
        try {
            & docker compose stop clickhouse
            if ($LASTEXITCODE -ne 0) { throw 'ClickHouse stop failed' }
            $env:TRACEFORGE_EXPECT_DB_DOWN = '1'
            & go test -count=1 -tags integration ./tests/integration -run '^(TestLogDatabaseUnavailable|TestQueryDatabaseUnavailable|TestDatabaseUnavailable)$' -v
            if ($LASTEXITCODE -ne 0) { throw 'Mixed-signal outage checks failed' }
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
            if (-not $ready) { throw 'Backend did not recover' }
        }
        & go test -count=1 -tags integration ./tests/integration -run '^(TestMixedSignalLogAndTraceBudgets|TestFreshExportAfterRecovery|TestKnownSession2TraceAPI)$' -v
        if ($LASTEXITCODE -ne 0) { throw 'Fresh trace/log persistence after recovery failed' }
    }
} finally {
    $env:CLICKHOUSE_PASSWORD = $savedPassword
    $env:TRACEFORGE_EXPECT_DB_DOWN = $savedFailureFlag
}
