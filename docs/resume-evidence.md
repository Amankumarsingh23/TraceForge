# Resume evidence

Claims must match recorded executions in the session journal. No benchmark numbers have been produced.

| Capability | Status | Evidence |
| --- | --- | --- |
| Go HTTP gateway/payment instrumentation | IMPLEMENTED | `internal/demo`, real HTTP component tests |
| W3C trace propagation and span hierarchy | IMPLEMENTED | recording exporters assert five spans and exact parent IDs |
| OTLP/gRPC export to Collector debug | IMPLEMENTED; execution results in journal | smoke script checks actual exported trace identifiers |
| Local ClickHouse/PostgreSQL infrastructure | PROVISIONED; readiness results in journal | Compose health checks and authenticated SQL |
| Custom OTLP ingestion and ClickHouse span persistence | PLANNED | Session 2 |
| Trace reconstruction/search/waterfall | PLANNED | Sessions 3–5 |
| Correlated logs and AgentOps instrumentation | PLANNED | Sessions 6–7 |
| RAG metrics, versioning, experiment comparisons and gates | PLANNED | Sessions 8–9 |
| Service graphs, metrics, SLOs, alerts and sampling | PLANNED | Sessions 10–13 |
| Tenant isolation and production privacy/security hardening | PLANNED | Session 14, with controls introduced as needed earlier |
| Reproducible scale benchmarks and final demonstrations | PLANNED | Session 15 |

Do not describe this session as a working ClickHouse trace backend, production observability platform or RAG evaluation system. A successful SQL query proves database access, not span ingestion. A debug-exported span proves Collector receipt, not durable persistence.
