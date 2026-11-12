# Resume evidence

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
| Waterfall frontend | PLANNED | Session 5 |
| Correlated logs and AgentOps instrumentation | PLANNED | Sessions 6–7 |
| RAG metrics, versioning, experiment comparisons and gates | PLANNED | Sessions 8–9 |
| Service graphs, metrics, SLOs, alerts and sampling | PLANNED | Sessions 10–13 |
| Tenant isolation and production privacy/security hardening | PLANNED | Session 14, with controls introduced as needed earlier |
| Reproducible scale benchmarks and final demonstrations | PLANNED | Session 15 |

Sessions 2–4 establish local ingestion, bounded batching and trace queries. Do not describe this as a production platform, unconditional at-least-once/exactly-once system or RAG evaluator. Absence of gaps does not prove completeness. Debug output/readiness alone does not prove stored spans; independent IDs/rows do. The local batching workload measured about 7.2–8.0k confirmed rows/sec for its per-request control and about 9.8–10.0k for targets 100/500 at a configured 10k offered rate, with differing latency/memory. Any use of these numbers must retain workload, hardware, short-run and non-capacity qualifications. No remote CI execution or large-scale benchmark is claimed.
