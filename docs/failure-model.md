# TraceForge failure model — through Session 4

Success means the receiver observed ClickHouse's synchronous insert confirmation for that export's accepted rows. It does not mean replicated/power-loss-safe storage, exactly-once delivery, or unconditional eventual delivery. `async_insert=0` and raw MergeTree rows remain in use; `insert_deduplicate=0` keeps replay behavior explicit.

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
