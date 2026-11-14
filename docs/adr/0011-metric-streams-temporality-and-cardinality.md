# ADR0011: independent metric streams, temporal normalization and bounded admission

Status: accepted, Session11.

## Decision

Add an independent official OTLP metrics receiver, request-batched synchronous storage adapter and pure canonical temporal engine. Keep raw normalized typed points with exact integer/time strings and full canonical stream keys. Index discovery fields in a new raw MergeTree table; avoid schema/aggregation optimization work planned for Session13.

Use full resource+scope+name+unit/type/temporality/monotonicity+typed dimensions to identify streams, including explicit lifetime writer IDs. Store the key alongside its hash and diagnose conflicting definitions. Point replay is identity-aware; conflicting values select deterministic earliest-ingestion/full-content canonical observations and remain unreliable. Do not reuse trace-span identity rules for metric intervals.

Normalize each writer's cumulative/delta sequence before combining compatible populations. Require fully covered intervals; use bounded predecessor hydration. Preserve baselines, resets, missing points, overlaps and unknown starts. Treat unknown source continuity as unreliable. Gauges are observations, not totals. Histogram CDFs are converted before merging; quantiles are nearest-rank bucket ranges rather than false exact values. No cumulative min/max subtraction or arbitrary rebinning.

Use two metric persistence slots and immediate per-export batches instead of copying private trace/log queue implementations. Queue0/wait0 limits normalized memory and confirms database persistence before ACK; the tradeoff is smaller inserts at high export rates. Explicit local lifetime admission caps streams/resources/writers, preserves overflow series and survives ambiguous writes. Receiver restart resets local admission, not stored data. This is not a durable tenant quota or exactly-once source-event system.

## Consequences and alternatives

Summing cumulative readings, differencing unrelated instances, adding overlapping deltas or averaging histogram percentiles would produce false traffic/distributions. Redacting identity attributes into a shared placeholder could merge unrelated streams; recognized sensitive/unsupported identity metadata is rejected instead. Unknown interval boundaries are not proportionally split into invented exact counts. Unsupported optional fields/types are point rejections, not silently stored successes.

V1 bounds native-resolution queries and returns explicit budget errors rather than truncated inputs or a full PromQL engine. Complete interval coverage and observed rate are separate from traffic capture completeness. Shared ClickHouse capacity remains a limitation despite independent signal budgets. Next.js fixed proxies expose actual metadata and diagnostics with exact raw points, signed/current-state distinctions and no fake data. Metric export completion and trace start windows can legitimately give different rate denominators.

Verified with actual100-checkout workload, replay, payment restart, two independent cumulative writers, compatible/incompatible histogram fixtures, real SDK overflow, backend cardinality caps, database failure/no-false-ACK, browser and prior regressions. See [metric contract](../metrics/metric-semantics.md) and [session report](../engineering-journal/session-11.md). SLOs/alerts, ExponentialHistogram/Summary, durable quotas, production IAM and major storage optimization remain separate future work.
