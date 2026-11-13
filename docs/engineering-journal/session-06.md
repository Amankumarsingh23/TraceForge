# Session 06 — Structured logs, bounded OTLP persistence and exact correlation

Implemented 2026-11-12–13; final verification 2026-11-13 (Asia/Calcutta). Starting commit: `3e57423`.

## Result and architecture

Real gateway/payment `slog` records now travel through otelslog, an explicitly owned OTel LoggerProvider/batch exporter, the existing Collector, official LogsService and a separate bounded log writer into ClickHouse. Go exposes bounded search/exact correlation APIs. Next.js adds `/logs` and all-trace/selected-span log panels beside the existing waterfall/inspector. No production fixture fallback, metrics ingestion, LLM/RAG feature, schema overhaul or remote push.

Read HEAD/status/AGENTS, README/architecture, Session 4/5 journals, receiver/normalizer/batcher/writer, actual query/frontend contracts and pinned Collector/OTel source. New log dependencies were selected from official module engine/require metadata and documentation: otelslog 0.21.0, SDK log 1.47.0 and OTLP log gRPC exporter 0.23.0. Existing OTel/driver/protocol versions were preserved. Service resources are shared between trace/log SDKs. Context-aware calls use the actual validation/payment span contexts; startup legitimately has no trace/span ID. Initialization, asynchronous export failures and bounded provider shutdown remain explicit.

Log and trace queues/resources are separate; database connections, CPU and transport are shared. The log engine adapts the proven Session 4 lifecycle without a generic rewrite of the trace engine. Shared typed sanitizers are exported through narrow wrappers; existing trace normalization behavior is unchanged. Main drains both signals concurrently before pool/gRPC/HTTP shutdown. Collector has a distinct bounded log exporter and no detailed raw log debug branch.

## Schema, identities, contracts and privacy

Repeatable migration 0002 creates `traceforge.logs`: generated random storage-row ID, optional string source-event UID, optional trace/span context, flags, severity, nullable source/observed DateTime64(9), effective time/source, body text/typed JSON, attributes/resource/scope/known record metadata, schema URLs and receiver-ingestion time. Monthly partitions and `(effective_time, log_id)` sorting favor bounded time scans, with trace-correlation scanning tradeoffs. No foreign key or deduplication. Repeated/replayed events remain physical rows with independent IDs. Generated IDs are not exactly-once source identities.

`GET /api/v1/logs` defaults to one hour; `GET /api/v1/traces/{trace_id}/logs` defaults to 24 hours without requiring an observed trace. Filters: bounded UTC `from`/`to`, service, numerical minimum severity, trace ID, optional span ID requiring trace ID, case-sensitive literal text, limit and opaque cursor. Ordering is effective time descending then log ID descending. Cursor parsing binds exact nanoseconds and all filters, rejects invalid/changed values and retains non-snapshot limitations. Queries are parameterized with deadlines, concurrency/read/response caps and sanitized structured errors.

Source time wins, then observed time, then an explicitly labeled receiver-ingestion fallback; absent original fields remain null. The API uses precise RFC3339Nano strings; typed protobuf JSON retains int64 strings/type tags/base64 bytes. Next relative arithmetic uses BigInt. Context may be standalone or trace-only; this implementation rejects malformed/all-zero present IDs and span-only records. Time proximity never establishes correlation. Same timestamps do not imply emission order: random row IDs only provide deterministic pagination ties.

Known sensitive keys are recursively sanitized on cloned log/resource/scope attributes and nested body structures. Known free-form assignment/auth/private-key patterns and URL credentials/sensitive query fields are removed before all duplicated stored JSON/text representations. Limits protect record count, bytes, strings, depth/items and wide traversal work stacks before recursive protobuf sizing/cloning. Unknown future wire fields are not exposed by protobuf JSON; nonsensitive-key binary content is opaque. This is bounded key/pattern redaction, not comprehensive DLP. ADR 0006 records exact limits, fields and limitations.

## Executed checks

| Check | Actual result |
| --- | --- |
| gofmt, go mod tidy, go vet, go build | Passed |
| Native ordinary Go tests | **163 passing cases: 68 top-level + 95 subtests**, eight tested packages; prior 119 cases preserved |
| Linux Docker vet and race tests | Passed against final Go source; `artifacts/session06/linux-race-final.log` |
| Docker rebuild, startup, repeatable migrations, quiet Compose validation | Passed |
| Live Session 6 log/privacy/replay/correlation/mixed-signal suite | Passed |
| Existing live trace storage/query/graph/pagination/replay/limits/demo regressions | Passed |
| ClickHouse stop: logs/traces/query refusal, restoration and fresh signal writes | Passed |
| Existing isolated real SIGTERM and SIGKILL trace tests with multi-signal server | Passed; `trace-shutdown-regression.log` |
| Frontend lint, type-check, production build | Passed |
| Frontend unit/component tests | **46 passed in seven files**; prior 28 preserved |
| Real Chromium, production `next start`, both controlled outage cases enabled | **19 passed in 38.4s**, none skipped in accepted run |
| Production npm audit | 0 vulnerabilities at final check; no dependency changes to frontend |

The 44 added Go cases cover all-group conversion, actual bridge OTLP IDs/resource identity, standalone/trace-only records, identifier/severity/time/depth/size rejection, timestamp fallbacks, typed int64/bytes/maps/arrays, redaction/immutability, independent event identities, exact partial-success counts, no early acknowledgment, overload/cancellation/shared writes/final drain/timeouts, query/cursor/error contracts and physical writer rows. A wide-array regression verifies rejection before an expanding validation work stack. Linux race verification ran after that fix. Native Windows race/remote CI are not claimed. JavaScript dependency `flatted` contains an unrelated Go package discovered by `./...`; it has no tests and is not tracked.

The new 18 frontend cases cover numerical severity ranges, precise offsets, correlation labels/paths, approved proxy validation/status, safe text, empty/loading/unavailable/fallback states, stale responses, span versus trace toggling and pagination reset. Real browsers verify failed-span logs and trace navigation, delayed/all-trace scope, search severity/standalone records, repeated/replayed rows, cursor/reset, trace-only logs, missing associated trace, mobile expansion and actual DB outage/recovery. All 12 existing trace browser scenarios still pass, including 1000 spans, anomalous forests and 400/404/413.

The existing development-only minimatch audit finding documented in Session 5 was not force-fixed or hidden; this session changes no frontend dependency versions. A clean production audit is not a proof that the system has no vulnerabilities.

## Real SDK/Collector demo evidence

Final refreshed demos from `artifacts/session06/demo.json`:

| Scenario | Actual trace ID | payment.process span ID | Trace logs / selected-span logs | HTTP / recorded payment interval |
| --- | --- | --- | --- | --- |
| Normal | `da8ef589b566621ff7fc0218331ce51c` | `5bdb4a95be16ba6e` | 5 / 2 | 200 / 0 ns |
| Delay 500ms | `3b1fc95d558b87c40e96b613d88eca70` | `07ba8f0bc793bb53` | 6 / 3 | 200 / 500768700 ns |
| Failed payment | `e03a4b2a8648f6ec0b18157d4996bc4b` | `66d9a071e4c1eae8` | 5 / 2 | 502 / 0 ns; span ERROR |

Payment logs are emitted inside `payment.process`: start/completion for success, an additional WARN `controlled payment delay` with synthetic delay metadata, or ERROR `payment failure` carrying `error_type=synthetic_provider_failure`. Trace-level rows additionally include gateway start/validation/completion or failure. Tests independently match every demo log's trace/span/service against the observed graph and specifically verify the validation log belongs to `checkout.validate`. These are individual recorded intervals/counts, not a throughput benchmark; a recorded zero interval is not a claim of zero CPU work.

Actual failed-span API excerpt (both source times happened to share the same clock tick):

```json
[
  {"log_id":"2e841c684994d54992af286afe8f4e78","trace_id":"e03a4b2a8648f6ec0b18157d4996bc4b","span_id":"66d9a071e4c1eae8","service_name":"payment","severity_number":9,"body_text":"payment started","effective_time":"2026-11-13T04:42:48.1787004Z"},
  {"log_id":"155497467ffac5bcbe7cdc59c3b26c0e","trace_id":"e03a4b2a8648f6ec0b18157d4996bc4b","span_id":"66d9a071e4c1eae8","service_name":"payment","severity_number":17,"body_text":"payment failure","effective_time":"2026-11-13T04:42:48.1787004Z"}
]
```

The full actual API body is saved locally in `artifacts/session06/final-api-error.json`. Legitimate startup records were independently queried with absent IDs. A wire-level SDK test inspects exported protobuf ID bytes, not console text; live demo evidence travels through the real Collector into ClickHouse.

## Independent SQL, replay, early arrival and redaction

Controlled fixture service `s06-87cc4ae6bf7f1c4c`, trace `9ec35b2ed516937271014df23f7c4a77`, span `b309b83033419d3c` includes three genuinely identical events, trace-only, standalone, missing-time and nested-secret/large-integer cases. One invalid identifier per export is rejected; seven valid records persist. Replaying the same export retains seven more physical rows and source UIDs without deduplication. Another trace `4cbf432992d1856f7e663b02af4354a5` deliberately reuses the same span ID but never enters the first trace's selected-span results.

Log APIs returned records before the associated trace existed (trace detail 404); later trace ingestion did not change log row counts. The fixture also demonstrates records referencing an unobserved span are not dropped/reattached. Seven cursor pages at limit 2 returned all 14 identities without repetition. Literal `%_?` matched no rows rather than acting as a pattern.

Independent authenticated SQL was run via container stdin without printing credentials. Saved output in `artifacts/session06/final-sql.txt`:

```json
{"physical_rows":14,"unique_row_ids":14,"trace_logs":10,"selected_span_logs":8,"standalone_rows":4,"ingestion_fallback_rows":2,"recognized_secret_matches":0}
{"exact_event_ns":"1794497442123456789"}
```

Counts filter this unique synthetic service. The secret check examines concatenated stored body/text/attributes/resource/record representations for the controlled secret marker, and API assertions separately check serialized returned data. The structured int64 `9223372036854775807` remained a decimal string and source nanoseconds survived actual storage/readback. These checks cover the recognized test secrets, not arbitrary confidential content.

## Mixed signals, failure and lifecycle evidence

Eight concurrent log exports plus eight trace exports completed with full persistence responses. Both independent queue/job/payload counters settled to zero; final direct snapshots also showed log pending/reserved=0 and trace pending/reserved=0. This is a bounded integration workload, not a sustained-capacity or fairness benchmark.

The full smoke workflow stopped ClickHouse, observed LogsService Unavailable/no response acknowledgment, log search/correlation 503, trace export Unavailable, trace queries 503, liveness 200 and readiness 503, then restored DB readiness and confirmed new trace/log writes. Browser tests separately stopped/restored the database for both old trace and new log views. They showed actual errors, no fake empty success, and recovered. Restoration uses finally. Recovery does not prove every outage-era SDK/Collector record survived.

Deterministic log tests verify queue/record/byte/handler bounds, timer/size/final flushing, per-ticket rejection counts, cancellation isolation and ambiguous failures. They hold a writer to prove memory admission is not success. Real trace SIGTERM/SIGKILL fixtures rechecked the main server's extended lifecycle. Pending-log forced-process-kill behavior was not independently exhaustively tested; memory-loss/unknown-write limits remain stated.

## Browser artifacts and commands

Final browser log: `artifacts/session06/browser-final.log`. Actual screenshots were visually inspected, including desktop failed-span correlation, logs search and mobile layout:

```text
artifacts/session06/screenshots/failed-span-correlated-logs.png
artifacts/session06/screenshots/error-log-search.png
artifacts/session06/screenshots/mobile-correlated-logs.png
artifacts/session06/screenshots/logs-unavailable.png
```

The existing Session 5 screenshots were refreshed by its regression suite, without replacing actual product data. The Codex browser-open request returned queued; that is not additional rendering evidence. QA used Chromium and the built local server. The temporary preview is stopped after final QA; backend/demo services are preserved.

Commands used include pinned `go get` for the three log packages, gofmt/tidy/vet/test/build, `docker compose config --quiet`, `up -d --build --wait`, Collector restart, `scripts/restart-demo.ps1`, `scripts/session06-smoke.ps1`, `scripts/session04-smoke.ps1 -SkipDatabaseFailure`, Linux verify target, npm lint/typecheck/test/build and Playwright with `TRACEFORGE_BROWSER_OUTAGE=1`. README gives PowerShell reproduction and portable npm/Go paths. Credentials were loaded only in test process memory/restored in finally; `.env` and volumes were preserved.

## Setbacks, limitations and next session

Initial bridge test assumed a resource-attribute order; actual SDK sorting required key lookup. A frontend test used an exact button name that omitted its arrow; corrected the test matcher. A wide-array review found transient validation-stack growth before rejection; fixed the precheck, added a meaningful allocation regression, rebuilt and reran final Linux race/live checks. Complex combined orchestration commands were rejected before execution by tool policy; split operations and a reviewable, process-path-checked restart script completed the authorized work. No failed attempt is represented as a pass.

Local plaintext/synthetic use has no production auth/RBAC/tenant isolation. Independent budgets still share infrastructure. Key/pattern redaction is not DLP; binary/unknown fields and future schema handling have limits. Source clocks can be coarse/skewed; timing never creates correlation or proves root cause. Log IDs do not prove source identity/delivery. Queues are in memory, retries finite, pagination non-snapshot and trace completeness unknown. No sustained log scale benchmark, cross-browser/screen-reader certification, durable queue or remote CI execution was performed.

Session 7 can now build on actual correlated tracing/logging. No model calls, agent/RAG evaluation, metrics ingestion, service map, SLO/alerting or enterprise hardening was implemented. Intended local commit: `feat: add bounded OTLP logs and trace span correlation`.

## Exact changed files

```text
ARCHITECTURE.md
cmd/shop-demo/main.go
cmd/traceforge-server/main.go
docs/adr/0006-log-identity-storage-and-correlation.md
docs/engineering-journal/session-06.md
docs/failure-model.md
docs/resume-evidence.md
go.mod
go.sum
internal/demo/http.go
internal/ingest/normalize.go
internal/logs/batcher_test.go
internal/logs/batcher.go
internal/logs/config.go
internal/logs/metrics.go
internal/logs/model.go
internal/logs/normalize_test.go
internal/logs/normalize.go
internal/logs/query_test.go
internal/logs/query.go
internal/logs/server.go
internal/storage/clickhouse.go
internal/storage/logs_test.go
internal/storage/logs.go
internal/storage/migrations/0002_logs.sql
internal/telemetry/logging_test.go
internal/telemetry/logging.go
internal/telemetry/tracing.go
otel-collector.yaml
README.md
scripts/restart-demo.ps1
scripts/session06-smoke.ps1
tests/integration/logs_test.go
web/src/app/api/logs/route.ts
web/src/app/api/traces/[traceId]/logs/route.ts
web/src/app/globals.css
web/src/app/layout.tsx
web/src/app/logs/page.tsx
web/src/components/correlated-logs.tsx
web/src/components/detail-explorer.tsx
web/src/components/error-panel.tsx
web/src/components/log-results.tsx
web/src/components/logs-explorer.tsx
web/src/components/logs.test.tsx
web/src/components/navigation.tsx
web/src/lib/logs-proxy.test.ts
web/src/lib/logs-proxy.ts
web/src/lib/logs.test.ts
web/src/lib/logs.ts
web/src/lib/proxy.ts
web/tests/logs.spec.ts
```
