# TraceForge — Session 4

A real local tracing backend: gateway → payment → Collector → bounded Go admission/batching → ClickHouse → trace query API. Exports still wait for confirmed database writes. Detail reconstructs observed spans with duplicate, orphan, cycle and timing diagnostics. Internal ingestion metrics and a synthetic OTLP load generator are available. PostgreSQL remains provisioned without an application schema. There is no UI, user OTLP logs/metrics ingestion or AgentOps evaluation yet.

## Prerequisites and versions

PowerShell 7 (`pwsh`), Docker Desktop with Linux containers, Go 1.27.4. Development tooling: Docker 29.9.2, Compose 5.6.1. Pinned dependencies: OTel SDK/exporter 1.47.0, otelhttp 0.72.0, ClickHouse Go v2.51.0, OTLP generated messages 1.11.0; see `go.mod` and `go.sum`. Images: Collector 0.165.0, ClickHouse 26.8.36.3-alpine, PostgreSQL 18.6. The receiver builds with `golang:1.27.4-bookworm` into a nonroot scratch image.

Go was absent on this machine. A checksum-verified portable copy is at `$env:LOCALAPPDATA\TraceForge-tools\go`. If using it, set PATH in each terminal:

```powershell
$env:PATH = "$env:LOCALAPPDATA\TraceForge-tools\go\bin;$env:PATH"
Set-Location 'C:\placement projects\TraceForge'
go version
docker --version
docker compose version
docker info
```

Implementation references: official [Go instrumentation guide](https://opentelemetry.io/docs/languages/go/getting-started/) and [exporter documentation](https://opentelemetry.io/docs/languages/go/exporters/). Versions were checked against official release feeds, the Go module proxy and official Docker image tags.

## Configure infrastructure

For the first setup, copy `.env.example` and generate two distinct local passwords. Do not overwrite existing credentials when reusing database volumes:

```powershell
Copy-Item .env.example .env
$chPassword = [Convert]::ToHexString([Security.Cryptography.RandomNumberGenerator]::GetBytes(24))
$pgPassword = [Convert]::ToHexString([Security.Cryptography.RandomNumberGenerator]::GetBytes(24))
Set-Content .env "CLICKHOUSE_PASSWORD=$chPassword`nPOSTGRES_PASSWORD=$pgPassword"
Remove-Variable chPassword, pgPassword
docker compose config --quiet
docker compose up -d --wait --wait-timeout 180
docker compose ps
```

`.env` is ignored. Use `config --quiet`; expanded Compose output contains passwords. Changing `.env` does not rotate credentials in an existing PostgreSQL volume. All published ports bind to 127.0.0.1. Collector and demo transport are plaintext local gRPC/HTTP for synthetic data. Production authentication, TLS and tenant isolation are future work. Use only synthetic requests and baggage; HTTP instrumentation can capture URL metadata.

Check authenticated SQL connectivity; passwords expand inside the containers:

```powershell
docker compose exec -T clickhouse sh -c 'clickhouse-client --user traceforge --password "$CLICKHOUSE_PASSWORD" --query "SELECT 1"'
docker compose exec -T postgres sh -c 'PGPASSWORD="$POSTGRES_PASSWORD" psql -h 127.0.0.1 -U traceforge -d traceforge -v ON_ERROR_STOP=1 -c "SELECT 1"'
```

Both should return 1. PostgreSQL's health check establishes readiness; its SQL command additionally verifies authenticated connectivity. The receiver creates ClickHouse's `traceforge.spans` table through migration 0001. PostgreSQL still has no application schema.

## Start or upgrade the backend

Reuse the existing `.env`, volumes, gateway and payment processes. Do not repeat first-time password generation. Build/start the receiver, then explicitly restart the Collector to reload its bind-mounted configuration (Compose does not detect file-content changes):

```powershell
docker compose config --quiet
docker compose up -d --build --wait --wait-timeout 180
docker compose restart otel-collector
Invoke-RestMethod 'http://localhost:8082/healthz'
Invoke-RestMethod 'http://localhost:8082/readyz'
docker compose ps
docker compose logs --tail=30 traceforge-server otel-collector
```

Only the new receiver and the Collector need startup/reload; existing databases and demos retain their configuration. Startup performs authenticated ClickHouse Ping and the embedded, repeatable migration `internal/storage/migrations/0001_spans.sql`. Run it explicitly again without restarting the server:

```powershell
docker compose exec -T traceforge-server /traceforge-server --migrate-only
```

Receiver OTLP is published on loopback 4319, health on loopback 8082. Within Compose it listens on the container interfaces so Collector can resolve `traceforge-server:4319`. The demo still exports to localhost:4317. `/healthz` is process liveness; `/readyz` makes a fresh bounded ClickHouse Ping and returns 503 during database downtime. Startup dependencies supplement actual database checks.

For an optional host-run server, stop only the Compose receiver to free 4319/8082, load the existing password into the current shell without printing it, and run:

```powershell
docker compose stop traceforge-server
foreach ($line in Get-Content .env) {
    if ($line -match '^CLICKHOUSE_PASSWORD=(.+)$') { $env:CLICKHOUSE_PASSWORD = $Matches[1] }
}
$env:CLICKHOUSE_ADDR = 'localhost:8123'
$env:CLICKHOUSE_PROTOCOL = 'http'
go run ./cmd/traceforge-server
# Ctrl+C to stop; then restore the Compose receiver:
docker compose start traceforge-server
Remove-Item Env:CLICKHOUSE_PASSWORD,Env:CLICKHOUSE_ADDR,Env:CLICKHOUSE_PROTOCOL
```

Container receiver uses the native client at clickhouse:9000; optional host mode uses the official driver's HTTP transport at 8123, so no extra native database port is published. `CLICKHOUSE_USER` defaults to traceforge; database is deliberately fixed to traceforge. No DSN/password is logged. Host defaults bind gRPC and health to 127.0.0.1.

## Run two processes

Terminal 1:

```powershell
$env:OTEL_EXPORTER_OTLP_ENDPOINT = 'http://localhost:4317'
$env:OTEL_EXPORTER_OTLP_PROTOCOL = 'grpc'
go run ./cmd/shop-demo --service payment --port 8081
```

Terminal 2, with the same PATH setup:

```powershell
$env:OTEL_EXPORTER_OTLP_ENDPOINT = 'http://localhost:4317'
$env:OTEL_EXPORTER_OTLP_PROTOCOL = 'grpc'
go run ./cmd/shop-demo --service gateway --port 8080 --payment-url http://localhost:8081
```

These values are also the defaults. Endpoint configuration requires an explicit `http://host:port` URL; unsupported protocols fail visibly. Creating the exporter does not prove Collector reachability. Asynchronous export errors appear on stderr. Providers and propagators are passed explicitly to instrumentation.

Terminal 3:

```powershell
$result = Invoke-RestMethod 'http://localhost:8080/checkout'
$result
Invoke-RestMethod 'http://localhost:8080/checkout?delay_ms=500'
Invoke-WebRequest 'http://localhost:8080/checkout?fail=true' -SkipHttpErrorCheck
docker compose logs --tail=250 otel-collector
docker compose logs --no-color otel-collector | Select-String -SimpleMatch $result.trace_id -Context 2,10
```

Success/delay returns 200. Payment failure returns payment 503 and gateway 502 with error spans. `delay_ms` is an integer in 0..2000; `fail` is true/false. Invalid controls return 400. No random behavior or external payment calls exist. The JSON `trace_id` comes from the current request.

## Trace hierarchy

```text
GET /checkout                 SERVER, gateway
├── checkout.validate         INTERNAL, gateway
└── HTTP GET                  CLIENT, gateway (otelhttp names it)
    └── GET /charge           SERVER, payment, remote parent
        └── payment.process   INTERNAL, payment
```

Five distinct span IDs share one trace ID. Payment server's parent equals gateway client's ID. Validation and client are siblings. Gateway is a root unless the caller supplies incoming W3C context. Export order need not follow parent order.

Inspect `Trace ID`, `ID`, `Parent ID`, `Name`, `Kind`, resource `service.name`, timestamps, statuses and exception events. Allow approximately five seconds for SDK batching. Debug output proves receipt, not durable storage. Always-sampling and bounded in-memory SDK buffering are demo choices; crashes, queue overflow and export failures can lose spans.

## Verify

```powershell
gofmt -w cmd internal
go mod tidy
go vet ./...
go test -v ./...
go build -o bin/shop-demo.exe ./cmd/shop-demo
go build ./...
docker compose config --quiet
.\scripts\smoke.ps1
.\scripts\session02-smoke.ps1
# Linux race checks inside the existing Go build image:
docker buildx build --target verify --output=type=cacheonly .
```

The smoke script expects both services already running. It checks normal, 500ms-delay and failure responses, parses actual exported traces, verifies five operations and their parent links, and runs authenticated SQL checks. Its parser targets Collector 0.165.0's debug format and is independent of export ordering. It preserves containers and volumes.

Component tests use separate in-memory tracer providers and real HTTP servers. They check wire traceparent, shared identity, distinct IDs, kinds, hierarchy, errors, bounds, incoming context, deliberately lost propagation, invalid initialization, baggage and graceful draining. Receiver tests use actual gzip-compressed gRPC calls over an in-memory listener and a storage fake to assert success/partial/error semantics and the write-before-ack boundary. Writer tests assert one prepared batch, one send, and cleanup on failures. Linux CI and the Docker verify target run the race detector; Windows race testing still requires a C compiler.

Session 2 smoke uses the existing password without rewriting `.env`, starts from fresh random trace IDs, checks actual stored rows against Collector IDs, preserves typed/nanosecond data, verifies redaction, partial success, and deliberately replays a span to demonstrate duplicates. **It temporarily stops ClickHouse**, asserts a direct gRPC `Unavailable` response plus liveness/readiness behavior, restores ClickHouse in `finally`, and verifies a new stored export. Avoid unrelated traffic during this controlled outage. To skip the outage:

```powershell
.\scripts\session02-smoke.ps1 -SkipDatabaseFailure
```

The demo processes must already be running. The script expects the default local database HTTP endpoint and receiver ports; clear unrelated `CLICKHOUSE_ADDR`/`CLICKHOUSE_PROTOCOL` overrides first. Integration Go files are tagged `integration` so ordinary tests do not need Docker. Failure-only and recovery tests can be run directly after loading `CLICKHOUSE_PASSWORD` as shown above:

```powershell
try {
    docker compose stop clickhouse
    $env:TRACEFORGE_EXPECT_DB_DOWN = '1'
    go test -count=1 -tags integration ./tests/integration -run '^TestDatabaseUnavailable$' -v
} finally {
    Remove-Item Env:TRACEFORGE_EXPECT_DB_DOWN -ErrorAction SilentlyContinue
    docker compose start clickhouse
}
# Wait until readyz is 200, then:
go test -count=1 -tags integration ./tests/integration -run '^TestFreshExportAfterRecovery$' -v
```

## Inspect the stored trace

Generate a fresh request, allow batching/forwarding, then query using authenticated ClickHouse stdin. These are direct SQL checks, not a trace reconstruction API:

```powershell
$result = Invoke-RestMethod 'http://localhost:8080/checkout'
$traceId = $result.trace_id
if ($traceId -notmatch '^[a-f0-9]{32}$') { throw 'Invalid trace ID' }
Start-Sleep -Seconds 6
$sql = "SELECT trace_id,span_id,parent_span_id,service_name,span_name,duration_ns,status_code FROM traceforge.spans WHERE trace_id='$traceId' ORDER BY start_time,span_id FORMAT PrettyCompactMonoBlock;"
$sql | docker compose exec -T clickhouse sh -c 'clickhouse-client --user traceforge --password "$CLICKHOUSE_PASSWORD" --multiquery'
$sql = "SELECT service_name,count() AS span_count,min(start_time),max(end_time) FROM traceforge.spans WHERE trace_id='$traceId' GROUP BY service_name;"
$sql | docker compose exec -T clickhouse sh -c 'clickhouse-client --user traceforge --password "$CLICKHOUSE_PASSWORD" --multiquery'
```

Expect three gateway and two payment spans in an ordinary request with no replay. `span_kind`: 1 internal, 2 server, 3 client. `status_code`: 0 unset, 1 OK, 2 Error. Failed payment produces error statuses plus exception events. `delay_ms=500` increases stored payment processing duration; no exact wall-clock overhead is promised. All scenario IDs and SQL outputs from verification are recorded in `docs/engineering-journal/session-02.md`.

## Acknowledgments, limits and privacy

The official `TraceServiceServer.Export` traverses every resource and scope. An empty request succeeds without storage. Valid rows enter a bounded queue and may share one synchronous prepared insert with other requests. Each handler waits for its own completion signal; only successful `Send` permits acknowledgment. Full success leaves `partial_success` unset. Invalid spans are counted per original request, with partial success only after its valid subset writes. Clients must not retry partial success. All-invalid requests make no insert and report every rejection.

Malformed envelopes/excessive span count return `InvalidArgument`. The gRPC 4MiB decompressed cap preserves oversized-message `ResourceExhausted` behavior. Temporary insertion/admission failure returns `Unavailable` with RetryInfo; caller cancellation/deadline statuses are retained. Admission waits at most 50ms by default across handler and payload reservations. A request that cannot fit configured hard budgets returns `InvalidArgument`. Errors do not include credentials or raw telemetry.

The existing 1000-span/group/scope, 64KiB-span, 16KiB-resource/scope, 128-event/link and nested attribute limits remain (see `docs/normalization.md`). Session 4 adds explicit transport/handler/queue/job/span/payload caps described below. Export handling retains a 5s deadline; shared writes have a 3s lifecycle deadline. Startup still requires database readiness. Receiver shutdown stops admissions, drains/flushes within a configurable 10s default, fails unresolved outcomes, closes the pool and bounds remaining worker/gRPC/HTTP shutdown. Compose grants 20s.

This is **not exactly-once or unconditional at-least-once delivery**, replicated durability or guaranteed atomic multi-partition insertion. `async_insert=0` waits for normal synchronous insert confirmation. `insert_deduplicate=0` makes physical replay explicit. Lost responses and ambiguous writes can produce duplicates; MergeTree ordering enforces no uniqueness. Read-time deduplication does not change this contract. No WAL or durable spool exists.

Collector uses gzip OTLP/gRPC, a bounded in-memory queue of 100 requests with two consumers, and retries with 1–5s backoff for up to 60s. It can lose queued spans on crashes, overflow, expired retry windows or permanent errors. Debug output is a separate branch and can show a span that never persisted. Known secret-bearing attributes are redacted at the persistence boundary, but upstream Collector debug logs are unsanitized; only synthetic traffic is appropriate. See `docs/normalization.md` for supported fields and explicit redaction limits.

## Session 3 query API

Rebuild only the receiver for this session; no migration or Collector endpoint change is required. Keep `.env`, volumes, gateway and payment processes intact:

```powershell
docker compose up -d --build --wait --wait-timeout 180
$traceId = '656f80607b005aa65927df202991a837'
$detail = Invoke-RestMethod "http://localhost:8082/api/v1/traces/$traceId"
$detail | ConvertTo-Json -Depth 40
$detail.diagnostics
```

This historical Session 2 trace has one observed root, three gateway spans and two payment spans. A new empty checkout request can be triggered with the existing `/checkout` endpoint; allow approximately one SDK batch interval before detail lookup. For a portable new installation without historical volumes, use a newly generated trace ID instead.

Search recent traces, filter by service, and follow the returned cursor:

```powershell
$page = Invoke-RestMethod 'http://localhost:8082/api/v1/traces?limit=10'
$page = Invoke-RestMethod 'http://localhost:8082/api/v1/traces?service=payment&limit=10'
$page.traces | Format-Table trace_id,unique_span_count,has_error_span
if ($page.next_cursor) {
    $next = Invoke-RestMethod ("http://localhost:8082/api/v1/traces?cursor=" + [Uri]::EscapeDataString($page.next_cursor))
}
# Hydrate a selected trace; detail retains every observed service:
if ($page.traces.Count -gt 0) {
    Invoke-RestMethod ("http://localhost:8082/api/v1/traces/" + $page.traces[0].trace_id) | ConvertTo-Json -Depth 40
}
```

Use explicit UTC bounds to revisit an older trace. Default discovery only covers the previous hour, which may exclude earlier session traces:

```powershell
$from = [Uri]::EscapeDataString('2026-11-11T18:00:00Z')
$to = [Uri]::EscapeDataString('2026-11-12T18:00:00Z')
Invoke-RestMethod "http://localhost:8082/api/v1/traces?from=$from&to=$to&service=payment&status=error&limit=20"
```

`from` is inclusive and `to` exclusive, based on span start time. Times must be RFC3339/RFC3339Nano UTC strings ending in Z. The maximum window is 24 hours. `service` is an exact ASCII identifier of 1–128 characters using letters, digits, dot, underscore, colon, slash or hyphen. `status=error` is the only status filter. When both are present, the same observed physical span must match service and error. Unknown/repeated filters, malformed time/ID/cursor values, and out-of-range limits return structured 400 errors.

Summaries aggregate **all raw observations starting in the window**, even when service/status qualify the trace. They return distinct span-ID counts, observed services, earliest start/latest end, latest start and any observed error. An error span does not prove the entire root request failed. Conflicting raw versions can influence summaries; detail independently selects canonical records over the entire trace, including spans outside the search window. `summary_scope` makes this distinction explicit.

Results sort by latest observed start descending, then trace ID ascending. Cursors hold the absolute window, filters, page size and last ordering tuple. Omitted parameters inherit cursor values; explicit ones must match. Cursors are versioned unpadded URL-safe base64, not authentication tokens. They are not snapshots: late spans may alter page membership/position, so concurrent ingestion can cause repeats or omissions across pages.

Detail returns `root_spans`, `orphan_spans`, and `disconnected_spans` (cycle components), with nested `children`. Parent IDs always remain the original observations. Exact replays collapse at read time; conflicting span identities prefer earliest ingestion time and then lexicographically smallest normalized payload. Diagnostics expose replay counts, conflicting IDs/variant counts, missing parents, root count, cycles and timing warnings. JSON object key order/whitespace and ingestion metadata do not create false conflicts; array order/default-presence distinctions remain significant. Raw rows are never deleted or rewritten.

Cycles are reported as sorted member-ID sets. For a finite forest, only the smallest cycle member's parent edge is detached in the returned representation, with `detached_parent_edge=true`; its original `parent_span_id` is retained. Orphans are direct references to missing parents and retain their descendants. `completeness` is always `unknown`; `has_detected_gaps=false` only means no missing parent reference was found. Links remain stored link metadata and never become parent edges.

Raw times/durations, relative offsets from the earliest canonical span and the canonical observed envelope are preserved. Warnings identify child timing outside parent bounds or inconsistent durations; no clock correction, summed-duration request estimate, exclusive time or critical path is computed. Detail JSON retains typed Protobuf JSON attributes/events/links and full known metadata from Session 2. Nanosecond offsets/durations require a client capable of handling 64-bit integers if exact large values matter.

Default detail cap is 1000 **physical rows before deduplication**, configurable with `--max-trace-rows` up to 2000. The repository reads one sentinel row beyond the cap and returns 413 instead of a partial tree. Payload/response caps are 8MiB/16MiB. Search defaults to 20 summaries and accepts up to 100 (`--max-page-size`, hard maximum 200). Query handlers allow four active reads. HTTP query timeout defaults to 2s (`--query-timeout`, maximum 4s); repository operations cap at 3s and ClickHouse execution at 2s, with additional 1M-row / 128MiB-read / 256MiB-memory / 32MiB-result bounds. Database resource-limit failures are errors, never silently truncated pages.

Errors are structured JSON: 400 invalid input, 404 unknown trace, 413 detail/response too large, 503 unavailable/busy/resource-limited backend, 504 expired query deadline, 408 canceled request, 500 unsafe stored data. Driver-internal connection/dial timeouts count as 503 when the request deadline has not expired. The API shares existing loopback port 8082; remote unauthenticated deployment remains out of scope.

Run verification and inspect replay/anomaly diagnostics:

```powershell
go test ./...
go vet ./...
go build ./...
.\scripts\session03-smoke.ps1
# Or avoid the controlled database outage:
.\scripts\session03-smoke.ps1 -SkipDatabaseFailure
# Reuse the Session 2 Linux compiler/race-cache image when present:
docker run --rm --mount "type=bind,source=$PWD\internal,target=/src/internal,readonly" --mount "type=bind,source=$PWD\cmd,target=/src/cmd,readonly" --mount "type=bind,source=$PWD\tests,target=/src/tests,readonly" traceforge-session02-checks sh -c 'go vet ./... && go test -race ./...'
# Portable alternative if that local image is absent:
docker buildx build --target verify --output=type=cacheonly .
```

The smoke script expects default ports/limits, existing demo processes and the historical Session 2 trace for its historical check. It prints a new real trace ID replayed from five rows to ten while detail retains five logical nodes and reports five exact duplicates. It inserts uniquely scoped orphan/cycle/conflict fixtures and a 1001-row limit fixture without modifying original demonstration rows. Nanosecond-window fixtures test inclusive/exclusive boundaries and tied keyset positions. It temporarily stops ClickHouse unless skipped, checks query 503 and ingestion Unavailable, restores in `finally`, and verifies historical query/fresh ingestion after recovery. Existing `.env` is only read for credentials, never rewritten. Exact results and example IDs are in the Session 3 journal.

## Session 4 batching, diagnostics and load tests

Prometheus client_golang v1.25.0 supplies maintained internal instrumentation. No user OTLP metrics pipeline was added. Inspect real counters, bounded gauges/histograms, runtime/process data and a bounded diagnostic snapshot:

```powershell
Invoke-WebRequest http://localhost:8082/metrics | Select-Object -ExpandProperty Content
Invoke-RestMethod http://localhost:8082/debug/ingestion | ConvertTo-Json -Depth 5
```

Counters include requests/spans received, invalid spans, reported successful physical rows, failed insert attempts and admission rejections by fixed reason. Histograms include batch spans, queue/flush/insert/export latency. No trace IDs, service names or customer strings are metric labels. Snapshot history contains at most 2048 recent batches; `metric_samples_complete` in load reports indicates whether exact insert quantiles cover the entire measured run. `/healthz` stays independent of queue fullness; `/readyz` still performs a current DB check.

Production defaults and units:

| Flag | Default | Unit/meaning |
| --- | --- | --- |
| `--queue-requests` | 64 | Reserved normalizing + queued channel positions |
| `--max-inflight` | 32 | Active Export handlers |
| `--pending-requests` | 128 | All owned jobs, including active batches/canceled callers |
| `--buffer-spans` | 16384 | Input span reservations across owned jobs |
| `--buffer-bytes` | 134217728 | Conservative payload reservation units, not RSS |
| `--batch-target` | 1000 | Span target; whole requests stay intact |
| `--batch-wait` | 100ms | Oldest queued job's batching deadline |
| `--workers` | 1 | Fixed concurrent persistence workers, maximum 4 |
| `--admission-wait` | 50ms | Combined short admission wait |
| `--write-timeout` | 3s | Shared write deadline |
| `--drain-timeout` | 10s | Shutdown drain opportunity |

Reservations happen before row normalization. Repeated resource/scope metadata is charged per span with a conservative 64x protobuf allowance plus overhead, followed by a retained-row check. This overestimates payload, does not measure exact heap, and excludes some transport/runtime/GC overhead. Eight accepted gRPC connections with eight streams each, plus 4MiB per-message and application handler bounds, constrain decoded work separately. Arbitrarily increasing queue slots does not increase sustained persistence capacity.

Workers flush at size, time or shutdown. Already-ready work can still be combined after a deadline, without waiting for more. A request larger than the target is written alone within existing request limits; it is not split or truncated. A canceled queued job may be skipped, but a job already in a shared write can still persist after its caller leaves. Workers use lifecycle contexts so one caller cannot cancel other callers' work. Every job's one-slot completion channel prevents abandoned callers blocking a worker. See [failure-model.md](docs/failure-model.md) for loss/ambiguity cases.

Build and run a small load directly through the normal receiver. Load the existing password into this terminal for independent SQL checks; do not rewrite `.env`:

```powershell
foreach ($line in Get-Content .env) {
    if ($line -match '^CLICKHOUSE_PASSWORD=(.+)$') { $env:CLICKHOUSE_PASSWORD = $Matches[1] }
}
go build -o bin/otel-loadgen.exe ./cmd/otel-loadgen
.\bin\otel-loadgen.exe --requests 40 --spans-per-request 20 --concurrency 8 --connections 4 --seed 42 --output artifacts/load-small.json
```

`--spans` overrides request count; `--duration` plus positive `--rate` derives a bounded span count. `--rate` is offered spans/sec (zero means unpaced), `--services` controls synthetic resource groups, `--run-id`/`--base-time` can reproduce a dataset, and each default run generates a distinct prefix. Workloads are capped at 200000 spans/10000 requests/64 clients; individual exports remain at most 1000 spans. The command has a 45s load ceiling and performs no automatic retries. It records actual attempted/generated/acknowledged/retryable/permanent/unknown outcomes, nearest-rank latencies and independently queried physical/unique rows. Unknown outcomes overlap retryable errors; they are not disjoint totals.

Check `storage_verified` before using physical row fields. If DB accounting is unavailable, the report is still saved with that fact, stdout says unverified and the command exits nonzero. `--verify-storage=false` intentionally skips SQL accounting. If the receiver stops, client outcomes can remain useful while final diagnostics are unavailable. Memory/queue samples run every 100ms and can miss peaks; reservation high-water counters are separate. Benchmark and SQL accounting windows are recorded explicitly.

Compare batch targets on an optional receiver at 4320/8083, with 2 CPUs, 256MiB memory and one worker. The main Collector/receiver/query ports stay unchanged:

```powershell
.\scripts\benchmark-session04.ps1 -Requests 600 -Repetitions 2 -Rate 10000
```

The script selects the controlled per-request baseline and targets 100/500/1000/5000 through process-local `TRACEFORGE_BENCH_MODE`, `TRACEFORGE_BENCH_TARGET` and `TRACEFORGE_BENCH_WAIT` variables. To start one configuration manually:

```powershell
$env:TRACEFORGE_BENCH_TARGET = '500'
$env:TRACEFORGE_BENCH_WAIT = '50ms'
docker compose -f compose.yaml -f compose.benchmark.yaml up -d --wait traceforge-benchmark
.\bin\otel-loadgen.exe --endpoint localhost:4320 --http-endpoint http://localhost:8083 --requests 300 --spans-per-request 50 --concurrency 32 --rate 10000 --output artifacts/load-500.json
docker compose -f compose.yaml -f compose.benchmark.yaml stop traceforge-benchmark
Remove-Item Env:TRACEFORGE_BENCH_TARGET,Env:TRACEFORGE_BENCH_WAIT
```

`--ingestion-mode=direct` implements the one-write-per-request control using the same queue/budgets/worker count (target 1, wait 0). It isolates coalescing rather than reproducing the old binary's exact four-writer concurrency. [Measured results](docs/benchmarks/session-04.md) and [all ten CSV runs](docs/benchmarks/session-04.csv) are versioned. These short laptop runs do not establish production or sustainable-capacity guarantees.

Failure/recovery and shutdown verification:

```powershell
.\scripts\session04-smoke.ps1
# Avoid deliberate DB outage, or isolated SIGTERM/SIGKILL tests if needed:
.\scripts\session04-smoke.ps1 -SkipDatabaseFailure -SkipShutdown
# A separate two-request outage report; restores ClickHouse in finally:
.\scripts\outage-load.ps1
```

The full smoke runs prior trace APIs/storage tests, stops/restores ClickHouse, then tests graceful SIGTERM and forced SIGKILL only on the isolated receiver. It restores/stops that receiver in finally. Forced-kill fixtures demonstrate lost queued memory without successful acknowledgments. Tests use fresh IDs; original demo rows stay intact. For manual DB recovery use `docker compose stop clickhouse`, then `docker compose start clickhouse` and poll `/readyz`; a gateway HTTP 200 never proves telemetry persistence.

Raw report prefixes permit direct SQL inspection as shown in the benchmark report. Optional cleanup removes only the selected prefix with synthetic service/tag markers:

```powershell
.\scripts\cleanup-load.ps1 -ReportPath artifacts/load-small.json
Remove-Item Env:CLICKHOUSE_PASSWORD
```

## Stop and troubleshoot

Press Ctrl+C in each demo terminal; demo HTTP draining and tracer flush each have five-second bounds. The receiver uses the separate Session 4 drain lifecycle above. Forced termination bypasses graceful cleanup.

```powershell
docker compose down
```

This preserves named volumes. Removing volumes deletes database data. Collector logs do not provide telemetry database durability.

- Go missing: install Go or use the portable PATH command in each terminal.
- Docker pipe errors: start Docker Desktop (`docker desktop start`), then retry `docker info`.
- Port occupied: inspect `Get-NetTCPConnection -LocalPort 8080,8081,8082,4317,4319,8123,5432`; resolve the conflict or change demo ports and payment URL together.
- Gateway 502: check payment is listening and the payment URL matches it.
- No spans: check endpoint/protocol, service stderr and Collector logs; allow a batch interval. HTTP success alone does not establish export.
- Debug spans but no rows: check `docker compose logs traceforge-server otel-collector`, `/readyz`, and whether Collector was restarted after its config change. Old permanent-error exports are not recovered automatically.
- Database unhealthy: inspect `docker compose logs clickhouse postgres`; verify credentials match existing volumes.
- Split trace: retain `r.Context()` and use `otelhttp.NewTransport`; a plain transport does not inject context. A test demonstrates this failure without changing the working implementation.

See `ARCHITECTURE.md`, ADR 0004, the failure model and `docs/engineering-journal/session-04.md` for current behavior and evidence. Prior journals remain historical. A new local Session 4 teaching note is Git-ignored; earlier notes are preserved. Session 5's UI has not begun.
