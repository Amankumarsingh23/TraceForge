# Resume evidence

Session 8 adds fixture-verified trace-linked RAG evaluation: 35 author-reviewed synthetic cases, exact retrieval/context metrics, constrained fact/citation/abstention judgments, transactional PostgreSQL persistence, bounded read-only Go APIs and focused Next run/case investigation. [Measured evidence and limitations](engineering-journal/session-08.md) include K sweeps, context truncation, seven explicit faulty-output transforms, execution/trace-lookup failures and deterministic repeat. Independent human calibration, semantic review of free-form live answers, LLM judging and live-model quality remain pending.

The historical Session 7 checkpoint added fixture-verified AgentOps execution and canonical attempt summaries; it did not include evaluation. Its Python/Collector/SQL/API/browser evidence, hypothetical pricing, replay invariance and source minimization are recorded in [its journal](engineering-journal/session-07.md). The compatible live adapter remains mock-tested only. Session 8 adds the constrained evaluator above; neither milestone establishes live-model quality or actual billing.

Claims must match recorded executions. Session 4 adds short, hardware/workload-specific local benchmark evidence; it does not establish universal or sustainable capacity.

| Capability | Status | Evidence |
| --- | --- | --- |
| Go HTTP gateway/payment instrumentation | IMPLEMENTED | `internal/demo`, real HTTP component tests |
| W3C trace propagation and span hierarchy | IMPLEMENTED | recording exporters assert five spans and exact parent IDs |
| OTLP/gRPC export through Collector | IMPLEMENTED AND VERIFIED | Session 2 integration checks compare stored identifiers to Collector output |
| Local ClickHouse/PostgreSQL infrastructure | PROVISIONED; readiness results in journal | Compose health checks and authenticated SQL |
| Custom OTLP/gRPC trace receiver | IMPLEMENTED AND VERIFIED | gzip-compatible generated service, limits, validation, full/partial/failure tests |
| ClickHouse span persistence | IMPLEMENTED AND VERIFIED | synchronous batches, repeatable migration, real five-span gateway/payment traces |
| Storage failure and recovery semantics | IMPLEMENTED AND VERIFIED | direct Export Unavailable during DB stop, healthz 200 / readyz 503, fresh insert after restart |
| Exactly-once delivery or physical-row uniqueness | NOT IMPLEMENTED | replay still creates raw duplicate rows; finite buffering/retry can lose spans |
| Read-time span deduplication and reconstruction | IMPLEMENTED AND VERIFIED | exact/conflicting duplicate diagnostics; deterministic canonical selection; orphan/cycle/timing handling |
| Bounded trace detail/discovery APIs | IMPLEMENTED AND VERIFIED | parameterized ClickHouse reads; service/error filters; nanosecond keyset pagination; physical-row limits |
| Bounded cross-request ingestion batching | IMPLEMENTED AND VERIFIED | queue/job/span/payload limits, fixed workers, size/time/final flush, per-request persistence completion |
| Backpressure, cancellation and shutdown | IMPLEMENTED AND VERIFIED | bounded waits, retryable refusal, shared-write context ownership, fake failure tests and real SIGTERM/SIGKILL fixtures |
| Internal ingestion metrics | IMPLEMENTED AND VERIFIED | Prometheus counters/gauges/histograms and bounded runtime/batch diagnostics; not user OTLP metrics ingestion |
| Local batching comparison | MEASURED, LIMITED SCOPE | ten 30000-span runs; all stored/unique counts verified; see Session 4 CSV and environment/latency caveats |
| Thin Next.js trace explorer | IMPLEMENTED AND VERIFIED LOCALLY | actual Go/ClickHouse-backed search, service/error/UTC filters, cursor pagination, explicit error states; no direct DB logic |
| Precise interactive waterfall and inspector | IMPLEMENTED AND VERIFIED LOCALLY | BigInt timing, iterative backend forest, canonical logical bars, anomaly diagnostics, raw timestamps/attributes/events; real demo and 1000-span browser verification |
| OTLP structured log ingestion and storage | IMPLEMENTED AND VERIFIED LOCALLY | official LogsService, all groups, typed/redacted normalization, optional timestamps/context, independent bounded writer and repeatable migration 0002 |
| Trace/span log correlation and log search | IMPLEMENTED AND VERIFIED LOCALLY | exact trace+span predicates, standalone/trace-only rows, retained repeats/replays, bounded cursor API, actual SDK/Collector/SQL/browser verification |
| Python AgentOps execution instrumentation | IMPLEMENTED; FIXTURE VERIFIED | Actual BM25 over 12 docs, actual read-only tool, bounded loop, Python OTLP traces/logs through Collector/SQL/API/UI |
| Canonical attempt usage and estimated cost | IMPLEMENTED AND VERIFIED LOCALLY | Versioned hypothetical rates, unknown coverage, decimal nano-USD, replay-invariant accounting; no AI-specific ingestion/table |
| Live OpenAI-compatible adapter | IMPLEMENTED; MOCK TRANSPORT VERIFIED; LIVE PENDING | Explicit opt-in, bounded HTTPS requests, sanitized failures; no billable call or billing reconciliation |
| Versioned synthetic RAG evaluation and trace-linked results | IMPLEMENTED AND VERIFIED OFFLINE | Session 8: 35 cases/12 facts; actual retrieval/context measurements, constrained judgments, PostgreSQL constraints/transactions, SQL/API/browser/trace checks |
| Independent human/LLM-judge calibrated live quality | PENDING | No live generation, paid judge calls, human annotations or agreement analysis |
| Experiment comparisons and deployment gates | PLANNED | Session 9 |
| Service graphs, metrics, SLOs, alerts and sampling | PLANNED | Sessions 10–13 |
| Tenant isolation and production privacy/security hardening | PLANNED | Session 14, with controls introduced as needed earlier |
| Reproducible scale benchmarks and final demonstrations | PLANNED | Session 15 |

Sessions 2–6 establish local ingestion, bounded batching, trace/log queries and a working correlation UI. Do not describe this as a production platform, unconditional at-least-once/exactly-once system or RAG evaluator. Absence of gaps does not prove completeness. Debug output/readiness alone does not prove stored spans; independent IDs/rows do. The local batching workload measured about 7.2–8.0k confirmed rows/sec for its per-request control and about 9.8–10.0k for targets 100/500 at a configured 10k offered rate, with differing latency/memory. Any use of these numbers must retain workload, hardware, short-run and non-capacity qualifications. Session 5 screenshots and recorded demo delay/error observations are in its journal; a long bar is not proof of root cause. Session 6 adds verified log correlation, raw replay retention, privacy fixtures and mixed-signal failure recovery; it does not establish durable delivery or sustained log capacity. No remote CI execution or large-scale benchmark is claimed.
