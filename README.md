# TraceForge — Session 1

A real local distributed tracing demo: gateway → payment over HTTP; both export OTLP/gRPC to the Collector's detailed debug exporter. ClickHouse and PostgreSQL are provisioned without application tables. **No spans are persisted in either database.**

## Prerequisites and versions

PowerShell 7 (`pwsh`), Docker Desktop with Linux containers, Go 1.27.4. Development tooling: Docker 29.9.2, Compose 5.6.1. Pinned dependencies: OTel SDK/exporter 1.47.0, otelhttp 0.72.0; see `go.mod` and `go.sum`. Images: Collector 0.165.0, ClickHouse 26.8.36.3-alpine, PostgreSQL 18.6.

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

Both should return 1. PostgreSQL's health check establishes readiness; the SQL command additionally verifies authenticated connectivity. No application schema is created.

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
docker compose config --quiet
.\scripts\smoke.ps1
```

The smoke script expects both services already running. It checks normal, 500ms-delay and failure responses, parses actual exported traces, verifies five operations and their parent links, and runs authenticated SQL checks. Its parser targets Collector 0.165.0's debug format and is independent of export ordering. It preserves containers and volumes.

Component tests use separate in-memory tracer providers and real HTTP servers. They check wire traceparent, shared identity, distinct IDs, kinds, hierarchy, errors, bounds, incoming context, deliberately lost propagation, invalid initialization, baggage and graceful draining. Linux CI also runs the race detector. Windows race testing requires a C compiler.

## Stop and troubleshoot

Press Ctrl+C in each service terminal. Each server drains for up to five seconds, then closes remaining connections; tracing then receives five seconds to flush. Forced termination bypasses cleanup.

```powershell
docker compose down
```

This preserves named volumes. Removing volumes deletes database data. Collector logs do not provide telemetry database durability.

- Go missing: install Go or use the portable PATH command in each terminal.
- Docker pipe errors: start Docker Desktop (`docker desktop start`), then retry `docker info`.
- Port occupied: inspect `Get-NetTCPConnection -LocalPort 8080,8081,4317,8123,5432`; resolve the conflict or change demo ports and payment URL together.
- Gateway 502: check payment is listening and the payment URL matches it.
- No spans: check endpoint/protocol, service stderr and Collector logs; allow a batch interval. HTTP success alone does not establish export.
- Database unhealthy: inspect `docker compose logs clickhouse postgres`; verify credentials match existing volumes.
- Split trace: retain `r.Context()` and use `otelhttp.NewTransport`; a plain transport does not inject context. A test demonstrates this failure without changing the working implementation.

See `ARCHITECTURE.md`, the ADR, and `docs/engineering-journal/session-01.md` for design and actual results. Local teaching notes are Git-ignored in `learning-notes/`. Session 2 introduces TraceForge's own receiver and ClickHouse persistence; it has not been implemented.
