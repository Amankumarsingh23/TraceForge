# Session 13 — Measured storage changes, retention and sampling

Implemented after `1bb598c`; stopped at Session 13. Session 12's supplied theory review is accepted as 50/50. Session 3 theory, live-provider verification and independent semantic review remain separate pending work. No push, paid calls, new queue infrastructure, cluster deployment or Session 14 security work.

## What changed

Adopted one physical optimization: migration 0004 adds a log trace-ID bloom index. Explicit materialization backfills existing parts without changing raw rows. The sorting/partition keys, canonical tie-break, physical replay policy, synchronous persistence-confirmed ACK and query budgets remain intact. `internal/storage/database.go` validates a local `tf_bench_*` database override and routes the same prepared reads/writes/migrations to isolated databases; default remains traceforge. This is a benchmark facility, not tenant isolation.

Added Go SDK always-on and opt-in parent-based trace-ID-ratio configuration, strict validation, deterministic-ID policy tests and sampled/unsampled remote-parent propagation tests. Added a separate matching-version contrib Collector sampling profile; the ordinary core Collector remains unchanged. Python AgentOps/evaluation default SDK construction explicitly uses ALWAYS_ON, with an ambient always_off regression test; protected runs still must use the unsampled Collector. Trace 404 UI explains sampling/expiry/unexported possibilities without claiming the cause or inferring success/zero usage.

Added bounded SQL/API/ingestion/sampling/TTL harnesses under `scripts/session13`, same-dataset production-engine equivalence tests, cross-month trace and cumulative-predecessor tests, three actual browser scenarios, documentation and committed measured evidence. The Session 7 integration now waits finitely for independently exported logs instead of treating trace visibility as proof of log visibility.

## Actual baseline, selected optimization and evidence

The pinned engine is ClickHouse 26.8.36.3 ordinary MergeTree. Spans sort by trace/start/span; logs by effective-time/log-ID; points by name/service/stream/end. All partition monthly by the relevant event/end time. Full source/candidate DDL, dataset fingerprints/SHA256 configuration digests, query IDs, EXPLAIN, row/byte/memory/part/mark metrics, CPU profiles, failures and environment are committed in [the benchmark report](../benchmarks/session-13.md) and its data directory.

Final isolated run `20261114132435`: 100,000 logical/100,023 physical spans and 300,000 logical/300,096 physical spans. Fixed seed 42, five spans per trace, actual resource identity, three services, bounded attributes, error/delay patterns, replay and later conflicts. Candidate layouts copied identical records. Logs correlate to those identities. Actual stored normalized metric streams were copied over a bounded historical window, preserving delta/cumulative/reset/bucket identities; the complete required S12 scope was additionally copied without a silent LIMIT for adapter equality.

At 300k, the service-first layout reduced some service scans but exact-trace read rows rose from 8,192 to 23,830 and storage from 6,812,610 to 10,480,182 bytes. It remains experimental. ZSTD attribute payload storage was 6,557,901 bytes, but part/merge state and repeated synthetic strings confound attribution; no codec migration was adopted. The conflict-free snapshot prototype was cheaper to read but excludes ambiguous identities and is rebuilt, not an equivalent production incremental aggregate.

Log bloom storage: 17,552,740 -> 17,628,697 bytes, +75,957 bytes (~0.43%). EXPLAIN selects 1/38 granules. Important cache qualification: the initial warm baseline also read only 8,192 rows after the query-condition cache learned the predicate. The separate cache-controlled comparison disabled that cache for both layouts, preserving warm OS caches: 300,096 -> 8,192 read rows; HTTP p50/p95 11.08/36.33 -> 7.64/31.24 ms, n=10 each. At 100k the corresponding p95 was 33.41 -> 33.75 ms. Adoption therefore targets scan reduction for new/uncached predicates, not a universal latency improvement or cold-cache claim.

The actual Go API correlation comparison returned identical body checksums before/after materializing the isolated index. All trace/search/log/RED/graph/metric endpoint measurements and SQL-stage timings are reported separately. A Session 4 loadgen run offered/acknowledged 10,000 spans in 100 RPCs while 30 trace API queries ran; zero rejected/unknown outcomes. A separate mixed-signal SDK run offered 100 actual checkouts with concurrent trace/log/metric export and 60 API reads, all 200, with all 100 five-span traces and independent 100-request/10-error counters. These are local short runs, not capacity certification.

## Correctness, TTL and sampling

Production Go reconstruction compares reference/service-first layouts on actual replay/conflict fixtures; RED has two services and 20 confirmed paired requests, not an empty equality. Metric results/predecessors, the S12 23-eligible/19-bad BREACH, and canonical fixture provider 4-attempt/2-success counts remain equal. A separate monthly-boundary fixture hydrates both sides of a trace/edge and recovers a cumulative increment of 5 from a predecessor in the prior partition.

Main log index materialization preserved 8,842 physical rows and their tuple fingerprint. No TTL or deletion was enabled on project raw data or PostgreSQL history. Isolated TTL probes expired old data after explicit processing. The additional real-span expiry experiment copied fixture evidence to `tf_bench_expiry_20261114133715`: old trace returned 200 before a one-hour event-time TTL and 404 afterward, recent trace remained 200, and frozen evaluation `cb4b5f539c49c795439dfd06f1c07c37dcb72735ef7b7b7b8f12b9fb97317088` retained the expired trace ID/readability. One tiny isolated FINAL merge was explicitly controlled for this test only. Browser verification forwarded only that trace request to the actual isolated API; there was no fabricated error response or deletion of the original main trace.

Each sampling profile offered 80 normal, 10 delayed and 10 failed requests. Always-on retained 100 roots/500 spans, head 20% retained 19/95, tail retained 28/140 (8 normal, all 10 delayed and 10 failed in the low-pressure run). All retained low-pressure traces contained gateway/payment and five spans. Direct counters remained 100 requests/10 errors per service. Actual Go RED APIs independently returned retained populations 100/10, 19/3 and 28/10; outcome-biased tail errors are not availability inputs. Serialized stored row bytes are measured separately; per-profile compressed disk attribution is unavailable in shared merged parts.

Late errors did not reverse cached drop decisions: 20 roots, 4 retained before and after. A 400-error-root burst exceeded the 128-trace buffer; telemetry counted 272 early evictions and logs also showed exporter-queue-full loss, with only 28 traces stored at the finite check. Restart lost buffered context and a late error child appeared alone. Tail decisions/accepted spans are not durable complete traces. Protected Python tests and normal evaluation/browser regressions retain honest linkage behavior.

## Reproduction on this Windows checkpoint

Use the pinned portable Go/Node/npm and existing Python virtual environment. The helpers intentionally consume the preserved Session 11/12 local artifacts and fixed historical S12 cutoff; they are checkpoint experiments, not a general benchmark product. `.env` remains untouched and ignored. Load passwords privately into the current process without printing them:

```powershell
foreach ($line in Get-Content .env) {
  if ($line -match '^(CLICKHOUSE_PASSWORD|POSTGRES_PASSWORD)=(.+)$') {
    [Environment]::SetEnvironmentVariable($Matches[1], $Matches[2], 'Process')
  }
}
docker compose up -d --build --wait
go build -o bin/shop-demo-session13.exe ./cmd/shop-demo
go build -o bin/traceforge-server-session13.exe ./cmd/traceforge-server
go build -o bin/slo-evaluate.exe ./cmd/slo-evaluate
go build -o bin/otel-loadgen.exe ./cmd/otel-loadgen
examples/agent-demo/.venv/Scripts/python.exe scripts/session13/benchmark.py
examples/agent-demo/.venv/Scripts/python.exe scripts/session13/api_and_overhead.py
$env:TRACEFORGE_STORAGE_BENCH_DB = (Get-Content artifacts/session13/benchmark.json | ConvertFrom-Json).datasets[-1].db
go test -tags integration ./tests/integration -run '^TestSession13' -v
docker compose -f compose.yaml -f compose.sampling.yaml --profile sampling up -d sampling-collector
examples/agent-demo/.venv/Scripts/python.exe scripts/session13/sampling.py
examples/agent-demo/.venv/Scripts/python.exe scripts/session13/mixed.py
examples/agent-demo/.venv/Scripts/python.exe scripts/session13/audit.py
examples/agent-demo/.venv/Scripts/python.exe scripts/session13/index_cache.py
examples/agent-demo/.venv/Scripts/python.exe scripts/session13/expiry.py
```

The API helper's default revisions 22/23 are immutable on a retry; use --revision-base 24 (or another unused pair) when measuring new source reads. The captured final native helper verified 36 physical reference request points versus 108 isolated points with the same 23/19 logical budget; these were fresh evaluations, not cache-only comparisons. Rebuild all helpers before measuring. Run `report.py` after the final evidence files exist to publish sanitized evidence. Start a production-built Next server separately for Playwright; `sampling.spec.ts` needs the owned expiry backend on 8085 until its third test finishes. Helpers use fresh ports 8086/8087 and leave only that explicitly recorded expiry process for browser testing. Clean up owned preview/expiry PIDs and stop only sampling-collector afterward; preserve the base stack. Never overlap outage scenarios with live integration checks.

## Verification and failed trials

- 260 native Go tests/subtests passed; Go build/vet and Linux Docker race target passed.
- New isolated storage integrations passed canonical layouts, nonempty RED/edges, provider usage, exact SLO equality, monthly hydration and predecessor recovery. Existing trace/log/storage/AgentOps and metric temporal/cardinality/SDK overflow integrations passed. PostgreSQL's 12-worker incident concurrency/revision regression passed.
- 86 Python tests passed with PostgreSQL enabled; Ruff and pip check passed. Existing Starlette/httpx and SDK LoggingHandler deprecations are reported, not suppressed.
- 67 frontend tests passed; lint, typecheck and production build passed.
- All 45 existing browser cases passed, including PostgreSQL/ClickHouse outages and trace/log/metric/evaluation/comparison/SLO/alert pages. Three new actual sampling/expiry cases passed. An initial new locator matched Next's route announcer too; it was narrowed to the error panel and all three cases reran successfully. Desktop/mobile behavior from existing scenarios remains.
- Raw benchmarks record memory-limit failures at 300k expanded generation and service-first backfill; 50k batches fixed them. The first dataset lacked resource identity, so its empty RED result was rejected and regenerated. A post-migration duplicate-index DDL was fixed by stripping that index from reference DDL. An invalid analytics bucket and non-JSON health probe were corrected. A SQL REPLACE alias initially filtered out the recent TTL copy; a subquery isolated the predicate, then old/recent/metadata status checks passed. A stale CLI binary was rebuilt before the final database-isolation evaluation. No failed trial is presented as a successful measurement.

Learning notes are one ignored local `session-13-storage-and-sampling.md`; all earlier notes and credentials were preserved. No production retention, total-traffic guarantee, notification system or security certification was added. Session 14 is ready for its supplied security scope, not silently implemented here.

## Five worked review answers — for self-study, not an assigned grade

1. Batches create sorted/compressed parts; compatible background merges reduce part overhead and maintain storage efficiency. With fixed 8,192-row granules, ceil(10,000,000/8,192) = **1,221**. Near-unique trace partitions fragment merges/lifecycle management. MergeTree primary keys do not enforce uniqueness; TraceForge must retain canonical replay/conflict handling.
2. Trace-first favors full trace lookup; service-first favors service/time scans. 500/125 = **4x** latency speedup; (2,000,000−250,000)/2,000,000 = **87.5%** fewer rows; (520−400)/400 = **30%** more disk. EXPLAIN and repeated equal-input measurements are needed because caches, merges, selectivity and other workloads can change the trade-off.
3. A naive insert-time count adds the replay twice. Later source deduplication does not send a compensating subtraction to an incremental view. Rebuild versioned snapshots using complete canonical selection/cutoffs and reconcile late evidence, or implement an explicitly identity-aware replacement/retraction design. Retained evaluation metadata should remain readable and explain an unavailable raw trace link; absence is not success or zero usage.
4. Actual error rate = 200/20,000 = **1%**. Healthy retained = 19,800×5% = **990**; total retained = **1,190**; retained error fraction = 200/1,190 ≈ **16.8067%**. Tail selection favors failures, so that fraction is not availability. Head sampling decides before observing the full outcome; tail sampling buffers spans by trace ID to decide from observed attributes/outcomes, at the cost of volatile state, delay and capacity loss.
5. One 600→100 ms trial does not establish a reliable 6x gain. Fix membership/schema/settings, verify full canonical outputs, distinguish cold/warm/query-result/query-condition caches, record merge/part state, repeat/interleave trials and separate database from end-to-end API timing. Measure read bytes/rows/memory/CPU, storage, insert ACKs/throughput, projection/index build and merge overhead. A local bounded dataset does not reproduce production concurrency, skew, hardware, failures or retention.
