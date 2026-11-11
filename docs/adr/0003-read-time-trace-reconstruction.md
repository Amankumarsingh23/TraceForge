# ADR 0003 — Read-time reconstruction and bounded trace queries

Status: Accepted for Session 3.

## Decision

Retain Session 2's raw MergeTree rows and acknowledgment semantics. Reconstruct on reads in the pure `internal/traces` package rather than relying on arrival order or replacing the storage engine. Build a map from span ID to a canonical observation within one trace; link recorded parents after all identities are known. Expected linking/traversal cost is O(n), with O(n log n) deterministic sorting plus work proportional to bounded payload bytes. Memory is proportional to the loaded records and their payloads.

Exact duplicates compare all known normalized content, excluding `ingested_at`. JSON object key order/whitespace and time-zone formatting are normalized without converting numbers to float64. Array order and presence/default distinctions remain significant; this is conservative content equivalence, not arbitrary semantic equivalence of all possible OTLP messages. Repeated versions within conflicting groups still count as exact replay rows.

For conflicts choose the earliest ingestion timestamp, then lexicographically smallest normalized payload to resolve millisecond timestamp ties. Report variants and conflicting span IDs. This is deterministic across database/arrival order, not a claim that the chosen observation is true. Raw rows remain available for investigation.

## Incomplete and invalid observations

Return parentless root candidates separately from direct orphans with missing parents. Preserve descendants under their observed component. Return every cycle's involved IDs using iterative three-state parent traversal. Detach the parent edge of the lexicographically smallest cycle ID only in the returned forest; preserve its recorded parent ID and flag the detached edge. Its component appears in `disconnected_spans`. No synthetic nodes, clock corrections, exclusive-time subtraction or critical-path calculation are introduced.

Completeness is always `unknown`. `has_detected_gaps` means missing parent references were detected; its absence does not prove all branches arrived. Cycles, conflicting observations and multiple roots are separate diagnostics. Detail timing/envelope uses canonical records; raw times and durations are preserved even when a mismatch is diagnosed.

## Query and pagination design

Extend the official ClickHouse driver repository with context-aware parameterized reads. Exact trace lookup loads every observed service with `maxRows + 1`; exceeding limits is an error, not a silently truncated tree. Queries have execution/read/memory/result caps and close/check row iterators. HTTP and database operation deadlines are separate from transport dial failures: an expired request/query deadline is 504, while backend connection failure is 503.

Search aggregates raw observations whose span start is within `[from, to)`, defaulting to a fixed one-hour UTC window with a 24-hour maximum. A service/error predicate qualifies trace IDs, with both predicates applied to the same physical observation. Summaries include all services in that window and `uniqExact(span_id)`; any observed error is not proof the root request failed. Full detail lookup has no inherited service/time filter.

Order summaries by latest observed start descending, then trace ID ascending. Use a versioned URL-safe base64 keyset cursor containing the absolute window, filters, page size and final ordering tuple. Missing query filters inherit cursor values; supplied values must match. Nanosecond boundaries are bound as int64 through `fromUnixTimestamp64Nano`, avoiding the driver's default seconds-scale time binding. Cursors are validated pagination state, not signed authentication tokens.

There is no snapshot across requests. Late spans/conflicting versions may change membership, summary time, canonical selection or page position and cause omissions/repeats across pages. Fixed bounds and keyset ordering improve predictability without eliminating this limitation. No separate search infrastructure or index/schema rewrite is justified by this session.

## Scope and references

Keep HTTP health/query routes on loopback host port 8082 and OTLP on its existing endpoints. No UI, logs/metrics, ingestion queue, multi-tenant RBAC or new dependencies. [OTel parent/link semantics](https://opentelemetry.io/docs/specs/otel/trace/api/) and the [official ClickHouse Go driver](https://github.com/ClickHouse/clickhouse-go/tree/v2.51.0) underpin this design.
