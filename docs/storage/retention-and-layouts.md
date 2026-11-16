# Storage layouts, migration and retention — Session13

## Actual baseline and adopted change

ClickHouse26.8.36.3-alpine uses ordinary MergeTree tables. Spans partition monthly by start_time and sort `(trace_id,start_time,span_id)`; logs partition monthly by effective_time and sort `(effective_time,log_id)`; metric_points partition monthly by point end time and sort `(metric_name,service_name,stream_id,end_ns)`. Baseline dimensions use LowCardinality where already defined; payloads are String and IDs are fixed/string columns. Full original/candidate DDL and the actual post-migration schema are retained in the benchmark JSON/audit, not inferred from illustrative layouts.

Adopted migration0004 adds only `s13_trace_bloom trace_id TYPE bloom_filter(0.01) GRANULARITY 1` to logs. False positives may cause extra reads, never permission to omit matching rows. The time sort order, source events, physical duplicates and exact pagination remain unchanged. At300k rows EXPLAIN selected1/38 granules, and equal query-result checksums/Go regressions support correctness. Added disk was75,957 bytes (about0.43%). API latency is noisy and did not consistently improve; the adoption claim is reduced scanning, not universal faster responses.

Startup adds the index idempotently for new parts. Run `./scripts/session13-materialize.ps1` explicitly to backfill old parts with `mutations_sync=2`. The measured existing-project backfill preserved8,842 physical rows and their tuple fingerprint. Existing parts are not rewritten on every startup. Inspect `system.mutations`, `system.parts` and EXPLAIN after backfill. Rollback is `ALTER TABLE traceforge.logs DROP INDEX IF EXISTS s13_trace_bloom`: it removes only the acceleration structure, preserves rows, and queries continue to use the original time layout. Reapplying startup migration adds it again, so reverting the migration is part of a permanent rollback.

Service-first spans and a ZSTD(3) attribute codec remain isolated experiments. The service layout improved some scans but increases storage and exact-trace read work; filtered discovery cannot replace full canonical hydration. Codec measurements used highly repetitive synthetic payloads and differing merge states, so they do not establish a general codec win. No table replacement, ReplacingMergeTree, production projection or destructive backfill was adopted. Existing column-pruned discovery queries and database read/byte/memory/deadline limits remain. A validated `CLICKHOUSE_DATABASE=tf_bench_*` override enables the same Go code on isolated benchmark databases; default remains traceforge and no HTTP caller chooses a database.

## Canonical summaries and late evidence

The measured request snapshot first groups trace/span identities and counts only a deliberately conflict-free SERVER subset. It is rebuilt from raw rows, then queried as a tiny immutable snapshot. Its lower read cost is measured, but it is not the production reference: it excludes conflicts rather than implementing the complete Go earliest-ingestion/full-normalized-content tie-break. It cannot replace RED, SLO or provider accounting.

A production-safe snapshot design would run the complete canonical engine, bind an explicit source cutoff/version, include diagnostics, write a new snapshot atomically, and reconcile after late/conflicting records. Replays must not add events. Raw insert-time SUM materialized views cannot retract duplicate contributions when a later source merge occurs. Cumulative metric resets, predecessors, overlapping deltas and incomplete coverage need interval semantics before aggregation. Historical SLO decisions remain immutable; revised evidence gets a new revision. Source retention limits what a rebuild can recover. This session does not implement durable incremental summaries or long-window SLO rollups.

## Preservation-first retention policy

No TTL is enabled on existing spans/logs/metric_points, PostgreSQL evaluations or incident history. There is no automatic cleanup of previous benchmark databases. This intentionally preserves project evidence. Benchmarks use isolated namespaces/databases and do not drop originals.

For a future explicit retention decision, choose each signal separately:

|Signal|Lifecycle basis and required consideration|
|---|---|
|Traces|Event/start time supports time partitions, but late ingested old events may already qualify for expiry; ingestion-time retention instead preserves a minimum investigation interval. Trace trees can span partitions and expiry boundaries.|
|Logs|Decide whether effective/event time or ingestion time controls lifetime; standalone/fallback-time records and span correlations may expire independently.|
|Metrics|Keep at least the supported window plus bounded predecessor horizon, permitted lateness and investigation margin. Current raw windows and predecessor reads are each<=1h;30-day SLOs still require future durable summaries/retention design. Never silently change missing predecessors into zero.|
|Evaluations/incidents|Retain frozen policy/evidence and lifecycle history independently from raw telemetry, with a separately approved policy. Current SQL immutability triggers prevent routine destructive history edits.|

Illustrative future7-day traces/logs and35-day metrics are planning examples only, not active settings or verified30-day coverage. Retention is a product/evidence decision as well as a storage cost decision. Namespace selection is not tenant authorization.

The measured isolated TTL probe stops TTL merges, inserts one2-day-old and one current record under a1-day event-time TTL, observes both, restarts TTL merges, then explicitly MATERIALIZE TTL with a bounded wait. Only the current row remains query-visible. This demonstrates supported TTL processing, not immediate disk deletion at the timestamp: inactive parts and files may remain until later cleanup. No ordinary-table OPTIMIZE FINAL is used to conceal fragmentation.

When raw telemetry expires or was sampled/unexported, its link must show an honest missing result. The trace UI explains these possible causes without diagnosing one from a404, or inferring successful execution/zero usage. Frozen evaluation/incident metadata can remain readable with an unavailable trace link. Actual sampled-out/retained trace browser cases and existing evaluation/alert regressions verify this behavior; no existing project trace was deleted to manufacture expiry evidence.

Compatibility was checked with actual pinned-version DDL, EXPLAIN and TTL execution, plus current official [skipping-index](https://clickhouse.com/docs/optimize/skipping-indexes), [TTL](https://clickhouse.com/docs/sql-reference/statements/alter/ttl), [EXPLAIN](https://clickhouse.com/docs/sql-reference/statements/explain) and [query-log](https://clickhouse.com/docs/operations/system-tables/query_log) documentation on2026-11-14.

The cache qualification matters: default warmed baseline predicates also pruned to8192 read rows. The additional cache-controlled experiment supports index savings for new/uncached predicates, not attribution of the warmed cache benefit to the index. The real-span TTL fixture also verified old trace200-to-404, recent trace200 and retained PostgreSQL evaluation200, using one tiny explicitly controlled isolated FINAL merge and a real API/browser forwarding check. No main raw table expired.
