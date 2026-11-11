# Resume evidence

Claims must match recorded executions in the session journal. No benchmark numbers have been produced.

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
| Waterfall frontend | PLANNED | Session 5 |
| Correlated logs and AgentOps instrumentation | PLANNED | Sessions 6–7 |
| RAG metrics, versioning, experiment comparisons and gates | PLANNED | Sessions 8–9 |
| Service graphs, metrics, SLOs, alerts and sampling | PLANNED | Sessions 10–13 |
| Tenant isolation and production privacy/security hardening | PLANNED | Session 14, with controls introduced as needed earlier |
| Reproducible scale benchmarks and final demonstrations | PLANNED | Session 15 |

Sessions 2–3 establish local ClickHouse-backed ingestion plus trace reconstruction/query APIs, with evidence in their journals. Do not describe this as a production platform, unconditional at-least-once/exactly-once delivery system or RAG evaluation system. Absence of missing-parent warnings does not establish trace completeness. SQL readiness alone proves database access; source IDs compared to persisted/reconstructed records establish the tested path. Debug output alone still does not establish persistence. Windows ordinary checks and Docker Linux race checks are recorded separately; no remote CI run or scale benchmark is claimed.
