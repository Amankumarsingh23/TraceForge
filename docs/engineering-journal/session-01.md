# Session 01 — Foundation and first distributed trace

Date: 2026-11-11 (Asia/Calcutta).

## Scope and implementation

Implemented a standard Go OTel initialization package, one CLI with gateway/payment service modes, instrumented HTTP client/server boundaries, two manual internal spans, bounded deterministic delay/failure controls, graceful HTTP shutdown, local infrastructure, component tests, smoke script and Linux CI configuration. No custom ingestion, storage schema, persistence, query API or AgentOps functionality exists.

Files: `cmd/shop-demo/main.go`; `internal/demo/http.go` and tests; `internal/telemetry/tracing.go` and tests; `go.mod`/`go.sum`; `compose.yaml`; `otel-collector.yaml`; `.env.example`; `.gitignore`; `.github/workflows/ci.yaml`; `scripts/smoke.ps1`; README, architecture, ADR and resume evidence. `AGENTS.md` preserves the session agreement. Local teaching notes remain ignored.

## Environment and resolved blockers

- Go was absent from PATH and the standard installation directory. Downloaded official Go 1.27.4 Windows amd64 archive into `%LOCALAPPDATA%/TraceForge-tools`, checked SHA256 `178ab98950ac69ab25069307dc6ade79f3816e414cce95b4da166a115cde1e7e` against the official release feed, and extracted it locally. PATH changes apply only to invoked shells.
- Docker client/Compose were installed (29.9.2/5.6.1), but engine access initially failed. `docker desktop start` restored access to the Linux engine.
- An initially guessed ClickHouse tag `26.10.1.1181` did not exist; queried official image tags and selected the available explicit `26.8.36.3-alpine`. No Compose run used the invalid tag.
- PostgreSQL initially started at 18.1; after checking current official tags, pinned 18.6 and recreated its container against the same session-created volume. Final smoke checks use 18.6.
- Git author identity was initially missing. User supplied repository-local identity Aman Kumar Singh / amansingh23@iitk.ac.in for the session commit.

## Commands executed

Environment/version discovery: `go version` (initially unavailable), `docker --version`, `docker compose version`, `docker info` (initially failed), `docker desktop start`, official Go release feed/module proxy/Collector release/official image-tag queries, Docker manifest checks. Portable archive download, checksum validation and extraction followed.

Verification/build commands:

```powershell
gofmt -w cmd internal
go mod tidy
go vet ./...
go test -v ./...
go test -json ./...
go test -race ./...
go build -o bin/shop-demo.exe ./cmd/shop-demo
docker compose config --quiet
docker compose up -d
docker compose up -d --wait --wait-timeout 180
docker compose ps
.\scripts\smoke.ps1
docker compose logs --no-color --no-log-prefix --tail=45 otel-collector
```

Built executable launched as two hidden Windows processes with `--service payment` / `--service gateway`; output files and PIDs reside in ignored `artifacts/`. The smoke script ran actual HTTP calls and authenticated SQL commands documented in README.

## Results

`gofmt`, `go mod tidy`, `go vet`, ordinary Go tests, build and Compose validation passed. Tests: 9 top-level tests plus 3 distributed-trace subtests, all passed; 2 tested packages, CLI has no test files. Component tests verify real HTTP propagation and all five span relationships, error statuses/events, deterministic delay, invalid controls/configuration, incoming context, baggage, a deliberate split trace and active-request draining.

Windows `go test -race ./...` failed with `-race requires cgo; enable cgo by setting CGO_ENABLED=1`. No C compiler was found. Race-enabled execution is configured in Linux CI but has not been observed running remotely; do not claim a race-detector pass.

Docker-backed smoke passed all three scenarios and both SQL checks. Final containers: ClickHouse healthy, PostgreSQL healthy, Collector running. Collector has no health endpoint in this configuration; successful OTLP receipt is independently proven by parsed debug output.

First recorded real traces:

| Scenario | HTTP | Trace ID |
| --- | --- | --- |
| Normal | 200 | `b0cb09be331b6f4d980c2e1c7a4a8f4d` |
| 500ms payment delay | 200 | `decb8c33c5daa7b75880b5cf0fa3fc81` |
| Payment failure | 502 at gateway, 503 at payment | `8e333eccd23ca2cfd03880776a29c835` |

Normal trace parent evidence from Collector:

| Operation | Span ID | Parent ID |
| --- | --- | --- |
| Gateway server | `569087496e53571d` | none |
| Validation | `fe7f5cef76cfd0bf` | `569087496e53571d` |
| Gateway HTTP client (`HTTP GET`) | `28a01915f339a09e` | `569087496e53571d` |
| Payment server | `142c73c65eaaf617` | `28a01915f339a09e` |
| Payment processing | `15fb8eae57017e3b` | `142c73c65eaaf617` |

Collector output also contains HTTP status 503 on the failed client span, Error on the gateway server span, and a fixed `payment call failed` exception event. Both authenticated database `SELECT 1` calls returned 1. Databases still have no telemetry/application metadata tables.

Observed smoke wall times included substantial client/environment overhead and are not benchmarks. The component delay test verifies payment span duration is at least its requested delay; smoke verifies at least 500ms elapsed for the delayed response. No throughput, p95, compression or capacity numbers are claimed.

## Limitations and next session

Local plaintext, synthetic-only traffic; no production auth/redaction/tenant isolation claim. In-memory batch processing and debug output are not durable storage. Exporter initialization is asynchronous, and unavailable Collector errors surface on stderr. Force-killing a process can lose queued spans. Windows race checks are blocked by missing cgo/compiler support. CI is authored but remote execution is unverified.

Session 2: own OTLP/gRPC receiver, validation, ClickHouse span schema and persistence with explicit acceptance/durability semantics. Stop at this session boundary.

Intended commit message: `feat: add Session 1 distributed tracing foundation`.
