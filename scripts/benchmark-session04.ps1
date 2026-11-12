param([int]$Requests=300,[int]$Repetitions=2,[double]$Rate=10000)
$ErrorActionPreference='Stop'
if ($Requests -lt 20 -or $Requests -gt 1000 -or $Repetitions -lt 1 -or $Repetitions -gt 3) { throw 'Use 20..1000 requests and 1..3 short repetitions' }
$savedPassword=$env:CLICKHOUSE_PASSWORD
$savedMode=$env:TRACEFORGE_BENCH_MODE
$savedTarget=$env:TRACEFORGE_BENCH_TARGET
$savedWait=$env:TRACEFORGE_BENCH_WAIT
$directory=Join-Path $PWD 'artifacts/session04-benchmarks'
New-Item -ItemType Directory -Force $directory | Out-Null
try {
    foreach ($line in Get-Content .env) { if ($line -match '^CLICKHOUSE_PASSWORD=(.+)$') { $env:CLICKHOUSE_PASSWORD=$Matches[1] } }
    & go build -o bin/otel-loadgen.exe ./cmd/otel-loadgen
    if ($LASTEXITCODE -ne 0) { throw 'Load generator build failed' }
    $matrix=@(
        @{Name='per-request';Mode='direct';Target=1;Wait='0ms'},
        @{Name='target-100';Mode='batched';Target=100;Wait='20ms'},
        @{Name='target-500';Mode='batched';Target=500;Wait='50ms'},
        @{Name='target-1000';Mode='batched';Target=1000;Wait='100ms'},
        @{Name='target-5000';Mode='batched';Target=5000;Wait='100ms'}
    )
    foreach ($config in $matrix) {
        $env:TRACEFORGE_BENCH_MODE=$config.Mode
        $env:TRACEFORGE_BENCH_TARGET=[string]$config.Target
        $env:TRACEFORGE_BENCH_WAIT=$config.Wait
        & docker compose -f compose.yaml -f compose.benchmark.yaml up -d --wait --wait-timeout 120 traceforge-benchmark
        if ($LASTEXITCODE -ne 0) { throw 'Benchmark receiver failed to start' }
        & .\bin\otel-loadgen.exe --endpoint localhost:4320 --http-endpoint http://localhost:8083 --requests 20 --spans-per-request 50 --concurrency 8 --connections 4 --seed 42 --output "$directory/$($config.Name)-warmup.json"
        if ($LASTEXITCODE -ne 0) { throw 'Warm-up failed' }
        for ($repeat=1;$repeat -le $Repetitions;$repeat++) {
            & .\bin\otel-loadgen.exe --endpoint localhost:4320 --http-endpoint http://localhost:8083 --requests $Requests --spans-per-request 50 --concurrency 32 --connections 4 --rate $Rate --seed 42 --output "$directory/$($config.Name)-$repeat.json"
            if ($LASTEXITCODE -ne 0) { throw 'Measured run failed' }
        }
    }
    $summary=foreach ($file in Get-ChildItem $directory -Filter '*.json' | Where-Object { $_.Name -notlike '*warmup*' }) {
        $r=Get-Content $file.FullName -Raw | ConvertFrom-Json
        [pscustomobject]@{Configuration=$file.BaseName;RunID=$r.load.config.run_id;Generated=$r.load.generated_spans;Ack=$r.load.acknowledged_spans;Stored=$r.stored_rows;Unique=$r.unique_stored_spans;RowsPerSecond=$r.confirmed_rows_per_second;AckP50MS=$r.load.p50_ack_ms;AckP95MS=$r.load.p95_ack_ms;AckP99MS=$r.load.p99_ack_ms;InsertP50MS=$r.insert_p50_ms;InsertP95MS=$r.insert_p95_ms;MeanBatch=$r.mean_batch_size;Rejections=$r.After.admission_rejections-$r.Before.admission_rejections;PeakQueue=$r.sampled_peak_queue;PeakHeap=$r.sampled_peak_heap_bytes;PeakRSS=$r.sampled_peak_resident_bytes;Elapsed=$r.load.elapsed_seconds;Duplicates=$r.physical_duplicates;CompleteBatchSamples=$r.metric_samples_complete;Settled=$r.accounting_settled;ConfiguredRate=$r.load.config.configured_spans_per_second;ActualOfferedRate=$r.load.actual_offered_spans_per_second;AckRate=$r.load.acknowledged_spans_per_second;PeakReservedBytes=$r.After.peak_reserved_bytes;PeakGoroutines=$r.sampled_peak_goroutines;ActiveParts=$r.active_database_parts;PartsAvailable=$r.parts_count_available;StorageWindow=$r.storage_window_seconds}
    }
    $summary | Export-Csv -NoTypeInformation "$directory/summary.csv"
    $summary | Format-Table Configuration,RowsPerSecond,AckP95MS,MeanBatch,Rejections,PeakRSS
} finally {
    & docker compose -f compose.yaml -f compose.benchmark.yaml stop traceforge-benchmark
    $env:CLICKHOUSE_PASSWORD=$savedPassword
    $env:TRACEFORGE_BENCH_MODE=$savedMode
    $env:TRACEFORGE_BENCH_TARGET=$savedTarget
    $env:TRACEFORGE_BENCH_WAIT=$savedWait
}
