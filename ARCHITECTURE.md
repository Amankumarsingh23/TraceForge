# Session 6 architecture

```mermaid
flowchart LR
    user[Local synthetic caller] --> gateway[Go gateway :8080]
    gateway -->|HTTP and W3C traceparent| payment[Go payment :8081]
    gateway -. OTLP gRPC .-> collector[Collector :4317]
    payment -. OTLP gRPC .-> collector
    collector --> debug[Detailed debug output]
    collector -->|OTLP gRPC gzip :4319| receiver[Go TraceService receiver]
    collector -->|Separate bounded log exporter :4319| logreceiver[Go LogsService receiver]
    logreceiver --> logqueue[Independent bounded log jobs]
    logqueue --> logwriter[Fixed log writer / size-time flush]
    logwriter --> logstore[ClickHouse logs / time-first MergeTree]
    receiver --> admission[Bounded admission and payload reservations]
    admission --> queue[64-slot queue]
    queue --> workers[Fixed size/time batch workers]
    workers -->|Synchronous prepared batch| ch[ClickHouse MergeTree spans]
    workers -. per-request completion .-> receiver
    metrics[Internal metrics and bounded diagnostics] --> receiver
    caller[Local engineer] -->|Loopback :3000| ui[Next.js trace explorer]
    ui --> proxy[Narrow same-origin trace routes]
    proxy -->|HTTP :8082| api[Go trace query API]
    proxy --> logapi[Bounded Go log search/correlation API]
    logapi --> logstore
    api -->|Bounded parameterized reads| ch
    api --> graph[Pure read-time reconstruction]
    health[HTTP :8082 healthz / readyz] --> receiver
    pg[PostgreSQL: provisioned, no app schema]
```

Gateway/payment, Collector forwarding, official trace/log OTLP services, independently bounded persistence, redaction, health, trace reconstruction/querying, log search/correlation and the Next.js UI are implemented. Trace diagnostics remain explicit. Detailed log debug export is disabled; the trace debug path remains. PostgreSQL has no application schema. OTLP metrics ingestion and AgentOps/RAG remain planned.

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

Collector exports to `traceforge-server:4319` over the existing network; the receiver registers gzip decompression. A connection/stream cap bounds transport concurrency; 32 handlers may normalize/wait by default. Each request retains the 4MiB/1000-span/five-second and prior field limits. Queue positions, all owned tickets, spans and conservative payload bytes are reserved before normalization; metadata is charged per span. See ADR 0004 for units and defaults. These are payload/work bounds, not exact heap/RSS limits.

Each export owns a buffered completion channel. A bounded channel and fixed workers combine whole requests into size/time batches before using the unchanged ClickHouse writer. Success still waits for `batch.Send`; each request retains its own partial-success rejection count. Empty/all-invalid requests do not write. Saturation waits briefly then signals retryable Unavailable with RetryInfo. Single requests that cannot fit configured hard budgets fail explicitly. No internal retries or fire-and-forget inserts are added.

Cancellation before writing may skip a job. Cancellation after write ownership can leave an unknown outcome and must not cancel other jobs in a shared batch. Worker lifecycle/write contexts are separate from caller contexts. Budgets remain owned through queued/collected/active states, including abandoned callers, until completion. A one-result buffer avoids blocked workers when a caller has left.

The initial MergeTree has monthly partitions and trace-ID-first sorting. This supports direct trace lookup, not uniqueness. Service/time queries may need broader scans; no alternative layout has been benchmarked. Complete known sanitized span/resource/scope messages preserve metadata omitted from extracted columns, including flags and dropped counts.

## Delivery guarantees

The acceptance boundary is ClickHouse's normal synchronous insert response (`async_insert=0`). There is no receiver WAL, durable queue, distributed transaction or exactly-once claim. Multi-partition failure may be ambiguous. Lost acknowledgments and retries can duplicate rows; driver inserts explicitly disable deduplication. A deliberate replay integration test proves two rows can share the same trace/span IDs.

Collector has a bounded in-memory queue of 100 requests, two consumers and retries up to 60 seconds with 1–5s backoff. This is a separate acceptance boundary: application export can complete before backend storage. Queue overflow, permanent errors, retry expiry and restarts can lose telemetry. The debug exporter is an independent diagnostic branch and does not establish persistence.

## Local infrastructure

Host ports 8080/8081/4317/4319/8082/8123/5432 bind to loopback. Databases require existing generated passwords from ignored `.env`. Receiver uses native ClickHouse on the internal network; optional host mode uses authenticated HTTP on 8123. Named volumes preserve database files. Only ClickHouse has an application table. Containers share a project-local bridge network; they are not designed for untrusted workloads. The receiver image runs as nonroot with no shell.

The receiver checks database readiness and applies unchanged migration 0001 before accepting traffic. `/healthz` is process liveness; `/readyz` still makes a bounded DB Ping, independent of queue fullness. SIGTERM stops admissions, drains/final-flushes within the configured deadline, fails unresolved jobs and cancels the worker lifecycle if necessary. The pool closes, worker exit is bounded, then gRPC/HTTP stop. Compose grants 20s. SIGKILL bypasses this and can lose queued memory. Collector config is unchanged and still needs explicit reload if edited.

`/metrics` uses a private Prometheus registry with bounded labels; `/debug/ingestion` exposes counters and at most 2048 batch samples. This is internal self-monitoring, not user OTLP metric ingestion. An optional isolated benchmark receiver uses 4320/8083 and CPU/memory limits; the main pipeline remains unchanged.

## Query interpretation

`internal/storage/query.go` uses the existing driver and unchanged schema. Exact trace lookup reads all services with a physical-row sentinel and payload caps. `internal/traces` canonicalizes bounded records, chooses deterministic conflict winners, links parents by map lookup, detects cycles iteratively and creates a finite sorted forest. Orphans/cycle components remain visible; raw parent IDs and timing are never repaired. Output completeness remains unknown.

`internal/queryapi` validates canonical IDs, bounded UTC windows, page sizes, service/error filters and versioned cursors. It shares the existing HTTP listener through a separate mux; health routes are preserved. Four active query operations, context deadlines and database execution/read/memory bounds prevent unbounded reads. Errors are sanitized and oversized traces are rejected rather than partially reconstructed.

Discovery aggregates raw observations starting in the time window. Service/error filters identify trace IDs on the same matching span; summaries retain other services within the window. Detail has neither service nor time filters. Search ordering is latest start descending, trace ID ascending, with integer nanosecond bindings and a filter-consistent keyset cursor. Pagination is not a snapshot during concurrent/late ingestion. Detail canonical conflict choices can differ from raw-observation summaries; the distinction is explicit.

See ADR 0003 for content equivalence, deterministic duplicate selection, cycle-edge presentation and consistency limits. None of this changes the Session 2 acknowledgment boundary, retry/loss possibilities or raw storage uniqueness behavior.

## Frontend investigation path

The Windows-host Next.js App Router app in `web/` serves `/traces` and `/traces/[traceId]` on loopback 3000. Its Node route handlers accept only approved trace GET paths/parameters, construct the Go API URL from server-only `TRACEFORGE_API_URL`, preserve upstream status/bytes, apply a 5s deadline and 16MiB body cap. Browser headers/credentials are not forwarded; redirects are rejected. Transport failures are sanitized. Go remains the source of database/query/reconstruction semantics. No backend response/schema change was needed.

Client requests have 7s deadlines and cancellation plus generation guards. Applied absolute UTC filters bind to opaque cursor history; a new search clears it. Manual refresh makes no live/snapshot promise. Search shows window-scoped raw-observation summaries; detail independently loads all observed services. Invalid/missing/oversized/unavailable states are distinct from empty success.

The waterfall iteratively traverses the supplied forest with visited-ID guards, preserving roots/orphans/detached cycle components. Collapse affects descendants; supported traces scroll, with no recursive React tree. RFC3339Nano strings become BigInt ns before offsets/durations/ratios are computed. Fixed colors and minimum tiny-span markers do not change numeric durations. The inspector renders stored redacted attributes/scope/events/links as escaped text. Counts, conflicts, timing warnings and unknown completeness remain visible. No critical-path or overall-outcome inference is made. See ADR 0005 and the Session 5 journal for contracts, checks and screenshots.

## Planned next step

Session 7 will introduce LLM/agent observability. No metrics ingestion/dashboard, service map, alerting, durable queue, enterprise auth or AgentOps/RAG implementation has been started. Session 4's delivery and measurement limits remain; Session 6 adds logs and exact ID correlation.

## Log-specific path and invariants

`slog` receives the active span context and uses an explicitly owned otelslog bridge/LoggerProvider/OTLP exporter. Shared resource construction aligns trace/log service identity. SDK queues are bounded and startup can be standalone. Collector uses a separate 64-request/one-consumer log exporter with finite retries; logs do not enter the detailed debug branch.

`internal/logs` adapts the proven trace queue lifecycle with independent counters/budgets and one default writer. Preflight bounds nested values before protobuf cloning/sizing. Normalization sanitizes clones, preserves optional time/context, type tags, resource/scope metadata and a generated row ID. Source-event identity and trace/span correlation are separate. Genuine/replayed messages remain physical rows; no existing trace/span is required. Both engines drain concurrently before shared database-pool closure. Separate queues are not durable storage or total infrastructure isolation.

Migration 0002 sorts by `(effective_time, log_id)`; event/observed DateTime64(9) columns are nullable. Ordering uses event time, observed time or labeled ingestion fallback without inventing source time. The API emits precise RFC3339Nano strings and typed protobuf JSON. Bounded parameterized scans support service/severity/context/literal body filters. Two active log queries, deadlines, read/payload limits and filter-bound keyset cursors complement the trace limits. Pagination is non-snapshot.

`/logs` and correlated-log panels use only narrow same-origin Next routes. Shared forwarding preserves status/bytes, cancellation and bounds without browser-supplied destinations or credentials. Span filters bind both IDs; trace scope includes trace-only records. Missing trace links show 404; timing proximity never establishes a join. React escapes text and BigInt retains relative precision. Correlation's last-24h bounds, refresh and unknown completeness are explicit. See ADR 0006 for exact identity, privacy and delivery policy.
