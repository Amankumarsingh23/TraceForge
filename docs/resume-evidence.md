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
| Exactly-once delivery or deduplication | NOT IMPLEMENTED | deliberate replay creates duplicate rows; Session 3 will study strategies |
| Trace reconstruction/search/waterfall | PLANNED | Sessions 3–5 |
| Correlated logs and AgentOps instrumentation | PLANNED | Sessions 6–7 |
| RAG metrics, versioning, experiment comparisons and gates | PLANNED | Sessions 8–9 |
| Service graphs, metrics, SLOs, alerts and sampling | PLANNED | Sessions 10–13 |
| Tenant isolation and production privacy/security hardening | PLANNED | Session 14, with controls introduced as needed earlier |
| Reproducible scale benchmarks and final demonstrations | PLANNED | Session 15 |

Session 2 establishes a local ClickHouse-backed trace ingestion path with evidence in its engineering journal. Do not describe it as a production observability platform, exactly-once system or RAG evaluation system. SQL readiness alone proves database access; stored rows compared to source IDs establish ingestion. Debug output alone still does not establish persistence. Windows ordinary checks and Docker Linux race checks are recorded separately; no remote CI run or scale benchmark is claimed.
