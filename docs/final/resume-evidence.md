# Resume claims and measurement sources

## Suggested bullets

- Built a Go/OpenTelemetry observability core with bounded ingestion, persistence-confirmed acknowledgments, ClickHouse traces/logs/metrics and replay-safe service analytics.
- Implemented trace-linked offline RAG evaluation, immutable configuration comparisons/release gates, and transactional SLO incident lifecycles with explicit unknown evidence handling.
- Reduced rows scanned97.27% in a controlled ClickHouse log-correlation benchmark with approximately0.43% storage overhead; validated equal query results and documented cache/workload limits.

For a security-focused role, substitute: “Implemented scoped credential and tenant ownership controls with local TLS integration and adversarial fixture containment tests; documented remaining browser and development-dependency risks.” Do not imply independent certification or a verified secure browser journey.

| Quantitative statement | Source/workload/environment | Reproduce | Limitation |
| --- | --- | --- | --- |
| 300096→8192 rows,97.27%,about0.43% disk | S13 index-cache report, same deterministic300k local ClickHouse fixture; query-condition cache disabled | `scripts/session13/index_cache.py` with private DB env | Scan reduction; not universal latency/throughput |
| 100 requests/service,5 errors,100 pairs | S15 DemoA100 actual HTTP requests,80/15/5 mix, real SDK/Collector/storage | `scripts/session15-demo-a.ps1` | Short local synthetic population, distinct from SLO windows |
| 35 cases/run | S15 DemoB five actual offline fixture runs; dataset checksum in baseline.json | `scripts/session15-demo-b.ps1` | Constrained synthetic facts/citations, no live-model quality |
| 59 boundary checks and20 attack cases | Dated S14 boundary/adversarial evidence | S14 secure smoke and `security_eval.runner` | Fixture containment; one failure control unknown; browser blocked |

Exact final test counts/statuses belong to verification-matrix.md, not a rounded resume performance claim. “Production deployed,” “exactly once,” “complete traffic from sampled traces,” “36× faster,” “universal injection prevention,” “actual provider billing” and “production-approved AI releases” are unsupported. No remote push or deployment occurred. The learner theory total supplied by the user is not an engineering test result and is not restated as independent grading.
