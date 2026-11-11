# TraceForge — Session 2

A real local tracing backend: gateway → payment → OpenTelemetry Collector → Go OTLP/gRPC receiver → ClickHouse. The Collector also keeps its debug exporter for comparison. PostgreSQL remains provisioned without an application schema. There is no trace query REST API, UI, logs/metrics ingestion or AgentOps evaluation yet.

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

## Start or upgrade the Session 2 backend

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

The official `TraceServiceServer.Export` traverses every resource and scope. An empty request succeeds without touching storage. Valid rows from a request are synchronously inserted in one prepared batch; only a successful `Send` permits acknowledgment. Full success leaves `partial_success` unset. Invalid spans are counted exactly and the valid subset is inserted before returning partial success; clients must not retry a partial-success response. If all spans are invalid, none are inserted and all are reported rejected.

Malformed envelopes/excessive span count return `InvalidArgument`. The gRPC 4MiB decompressed message cap returns `ResourceExhausted` for oversized wire requests. Temporary insertion failure returns `Unavailable`; canceled/deadline contexts retain their respective status. Concurrency saturation returns `Unavailable` immediately rather than creating an application queue. Error descriptions do not include database passwords or raw telemetry.

Limits: 1000 spans/resource groups/total scopes per export, 4 active Export operations, 5s processing deadline, 64KiB per span, 16KiB resource/scope metadata, 128 events/links, and bounded nested attributes (details in `docs/normalization.md`). Startup requires database readiness; shutdown allows 6s for gRPC draining and then forces cancellation, followed by bounded HTTP health shutdown and connection cleanup.

This is **not exactly-once delivery**, replicated durability or guaranteed atomic multi-partition insertion. `async_insert=0` waits for ClickHouse's normal synchronous insert confirmation. `insert_deduplicate=0` makes replay behavior explicit. A response lost after insertion or an ambiguous write failure can lead to duplicates on retry; MergeTree ordering enforces no uniqueness. No WAL, durable spool or deduplication is implemented.

Collector uses gzip OTLP/gRPC, a bounded in-memory queue of 100 requests with two consumers, and retries with 1–5s backoff for up to 60s. It can lose queued spans on crashes, overflow, expired retry windows or permanent errors. Debug output is a separate branch and can show a span that never persisted. Known secret-bearing attributes are redacted at the persistence boundary, but upstream Collector debug logs are unsanitized; only synthetic traffic is appropriate. See `docs/normalization.md` for supported fields and explicit redaction limits.

## Stop and troubleshoot

Press Ctrl+C in each service terminal. Each server drains for up to five seconds, then closes remaining connections; tracing then receives five seconds to flush. Forced termination bypasses cleanup.

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

See `ARCHITECTURE.md`, ADR 0002 and `docs/engineering-journal/session-02.md` for the current design and actual results. The Session 1 journal remains historical evidence. Existing Git-ignored learning notes were left untouched per the Session 2 instructions. Session 3's reconstruction, deduplication strategy and query APIs remain planned.
