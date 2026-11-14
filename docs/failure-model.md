# TraceForge failure model — through Session 11

Success means the receiver observed ClickHouse's synchronous insert confirmation for that export's accepted rows. It does not mean replicated/power-loss-safe storage, exactly-once delivery, or unconditional eventual delivery. `async_insert=0` and raw MergeTree rows remain in use; `insert_deduplicate=0` keeps replay behavior explicit.

## Log signal and correlation additions

The same acknowledgment boundary now applies to LogsService. Valid subsets must persist before a partial response reports the exact rejected record count; database failure returns retryable Unavailable without a success/partial acknowledgment. Empty exports do not write. Log and trace budgets are independent, while CPU/network/database connections remain shared. SDK acceptance and Collector queue admission precede backend persistence and cannot prove it.

The SDK can drop logs when its 512-record memory queue is full; its export deadlines/shutdown are bounded. Collector logs have a separate 64-request memory queue, one consumer, finite retries and no detailed raw log debug export. Receiver logs have bounded owned jobs/records/payload and a fixed writer. Crashes/queue overflow/retry expiry can lose records. Canceled/failed exports may still have persisted rows; retries can add duplicates. Generated storage-row IDs and optional source UIDs do not establish exactly-once delivery. Identical legitimate messages are never collapsed.

Both engines drain concurrently on graceful shutdown under the existing bounded lifecycle. A deadline fails unresolved outcomes, cancels lifecycle writes and closes the shared pool; a noncooperating driver still depends on forced process exit. SIGKILL bypasses drain, and an active database write can have an unknown outcome. Deterministic log engine tests cover cancellation/final flush/timeout; existing live trace termination checks verify the shared server lifecycle. No durable log queue is claimed.

Missing event/observed timestamps remain unknown. Effective time may use an explicit receiver-ingestion fallback; this is not recovered source timing. Clock skew can yield negative/out-of-span relative offsets. Trace/span IDs establish correlation, not timing proximity, service name or event identity. Standalone/trace-only records and logs arriving before spans remain valid. Search time windows/late arrivals/non-snapshot cursors can hide records until bounds are changed or pages refreshed; an empty window does not prove no logs exist.

Log queries distinguish successful empty data from unavailable/canceled/oversized results. The UI uses explicit errors, deadlines and stale-response guards, and links to unavailable traces honestly return 404. Existing trace completeness remains unknown. Key/pattern redaction runs before insertion but is not DLP: unrecognized confidential text, binary values under nonsensitive keys and future/unknown metadata can escape semantic recognition. All use remains restricted synthetic local development without production auth/TLS/tenant isolation.

| Failure/state | External behavior | Persistence possibilities |
| --- | --- | --- |
| Malformed envelope/message/count | InvalidArgument or the existing oversized-message ResourceExhausted; no partial success fabricated | Request is not admitted for persistence |
| Invalid individual spans | Exact rejected count after accepted subset writes | Only valid rows are eligible |
| Request cannot fit configured hard span/payload budget | InvalidArgument | No normalized job admitted |
| Admission full past its short wait | Unavailable + RetryInfo unless caller context already expired | That export was not admitted; caller can retry |
| Caller cancels before normalization/admission | Canceled/DeadlineExceeded | No admitted write |
| Caller cancels while queued/collected, before writing | Handler fails; worker skips it when observed | No write if cancellation was observed before write ownership |
| Caller cancels after shared write begins | Caller may see Canceled/DeadlineExceeded | It may still persist; other callers' work is not canceled |
| Database reports a failed/timed-out insert | Unavailable for affected waiting exports | Failure can be ambiguous; some/all rows may exist |
| Successful insert, response lost | Caller may retry | Physical duplicates can result |
| Graceful SIGTERM | New admissions stop, pending work gets bounded drain/final flush | Confirmed writes may succeed; unresolved outcomes fail/are unknown |
| Graceful drain deadline exceeded | Worker context canceled; unresolved results are never success | Already active writes may have persisted; connections are closed and waits bounded |
| SIGKILL/process/host crash | No graceful cleanup/completion guarantee | Queued memory can be lost; active inserts may be ambiguous |
| Collector overflow/retry expiry/restart | Subject to Collector behavior/finite retries | Data can be lost before backend confirmation |

There is no backend retry loop: one batch attempt has one result delivered to all its outstanding tickets. Retrying belongs upstream. Read-time exact-duplicate collapse and conflict diagnostics improve trace interpretation; they do not remove physical rows or upgrade delivery semantics.

## Bounds and accounting

Queued/normalizing positions, active handlers, all owned tickets, reserved spans and conservative payload bytes have separate caps. Dequeue does not release a job's payload budget; completion does. A canceled handler can return before its active write finishes, so worker ownership survives the handler. Completion channels are buffered and only reported once.

Payload reservations are not a hard Go heap/RSS ceiling. Decoded protobufs, runtime/GC overhead, temporary copies, HTTP/gRPC machinery and diagnostics also consume memory. Transport limits, application budgets, fixed workers and bounded diagnostic rings address different resource sources. Container limits constrain the benchmark process separately.

Request/worker contexts have different roles. Canceling an individual request cannot cancel a shared batch; a lifecycle cancellation or write deadline can. A result may be confirmed internally after the caller has gone, so counters for confirmed physical rows need not equal acknowledged spans. Likewise received spans include invalid/rejected/failed requests. Independently query storage for end-to-end accounting.

## Verified evidence

- Controlled fake writers verify shared success/failure, no early acknowledgment, size/time flushes, expiration under backlog, bounded overload, cancellation at both stages, ambiguous results and bounded shutdown/worker exit.
- A real four-job SIGTERM test observed queued work, received four successes and independently found each stored row.
- A separate isolated SIGKILL test observed four pending jobs, received no successes and found zero rows for those fixture IDs. It demonstrates loss of that pre-write memory, not a rule that every kill must leave zero rows.
- Database downtime returned Unavailable to ingestion and 503 to queries, while liveness stayed up and readiness went down. Fresh writes/queries recovered after restart.
- The outage load generator saved a report with two retryable/unknown exports, zero acknowledged spans and unverified storage while the DB was down; a subsequent independent query found zero fixture rows.

See session journals for IDs and actual command outputs. All local traffic remains synthetic/plaintext and loopback-published. Known attribute redaction is not comprehensive DLP or authentication; Collector debug output remains upstream of that boundary.

## Session 10: trace-derived analytics failure boundaries

Service RED is canonical observed SERVER evidence, not unsampled traffic or business success. UNSET and missing HTTP response status remain explicit. Child ERROR alone never changes the SERVER denominator/numerator. Invalid durations do not become zero samples. Empty sample statistics are null.

Two bounded reads discover candidate traces and hydrate their complete rows; no N+1 and no time filter before CLIENT/SERVER pairing. Cross-window pairs belong to CLIENT start. Raw conflicting variants can increase candidate discovery conservatively, then canonical selection decides eligibility. Physical replay does not increase logical counts; contradictory variants remain diagnosed. Sampling, missing/late exports and non-snapshot reads can still change results and topology completeness remains unknown.

Too many candidates/physical rows/bytes/services/edges/buckets or known ClickHouse scan/memory overflow means413 with **no partial result**. Narrowing identity filters does not bypass discovery limits. Deadlines/cancellation propagate; storage unavailability is503, never an empty service graph. Two analytics slots limit concurrency. ClickHouse limits may overshoot within a processed part and are not process isolation. General Go process-heap profiling is not claimed.

Unmatched CLIENTs, missing SERVER parents, wrong parent kinds, ambiguous SERVER children, same-service pairs, missing names, duplicates/conflicts, cycles and timing warnings remain diagnostics. Names/timing/SpanLinks do not create confirmed edges; missing SERVER is not proof a remote operation was not received. Distinct retry IDs remain distinct attempts. CLIENT/SERVER status and latency evidence remain independent; no exact network-latency subtraction.

Stable service IDs include namespace/environment/workload; unknown origin is not labeled live. Identity filters and hashes are not authentication or tenant isolation. Existing loopback restrictions still apply. Same-origin fixed proxies do not accept arbitrary destinations or expose database credentials. Trace/log links use actual observations; existing correlated logs are explicitly limited to the last24 hours. Full definitions and budgets: [analytics contract](analytics/metric-definitions.md).

## Session11 metric boundaries

Metric success confirms synchronous ClickHouse persistence for accepted points, not SDK/Collector admission, replicated durability or source-event exactly-once behavior. Failed/ambiguous writes return UNAVAILABLE and can replay physical rows on retry. Partly invalid/unsupported/cardinality-limited exports persist valid siblings and count rejected_data_points exactly. Two metric slots, no inter-request queue and bounded request batches protect separate trace/log budgets; database/pool/CPU remain shared resources. Metric admission closes/drains before pool shutdown.

Canonical stream/point replay prevents duplicate exported observations inflating increments/distributions. Conflicting content/definitions and unknown accumulation starts remain unreliable. Separate instances normalize independently. Missing initial history is not counted as new traffic; compatible missing cumulative exports can recover only combined increments. Explicit no-value is not zero and does not restart a cumulative sequence. Unexplained decreases cannot create negative monotonic rates or invented resets. Delta overlaps are excluded; partial boundary intervals are not split proportionally into exact counts.

Gauge observations are not totals or indefinitely current values. Histogram boundaries/units/source populations must agree before incremental distributions merge; cumulative min/max and per-series percentiles are not subtracted/averaged. Overflow/infinite percentile buckets keep unbounded endpoints. Incomplete metric coverage uses observed seconds and withholds complete-window rates; selected span-start and exported completion intervals can legitimately produce different rates.

V1 rejects unsupported/nested/exemplar-bearing or sensitive identity data instead of redacting distinct streams into collisions. Full canonical keys protect hash collision/conflict handling. Bounded SDK overflow preserves aggregate observations while losing original dimension coverage, which is surfaced. Local backend admission limits reset at receiver restart and do not enforce a durable multi-node quota. Query budgets fail explicitly; predecessor history and two-read non-snapshot visibility remain limited. Actual database outage returned503 for reads and UNAVAILABLE for metric export, then recovered.

No metrics feature grants authenticated tenant isolation, comprehensive DLP, production TLS/IAM, SLO correctness or complete production traffic. See [metric semantics](metrics/metric-semantics.md) and [actual Session11 evidence](engineering-journal/session-11.md).
