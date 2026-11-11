# Session 02 — Real OTLP ingestion and ClickHouse persistence

Date: 2026-11-11 (Asia/Calcutta). Session 1 commit preserved: `d279c34`.

## Implemented architecture and file changes

Gateway/payment still send OTLP to the existing Collector on localhost:4317. The Collector now forwards gzip OTLP/gRPC to `traceforge-server:4319` and keeps debug output as a diagnostic branch. The Go receiver validates every resource/scope/span group, redacts known sensitive attributes, normalizes rows and sends one synchronous ClickHouse prepared batch per request. Only confirmed inserts allow success. PostgreSQL remains unchanged and has no application schema.

Added: `cmd/traceforge-server/main.go`; `internal/ingest/{normalize,server}.go` and tests; `internal/storage/clickhouse.go`, writer tests and embedded `migrations/0001_spans.sql`; `Dockerfile`, `.dockerignore`; tagged `tests/integration/storage_test.go`; `scripts/session02-smoke.ps1`; normalization contract; ADR 0002; this journal. Updated Compose, Collector config, Go modules, README, architecture, resume evidence and CI formatting coverage. No Session 1 service code or historical journal was rewritten. Actual `.env`, volumes and ignored learning notes were left untouched, as requested by Session 2.

New driver: official ClickHouse Go v2.51.0, verified against the Go module proxy and upstream API. Existing OTel 1.47.0, otelhttp 0.72.0, OTLP generated messages 1.11.0, Collector 0.165.0 and database versions were retained. The driver's transitive x/net/x/sys/x/text requirements updated in `go.mod`/`go.sum`. `.env.example` documents optional host-run connection variables; the actual `.env` is unchanged.

## Commands actually executed

Repository inspection: Git status, AGENTS, README, architecture, Session 1 journal, Go modules, Compose/Collector/CI configuration, existing listeners and containers. Toolchain: portable Go 1.27.4 Windows amd64, Docker Desktop Linux engine 29.9.2 and Compose 5.6.1. No toolchain installation was needed this session.

```powershell
go get github.com/ClickHouse/clickhouse-go/v2@v2.51.0
gofmt -w cmd internal tests
go mod tidy
go vet ./...
go test ./...
go test -json ./...
go test -tags integration ./tests/integration -run '^$'
go build ./...
docker compose config --quiet
docker compose build traceforge-server
docker compose up -d --build --wait --wait-timeout 180
docker compose restart otel-collector
docker build --target verify --tag traceforge-session02-checks .
docker buildx build --target verify --output=type=cacheonly .
docker run --rm --mount "type=bind,source=$PWD\internal,target=/src/internal,readonly" --mount "type=bind,source=$PWD\cmd,target=/src/cmd,readonly" --mount "type=bind,source=$PWD\tests,target=/src/tests,readonly" traceforge-session02-checks sh -c 'go vet ./... && go test -race ./...'
.\scripts\session02-smoke.ps1
.\scripts\session02-smoke.ps1 -SkipDatabaseFailure
docker compose exec -T traceforge-server /traceforge-server --migrate-only
Invoke-RestMethod 'http://localhost:8082/readyz'
docker compose ps
docker compose logs --tail=65 --no-color otel-collector
docker compose logs --tail=20 --no-color traceforge-server
```

All Go commands used the existing portable toolchain through a process-local PATH. The smoke script invokes actual tagged gRPC/SQL/demo integration tests, `docker compose stop clickhouse`, `docker compose start clickhouse`, readiness polling and a fresh recovery export. It loads the existing password without displaying or rewriting it. Authenticated SQL queries were piped through `clickhouse-client --multiquery` in the existing ClickHouse container; passwords expand only inside the container.

## Checks and actual outcomes

Windows formatting, tidy, vet, build, ordinary tests, integration compile and Compose validation passed. Ordinary tests: **22 top-level tests plus 31 subtests, 53 passing test events**, across four tested packages; both commands have no direct test files. The new tests cover identifier/time/size/count/malformed-input rules, typed metadata, recursive redaction, preservation of ordinary URL values, every resource/scope group, gzip gRPC, full/partial/error responses, no acknowledgment before the write, cancellation/deadlines, bounded concurrency, readiness, batching and failure cleanup. Session 1's tests still pass.

**Linux `go vet ./...` and `go test -race ./...` passed inside the Docker verify build target**, including demo, ingest, storage and telemetry packages. The final source was also checked by mounting only code directories read-only into the already built verification image, reusing its compiler cache without exposing local credentials/notes. The official Go bookworm build image supplies Linux cgo/compiler support. This is local Docker evidence, not a remote GitHub Actions run. Native Windows race checking remains unavailable as recorded in Session 1; no Windows race pass is claimed.

Integration workflow passed **4 top-level tests plus 6 subtests** across its three Go invocations: OTLP storage contract (empty, multiple groups/scopes, partial/replay), real demo (normal/delay/failure), database outage and fresh recovery. Migration ran twice in the contract test and again via `--migrate-only`, successfully and without deleting data.

After final normalization cleanup, a further non-disruptive regression run passed the storage contract, all demo scenarios and a fresh export without another outage. Its normal trace `15456d7407fd32d012c2b9914a66a3aa`, delayed trace `cd804e5ab674f3b120e8b0169df29b0a` and failed trace `5c1348299175db0d4794455c55130a78` each stored five rows with five distinct span IDs and matching Collector parents. The delayed payment-processing duration was 500893600ns. Fresh export `9471ffa4f48c1b740a9ef1bc4035cc1e` stored one row. The detailed SQL evidence below records the earlier complete outage/recovery workflow, whose rows remain queryable.

### A failure found and fixed

The first full integration attempt passed direct gRPC insert checks but failed all three Collector-forwarded demo scenarios with zero stored spans. Collector logs reported `Unimplemented: Decompressor is not installed for grpc-encoding "gzip"`. The Collector defaults to gzip, while the new server initially lacked registration. Added the standard gRPC gzip registration and changed the unit gRPC client to send compressed requests. Rebuilt/recreated only the receiver and reran the full smoke workflow successfully. Permanently rejected exports from that first attempt were dropped by the Collector; there is no claim that they were recovered.

A manual SQL inspection issued while the smoke script was intentionally stopping ClickHouse also failed because the container was stopping; the same inspection succeeded after recovery. Neither failure was treated as a pass. Initial and final logs remain locally under ignored `artifacts/`.

## Real distributed-trace evidence

The following IDs came from real HTTP checkout responses, and persisted span/parent IDs were compared against Collector debug output. Each scenario stored **5 rows / 5 distinct span IDs / 0 observed duplicates** in the recorded run:

| Scenario | Trace ID | Evidence |
| --- | --- | --- |
| Normal | `656f80607b005aa65927df202991a837` | gateway 200; gateway 3 spans, payment 2 |
| 500ms payment delay | `c1b83f7f5ba2811c294ac5ef1aaca2ff` | stored `payment.process.duration_ns = 500563900` |
| Failed payment | `85f9c363de904bc568806d4aeccf8fe6` | gateway 502/payment 503; gateway/client/payment/process status 2; exception event preserved |

Actual ClickHouse query:

```sql
SELECT trace_id, span_id, parent_span_id, service_name, span_name,
       duration_ns, status_code
FROM traceforge.spans
WHERE trace_id = '656f80607b005aa65927df202991a837'
ORDER BY start_time, span_id;
```

Output below omits the repeated trace ID column for readability; all five rows carry the ID in the WHERE clause:

| span_id | parent_span_id | service_name | span_name | duration_ns | status_code |
| --- | --- | --- | --- | ---: | ---: |
| d672c3c483420102 | f86c1405e17684a5 | gateway | HTTP GET | 19286900 | 0 |
| ebc22e80f8efe8d7 | f86c1405e17684a5 | gateway | checkout.validate | 0 | 0 |
| f86c1405e17684a5 | absent | gateway | GET /checkout | 21424400 | 0 |
| 26138842f5d84d28 | d672c3c483420102 | payment | GET /charge | 539100 | 0 |
| ab4355622f348af5 | 26138842f5d84d28 | payment | payment.process | 0 | 0 |

Zero durations for tiny operations are valid observations from this Windows demo, not fabricated minimum durations. Duration totals are not summed to infer a critical path. The delayed processing duration was greater than baseline and at least the requested 500ms. These are functional observations, not scale/latency benchmarks.

```sql
SELECT service_name, count() AS span_count
FROM traceforge.spans
WHERE trace_id = '656f80607b005aa65927df202991a837'
GROUP BY service_name ORDER BY service_name;
```

Actual output: `gateway 3`, `payment 2`.

## OTLP contract and failure evidence

- Full success: request with 3 resource groups × 2 scopes × 2 spans stored 12 rows, 3 service names and 2 scope names; response had partial_success unset. Recorded trace `75cf66f703825632a0d14334d6b349e5`. Stored start/end retained exact source nanoseconds; protobuf JSON round-tripped max int64 and removed synthetic secret values.
- Partial success: one valid span plus two invalid spans stored exactly one row and returned rejected_spans=2. An intentional second full-success export of the valid span produced **two rows with one distinct span ID** for trace `cfcea26f8423cb95d6c79abeba6d896d`. This demonstrates duplicate risk; clients should not retry the original partial-success response.
- Empty request: full success with no write. Unit tests also verify all-invalid requests report every rejection and make no write.
- ClickHouse outage: after `docker compose stop clickhouse`, a direct gRPC export with valid data returned **Unavailable with no successful response**. Trace `08916e6fe2be82c10dd29a7a07c21b94`; `/healthz`=200, `/readyz`=503. A post-recovery SQL count for this trace returned **0**.
- Recovery: the script's finally block restarted ClickHouse, waited for readiness and exported a fresh span. Trace `9a029942289af1f146d38ed5ff85cdf9` stored **1** row and got full success. Existing stored traces remained present after restart.

Authenticated SQL aggregate output:

```text
trace_id                           rows  distinct_spans
656f80607b005aa65927df202991a837       5       5
85f9c363de904bc568806d4aeccf8fe6       5       5
c1b83f7f5ba2811c294ac5ef1aaca2ff       5       5
cfcea26f8423cb95d6c79abeba6d896d       2       1
outage trace row count: 0
```

## Limits, learning and Session 3 readiness

The implemented acceptance boundary is a successful synchronous ClickHouse Send, not receipt into Go memory. Lost acknowledgments and ambiguous inserts can yield duplicates; no exactly-once, guaranteed atomic multi-partition insertion, replication or power-loss durability claim is made. The receiver has no durable spool. Collector buffering/retries are finite and in memory, and the first failed integration demonstrates permanent-error loss. Recovery proves fresh writes work, not recovery of every failed export.

The MergeTree monthly partition is a time-organization unit; trace-ID-first sorting supports one-trace lookup and sparse range skipping. Neither defines uniqueness. Service-centric queries may scan more data until later measured schema work. JSON String fields preserve bounded variable metadata while key columns support simple direct SQL.

Security remains local-only plaintext ingestion with generated database authentication and heuristic attribute redaction. It is not production authentication, comprehensive privacy or tenant isolation. The Collector's debug branch precedes storage redaction. Unknown wire fields are intentionally omitted from protobuf JSON. See the normalization contract for precise limits and unsupported fields.

Ready for Session 3: actual persisted span records, tested identifiers and parent links, retry/duplicate evidence and SQL inspection are available. Reconstruction, missing-parent behavior, deduplication strategies and trace query APIs remain unimplemented. Do not begin them in this session.

Commit message: `feat: persist OTLP traces in ClickHouse with tested acknowledgments`.
