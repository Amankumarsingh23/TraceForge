# Session12 — Observed SLOs, exact budgets and persistent alerts

Implemented only Session12 after99ec127. No push, external notifications, production security, long-window rollups, sampling or Session13 optimization. Session3 learner assessment remains separately pending.

## Implementation

`internal/reliability` contains validated immutable definitions, exact rational budgets, matched direct metric and canonical provider adapters, pure lifecycle transitions, PostgreSQL migration/store, bounded GET APIs and opt-in scheduler. `internal/queryapi/reliability.go` exposes the existing typed AgentOps canonical interpretation. `cmd/slo-evaluate` registers local definitions and freezes evaluations. Server startup connects the independent bounded metadata pool and scheduler; telemetry ingestion remains separate.

Next `/slos`, `/slos/[key]`, `/alerts`, `/alerts/[id]`, `/provider-reliability` and the narrow same-origin reliability proxy show definition provenance, cutoff/revision, counts, coverage, exact budget, both burn windows, separate evidence/incident state and contributing evaluation/metric/trace/log links. Read APIs return structured bounded errors; no HTTP mutation API was added. See the SLI contract, alerting policy and ADR0012 for exact semantics and limits.

## Reproduce

Use the existing local `.env` without committing credentials. Start `docker compose up -d --build --wait`; build `go build -o bin/shop-demo-session06.exe ./cmd/shop-demo` and `go build -o bin/slo-evaluate.exe ./cmd/slo-evaluate`. Run `./scripts/session12-smoke.ps1` in PowerShell7. The script imports local database passwords privately, creates fresh definitions/scope, runs real traffic, enables the backend scheduler temporarily, restarts the backend, stops/restores ClickHouse, replays metrics/traces and records evidence under ignored `artifacts/session12`. It starts only the fixture Python app and never enables paid/live calls. The override is removed from effective Compose configuration on exit; scheduling defaults disabled.

Definitions are JSON arrays of1–8 validated objects. `bin/slo-evaluate.exe --file artifacts/session12/definitions.json --register-only` freezes versions. Add `--key KEY --cutoff 2026-11-14T06:28:39Z --revision 1` to evaluate a whole-second UTC cutoff. Same-ID retry returns stored evidence; revision2 explicitly records late evidence without changing lifecycle at that cutoff. CLI input<=64KiB, evaluation<=4MiB. Backend lists<=50 records, response/read<=8MiB. Database work has deadlines and bounded pools. Do not expose these unauthenticated local endpoints publicly.

Start frontend with `npm --prefix web run build` then `npm --prefix web run start`. The configured pinned Node/npm toolchain is needed. Open `/slos` or a frozen evaluation URL listed below. Run Go tests/vet; Docker `verify` target provides Linux race checks; Python pytest with `TRACEFORGE_PG_TEST=1` includes metadata integrations; frontend lint/typecheck/test/build and Playwright require the actual local backends. Browser tests consume prior session artifacts; they are not stand-alone mocks.

## Actual measured workload — 2026-11-14 UTC

Scope `session12-20261114062758`, service payment, environment local, mode synthetic, route `/charge`, declared writer `574490dd-33dc-40b7-ad91-d39099ed87b0`. Availability key `s12-20261114062804-availability-v1`: target90%,20s objective/long,10s short,2x AND burn, min3 events, >=60% supported interval coverage per writer, confirm1/recover1. These permissive, short demo settings apply only to the observed subset.

The driver made123 actual checkouts: warmup1, healthy25, failure25, repeated failure6, post-restart failure6, recovery34, latency26. There were37 injected failures; the latency phase injected300ms delay in8 requests. Exported whole intervals select subsets of traffic; reported SLO counts are not forced to equal generated workload counts.

| Cutoff UTC | Selected overall eligible/good/bad | Allowed bad | Consumed/burn | Remaining | Result |
|---|---|---|---|---|---|
|06:28:20|22/22/0|11/5|0|11/5|MEASURED, target met|
|06:28:39|23/4/19|23/10|190/23|−167/10|MEASURED, BREACH|
|06:28:57|13/0/13|13/10|10|−117/10|MEASURED, BREACH after restart|
|06:29:01|unavailable|N/A|N/A|N/A|UNKNOWN/BACKEND_UNAVAILABLE|
|06:29:31|23/23/0|23/10|0|23/10|MEASURED, target met|
|06:30:00 latency|21/14/7|21/10|10/3|−49/10|MEASURED, latency target violated|

At firing, long window06:28:19–06:28:39 had SLI4/23 and supported coverage89.9988935%; short06:28:29–06:28:39 had11/0/11, burn10, coverage80.000177%. Both burns>=2. Latency threshold0.1s is an exact exported boundary;7 of8 generated delays occur in the selected supported intervals. Latency SLI14/21=2/3. No estimated in-bucket threshold or fabricated p95 was used.

Firing evaluation `2f0c2d3bc9e45f83aee9e7b4032682b37f11b47e51e467fc8be39b93a0f460fb`; unavailable `9bddff810c07fc58432979eec88881321d6db05600657f06d33f887b40b1296d`; recovery `c2d051610a0c6ed03aeb82e35976174aaffa42e374a2818586aae00a9fc88247`.

Incident `3d2125fa8f9cca81a855dda2563c3e03c4f7bda87e79e9ac0d840ec45beff0c3` fired06:28:39 and resolved06:29:12 through the actual scheduler. Repeated breaches, metric replay/revision2 and backend restart retained that incident. A real ClickHouse stop produced UNKNOWN while it remained FIRING. Exactly two events persist: FIRING `0f546ed2dda0abdd56b189a12044b42ad2d809b1272debd962ca1903b5ed2a37`, RESOLVED `691d8bcd3cfc06c637f5d020166505b643668831e6928341851af998234e8f89`. The scheduler's recovery evaluation `e12181e535d8f2d30f83c88b9da1b818e8ce217da4a7593ca2a6ab488958f344` ended the burn condition; the later all-good manual evaluation is not the first resolution.

Three fixture agent runs (normal, retry, permanent_error) at cutoff06:30:10 yielded4 attempts/2 successes,3 logical operations/2 successes,1 retry-recovered operation,1 timeout,1 provider error and0 rate-limit outcomes. Only2/4 attempts have complete usage; missing usage is incomplete, not zero cost. Attempt SLI1/2 at target0.8 gives allowed4/5, consumed5/2, remaining−6/5. Logical SLI2/3 at target0.9 gives allowed3/10, consumed10/3, remaining−7/10. Replay revision retained these counts. Observed fixture mean/p95 attempt durations0.4536/0.8084ms are local fixture timings, not network inference measurements. Pure tests separately cover rate limiting.

Final scope review added separate logical-operation mean/p95 durations. Frozen provider revision3 `48ffd7a7502ac9fb18405b39ef904c72cc9c8c74f9f85814d63aa54611767fce` re-evaluated the same actual stored window: logical mean21.787966666666666ms, p95 nearest-rank63.873ms across3 operations, with unchanged4-attempt/3-logical counts. Logical duration includes orchestration/retry time; it must not be substituted for attempt latency. Earlier immutable revisions remain unchanged. The smoke script now records this additional revision too.

Actual trace IDs: normal `a052d602231c2156995300176889c235`, retry `aa98fb79c488dd620e600a58cacce607`, permanent error `0edc8d9ed8ed4d19e31d5d188a612d0d`. Provider key `s12-20261114062804-provider-v1`; logical key same prefix `logical-v1`. Live key same prefix `live-v1` selects openai/live and yields UNKNOWN/NO_OBSERVATIONS: no paid calls or fixture substitution.

An earlier driver attempt miscompared PowerShell local DateTime ticks with UTC JSON and failed its scheduler assertion although the scheduler had fired. Fixed UTC normalization/string parsing, reran a fresh scope successfully. Its immutable historical rows were retained, so the one-incident assertion is scoped to the final SLO, not all local history. A regression attempt initially lacked running demo processes; they were restarted. Another overlapped a controlled browser outage; sequential rerun is recorded in the final checks below.

## Final verification

| Check | Actual result |
|---|---|
|Go native `./internal/... ./cmd/...`|257 passing tests/subtests; no failures|
|Linux Docker verify target|`go vet ./...` and `go test -race ./...` passed|
|PostgreSQL reliability integration|Passed12-worker deduplication, immutable versions, UNKNOWN/stale/revisions, reopen, recovery/new episode|
|Prior trace/log/storage and AgentOps integrations|Sequential Session06/03 and Session07 smoke scripts passed after restoring demo processes and excluding concurrent outages|
|Python with PostgreSQL enabled|85 passed; Ruff and pip check passed; existing Starlette/httpx deprecation warning|
|Frontend|67 tests passed; lint, typecheck and production build passed|
|Browser against actual backends|45 passed including controlled PostgreSQL/ClickHouse outages; final6 reliability checks rerun passed after duration/threshold presentation changes|
|Visual review|Desktop fixture/provider and390px mobile latency screenshots inspected; artifacts remain local/ignored|

`artifacts/session12` holds native JSON test output, Linux verify logs, sequential regression output, full/final browser logs, immutable demo JSON and screenshots. The smoke-script duration-revision addition was syntax-checked; it uses the same evaluator call exercised manually. The complete workload driver passed before that small report addition. Learning notes are ignored and excluded from the session commit. No failed check remains unresolved; intermediate setup failures are recorded above rather than hidden.

## Review answers

1. 200000 requests,140 errors =>199860 good,99.93% SLI.99.9% target allows200 errors;70% consumed,60 remaining; target met. SLI measures, SLO sets an objective, SLA specifies a contractual commitment.
2. Total1000,good940,slow60 =>94% SLI versus95%; allowed50,120% consumed,−10 remaining, target violated. At300ms the250–500ms bucket cannot identify the good count (700–940 possible); no exact evaluation without finer observations.
3. 2% /0.1%=20x. At constant traffic/error rate a full30-day budget exhausts in1.5days=36hours. Long20x/short8x do not satisfy14.4x AND; missing short data yields UNKNOWN. The windows combine sustained severity with recent activity.
4. T1 FIRING,T2 same FIRING,T3 UNKNOWN evidence with FIRING preserved,T4 same FIRING,T5 RESOLVED. One incident,two lifecycle events. Backend absence is no recovery evidence. PostgreSQL row locks, unique active fingerprints and deterministic evaluation/event IDs prevent concurrent duplicate episodes.
5. Ten logical operations make12 attempts:10 successes,2 timeouts. Attempt success10/12=83.333…%; logical success10/10=100%. Unknown timeout usage makes cost incomplete without changing known operational outcomes. Deterministic fixture behavior demonstrates control flow, not live provider reliability or answer quality.

## Limits

Raw windows<=1h; no durable rollups, retention optimization, production alert delivery, independent semantic grading, authentication or tenant enforcement. Expected-writer coverage is conditional and cannot establish total traffic capture. PostgreSQL loss prevents state persistence; no recovery is inferred. Metadata histories grow without a retention job in this session. Local audit triggers do not protect against an administrator. No production SLA or live-provider claim is made.
