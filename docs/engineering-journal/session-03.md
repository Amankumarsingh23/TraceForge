# Session 03 — Trace reconstruction and bounded query APIs

Date: 2026-11-12 (Asia/Calcutta). Starting HEAD: `456bd88`.

## Scope and exact files

Implemented read-time trace interpretation and HTTP detail/discovery over the existing ClickHouse store. Kept OTLP ingestion, raw MergeTree schema, acknowledgments, Collector configuration, demo endpoints, credentials and volumes unchanged. No new dependencies were added. `.env` and ignored learning notes were not modified; previous session journals remain historical records.

Added:

- `internal/traces/model.go`: stored/query models and explicit diagnostics/errors.
- `internal/traces/reconstruct.go` and `reconstruct_test.go`: canonical records, replay/conflict grouping, parent linking, cycle detection, finite sorted forest, timing and gap diagnostics.
- `internal/queryapi/http.go`, `search.go`, `http_test.go`: routes, sanitized errors, limits, filters and versioned keyset cursors.
- `internal/storage/query.go` and `query_test.go`: context-aware parameterized detail/discovery reads, sentinel/payload bounds, nanosecond binding, decoding and iterator cleanup.
- `tests/integration/query_test.go`: actual API/storage/OTLP replay, synthetic graph, pagination/boundary, limit and outage checks.
- `scripts/session03-smoke.ps1`: reproducible Windows workflow with database restoration in finally.
- `docs/adr/0003-read-time-trace-reconstruction.md` and this journal.

Modified `cmd/traceforge-server/main.go` to mount query routes alongside health routes on the existing port 8082, add bounded query flags and allow a 6s HTTP write deadline. Updated README, architecture and resume evidence. No schema rewrite, worker queue, frontend, logs/metrics, RAG feature or remote push.

## Implemented behavior

`GET /api/v1/traces/{trace_id}` loads all observed services for a canonical nonzero lowercase trace ID. Physical-row replay counts do not inflate the logical forest. Exact normalized content excludes ingestion-only metadata. Conflicts select earliest ingestion time with a payload-based tie-break and emit diagnostics; this does not establish which record is true.

Parentless roots, direct orphans and cycle components remain separate. Iterative parent traversal detects self/long cycles; only one display edge per cycle is detached and flagged, while original parent IDs remain. Root/sibling ordering is deterministic by raw start time then span ID. Raw start/end/duration and relative offsets/envelope are preserved. Completeness is always unknown; missing references and timing anomalies are evidence, not invented repairs.

`GET /api/v1/traces` discovers trace summaries from raw spans starting in a bounded UTC window. Service/status predicates qualify the same physical observation, then summaries include all observed services in that window. Full detail is hydrated without the discovery filter. Cursor state includes absolute bounds/filters/page size and the latest-start/trace-ID tuple. Nanosecond integer binding avoids the driver's default seconds precision. Late telemetry can move records between pages; this is not snapshot pagination.

## Commands executed

Read Git status/HEAD, AGENTS, README, architecture, Session 2 journal, actual migration/schema, storage and server wiring, tests, Compose and Dockerfile. Confirmed Go 1.27.4 Windows amd64, running Docker/Linux services and the reusable Session 2 Linux verification image. Consulted official OTel parent/link documentation and the existing official ClickHouse driver's binding/query implementation; retained all pinned versions.

```powershell
gofmt -w cmd internal tests
go mod tidy
go vet ./...
go test ./...
go test -json ./...
go test -tags integration ./tests/integration -run '^$'
go test ./internal/traces ./internal/storage
go build ./...
docker compose config --quiet
docker compose up -d --build --wait --wait-timeout 180
.\scripts\session03-smoke.ps1
go test -count=1 -tags integration ./tests/integration -run '^TestTraceDetailLimitAPI$' -v
docker run --rm --mount "type=bind,source=$PWD\internal,target=/src/internal,readonly" --mount "type=bind,source=$PWD\cmd,target=/src/cmd,readonly" --mount "type=bind,source=$PWD\tests,target=/src/tests,readonly" traceforge-session02-checks sh -c 'go vet ./... && go test -race ./...'
```

Password loading was process-local from the existing ignored `.env`; no credential was printed or rewritten. Smoke invokes actual tagged Go tests, stops ClickHouse only for the controlled outage, restores it, waits for `/readyz`, and rechecks queries and fresh ingestion. Existing original demonstration rows are never replayed/deleted/changed: replay and anomaly tests use new trace IDs.

Manual evidence: raw `Invoke-WebRequest` detail/search responses saved to ignored `artifacts/session03-{known-trace,replay,anomaly,search}.json`. Authenticated SQL was piped to the existing container's `clickhouse-client --multiquery`, using its environment password without expansion in reported commands. Full unit, Linux, integration and SQL logs remain in ignored `artifacts/`. README contains executable PowerShell equivalents.

## Verification results

Formatting, tidy, vet, ordinary tests, build, tagged integration compilation and Compose validation passed. **35 top-level ordinary tests plus 69 subtests = 104 passing test events** across six tested packages; the two commands have no direct tests. These include the prior 53 cases and new reconstruction, HTTP and repository checks. Linux Docker `go vet ./...` and `go test -race ./...` passed against the final source with read-only code mounts; no remote CI or Windows-native race pass is claimed.

Reconstruction tests cover empty/single/branching/chained/out-of-order input, exact and conflicting duplicates, ingestion-time and payload tie-breaks, missing parents, multiple roots, disconnected components, self/long/multiple cycles, shuffled deterministic output, no input mutation, overlap/skew/inconsistent durations, links versus parents and trace-scoped identity. A 2000-node chain reconstructed and survived JSON encode/decode without recursive graph traversal failure. Oversized physical rows and unsafe JSON are rejected.

HTTP/repository tests cover IDs, unknown traces, 400/404/408/413/500/503/504 outcomes, sanitized errors, bounds, repeated/unknown filters, injection-shaped service values, cursor mismatch/structure, fixed defaults across pages, nanosecond position preservation, cancellation/deadlines, active-query limits, one-extra-row sentinel, row cleanup/errors and parameter binding.

Live verification covers **10 distinct top-level integration tests plus 6 scenario subtests**; the historical trace check is repeated after recovery. Session 2 storage/type/redaction and normal/delayed/failed persistence checks still pass. The physical-limit test additionally inserts a new trace with 1001 rows/one unique span ID and verifies HTTP 413 before deduplication. Exact fixture evidence follows.

The final complete script rerun also passed on the final source: replay trace `0c73ba17af98e6f1361f778391ec08c8` (10 physical/5 logical/5 exact replays), anomaly trace `66511608778d17ee86665bde20cb70bd` (8 physical/6 logical/one conflict/orphan/cycle), limit trace `28fff206989f4b54a7a05123f8cc0746` (HTTP 413), and fresh recovery trace `18d5ac66ec32e11539b5ee0f2d13dfae` (one stored row). Historical detail still had the same five spans. The detailed SQL/API excerpts below use the earlier inspected passing fixtures, which remain queryable.

### Failures found and corrected

- The first new HTTP test file had a missing closing brace. Initial gofmt/test parsing failed; fixed the test and reran formatting, vet and the full suite successfully.
- The first controlled database-outage API test observed 504 instead of expected 503. The driver wrapped its own one-second dial timeout in DeadlineExceeded even though the API context was still live. Updated repository error mapping to distinguish expired caller/query context from transport unavailability and added a regression assertion. The complete workflow then returned query 503 and ingestion Unavailable, restored ClickHouse, and passed recovery checks. Initial/final logs are retained; neither initial failure is counted as a pass.
- An early human-readable PowerShell inspection used insufficient ConvertTo-Json depth and warned of display truncation. Subsequent inspections saved the raw HTTP response and decoded full JSON. The API did not truncate its graph.

## Actual trace tree and source preservation

Historical detail endpoint: `/api/v1/traces/656f80607b005aa65927df202991a837` returned five physical rows, five logical spans, one root, zero replay/conflict/orphan/cycle counts, no detected gaps and `completeness: unknown`. All stored IDs, parent IDs and timing fields matched the API.

```text
f86c1405e17684a5  gateway  GET /checkout
├── d672c3c483420102  gateway  HTTP GET
│   └── 26138842f5d84d28  payment  GET /charge
│       └── ab4355622f348af5  payment  payment.process
└── ebc22e80f8efe8d7  gateway  checkout.validate
```

This is the actual chronological/ID-tie output ordering for those raw start times, not insertion order. The historical dataset still contains three gateway and two payment spans after every test and database restart.

## Exact SQL/API evidence

```sql
SELECT trace_id, count() AS physical_rows, uniqExact(span_id) AS unique_spans
FROM traceforge.spans
WHERE trace_id IN (
 '656f80607b005aa65927df202991a837',
 'cbfad50160a80812de646942b4b781ff',
 '3bd444613ba046b687269c1b15bab82e',
 'fcdc262c709e0d8b198fa0028ce30c20')
GROUP BY trace_id ORDER BY trace_id;
```

Actual output:

```text
trace_id                          physical_rows  unique_spans
3bd444613ba046b687269c1b15bab82e          8             6
656f80607b005aa65927df202991a837          5             5
cbfad50160a80812de646942b4b781ff         10             5
fcdc262c709e0d8b198fa0028ce30c20       1001             1
```

Replay detail for `cbfad50160a80812de646942b4b781ff`: physical=10, unique=5, exact_duplicate_rows=5, conflicting_groups=0, root_count=1. One real new checkout trace was re-exported from its stored sanitized resource/scope/span protobuf JSON using the official OTLP client. Payment discovery found it with five unique spans and both services; detail retained gateway parents. Original Session 2 rows were not modified.

Synthetic detail for `3bd444613ba046b687269c1b15bab82e`:

```json
{
  "physical_rows": 8,
  "unique_spans": 6,
  "exact_duplicate_rows": 1,
  "conflicting_duplicate_groups": 1,
  "observed_root_count": 2,
  "orphan_span_count": 1,
  "missing_parent_ids": ["0000000000000003"],
  "cycles": [["0000000000000005", "0000000000000006"]],
  "multiple_roots": true,
  "has_detected_gaps": true,
  "completeness": "unknown"
}
```

Conflict span `0000000000000002` had two variants; its canonical name remained `fixture-2`. A child-starts-before-parent timing warning preserved the original parent and time. All six logical nodes remained reachable in the finite returned forest.

Manual discovery request:

```text
GET /api/v1/traces?from=2026-11-11T18:00:00Z&to=2026-11-12T18:00:00Z&service=payment&limit=3
```

Actual three returned summaries at inspection time:

```text
trace_id                           unique_span_count  has_error_span
d05ca126c123144b9a2c9ee9253110ef             5                true
6d6e50627ac0b338d4e75b64b81b7306             5                false
e801369a16515e71f9023623643ffb92             5                false
```

Pagination fixture service `s03-page-249475ac6235719e` returned five qualifying traces over **three pages (2/2/1)** without repeats, including tied nanosecond ordering positions. An extra trace exactly at the exclusive `to` was absent. Applying the same-service error predicate found two traces; error spans in the other service did not qualify the remaining traces. Summaries retained both services and distinct counts. Cursor mismatch/input cases were rejected by unit tests.

## Failure/recovery evidence

During ClickHouse stop, both detail and discovery returned structured sanitized **503**. Direct OTLP Export returned **Unavailable**, not a false success; `/healthz` stayed 200 and `/readyz` returned 503. After the finally block restarted ClickHouse and readiness recovered, historical detail again returned the same five-span graph. Fresh recovery trace `a937f86ef79e45eaacd335bf839b6f24` stored one row and received full success. This proves recovery of new operations, not recovery of every failed/queued export.

## Known limitations and next checkpoint

Canonical selection is deterministic, not proof of truth. Array-order and absent/default distinctions are conservative content differences; conflicting versions remain only as raw rows plus API diagnostics, not a full conflict-history endpoint. Detail envelopes use canonical records, while search describes all raw versions in the start-time window. These may differ under conflicts or partial windows.

Completeness remains unknown even for one root with no gaps. Cycle-edge detachment is display-only, not telemetry repair. No critical/exclusive time is computed. Search service names are scoped to documented ASCII identifiers; no arbitrary attribute search exists. Search pages are not snapshots under late/concurrent writes. Default detail bounds apply to physical rows, even exact replays. Server resource limits may fail a query instead of yielding a result; no truncation is presented as complete.

The existing small shared ClickHouse connection pool is retained; this is a local bounded query implementation, not an optimized capacity claim. Local plaintext and unauthenticated loopback access remain; stored redaction does not protect upstream Collector debug output or guarantee comprehensive privacy. No unconditional at-least-once/exactly-once, durability upgrade, remote CI pass or benchmark is claimed.

Session 4 readiness: real persisted traces can now be discovered and safely interpreted with explicit uncertainty. The next work is bounded ingestion, batching and backpressure. No Session 4 code has been started.

Commit message: `feat: add bounded trace queries and deterministic reconstruction`.
