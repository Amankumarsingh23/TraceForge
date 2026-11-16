# ADR0013 — Adopt a measured log index, preserve reference semantics

Status: accepted for Session13 local development.

The time-ordered log table scans many trace IDs for correlation. Same-dataset100k/300k experiments compared an index-free baseline with a trace-ID bloom index. With the query-condition cache disabled for both layouts, at300k EXPLAIN selected1/38 granules and read rows fell from300096 to8192, added disk was75,957 bytes (~0.43%) and result checksums matched. API p95 was noisy, so reduced scanning is the adoption criterion; no general latency claim.

Migration0004 adds the index without changing sort/partition keys, canonical engines or raw records. Explicit MATERIALIZE INDEX backfills existing parts; rollback drops only the index. Existing8,842 log records/fingerprint survived measured backfill. Ordinary MergeTree uniqueness is unchanged. A validated isolated-database override supports identical Go query code in benchmarks while defaulting to traceforge; it is not a tenant/security interface.

Service-first spans, payload codecs and rebuild-only conflict-free request snapshots remain experimental. Their storage, merge-state and semantic trade-offs do not justify replacing the reference. Existing read budgets and full-trace/predecessor hydration remain. No destructive TTL is enabled; actual expiration was limited to test tables.

Go SDK head sampling and a separate matching contrib Collector tail profile are opt-in. Defaults and protected Python evaluation providers remain unsampled. Direct metrics remain independent, and observed trace analytics/usage/link absence remain conditional. Pressure, late spans and restart show why bounded tail state cannot guarantee complete errors. Production security/routing and Session14 remain outside scope.

See `docs/benchmarks/session-13.md`, `docs/storage/retention-and-layouts.md` and `docs/observability/sampling.md` for reproducible evidence, current documentation links, rejected alternatives and failure records.

The cache qualification matters: default warmed baseline predicates also pruned to8192 read rows. The additional cache-controlled experiment supports index savings for new/uncached predicates, not attribution of the warmed cache benefit to the index. The real-span TTL fixture also verified old trace200-to-404, recent trace200 and retained PostgreSQL evaluation200, using one tiny explicitly controlled isolated FINAL merge and a real API/browser forwarding check. No main raw table expired.
