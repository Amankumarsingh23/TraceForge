# Session 3 architecture

```mermaid
flowchart LR
    user[Local synthetic caller] --> gateway[Go gateway :8080]
    gateway -->|HTTP and W3C traceparent| payment[Go payment :8081]
    gateway -. OTLP gRPC .-> collector[Collector :4317]
    payment -. OTLP gRPC .-> collector
    collector --> debug[Detailed debug output]
    collector -->|OTLP gRPC gzip :4319| receiver[Go TraceService receiver]
    receiver -->|Synchronous prepared batch| ch[ClickHouse MergeTree spans]
    caller[Local engineer] -->|HTTP :8082| api[Trace query API]
    api -->|Bounded parameterized reads| ch
    api --> graph[Pure read-time reconstruction]
    health[HTTP :8082 healthz / readyz] --> receiver
    pg[PostgreSQL: provisioned, no app schema]
```

Gateway/payment, Collector forwarding/debug output, the official OTLP/gRPC receiver, normalization/redaction, ClickHouse span persistence, dynamic health endpoints and trace detail/discovery APIs are implemented. Reconstruction handles duplicates, conflicting versions, orphans, roots, cycles and timing anomalies. PostgreSQL remains provisioned without an application schema. There is no frontend, logs/metrics pipeline or RAG runner.

## Request and span lifecycle

`cmd/shop-demo` selects a service mode. `internal/telemetry` creates a provider with resource attributes, always-on sampling, batch processing and an OTLP/gRPC exporter. `internal/demo` receives HTTP through `otelhttp.NewHandler`. Gateway starts and ends a validation span, then calls payment with the original request context through `otelhttp.NewTransport`. Transport injects the outgoing client's trace context into W3C headers. Payment extracts that context, creates its server span and a manual processing span.

The hierarchy is gateway server → gateway client → payment server → payment processing. Gateway validation is another child of gateway server. IDs, not log order, establish relationships. A valid incoming caller context can make gateway a non-root span.

## OpenTelemetry vocabulary

- **TracerProvider:** owns the SDK configuration, resource, sampling and processors; creates tracers.
- **Tracer:** an instrumentation-scoped handle used to start spans.
- **Span:** one timed operation, including identity, parent, attributes, events and status.
- **SpanProcessor:** handles started/ended spans; the batch processor queues completed spans for export.
- **Exporter:** serializes/sends completed spans to the Collector using OTLP/gRPC.
- **Propagator:** extracts/injects context across HTTP headers; W3C Trace Context and Baggage are explicit here.

## Failure and lifecycle behavior

Payment simulation supports 0..2000ms delay and deterministic 503. Gateway maps payment failures to 502. The processing span and gateway span record safe fixed exceptions; HTTP instrumentation marks server/client errors. Network diagnostics are not copied into span exception messages. Request cancellation interrupts the delay. Gateway's client timeout is four seconds; servers have header/read/write/idle limits. Interrupt/SIGTERM initiates bounded HTTP draining followed by bounded provider shutdown.

SDK initialization validates local endpoint shape but is asynchronous and does not establish Collector connectivity. The default SDK error handler reports export failures to stderr. In-memory queues, default retry limits, crashes and abrupt termination can lose spans. Debug output has no persistence guarantee and may reveal synthetic URL metadata or propagated baggage. This session does not claim complete redaction, tenant security or production delivery semantics.

## Persistence boundary

Collector exports to `traceforge-server:4319` over the existing network; the receiver registers gzip decompression. Four Export operations may run concurrently, each limited to 4MiB, 1000 spans and five seconds. Nested resource/scope/span groups are normalized to rows with typed Protobuf JSON for variable metadata; see `docs/normalization.md` for exact bounds and intentionally unsupported fields. Known sensitive attributes are sanitized before any persistence representation is generated.

Each request's accepted rows use one prepared multi-row insert via ClickHouse Go v2.51.0. Empty requests succeed without a write. Success is sent only after `batch.Send` succeeds. Individual invalid spans produce exact partial-success counts after valid rows are inserted. Malformed/oversized envelopes fail at request level. Storage failures return retryable Unavailable; cancellation/deadline statuses are retained. Global overload is rejected without an application-side queue.

The initial MergeTree has monthly partitions and trace-ID-first sorting. This supports direct trace lookup, not uniqueness. Service/time queries may need broader scans; no alternative layout has been benchmarked. Complete known sanitized span/resource/scope messages preserve metadata omitted from extracted columns, including flags and dropped counts.

## Delivery guarantees

The acceptance boundary is ClickHouse's normal synchronous insert response (`async_insert=0`). There is no receiver WAL, durable queue, distributed transaction or exactly-once claim. Multi-partition failure may be ambiguous. Lost acknowledgments and retries can duplicate rows; driver inserts explicitly disable deduplication. A deliberate replay integration test proves two rows can share the same trace/span IDs.

Collector has a bounded in-memory queue of 100 requests, two consumers and retries up to 60 seconds with 1–5s backoff. This is a separate acceptance boundary: application export can complete before backend storage. Queue overflow, permanent errors, retry expiry and restarts can lose telemetry. The debug exporter is an independent diagnostic branch and does not establish persistence.

## Local infrastructure

Host ports 8080/8081/4317/4319/8082/8123/5432 bind to loopback. Databases require existing generated passwords from ignored `.env`. Receiver uses native ClickHouse on the internal network; optional host mode uses authenticated HTTP on 8123. Named volumes preserve database files. Only ClickHouse has an application table. Containers share a project-local bridge network; they are not designed for untrusted workloads. The receiver image runs as nonroot with no shell.

The receiver checks database readiness and applies embedded migration 0001 before accepting traffic. `/healthz` checks process liveness; `/readyz` performs a current bounded database Ping. A database outage leaves liveness up and readiness down. gRPC shutdown drains for up to six seconds, then force-cancels; HTTP health shutdown is bounded and database connections close afterward. Collector config changes need an explicit reload/restart, independent of Compose startup ordering.

## Query interpretation

`internal/storage/query.go` uses the existing driver and unchanged schema. Exact trace lookup reads all services with a physical-row sentinel and payload caps. `internal/traces` canonicalizes bounded records, chooses deterministic conflict winners, links parents by map lookup, detects cycles iteratively and creates a finite sorted forest. Orphans/cycle components remain visible; raw parent IDs and timing are never repaired. Output completeness remains unknown.

`internal/queryapi` validates canonical IDs, bounded UTC windows, page sizes, service/error filters and versioned cursors. It shares the existing HTTP listener through a separate mux; health routes are preserved. Four active query operations, context deadlines and database execution/read/memory bounds prevent unbounded reads. Errors are sanitized and oversized traces are rejected rather than partially reconstructed.

Discovery aggregates raw observations starting in the time window. Service/error filters identify trace IDs on the same matching span; summaries retain other services within the window. Detail has neither service nor time filters. Search ordering is latest start descending, trace ID ascending, with integer nanosecond bindings and a filter-consistent keyset cursor. Pagination is not a snapshot during concurrent/late ingestion. Detail canonical conflict choices can differ from raw-observation summaries; the distinction is explicit.

See ADR 0003 for content equivalence, deterministic duplicate selection, cycle-edge presentation and consistency limits. None of this changes the Session 2 acknowledgment boundary, retry/loss possibilities or raw storage uniqueness behavior.

## Planned next step

Session 4 will study bounded asynchronous ingestion, batching and backpressure. No new ingestion queue, worker system or delivery guarantee is implemented here. Session 3 evidence is in `docs/engineering-journal/session-03.md`; prior journals remain unchanged historical records.
