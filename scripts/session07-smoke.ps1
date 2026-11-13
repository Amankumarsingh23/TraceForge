param()
$ErrorActionPreference = 'Stop'
# Start the fixture-only Python app separately; this script never enables live mode.
$health = Invoke-RestMethod 'http://127.0.0.1:8084/healthz' -TimeoutSec 3
if ($health.mode -ne 'fixture') { throw 'Session 7 default checks require fixture mode; no billable requests allowed' }
$savedPassword = $env:CLICKHOUSE_PASSWORD
try {
    if (-not $env:CLICKHOUSE_PASSWORD) {
        foreach ($line in Get-Content .env) { if ($line -match '^CLICKHOUSE_PASSWORD=(.+)$') { $env:CLICKHOUSE_PASSWORD = $Matches[1] } }
    }
    & go test -count=1 -tags integration ./tests/integration -run '^TestAgentExecutionStoragePrivacyAndReplay$' -v
    if ($LASTEXITCODE -ne 0) { throw 'Agent execution integration checks failed' }
} finally { $env:CLICKHOUSE_PASSWORD = $savedPassword }
