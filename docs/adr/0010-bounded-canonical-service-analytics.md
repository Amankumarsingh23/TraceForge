# ADR 0010: bounded canonical trace-derived service analytics

Status: accepted, Session 10.

## Decision

Use bounded two-query trace discovery/full hydration and the **existing** canonical reconstruction for trace-derived SERVER RED and confirmed CLIENT→SERVER edges. Keep all analytics in a small Go package independent of ClickHouse and HTTP; the storage adapter performs parameterized bulk reads. Use exact nearest-rank distributions within explicit row/byte/service/edge/bucket/time limits. Reject over-budget scopes instead of returning hidden partial results.

Preserve namespace, environment and explicit workload origin in a stable identity. Attribute requests to SERVER start, edges to CLIENT start; hydrate out-of-window counterparts before pairing. Show independent client/server errors and distributions, raw evidence diagnostics and actual trace/log links. Version this as `trace-red/v1`. No claims about unsampled requests, complete topology, business success or exact network latency.

## Alternatives and consequences

Raw SQL count/percentiles would be cheaper but count physical replay/conflicting variants inconsistently with trace detail. N+1 detail queries would reuse canonical logic but multiply query work and impose per-trace latency. Window-filtering both sides would lose boundary pairs. Service-name grouping would merge independent namespaces/workloads. Name/timing/SpanLink heuristics would manufacture confirmed topology. Averaging bucket percentiles would be mathematically wrong.

The selected method is exact for the bounded canonical observations and intentionally conservative when a window has too many candidates. Identity filters are applied after discovery, so they do not bypass global query budgets. Two queries are not a snapshot; late telemetry can change results. Structural Go bounds and ClickHouse query-memory settings are protective limits, not a process memory benchmark. General OTLP metrics, SLOs, preaggregation, index changes and optimization remain separate future sessions.

All endpoints share one envelope/computation to avoid divergent request/edge definitions. Sorting is fixed and all-or-error; no silent top-N. Namespace is scope metadata, not tenant authorization. Loopback-only development remains required until authenticated tenant-scoped APIs are implemented.

Evidence: actual 100-request gateway/payment workload and replay, controlled cross-window bulk read and candidate rejection, pure anomaly/quantile tests, production UI navigation and prior regressions. See [metric contract](../analytics/metric-definitions.md) and [session report](../engineering-journal/session-10.md).
