# TraceForge — Session 9

## Session 9: paired experiments and release-policy evidence

The existing evaluator now freezes a hashed experiment manifest and separates retrieval depth from metric cutoff. Pure Python compares stable case IDs, retains failures/N/A/coverage, computes paired quality/latency/cost deltas and descriptive/seeded-bootstrap diagnostics, then evaluates versioned PASS/FAIL/INCONCLUSIVE policies. PostgreSQL stores immutable manifests/comparisons/policies/decisions; Go remains read-only; `/comparisons` links both actual evaluations and execution traces. **Offline PASS is not production approval; no deployment or paid model/judge call occurs.**

With the fixture agent and healthy local stack running, invoke `.\scripts\session09-experiments.ps1` from the root. It reads/restores the existing PostgreSQL password without printing or editing `.env`, runs five actual35-case configurations, registers migration0002 and saves local evidence under ignored `artifacts/session09`. Baseline/repeat use retrievalK3/context2; candidates use context1 or retrievalK1, always scored at evaluationK3. An explicit missing-usage fixture tests cost evidence. Independent critical cases were designated from existing multi-document/truncation categories before candidate execution. Controlled gate-only fixtures remain separate from measured application runs.

Measured baseline correctness32/35 falls to28/35 for both real candidates (four regressions). Context1 preserves Recall@3=0.983333 but reduces supplied coverage0.955556→0.844444; retrievalK1 reduces Recall@3 to0.844444. Identical quality deltas are zero. All ordinary runs have35 matched correctness cases/30 retrieval-eligible pairs. Hypothetical complete cost is28000000 nano-USD ($0.028) per run,42000 input/3500 output synthetic tokens; missing usage is unknown. Local HTTP timing is descriptive only. Exact frozen IDs, distributions and decisions are in the journal/results JSON.

CLI from the Python directory with server-side `POSTGRES_PASSWORD`: `python -m evaluation.compare_cli BASELINE_ID CANDIDATE_ID --policy evaluation/policies/offline-v1.json --report ../../artifacts/session09/comparison.json`. The evaluation runner adds `--metric-k 3` independently of `--k`. Stable config IDs have content-derived immutable versions; new policy versions create separate decisions, same comparison/policy retries return the same record. Open `/comparisons` in the existing Next app and select a persisted gate link. Compare actual config differences, correctly labeled percentage-point deltas, matched/full-run cohorts, cost coverage, critical regressions and both trace links. Filters use server-generated case transitions.

New GET APIs: `/api/v1/experiments`, `/api/v1/comparisons`, `/api/v1/comparisons/{id}`, `/api/v1/comparisons/{id}/cases`, `/api/v1/gates/{id}`. Lists support limit1–50/after. Existing four-slot deadlines, read-only PostgreSQL pool, parameterized queries, cancellation and response caps apply. No write API or browser credentials. Production TLS/auth/tenant isolation and live/human semantic validation remain pending.

Checks reuse Go unit/vet/Linux race, Python Ruff/pytest/pip check (set `TRACEFORGE_PG_TEST=1` for actual persistence), frontend lint/typecheck/tests/build and browser checks. `TRACEFORGE_BROWSER_OUTAGE=1` enables controlled outages with restoration; do not overlap those with DB-dependent evaluation tests. Full policies/edge rules: [comparison contract](docs/evaluation/experiment-comparison.md), [gate policy](docs/evaluation/gate-policy.md), [ADR0009](docs/adr/0009-comparison-provenance-and-gates.md), [Session9 report and five answers](docs/engineering-journal/session-09.md). Learning note `learning-notes/session-09-experiments-and-gates.md` remains ignored. Stop after Session9; no service analytics/metrics work is included.

## Session 8: trace-linked RAG evaluation

The existing Python agent now supports an opt-in constrained synthetic output contract and context cap. A separate evaluation runner executes the actual `/ask` path, without sending relevance labels/reference facts to it. A versioned 35-case synthetic dataset (30 retrieval-eligible, five unanswerable) and 12 independent source facts provide exact retrieval and closed-domain quality checks. Results persist transactionally in PostgreSQL and link to actual ClickHouse traces through new bounded Go APIs and `/evaluations` views. These are fixture engineering measurements, not validated live-model quality or general semantic entailment.

```powershell
# Repository root; preserve existing .env and database volumes.
docker compose up -d --build --wait --wait-timeout 180
# In examples/agent-demo, install updated requirements.lock and start the fixture agent:
cd examples/agent-demo
.\.venv\Scripts\python.exe -m pip install -r requirements.lock
.\.venv\Scripts\python.exe -m agent_demo
```

In another root terminal, run `.\scripts\session08-evaluate.ps1` for a baseline or `.\scripts\session08-evaluate.ps1 -Experiments` for the explicit K=1/3/5, context-loss, faulty-output, timeout, unavailable-lookup and deterministic-repeat diagnostics. The wrapper reads the existing PostgreSQL password into process environment without printing/rewriting it, restores environment/location, refuses a live server, and applies the versioned metadata migration through the runner. Direct CLI from `examples/agent-demo`: `python -m evaluation.runner --k 3 --context-limit 2 --report ../../artifacts/session08/baseline.json`; set `POSTGRES_PASSWORD` server-side first. No paid calls are made by default.

The runner uses one sequential request at a time, at most one model call/attempt and no executable tools per document-only case, bounded HTTP/SQL/trace lookups, new run IDs and frozen hashes/configuration. `--case-id support-030` selects a diagnostic case. `--fault` identifies an explicit controlled transformation of the original actual output; both original and evaluated outputs are preserved. `--execution-failure timeout` injects a fixture failure; `--trace-unavailable` uses a deliberately unavailable local lookup while retaining the real trace identity. A result can persist before a trace is queryable. Source version/checksum and Git dirty state are recorded honestly.

Measured baseline K=3/context=2: Recall 0.983333, Precision 0.400000, Hit 1.000000, MRR 0.966667 over 30 eligible/scored cases (five N/A); context/required-fact coverage 0.955556; constrained overall correctness 32/35. Groundedness and citation support were 1.0 over 30 applicable cases, while three answers remained incomplete. These scores are specific to deterministic synthetic generation and author-reviewed labels. Independent human review/calibration, live generation and judge verification remain pending. See the journal for all actual numerators, run IDs and failure/repeat results.

Read-only endpoints: `GET /api/v1/evaluations`, `/{run_id}`, `/{run_id}/cases`, `/{run_id}/cases/{case_id}` under that prefix. Lists accept `limit` 1–50 and `after` cursor; defaults 25. Query deadlines, four slots/pool connections, read-only PostgreSQL sessions and 4MiB response cap apply. Missing traces do not block evaluation reads. PostgreSQL failures produce explicit evaluation errors while trace/log APIs retain their existing storage boundary. Credentials remain server-side; local Compose uses internal plaintext PostgreSQL and loopback host binding, not production tenant/auth/TLS enforcement.

Start the existing `web` production build/server and open `/evaluations`, select a run/case, inspect expected facts versus actual supplied IDs, claim/citation judgments, numerator/denominator/coverage and actual trace navigation. N/A, pending and execution failures remain distinct from zero. The UI prominently identifies diagnostic transforms. Free-form output gets semantic-review pending, never fabricated quality scores.

Checks: Python Ruff/pytest/pip check; opt in `TRACEFORGE_PG_TEST=1` with server-side PostgreSQL credentials for persistence tests. Go `go test ./cmd/... ./internal/...`, `go vet ./cmd/... ./internal/...`, `docker build --target verify -t traceforge-verify:s8 .`; PostgreSQL repository integration: `go test -tags integration ./internal/evaluations`. Existing Session 6/7 smoke scripts exercise prior storage/AgentOps paths; the Session 4 smoke covers isolated graceful/forced shutdown. Frontend lint/typecheck/tests/build and Playwright use real local SQL/API data; `TRACEFORGE_BROWSER_OUTAGE=1` enables controlled outage cases that restore services in finally.

Live evaluation requires both CLI `--allow-live --allow-billable` and the separately configured authorized Session 7 live server. No live or judge calls were performed. Static synthetic inputs are the only default dataset; raw application content remains excluded from general telemetry. Initial labels have implementation-author review, not independent human calibration. No deployment gates, automatic promotion or cross-run comparison platform exists.

Details: [metric formulas and edge policies](docs/evaluation/metric-definitions.md), [dataset/manual review rubric](docs/evaluation/dataset-and-rubric.md), [ADR 0008](docs/adr/0008-evaluation-provenance-and-postgres.md), [Session 8 evidence and five answers](docs/engineering-journal/session-08.md). Local `learning-notes/session-08-rag-evaluation.md` remains ignored and uncommitted.

## Session 7: AgentOps execution

`examples/agent-demo` is a bounded Python 3.13 support application: actual BM25 retrieval over 12 versioned synthetic documents, actual read-only order lookup, explicit fixture/live providers, and OTLP/gRPC traces and correlated logs through the existing Collector and generic storage. Default **fixture** mode is deterministic; it is not live inference. The separately configured HTTPS OpenAI-compatible adapter is mock-transport tested; live execution remains pending, with no billable calls made.

```powershell
docker compose up -d --build
cd examples/agent-demo
py -3.13 -m venv .venv
.\.venv\Scripts\python.exe -m pip install -r requirements.lock
.\.venv\Scripts\python.exe -m agent_demo
```

In another terminal at the repository root:

```powershell
$body = @{question='Refund for damaged delivery and order status DEMO-1002'} | ConvertTo-Json
$run = Invoke-RestMethod http://127.0.0.1:8084/ask -Method Post -ContentType application/json -Body $body
$run | Select-Object run_id,trace_id,mode,retrieved_document_ids,context_document_ids
Invoke-RestMethod "http://127.0.0.1:8082/api/v1/traces/$($run.trace_id)/agent-summary"
# Open /traces/<trace_id> in the existing Next app (web: npm run build; npm run start).
```

The AgentOps panel separates retrieved IDs/ranks/scores from supplied context IDs, logical calls from attempts, function entry from tool rejection, and known usage/cost subtotals from complete observed estimates. Operation buttons select actual waterfall spans and correlated logs. Conventional traces keep their existing UI. Trace completeness remains unknown; this is execution evidence, not answer-quality measurement.

`POST /ask`: question 1–1000 characters, `top_k` 1–5 (default 4; only two documents supplied). Limits can only be lowered: `max_model_calls` 1–3, `max_tool_calls` 0–2, `max_steps` 1–6, `max_attempts` 1–2 including the initial attempt per call. Body ≤8192 bytes/read ≤2s, four active agents, admission wait 50ms, execution deadline 20s, attempt deadline 5.5s, tool timeout 100ms. At most six provider attempts, 256 requested completion tokens each. Responses after execution starts carry run/trace IDs, including controlled errors. Loopback port 8084 has no authentication or tenant isolation; use only synthetic data.

Fixture `scenario` values: normal, no_results, retry, timeout, permanent_error, missing_usage, bad_arguments, unknown_tool, tool_timeout, unknown_order, max_steps. Use `max_steps:1` for the step-bound fixture. No-results substitutes an unmatched lexical query; provider errors are adapter injections, while tool timeout uses an actual cancellable delay. These fixtures do not establish real provider failure behavior.

### Usage, pricing and privacy

The bounded summary endpoint uses canonical reconstructed spans, grouping run IDs and summing only provider-attempt spans marked `traceforge.usage.accounting=attempt`. Parent/logical totals are never added again; physical telemetry replay cannot add usage. Missing/failed usage remains unknown, not zero. Known subtotals and complete-attempt coverage are explicit. The full observed estimate is null if any observed attempt lacks required usage/pricing; observed coverage never proves complete telemetry or actual billing.

Fixture usage is labeled `synthetic_fixture`; versioned prices are hypothetical: input $0.50/million, output $2.00/million, cached input $0.10/million. Cached input is subtracted from ordinary input; reasoning is already part of output. Python Decimal and Go rational arithmetic independently round each attempt half up to integer nano-USD. Two fixture calls report 3000 input/400 output and $0.002300000 estimated cost. Live pricing is unknown unless explicitly configured by provider and **response** model; no real prices are hardcoded.

Telemetry contains bounded IDs, versions, outcomes, counts and fixed messages. Raw questions/prompts/document text/answers/tool arguments/results, keys and provider error bodies are omitted at source. The existing receiver redactor preserves only four exact nonnegative integer GenAI usage keys up to 10 million; strings and credential-like keys remain redacted. This is not universal secret detection. Inputs remain in application memory and reach the configured provider in live mode. Credentials and `learning-notes/` must never be committed.

### Verify and optionally enable live execution

```powershell
# In examples/agent-demo:
.\.venv\Scripts\python.exe -m ruff check agent_demo tests
.\.venv\Scripts\python.exe -m pytest -q
.\.venv\Scripts\python.exe -m pip check
# Repository root, with fixture app and stack running:
.\scripts\session07-smoke.ps1
docker build --target verify -t traceforge-verify:s7 .
# Existing frontend: npm run lint/typecheck/test/build, then start and test:browser.
# TRACEFORGE_BROWSER_OUTAGE=1 enables the two controlled outage browser checks.
```

Live mode requires both `TRACEFORGE_AGENT_MODE=live` and `TRACEFORGE_ALLOW_LIVE=1`, plus process-environment `TRACEFORGE_PROVIDER_URL` (HTTPS `/v1` base), `TRACEFORGE_PROVIDER_API_KEY`, `TRACEFORGE_PROVIDER_MODEL`, optional `TRACEFORGE_PROVIDER_NAME` (default openai), and optional `TRACEFORGE_PRICING_FILE` matching `data/pricing.json`. There is no dotenv loading, credential discovery, automatic provider retry or fixture fallback. Choose a provider/model supporting nonstreaming Chat Completions with the implemented strict tool schema. Actual authentication/model compatibility remain unverified.

Only after separately authorizing billing and starting that live server, run `python -m agent_demo.smoke --allow-billable` from the activated environment. It sends exactly one question with one call/attempt, zero executable tools, 256 requested completion tokens, and prints only identity/error metadata. Default checks never invoke it. Never paste credentials into tracked files or transcripts.

See [Session 7 evidence and five answers](docs/engineering-journal/session-07.md), [ADR 0007](docs/adr/0007-agent-attempt-accounting.md), and local ignored `learning-notes/session-07-agentops-execution.md`. Recall@K, MRR, groundedness and experiment gates remain Sessions 8–9.

A local trace/log and AgentOps investigation tool: application → Collector → bounded Go writers → ClickHouse → query APIs → Next.js investigation, with Python evaluation metadata in PostgreSQL. Backend exports wait for confirmed writes. Trace completeness remains unknown; log rows preserve repeats/replays. OTLP metrics ingestion remains planned.

## Session 6: structured logs and exact correlation

The demo uses `slog` → `otelslog` 0.21.0 → log SDK 1.47.0 → OTLP/gRPC exporter 0.23.0, compatible with the existing pinned tracing stack. Both signals share service/version/environment resources. Context-aware logs describe checkout start/validation, payment start, controlled delay, failure and completion; inner operations carry their actual active span IDs. Startup is standalone. SDK buffering is capped at 512 records, 128/export, 1s interval and 3s export deadlines; shutdown flush is bounded. Constructing an exporter or printing a message does not prove persistence.

Upgrade without replacing `.env` or volumes. Restart Collector to reload the bound configuration; its separate log exporter has 64 queued requests, one consumer and finite 60s retries. It does not emit detailed log payload debug output. The trace pipeline is preserved.

```powershell
# Repository root, Go on PATH
docker compose config --quiet
docker compose up -d --build --wait --wait-timeout 180
docker compose restart otel-collector
go build -o bin/shop-demo-session06.exe ./cmd/shop-demo
.\scripts\restart-demo.ps1
```

The restart helper only stops known demo executables inside this checkout's `bin/`, refuses other port owners and starts hidden loopback services. If Windows prevents rebuilding the running executable, stop those demo processes first or use the `go run ./cmd/shop-demo ...` terminals below. Logs/PIDs stay under ignored artifacts. Use synthetic inputs only.

Open [log search](http://127.0.0.1:3000/logs), or select a waterfall span and inspect **Correlated logs**. Trace scope includes trace-only records; selected-span scope matches both IDs. Correlation uses an explicit last-24h effective-time window and refreshes for late arrivals. General search defaults to one hour and keeps applied absolute bounds until changed/cleared. An empty window does not prove no logs exist. Unobserved trace links lead to an honest 404; no matching span is fabricated.

```powershell
$checkout = Invoke-RestMethod 'http://localhost:8080/checkout?fail=true'
$traceId = $checkout.trace_id
# Poll after SDK/Collector batching; HTTP completion alone is not storage proof.
Invoke-RestMethod "http://localhost:8082/api/v1/traces/$traceId/logs" | ConvertTo-Json -Depth 25
$spanId = 'ACTUAL_PAYMENT_SPAN_ID' # copy payment.process from trace detail
Invoke-RestMethod "http://localhost:8082/api/v1/traces/$traceId/logs?span_id=$spanId" | ConvertTo-Json -Depth 25
Invoke-RestMethod 'http://localhost:8082/api/v1/logs?service=payment&min_severity=17&text=payment&limit=20' | ConvertTo-Json -Depth 25
```

Log endpoints accept `from`/`to` UTC RFC3339Nano (≤24h), service, `min_severity` 0–24, optional trace ID, span ID requiring trace ID, case-sensitive literal `text` (≤256 UTF-8 bytes), page size 1–100 and opaque cursor. Path/query trace IDs must agree. Ordering is `effective_time DESC, log_id DESC`; cursors bind all filters and exact nanoseconds. Pages are non-snapshot. Responses contain physical `logs`, `next_cursor`, actual bounds and `consistency=non_snapshot_raw_rows`. Errors are structured/sanitized. No SQL, regex or custom query language is accepted.

Migration 0002 adds `traceforge.logs`, sorted by effective time/log ID and partitioned monthly. Each observation gets a random storage-row identity; optional source `log.record.uid` stays separate. There is no span foreign key or event deduplication: three identical events stay three rows and replay adds rows. Typed protobuf JSON retains type tags, int64 strings, bytes and supported metadata. Optional event/observed times remain null; effective ordering prefers event, observed, then explicitly labeled receiver-ingestion fallback. API times are precise strings; frontend offsets use BigInt.

Log budgets are independent of traces: 32 queued/normalizing slots, 16 handlers, 64 owned jobs, 8192 reserved records, 64MiB conservative payload units, one writer, 256-record target/50ms assembly wait, 50ms admission and 3s write/5s export limits. Inspect `/debug/log-ingestion` and `/metrics/logs`; use exposed `--log-*` flags for local tuning. These payload units are not RSS, and database/CPU/network can still contend. Full/partial acknowledgments wait for the valid subset's synchronous write. Crashes, expired retries, cancellations and ambiguous writes can still cause loss/duplicates.

Known sensitive keys are recursively redacted before insertion, including log/resource/scope attributes and nested body maps/arrays. Recognized free-form secret patterns and URL credentials/sensitive query fields are sanitized. Binary values under nonsensitive keys remain opaque. This bounded policy can miss confidential data and over-redact; it is not comprehensive DLP. Raw payloads/driver details are not printed on rejection. Do not send real secrets or customer data.

### Verify Session 6

```powershell
go test ./...
go vet ./...
go build ./...
.\scripts\session06-smoke.ps1
docker buildx build --target verify --output=type=cacheonly .
# In web/, with a built UI running in another terminal:
npm run lint
npm run typecheck
npm test
npm run build
$env:TRACEFORGE_BROWSER_OUTAGE = '1'
npm run test:browser
Remove-Item Env:TRACEFORGE_BROWSER_OUTAGE
```

The smoke script writes actual `artifacts/session06/demo.json`/`fixtures.json` and restores its controlled DB outage in `finally`; `-SkipDatabaseFailure` omits that experiment. Browser regressions also need Session 5's `demo-evidence.json`; generate it once with `scripts/session05-demo.ps1` if absent. Do not run setup and browser checks concurrently. Synthetic-secret tests assert removal in actual SQL/API data. Exact counts, IDs, screenshots and limitations are in [the Session 6 journal](docs/engineering-journal/session-06.md); design decisions are in [ADR 0006](docs/adr/0006-log-identity-storage-and-correlation.md).

## Open the trace explorer

The UI runs on the Windows host with Node 24 LTS (verified 24.22.1), npm 12.2.2, Next.js 16.4.6 and React 19.3.1. Keep the backend and two demo processes running as described below. No frontend container or new credentials are needed. From a new PowerShell terminal:

```powershell
Set-Location 'C:\placement projects\TraceForge\web'
npm ci
$env:TRACEFORGE_API_URL = 'http://127.0.0.1:8082'
npm run dev
```

Open [the local explorer](http://127.0.0.1:3000/traces). Scripts bind loopback only. `TRACEFORGE_API_URL` is server-only and defaults to the origin shown; it must be an origin without credentials/path/query. The verified setup uses host → host-published Go port 8082. A future containerized frontend would need `http://traceforge-server:8082`, not container localhost. No `NEXT_PUBLIC_` credentials, wildcard CORS or direct ClickHouse connection is used.

This machine's bundled Node had no npm on PATH. A checksum-verified npm 12.2.2 is at `$env:LOCALAPPDATA\TraceForge-tools\npm-12.2.2\package\bin\npm-cli.js`. With that installation, substitute `node "$env:LOCALAPPDATA/TraceForge-tools/npm-12.2.2/package/bin/npm-cli.js"` for `npm` below. A normal Node/npm installation can use the ordinary commands.

`/traces` searches actual observations with UTC RFC3339 timestamps (maximum 24-hour window), service, supported `error` status and page size. It defaults to the last hour. “Clear filters” restores that recent window; older traces may require ID lookup or older bounds. Service/error filters qualify observed spans in the window; summaries can differ from full detail. Cursor pagination binds to applied filters and resets on search/clear. It is not snapshot-consistent while telemetry arrives. Unsupported operation/minimum-duration filters remain future work.

`/traces/{id}` shows every backend canonical span from roots, orphans and disconnected components. Select a row/bar for raw nanosecond timestamps, kind/status, attributes, scope, events and links. Expand/collapse and keyboard controls work in the scrollable waterfall. Diagnostics retain physical/logical distinctions, conflicts, missing parents, cycles, timing warnings and unknown completeness. “Contains error span” does not prove the whole request failed. Refresh is manual.

The shared axis uses observed earliest start/latest end, not summed child durations. RFC3339Nano strings become BigInt before subtraction; offsets never use rounded absolute ns numbers or millisecond-only Dates. Tiny/zero spans have a minimum visual marker and honest numeric durations. Anomalies are flagged rather than repaired. See [ADR 0005](docs/adr/trace-explorer-ui.md).

### Frontend checks and production run

```powershell
# In web/
npm run lint
npm run typecheck
npm test
npm run build
npm start
```

Stop the dev server before starting production on the same port. A dev startup is not a production-build check. No external fonts or UI component library are needed. The committed lockfile pins resolutions. Loading, empty results, invalid input, missing/oversized traces, unavailable backend and timeout states are explicit; stale responses cannot overwrite newer results.

### Reproduce the real browser demonstration

With gateway/payment and the base Compose stack running, the preparation script exports controlled test telemetry through OTLP, generates normal/delayed/failed checkout requests, waits for five persisted spans each, and saves IDs/responses under ignored artifacts. It also exports a 1000-logical-span trace and verifies the API count. It never rewrites `.env` or deletes historical data.

```powershell
# Repository root
$env:PATH = "$env:LOCALAPPDATA\TraceForge-tools\go\bin;$env:PATH"
.\scripts\session05-demo.ps1

# Keep a built UI running in another terminal, then:
Set-Location web
npm exec playwright install chromium
npm run test:browser
# Optional: include a controlled local DB stop/recovery (restored in finally).
$env:TRACEFORGE_BROWSER_OUTAGE = '1'
npm run test:browser
Remove-Item Env:TRACEFORGE_BROWSER_OUTAGE
```

Ordinary browser runs skip the explicit outage case and require generated `artifacts/session05/demo-evidence.json`. The preparation script's optional `-ReuseFixtures` uses its existing fixture log while refreshing checkout traces. Do not run preparation concurrently with browser checks. Screenshots stay under `artifacts/session05/screenshots/`; no fixtures are substituted into the product.

Manual demo requests remain `/checkout`, `/checkout?delay_ms=500`, `/checkout?fail=true`. Verified Session 5 `payment.process` intervals were 4263000 ns normal and 500617500 ns delayed. These are recorded observations, not benchmarks or a timer-precision guarantee. Failure returned gateway 502 and stored error status/exception events. Full IDs, exact checks and limitations are in [the Session 5 journal](docs/engineering-journal/session-05.md).

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

Both should return 1. PostgreSQL's health check establishes readiness; its SQL command additionally verifies authenticated connectivity. The receiver creates ClickHouse's `traceforge.spans` table through migration 0001. Session 8's Python runner separately applies PostgreSQL evaluation migration 0001.

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

See `ARCHITECTURE.md`, ADRs0004–0009, the failure model and Session4–9 journals for current behavior and evidence. Prior journals remain historical. Local learning notes are Git-ignored; each session has its own note. Offline comparisons/gates are implemented; live semantic review and Session10 analytics remain pending.
