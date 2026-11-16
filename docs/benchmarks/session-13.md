# Session13 measured local storage and sampling experiments

Final SQL run: `20261114132435`. Raw DDL, query text/IDs, checksums, EXPLAIN, query-log read/memory/parts/marks and failure records are retained in [session13/](session13/). HTTP timings include client/network overhead; DB durations below come from QueryFinish records. No cold-cache claim.

## Method

Windows host: i5-12500H,12 physical cores/16 logical processors,16,781,885,440 bytes RAM. Docker29.9.2/WSL2 Linux exposes16 CPUs and8,130,514,944 bytes RAM. ClickHouse26.8.36.3-alpine has no additional per-container CPU/RAM limit. SQL queries use2 threads,20s,512MiB memory/read budgets,3M read rows, query cache disabled. One warmup then10 sequential repetitions per workload/layout; p95 is nearest-rank (the maximum at n=10). OS/page caches were not flushed. Other local services and background merges can affect timings; layouts run sequentially, not a randomized capacity study.

Seed42 SQL fixtures contain five spans/trace, gateway/payment/worker, bounded128-byte attribute payloads, actual resource identity, delayed/error patterns, exact replay and later-ingested conflicts. SQL-generated records exercise stored-record semantics; they are not claimed as OTLP ingestion throughput. Candidate full rows and physical/logical fingerprints match. Separate real Go/API/SDK experiments cover ingestion and semantics.

## Same-dataset query comparisons

Workload names describe the actual SQL stages, not a substitute for Go canonicalization. RED discovery/dependency hydration are stages; service-filter is a raw physical-row query. Exact Go engine equality is tested separately.

|Rows|Workload|Layout|HTTP p50/p95 ms|DB p50/p95 ms|Median rows/bytes read|Median peak query memory|
|---:|---|---|---:|---:|---:|---:|
|100000|trace|spans|20.74/32.03|4.00/5.00|8192/264229|5564283|
|100000|trace|service_first|10.09/30.44|7.00/8.00|24576/6186217|15857761|
|100000|trace|codec|6.94/32.90|4.00/5.00|8192/264229|5559260|
|100000|search|spans|9.58/20.96|7.00/19.00|50000/3251671|6211080|
|100000|search|service_first|8.85/31.37|6.00/8.00|25576/1004231|6211928|
|100000|search|codec|9.24/29.44|7.00/12.00|50011/3252407|6221354|
|100000|red-discovery|spans|7.29/31.40|4.00/5.00|1000/41008|5455949|
|100000|red-discovery|service_first|26.67/32.77|4.00/5.00|1000/41008|5459549|
|100000|red-discovery|codec|11.13/32.19|3.00/5.00|1000/41008|5455949|
|100000|dependency-hydration|spans|6.98/32.00|4.00/5.00|8192/264229|5564627|
|100000|dependency-hydration|service_first|11.16/35.58|7.00/10.00|24576/6186217|15858817|
|100000|dependency-hydration|codec|7.08/28.64|4.00/5.00|8192/264229|5559604|
|100000|service-filter|spans|6.91/23.18|4.00/5.00|50000/850052|5531347|
|100000|service-filter|service_first|6.30/31.05|4.00/4.00|9192/118831|5533895|
|100000|service-filter|codec|6.40/31.64|4.00/5.00|50011/850239|5533343|
|100000|correlated-logs|logs|7.96/34.15|5.50/7.00|8192/1613832|9812888|
|100000|correlated-logs|logs_bloom|7.03/28.64|4.50/6.00|8192/1613832|9813272|
|100000|recent-logs|logs|7.79/31.93|5.00/6.00|16384/400174|10738509|
|100000|recent-logs|logs_bloom|15.09/29.82|5.00/7.00|16384/400174|10739869|
|100000|metric-stream|reference|5.68/23.43|3.00/3.00|16770/1425450|5408340|
|100000|slo-points|reference|26.07/41.39|14.50/20.00|16770/36792414|20127515|

### 100000 storage snapshot

|Table|Parts|Physical rows|Disk bytes|Uncompressed bytes|Bytes/row|Compression ratio|
|---|---:|---:|---:|---:|---:|---:|
|codec|3|100023|2170268|52094776|21.70|24.00|
|logs|2|100023|5876376|25807012|58.75|4.39|
|logs_bloom|2|100023|5901951|25807012|59.01|4.37|
|metric_points|1|16770|664254|39130152|39.61|58.91|
|service_first|3|100023|3530414|52095007|35.30|14.76|
|spans|5|100023|2300700|52094743|23.00|22.64|

Membership fingerprint (CityHash64 tuple XOR, not a cryptographic proof): `[{'physical': 100023, 'logical': 100000, 'checksum': 18032769415940090035}]`. SHA256 of this dataset's DDL/config: `52dcf171ed10fddbd537c70ea196ecf5c771354ab165885a8b93c56fcc24752d`. Equality also checks sorted query-result SHA256 and production Go canonical output.

TTL probe before: `[{'id': 1}, {'id': 2}]`; after explicit isolated MATERIALIZE TTL: `[{'id': 2}]`. Existing project telemetry has no destructive TTL.

Snapshot prototype: reference HTTP p50/p95 35.00/46.60ms; snapshot read 3.44/21.33ms. Identical result checksum. This conflict-free subset rebuild is experimental, not a production incremental materialized view.

### Additional dataset query comparison

|Rows|Workload|Layout|HTTP p50/p95 ms|DB p50/p95 ms|Median rows/bytes read|Median peak query memory|
|---:|---|---|---:|---:|---:|---:|
|300000|trace|spans|7.42/25.00|5.00/7.00|8192/264229|5536389|
|300000|trace|service_first|9.07/9.62|7.00/7.00|23830/3248579|6480895|
|300000|trace|codec|6.92/19.03|4.00/5.00|8192/264229|5600629|
|300000|search|spans|15.56/34.99|11.00/13.00|300000/19509831|6298169|
|300000|search|service_first|8.41/10.31|6.00/8.00|25534/1239195|6217065|
|300000|search|codec|15.10/37.79|12.00/16.00|300096/19516179|6304146|
|300000|red-discovery|spans|11.63/34.86|3.00/6.00|1000/41008|5455949|
|300000|red-discovery|service_first|6.62/24.21|4.00/5.00|1000/41008|5459549|
|300000|red-discovery|codec|9.11/30.43|4.00/11.00|1000/41008|5455949|
|300000|dependency-hydration|spans|6.63/29.57|4.00/6.00|8192/264229|5536717|
|300000|dependency-hydration|service_first|16.95/33.32|7.00/8.00|23830/3248579|6481615|
|300000|dependency-hydration|codec|6.60/32.02|4.00/5.00|8192/264229|5534638|
|300000|service-filter|spans|8.17/8.47|6.00/6.00|300000/5100052|5531291|
|300000|service-filter|service_first|5.68/19.19|3.50/4.00|9192/156316|5531875|
|300000|service-filter|codec|9.96/32.23|7.00/13.00|300096/5101684|5531291|
|300000|correlated-logs|logs|14.23/31.88|5.00/6.00|8192/1613832|9864373|
|300000|correlated-logs|logs_bloom|15.37/32.96|5.00/6.00|8192/1613832|9864757|
|300000|recent-logs|logs|24.63/31.79|5.00/6.00|16384/400174|10739409|
|300000|recent-logs|logs_bloom|20.46/31.57|5.50/6.00|16384/400174|10740769|
|300000|metric-stream|reference|11.83/31.57|4.00/4.00|16770/1425450|5408340|
|300000|slo-points|reference|27.87/42.74|12.00/16.00|16770/36792414|20127515|

### 300000 storage snapshot

|Table|Parts|Physical rows|Disk bytes|Uncompressed bytes|Bytes/row|Compression ratio|
|---|---:|---:|---:|---:|---:|---:|
|codec|2|300096|6557901|156299624|21.85|23.83|
|logs|2|300096|17552740|77427478|58.49|4.41|
|logs_bloom|2|300096|17628697|77427478|58.74|4.39|
|metric_points|1|16770|664254|39130152|39.61|58.91|
|service_first|2|300096|10480182|156299587|34.92|14.91|
|spans|4|300096|6812610|156298964|22.70|22.94|

Membership fingerprint (CityHash64 tuple XOR, not a cryptographic proof): `[{'physical': 300096, 'logical': 300000, 'checksum': 4651406630231117116}]`. SHA256 of this dataset's DDL/config: `88340d553e6e44089b99cea15cd8095013b7d5ec533d76db52ee73748156442f`. Equality also checks sorted query-result SHA256 and production Go canonical output.

TTL probe before: `[{'id': 1}, {'id': 2}]`; after explicit isolated MATERIALIZE TTL: `[{'id': 2}]`. Existing project telemetry has no destructive TTL.

Snapshot prototype: reference HTTP p50/p95 41.06/55.28ms; snapshot read 3.11/25.29ms. Identical result checksum. This conflict-free subset rebuild is experimental, not a production incremental materialized view.

## Actual Go API and ingestion

Isolated300k database plus later controlled fixture copies. Ten observations each; complete endpoints include application work, unlike SQL stage timings. The metric population includes the full required historical S12 scope; physical replays remain canonical. No silent query truncation.

|API|p50 ms|p95 ms|n|
|---|---:|---:|---:|
|trace|30.20|32.82|10|
|search|22.75|59.06|10|
|correlated-logs|27.11|38.22|10|
|recent-logs|30.58|32.44|10|
|service-red|13.94|31.31|10|
|graph|15.46|37.67|10|
|metric|47.11|67.22|10|
|correlated-logs-indexed|9.38|31.29|10|

Concurrent Session4 loadgen: 10000 offered/acknowledged spans, 100 successful RPCs, 0 rejected, 0 unknown; 3282.58 acknowledged spans/s; p50/p95 ACK 120.21/130.13ms. Thirty concurrent actual trace API reads returned200. This is one local trace-ingestion run, not production capacity.

A separate real mixed-signal SDK workload offered100 checkouts while60 trace/log/metric API requests ran concurrently; all reads returned200 and all100 traces/five spans plus independent100-request/10-error counters were observed. See mixed.json.

Five paired10k-row log SQL insert batches per layout (same rows, persistence-confirmed HTTP completion):
- write_plain: p50/p95 ACK 103.53/140.53ms; [{'physical': 50000, 'checksum': 11570562987550184197}].
- write_bloom: p50/p95 ACK 104.80/153.82ms; [{'physical': 50000, 'checksum': 11570562987550184197}].

Existing-main log index backfill preserved physical count/content fingerprint: `{'before': [{'physical': 9416, 'checksum': 2603038023246780583}], 'after': [{'physical': 9416, 'checksum': 2603038023246780583}]}`. This is additive index materialization, not raw-row deletion or a synchronous-retention promise.

## Sampling

Each profile offered80 normal,10 delayed150ms and10 failed actual requests; two services propagated one root trace identity. Observed retained counts:

|Profile|Normal|Delayed|Failed|Total roots|Stored spans|Direct requests/errors per service|
|---|---:|---:|---:|---:|---:|---|
|always|80|10|10|100|500|100/10|
|head|16|0|3|19|95|100/10|
|tail|8|10|10|28|140|100/10|

Late-span probe:20 offered healthy roots, 4 retained before late errors; 4 trace IDs afterward. Cached drop decisions did not change to retain late errors. Pressure:400 simultaneous error roots, 28 stored at the finite check; telemetry reported272 early buffer drops and Collector logs additionally reported exporter queue-full losses. These counters refer to different pipeline stages, not disjoint end-to-end guarantees. Restart probe retained one error child without its earlier buffered parent: `6be0b5c037d643a9bacba41009a8a3c9`.

Tail sampling kept10/10 errors only under the low-pressure test. The retained fraction10/28=35.714…% differs from actual10/100=10%; use direct metrics for availability. Head20% selected19/100 roots in this run, not a promised quota. No paid/live provider calls.

## Decision and limitations

Adopt only the log trace-ID bloom index: EXPLAIN pruned38 granules to1 at300k; the same result and small disk overhead support reduced scanning. HTTP/API p95 did not consistently improve, so no blanket latency speedup is claimed. Service-first traces trade exact-lookup scans/storage for some service scans and remain experimental. ZSTD payload codec measurements are confounded by part/merge state and highly repeated synthetic payloads; not adopted. Snapshot preaggregation excludes conflicting identities and is not an equivalent general incremental design; production canonical Go engines remain authoritative.

The first SQL dataset lacked resource identity and was excluded from nonempty RED conclusions. Expanded300k generation and service-first backfill each hit512MiB limits; bounded50k batches resolved them. Failed databases/queries were retained. A duplicate-index harness failure after migration was fixed by stripping the adopted index from reference DDL. Invalid bucket_seconds=1 was corrected to supported60. A Session7 regression exposed independent trace/log export timing; a finite log-visibility poll replaced the assumption that trace visibility guarantees logs. No failing measurements were silently discarded.

No cluster deployment, destructive retention of project evidence, traffic extrapolation, sampling completeness guarantee, production capacity or Session14 security claim. See session13 journal for all checks and reproduction commands.

## Query-condition cache control and raw-span expiry

The initial warm SQL comparison left the default query-condition cache enabled. Repeated baseline correlation reads consequently fell to8192 rows too: warmed read-row savings cannot be attributed to the bloom index. A separate identical-data experiment disabled that cache for both layouts while retaining warm OS caches. Its full queries, IDs, EXPLAIN and metrics are in index-cache.json/index-queries.json. Adoption applies to uncached/new trace predicates, not guaranteed repeated-query gains.

|Database size|Reference p50/p95 HTTP ms|Bloom p50/p95 HTTP ms|Reference/bloom rows read|
|---|---:|---:|---:|
|100000|9.01/33.41|9.23/33.75|100023/8192|
|300000|11.08/36.33|7.64/31.24|300096/8192|

A copied real fixture trace in `tf_bench_expiry_20261114133715` returned200 before a1-hour event-time TTL, then404 after explicitly controlled TTL processing/one tiny isolated FINAL merge. A recent copied trace still returned200; frozen PostgreSQL evaluation `cb4b5f539c49c795439dfd06f1c07c37dcb72735ef7b7b7b8f12b9fb97317088` remained200 with the expired trace ID. Browser verification forwarded that one trace API request to the real isolated backend, then showed the honest missing-evidence state. No fabricated response or main-table expiry. Shared-part per-profile disk attribution remains unavailable; actual serialized row bytes and CPU profiles are in audit.json.
