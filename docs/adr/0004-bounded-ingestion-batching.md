# ADR 0004 — Bounded batching with persistence-based acknowledgments

Status: Accepted for Session 4. Supersedes the one-write-per-export implementation in ADR 0002, while retaining its acknowledgment boundary and delivery limitations.

## Decision and ownership

Use a bounded channel and 1–4 fixed persistence workers. Each export owns a ticket containing its normalized rows and a buffered one-result channel. The handler waits for that ticket's database result. Several tickets may share one synchronous ClickHouse insert; successful queue admission alone is never a successful OTLP response. Validation rejection counts stay with their original handler and are returned as partial success only after its accepted subset is confirmed written.

Preserve the 4MiB decompressed message, 1000-span and existing field/redaction limits. The listener limits gRPC to eight connections with eight streams per connection; the application separately limits 32 active Export handlers by default. These constrain decoded/normalizing work; they are not a precise RSS bound. No goroutine is spawned per queued job.

Before normalization allocates rows, inspect the bounded protobuf envelope and reserve queue/job/span/payload budgets. Resource/scope metadata is charged per span because rows repeat it. The conservative byte reservation is 64 × (per-span protobuf + repeated metadata + 512 bytes) + 512 bytes, with an additional retained-row check after normalization. It intentionally overestimates payload and is not a Go heap/process-memory measurement. Invalid spans remain charged until their request finishes to cover allocated slice capacity.

Defaults: 64 normalizing/queued positions, 32 active handlers, 128 owned tickets, 16384 reserved input spans and 128MiB reserved payload. Ownership budgets include queued, collected and active writes; canceled callers cannot make retained jobs disappear from accounting. Wait at most 50ms in total for handler and payload admission, honoring the caller context. Temporary exhaustion returns Unavailable with RetryInfo. A single request that cannot fit configured hard budgets returns InvalidArgument rather than retrying forever. No server-side retry queue is added.

## Batch policy

Default target is 1000 spans, maximum wait 100ms from the oldest job's enqueue time, one worker. Requests are not split. A request larger than the target is written alone within the existing request limits; combining two jobs never knowingly exceeds the target. Targets are not promises that every insert reaches that size.

Flush at size, age, or shutdown. Once the age deadline has expired, combine work already ready without waiting for new arrivals. This prevents an old backlog from degenerating into tiny inserts while respecting the absence of further timer delay. A carry job belongs to the worker and remains budgeted until the next write. Idle workers block on channels/state notifications, not polling loops.

`--ingestion-mode=direct` is a controlled one-insert-per-request baseline using target 1/wait 0. It keeps the same queue/budgets/workers as the batched experiment to isolate coalescing; it is not a replay of the old binary's exact four-concurrent-write topology.

## Cancellation and shutdown

A caller canceled before writing may be skipped with a failure outcome and released budgets. A caller canceled after a shared write starts may still persist; its context must not cancel other tickets. Shared writes use the worker lifecycle context plus a 3s write deadline. Request handling retains a 5s deadline. Completion channels have capacity one, so abandoned callers cannot block workers. Ambiguous failure may leave physical rows and a retry may duplicate them.

SIGTERM stops new admissions first and allows admitted work/final batches to drain for a configurable default 10s. On deadline, unresolved tickets get failed/unknown outcomes, the worker lifecycle is canceled, and active-write budgets remain owned until the writer exits. Close the database pool, bound the final worker wait, then close gRPC/HTTP listeners. The driver must honor context/connection cancellation; there is no way to forcibly terminate an arbitrary misbehaving Go function. Compose allows 20s for this sequence. SIGKILL bypasses all of it and can lose queued memory.

## Metrics and evidence

Use Prometheus client_golang v1.25.0 with per-server registries. Labels are fixed rejection reasons only; no trace IDs, service names or other unbounded strings become labels. Export counters/gauges/histograms plus runtime/process metrics at `/metrics`. `/debug/ingestion` provides bounded counters and a 2048-entry recent-batch ring; incomplete sample coverage is explicitly flagged in load reports.

The generator uses valid synthetic OTLP and no automatic retries. Its unique run prefix, seed and base time make payloads reproducible and independently countable. Separate attempted/acknowledged/retryable/permanent/unknown outcomes from actual physical rows and unique trace/span identities. Unknown persistence outcomes overlap retryable failures and must not be summed as disjoint categories.

Benchmarks compare one-worker per-request control and targets 100/500/1000/5000 on an isolated receiver with fixed CPU/memory bounds, warm-ups and repeated runs. They measure this short local workload, not sustainable production capacity. See `docs/benchmarks/session-04.md` and its versioned CSV.

## Collector boundary and excluded work

Pinned Collector 0.165.0 retains its existing in-memory 100-request sending queue, two consumers and 60s retry window. Its exporter batching is not enabled in our config; the pinned [exporterhelper documentation](https://github.com/open-telemetry/opentelemetry-collector/blob/v0.165.0/exporter/exporterhelper/README.md) describes the optional queue batch settings. No obsolete batch processor or new tuning was necessary. Collector queuing protects transfer to this receiver; backend queuing controls ClickHouse work. Neither is durable here. [OTLP retry/partial-success semantics](https://opentelemetry.io/docs/specs/otlp/) still apply.

No Kafka, Redis, WAL, disk spool, ClickHouse fire-and-forget inserts, schema/index rewrite, exactly-once/unconditional at-least-once claim, frontend, user OTLP metrics ingestion or future-session functionality.
