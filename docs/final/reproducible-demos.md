# Reproduce the local demonstrations

Run PowerShell7 from the repository root. Supported pins: Go1.27.4; Node24.22.1; npm12.2.2; Python3.13.15; Next16.4.6; Collector0.165.0; ClickHouse26.8.36.3-alpine; PostgreSQL18.6. Exact current Docker/Compose versions are recorded with the verification evidence. The measured host has16GiB RAM; Docker was allocated roughly8GiB. This is the tested environment, not a proven minimum. Allow additional disk for images, dependencies and retained benchmark fixtures. Docker Desktop/WSL2 must be running.

## Initial setup

Copy `.env.example` to `.env` only if `.env` does not exist. Privately generate two distinct passwords for `CLICKHOUSE_PASSWORD` and `POSTGRES_PASSWORD`. Do not print/commit them. Install the pinned tools, place Go and npm on PATH, then:

```powershell
./scripts/session15-setup.ps1 -InstallDependencies
npm --prefix web run build
npm --prefix web run start
```

The last command remains in its own terminal. Setup preserves `.env`, named volumes, historical data and notes. It applies the existing non-destructive migrations, builds the demo/CLI, restarts only verified workspace demo processes, starts a fixture agent if absent and checks readiness. Later runs omit `-InstallDependencies`. Existing volumes were preserved during this session; a destructive empty-volume reinstall was not performed. The separate clean dependency install's exact outcome is in the verification matrix.

Endpoints: Next http://127.0.0.1:3000; Go readiness http://127.0.0.1:8082/readyz; legacy OTLP backend4319/Collector4317; gateway8080/payment8081; fixture agent8084. Database operator ports are loopback ClickHouse8123/PostgreSQL5432. **Legacy is deliberately unauthenticated local compatibility mode**, not an Internet deployment.

## Demo A: real service reliability

In another terminal with Go on PATH and local DB environment loaded:

```powershell
. ./scripts/session14-env.ps1 # existing S14 checkout; private keys are not printed
./scripts/session15-demo-a.ps1
```

On a fresh checkout without S14 private artifacts, load just the two DB password variables from `.env` into the process; the primary Legacy demos do not require tenant keys. The wrapper first runs the actual isolated80 normal/15 delayed/5 failed workload. It verifies100 canonical SERVER requests/5 errors per service,100 confirmed pairs, direct100 requests/5 errors/histogram100, replay stability and five increments from a new payment writer. It then executes a **separate** SLO lifecycle workload with its own namespace, cutoff, coverage and denominators; never add or equate these populations.

The lifecycle intentionally restarts/stops local backend/databases and must run without other integration workloads. Output is under `artifacts/session15/demo-a`. Its workload/reliability JSON contains actual query bounds, namespace, trace IDs, SLO key, evaluation IDs and incident ID. Open `/services?{trace_query}&bucket_seconds=60`, `/service-graph?{trace_query}&bucket_seconds=60`, a recorded failed `/traces/{id}`, `/slos/{slo_key}?evaluation={firing}`, and `/alerts/{incident_id}`. Logs are exact-ID correlated but window bounded; refresh/generate current fixtures when historical records leave the default window. Screenshots and genuine assertions are produced by `final-demo.spec.ts`.

## Demo B: offline quality and release evidence

```powershell
./scripts/session15-demo-b.ps1
```

This executes five35-case fixture runs: baseline, repeated baseline, context limit1, retrievalK1 and a missing-usage diagnostic. It persists comparisons and gates, including identical PASS, context/retrieval FAIL, stricter-policy FAIL and evidence-limited INCONCLUSIVE. Reports include the dataset/configuration checksums, actual case/run/trace/comparison/decision IDs and exact fact/citation diagnostics. Evaluation disables tools by contract; the separately tested ordinary agent order scenario demonstrates bounded read-only tools. No paid/live generation or actual provider billing occurs.

Open `/evaluations/{baseline_run_id}`, then `/comparisons/{context_regression.comparison_id}?decision={decision_id}` from `artifacts/session15/demo-b/evidence.json`. Filter regressed cases and open both real traces. Do not call an offline gate a production deployment approval.

## Verification and recovery

```powershell
go test ./...
go vet ./...
go build ./cmd/...
docker build --target verify -t traceforge-verify:s15 .
# Set private DB environment before DB-dependent checks.
$env:TRACEFORGE_PG_TEST='1'
go test -tags integration -count=1 ./internal/evaluations ./internal/reliability
go test -tags integration -count=1 ./tests/integration
Push-Location examples/agent-demo
.venv/Scripts/python.exe -m pytest -q
Pop-Location
npm --prefix web run lint
npm --prefix web run typecheck
npm --prefix web test
$env:TRACEFORGE_BROWSER_OUTAGE='1'
npm --prefix web run test:browser
```

Existing browser fixtures span multiple historical sessions; use their generating scripts documented in those journals. The new final demo spec needs both wrappers above. The metric-outage spec now compiles Go before stopping ClickHouse, runs one owned binary with a20-second Go timeout/25-second parent timeout, captures child output and always restarts the database with bounded readiness polling. Tests must run sequentially against dependencies they intentionally interrupt. Do not turn off intended assertions or count skipped cases as passes.

Rebuild with `docker compose up -d --build --wait --wait-timeout 180`; restart Collector after config changes. Diagnose readiness before generating traffic. Do not use `down -v` or remove data directories to make a check pass. Stop the foreground Next terminal and workspace-owned agent/demo PIDs recorded in ignored artifacts; verify executable paths before stopping them. `docker compose stop` preserves data and images. No recursive deletion is needed.

## Security profile

Use [Session14 ownership setup](../security/tenant-isolation.md) for synthetic tenant bootstrap, scoped keys, TLS CA, secure ports8444/4334 and per-owner Collectors4330/4331. Bootstrap is once-only and refuses existing credential output. Keys are never `NEXT_PUBLIC_*`; certificate verification stays enabled. This session preserves the authenticated browser launch policy blocker and does not repeat a rejected action under a different launcher. Lower-level previously executed TLS/authorization/isolation evidence remains explicitly dated. No security control or approval mechanism was bypassed.
