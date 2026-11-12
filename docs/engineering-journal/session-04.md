# Session 04 — Bounded ingestion, batching and measured recovery

Date: 2026-11-12 (Asia/Calcutta). Starting checkpoint: `e91f5a9`.

## Implemented architecture and files

The official OTLP receiver now reserves bounded resources before normalization, queues owned jobs and combines whole requests in fixed persistence workers. The unchanged ClickHouse writer still uses synchronous prepared inserts. Each original Export waits for its job's database result; per-request rejection counts survive coalescing. Query/reconstruction APIs, schema, redaction, Collector endpoints and actual `.env` are preserved.

Added `internal/ingest/config.go`, `batcher.go`, `metrics.go`, `batcher_test.go`; `internal/loadgen/load.go` and `load_test.go`; `cmd/otel-loadgen/main.go`; `compose.benchmark.yaml`; `tests/integration/batching_test.go`; scripts `benchmark-session04.ps1`, `session04-smoke.ps1`, `outage-load.ps1`, `cleanup-load.ps1`; ADR 0004, failure model, this journal and benchmark Markdown/CSV. Modified receiver wiring/shutdown, ingestion server/regression tests, base Compose stop grace, Go module pins, README, architecture and resume evidence. A new ignored Session 4 teaching note was created; prior notes were preserved and no learning notes are staged.

Prometheus client_golang 1.25.0 was checked against the official module proxy/upstream documentation and pinned with its normal transitive dependencies. Existing x/net and errdetails packages became direct imports without version upgrades. Internal metrics use private registries and bounded labels; user OTLP metric ingestion remains unimplemented.

## Old versus new behavior

Session 3 performed one insert per export and allowed four concurrent export operations. The default now allows 32 handlers, 64 reserved queued/normalizing positions, 128 owned jobs, 16384 reserved input spans and 128MiB conservative payload units. One worker targets 1000 spans or 100ms from the oldest enqueue; hard configuration limits and prior request/field validation remain. Queue/job/span/byte units are separate and budgets remain owned through active writes, including canceled callers.

Admission waits at most 50ms across handler and payload reservation. Temporary refusal is Unavailable with RetryInfo; an individual request that cannot fit configured hard budgets is InvalidArgument. Worker writes have a separate lifecycle/3s deadline, not a canceled caller's context. No server-side retry or durable spool is introduced. Empty/all-invalid requests make no write. Shared insertion success/failure is routed through each ticket's buffered completion channel, and partial success is only emitted after its valid rows confirm.

Shutdown stops admissions, gives jobs a configurable 10s final-flush/drain opportunity, fails unresolved outcomes and cancels the worker lifecycle on timeout. It closes the pool and bounds worker/listener shutdown; Compose allows 20s. SIGKILL can still lose memory. See `docs/failure-model.md` for the precise limitations.

## Commands and environment checks

Reviewed Git/AGENTS, README, architecture, ADRs 0002/0003, Session 3 journal, actual receiver/writer/shutdown/tests, schema and pinned Collector config. Go 1.27.4 Windows toolchain remains at the existing portable path. Docker Desktop's engine had stopped while work was paused; initial Compose rebuild failed with a missing pipe. `docker desktop start` restored Docker 29.9.2 / Compose 5.6.1. Named volumes/historical data survived. Missing demo listeners were restarted as hidden processes from the existing shop-demo command, with PIDs/output in ignored artifacts.

```powershell
go get github.com/prometheus/client_golang@v1.25.0
gofmt -w cmd internal tests
go mod tidy
go vet ./...
go test ./... -timeout 60s
go test -json ./...
go build ./...
go build -o bin/otel-loadgen.exe ./cmd/otel-loadgen
docker compose config --quiet
docker compose up -d --build --wait --wait-timeout 180
docker buildx build --target verify --output=type=cacheonly .
.\scripts\benchmark-session04.ps1 -Requests 600 -Repetitions 2 -Rate 10000
.\scripts\session04-smoke.ps1
.\scripts\session04-smoke.ps1 -SkipDatabaseFailure
.\scripts\outage-load.ps1
.\scripts\cleanup-load.ps1 -ReportPath artifacts/session04-first-load.json
```

Also inspected actual CPU/RAM/Docker resource limits, queried ClickHouse version 26.8.36.3, fetched `/metrics` and `/debug/ingestion`, and used authenticated SQL for physical/unique row counts. Scripts load the existing password only into their process environment, do not print/rewrite it and restore saved values in finally. No remote push or migration/index rewrite occurred.

## Actual test results

Windows formatting, tidy, vet, build, ordinary tests and Compose validation passed. **47 top-level tests + 72 subtests = 119 passing cases** across seven tested packages; the three commands have no direct test files. This includes all prior behavior plus deterministic batching/backpressure/cancellation/shutdown and synthetic generator validation/accounting tests.

Linux Docker `go vet ./...` and `go test -race ./...` passed for demo, ingest, loadgen, queryapi, storage, telemetry and traces against the final Go source. Native Windows race support remains unavailable as previously recorded; no Windows/remote-CI pass is claimed.

Controllable writers and an injectable clock verify coalescing, size/time triggers, oversized jobs staying whole, expired-backlog coalescing, bounded queue/span/byte/handler admission, shared failure/ambiguous outcomes, canceled queued/active callers, preserved partial-success counts, final shutdown flush, timeout failure and worker exit. No early acknowledgment is allowed while the fake database is held. Completion budgets return to zero. Generator tests prove valid reproducible IDs/timestamps/types and separate failed/unknown from acknowledged spans.

Prior live storage/query/graph/pagination/replay/limit/outage/recovery scenarios passed under the new pipeline. Real isolated SIGTERM and SIGKILL tests passed separately. The complete main workflow covers the prior 10 distinct top-level integration scenarios plus 6 scenario subtests, with historical recovery repeated, and adds 2 dedicated shutdown scenarios. Additional CLI load/outage/cleanup probes are recorded separately rather than counted as Go unit tests.

## Real batching and benchmark evidence

The first live probe offered 800 spans, acknowledged/stored 800, independently found 800 distinct trace/span identities and observed mean batch size 160. Its report is `artifacts/session04-first-load.json`. That probe was later deliberately cleaned to exercise scoped cleanup; it is not part of the final matrix.

The final matrix used a separate receiver on 4320/8083, one worker, 2 CPUs/256MiB, 32 clients/four shared gRPC connections, 50 spans/request, 600 requests/repetition, seed 42 and configured 10000 offered spans/sec. Each of five configurations had a 1000-span warm-up and two measured runs: **300000 measured spans**, all independently acknowledged/stored/unique with zero final-run rejections/duplicates. The controlled per-request baseline shares queue/admission/worker settings; it isolates coalescing and is not a measurement of the old binary's four-writer topology.

Actual ranges across both repetitions:

| Configuration | Confirmed rows/sec | Successful-ack p95 | Mean batch spans |
| --- | ---: | ---: | ---: |
| Per-request control | 7206–8012 | 242.12–375.59ms | 50 |
| Target 100 / 20ms | 9952–9973 | 62.74–92.75ms | 100 |
| Target 500 / 50ms | 9809–9971 | 66.53–73.25ms | 491.80–500 |
| Target 1000 / 100ms | 9641–9687 | 111.48–120.89ms | 967.74 |
| Target 5000 / 100ms | 9691–9729 | 127.59–132.30ms | 1034.48 |

All per-run p50/p95/p99, insert latency, rate, queue/memory, elapsed-time and part snapshots are in `docs/benchmarks/session-04.csv`; environment/method/limitations are in its Markdown report. Peak sampled Go heap was about 12.30MiB, RSS 34.75MiB, sampled queue 31/64 and goroutines 57. Exact reservation high-water stayed at or below 68.24MiB of the 128MiB budget. All runs settled with zero pending/reserved resources. Memory samples are not exact peaks and payload accounting is not RSS.

Independent example SQL:

```sql
SELECT count(), uniqExact(tuple(trace_id, span_id))
FROM traceforge.spans
WHERE startsWith(trace_id, '97494d8c2a8a686d');
-- 30000, 30000
```

The small/medium configurations served this offered workload with lower latency than larger targets. Larger targets did not prove higher sustainable capacity. No production sizing or arbitrary scalability claim follows from these 3–4.2s runs. Full caveats and the excluded pilot are in the benchmark report.

## Failure, recovery and actual stored identities

- Full Session 4 live regression kept historical trace `656f80607b005aa65927df202991a837` at five logical spans (three gateway, two payment) with unknown completeness. Replay trace `43108a439de0b6100c2ba6e4b96a34ce` stored ten physical rows and returned five logical spans/five exact duplicates. Graph fixture `5fc94c32dc6250d2f680d17cb369de28` retained orphan/cycle/conflict diagnostics. Pagination and 413 physical-row bounds remained correct.
- During DB stop, ingestion returned Unavailable, query endpoints returned 503, liveness stayed 200 and readiness returned 503. No success acknowledgment was inferred from application HTTP behavior. After restart, fresh trace `69ebfb6a8cb06317100a4e1c2fec1ae3` confirmed one stored row in the full outage workflow; a later non-outage regression also passed.
- Graceful isolated SIGTERM observed four pending jobs before stop. All four RPCs succeeded and independent queries found one row each: `34c2210a63e1233562094e24c3bc2150`, `3ac10c0a0db54f70ab2c2e96278be966`, `a81f9c6723e63ddfc9e9fec50a62885b`, `8e5d4fff9fab961160be7a17520a15c3`.
- Separate isolated SIGKILL observed four pending jobs. None received success and all four had zero rows: `c24b7d9d73fc153daee923018af7f5ff`, `fd5501d4154daae35b53f467612462aa`, `b2c64041bf5bc0058c5fe883c6a3c1fd`, `65fd238a822bb8e54021f6e771039a57`. This tests pre-write memory loss, not every possible active-write kill outcome.
- Outage generator prefix `a22b8fcc694de41e`: two attempted requests/20 valid spans, zero acknowledged spans, two retryable failures and two unknown outcomes. It saved JSON despite unavailable SQL accounting, marked storage unverified and exited 1 as expected. After DB recovery, an independent prefix count was **0**. Unknown outcomes overlap retryable failures; they are not additive categories.
- Cleanup removed only the initial probe's synthetic marker/prefix rows and returned zero remaining rows. Historical trace count remained **5**. Final benchmark prefixes remain stored for independent inspection.

## Problems found and corrected

The engine was stopped initially, causing a real failed Compose command; it was restored. An old cancellation/concurrency test assumed a canceled caller immediately freed the write slot. Updated it to reflect shared lifecycle ownership, release its controlled writer and then test concurrency independently; subsequent tests/races passed.

A 300-request pilot overlapped Linux compilation and exposed an expired-backlog flaw: the worker flushed an already-old first job alone rather than combining ready queued jobs. Added a nonblocking ready-drain path and a deterministic regression. The final 600-request matrix ran after compilation with the corrected code. Pilot reports, including three retryable rejections in one run, are retained and not mixed with final comparisons.

An auxiliary SQL shell-quoting command failed after the scoped cleanup itself succeeded. Repeated verification via standard-input SQL succeeded and confirmed the historical five rows. A complex inline outage command was rejected before execution by tool policy; the reviewable repository outage script completed the same bounded experiment successfully. No blocked/skipped check is represented as a pass.

## Remaining limitations and Session 5 readiness

In-memory queues/finite upstream retries can lose data. Canceled/failed/timeout responses may coexist with physical persistence and later duplicates. Read-time deduplication does not fix delivery semantics. The byte reservation is conservative payload accounting, not a process heap cap. Bounded diagnostics keep at most 2048 recent samples; reports flag incomplete coverage. Shutdown relies on context-aware writer cooperation and ultimately process termination for a truly stuck operation.

The shared database/background merges, fixed experiment order and short closed-loop workload limit performance conclusions. No long soak, cold-cache profile, offered-rate sweep to sustainable capacity, distributed-scale benchmark or remote CI execution was performed. Internal metrics are not user OTLP metric ingestion. Local plaintext/no production RBAC and existing redaction limitations remain.

Implementation is ready for Session 5's trace explorer/waterfall work. No frontend, service graph, alerting, Kafka/Redis/WAL, new span schema, AgentOps or RAG feature was started. Intended commit: `feat: add bounded ingestion batching and measured backpressure`.
