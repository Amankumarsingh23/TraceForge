param([switch]$SkipDatabaseFailure,[switch]$SkipShutdown)
$ErrorActionPreference='Stop'
$savedPassword=$env:CLICKHOUSE_PASSWORD
$savedFlag=$env:TRACEFORGE_TEST_BENCHMARK
$savedWait=$env:TRACEFORGE_BENCH_WAIT
$savedTarget=$env:TRACEFORGE_BENCH_TARGET
$savedMode=$env:TRACEFORGE_BENCH_MODE
try {
    foreach ($line in Get-Content .env) { if ($line -match '^CLICKHOUSE_PASSWORD=(.+)$') { $env:CLICKHOUSE_PASSWORD=$Matches[1] } }
    if ($SkipDatabaseFailure) { & .\scripts\session03-smoke.ps1 -SkipDatabaseFailure } else { & .\scripts\session03-smoke.ps1 }
    if (-not $SkipShutdown) {
        $env:TRACEFORGE_BENCH_MODE='batched'
        $env:TRACEFORGE_BENCH_WAIT='1s'
        $env:TRACEFORGE_BENCH_TARGET='5000'
        $env:TRACEFORGE_TEST_BENCHMARK='1'
        foreach ($test in @('TestLiveGracefulBatchDrain','TestLiveForcedKillPendingBatch')) {
            & docker compose -f compose.yaml -f compose.benchmark.yaml up -d --wait --wait-timeout 120 --force-recreate traceforge-benchmark
            if ($LASTEXITCODE -ne 0) { throw 'Shutdown fixture could not start' }
            & go test -count=1 -tags integration ./tests/integration -run "^$test`$" -v
            if ($LASTEXITCODE -ne 0) { throw 'Live shutdown/forced-kill test failed' }
        }
    }
    Invoke-RestMethod http://localhost:8082/debug/ingestion | Select-Object requests,received_spans,persisted_spans,pending_requests,reserved_bytes
} finally {
    & docker compose -f compose.yaml -f compose.benchmark.yaml stop traceforge-benchmark
    $env:CLICKHOUSE_PASSWORD=$savedPassword
    $env:TRACEFORGE_TEST_BENCHMARK=$savedFlag
    $env:TRACEFORGE_BENCH_WAIT=$savedWait
    $env:TRACEFORGE_BENCH_TARGET=$savedTarget
    $env:TRACEFORGE_BENCH_MODE=$savedMode
}
