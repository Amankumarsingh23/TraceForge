# Observed reliability contract — reliability/v1

An SLI is a measured ratio; an SLO is its chosen target over a window. An SLA is a contractual promise with consequences. TraceForge implements observed SLIs and local objectives, not contractual uptime certification.

Each immutable definition includes ID/version, source contract version, kind, service, namespace, environment, synthetic/fixture/live mode, provider or fixed HTTP route, declared metric writers, target, objective window, latency threshold, minimum count/coverage and versioned alert rules. Changing any content requires a new version. PostgreSQL stores a content checksum and rejects replacement of an existing version. The source version fixes eligibility and outcome semantics; arbitrary predicates are unsupported.

## Populations

Availability uses completed GET requests on `/checkout` or `/charge`; HTTP status >=500 is bad. Request and error monotonic Sum streams must have matching writer, dimensions and exact interval boundaries. Latency uses those same request intervals and explicit-histogram observations in seconds; good means duration <= the configured explicit boundary. Count must match the request count. A threshold inside a bucket returns INCOMPATIBLE, with no interpolation. These are completed observed requests, not an inbound-attempt denominator or synthetic probe uptime.

The metric adapter reuses Session11 canonical replay/reset/overlap handling. SDK scope, units, metric family and stream definitions must match. It excludes boundary-crossing intervals, then enforces minimum coverage independently for every declared writer and minimum count in every required window. Undeclared writers, missing streams, conflicting points, overlap, overflow and invalid arithmetic prevent a healthy decision. Counters are summed before division, never averaged across writers. Coverage describes supported exported interval time, not captured traffic. There is no durable per-scope receiver rejection ledger: declared writer and overflow checks cannot prove complete traffic or universally attribute ingestion quota loss.

Provider attempts use canonical `agentops/v1` CLIENT attempt spans, separate from logical model SERVER operations. Identity is trace/span plus parent/run consistency. Start time selects `[from,to)`; hydrated parents/attempts may lie outside that interval. Tools and aggregate summaries are not extra provider calls. Explicit successful outcomes count good; known ERROR outcomes count bad. Missing/ambiguous outcomes or reconstruction gaps prevent a measured ratio. Retries can improve logical success while lowering attempt success. Usage completeness is separate from known execution outcomes; missing tokens/cost are never zero. Fixture, synthetic and live populations cannot substitute for each other. Observed timeout/cancel classification does not establish provider receipt or answer quality.

## Exact arithmetic

For eligible N, good G, bad B and target T: G+B=N; SLI=G/N; allowed bad fraction=1-T; allowed bad events=N(1-T); consumed budget=B/[N(1-T)]; remaining=N(1-T)-B; burn=(B/N)/(1-T). Counts and rational results are serialized as strings. Fractional budgets remain fractional, and negative remaining budget is preserved. No rounding up to one event. Zero N has no ratio.

MEASURED permits arithmetic. NO_OBSERVATIONS, INSUFFICIENT_EVIDENCE, INCOMPATIBLE and BACKEND_UNAVAILABLE produce UNKNOWN and null budgets, with reason and diagnostics. Overall measurement and incident state are distinct. Raw queries support at most one hour; definitions may describe up to30 days but unsupported windows remain UNKNOWN. Durable long-window rollups are future work.

Every evaluation freezes the definition, common UTC cutoff, revision, source windows, counts, diagnostics, budgets and decisions. Its ID hashes definition identity/content, cutoff and revision. Retrying the same ID returns the stored result; late telemetry requires a new revision, which cannot rewrite lifecycle state at the same cutoff.
