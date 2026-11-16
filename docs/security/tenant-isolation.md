# Trusted ownership and migration

The authorization unit is the credential-resolved tenant. The deployment fixes three tenants: `legacy`, `alpha`, `beta`. Alpha and Beta have separate private backend processes, ClickHouse databases `tf_tenant_alpha`/`tf_tenant_beta`, and PostgreSQL schemas of the same names. Historical data remains in ClickHouse `traceforge` and PostgreSQL `public`, visible through Legacy only. No raw historical payload was rewritten or attributed from its attributes.

Logical span identity is `(trusted tenant, trace_id, span_id)`; log, metric stream/point, service, evaluation, comparison, SLO and incident identities similarly include their owning storage namespace. Equal trace/span IDs can coexist as Alpha=100ms and Beta=900ms because canonicalization and all aggregations run inside the selected database. Materialized ClickHouse `tenant_id` is a constant derived from the trusted database, not supplied OTLP attributes. It is excluded from `SELECT *` by default, preserving the existing canonical selectors and benchmark insert shape. Fingerprints can be equal across owners: their logical identity includes the schema/database. This avoids quietly changing prior normalization or scoring algorithms.

Every tenant backend sets its trusted owner from operator configuration. The ClickHouse store validates the fixed database selection; all existing qualified SQL is routed to that database. Evaluation/reliability PostgreSQL pools set **only** the owner schema in `search_path`; there is no `public` fallback. Migrations run with the same path, so unqualified foreign keys bind to owner-local tables. Dataset/case/run/comparison/gate and SLO/evaluation/incident/event joins and uniqueness stay inside that schema. ID-only detail lookups cannot search other schemas. The owner is not a user-selectable list filter. No shared response or canonical-result cache was introduced.

The gateway's backend map is fixed by trusted deployment configuration. It strips Authorization, Cookie and tenant/forwarding headers before internal proxying. HTTP reads require read; explicit SLO management/diagnostics require manage; protobuf exports require ingest. Internal management routes are mounted only for the private nonlegacy processes. Trusted CLI evaluators additionally require an online, matching manage credential for a nonlegacy job. A host administrator with database credentials remains outside this isolation model.

Collectors cannot safely mix clients and forward one undifferentiated credential. The secure profile instead has two private pipelines with distinct receiver bearer tokens, fixed export credentials, separate queues and verified TLS. An Alpha token at Beta's receiver is denied; forwarded traffic is reauthenticated and assigned by its token again. Revocation at the gateway therefore still blocks a queued request even if a static Collector configuration accepted it. A Collector ACK is volatile queue acceptance; only a synchronous direct gateway/backend ACK proves the database persistence boundary. No claim of durable Collector buffering is made.

## Local commands (PowerShell, repository root)

Keep `.env`, `artifacts/` and `learning-notes/` private/ignored. First bring up the ordinary databases with `docker compose up -d --wait`, then run `./scripts/session14-bootstrap.ps1` once. It builds, initializes private namespaces and keys/certificates, applies evaluation migrations in both schemas and starts the secure profile. Preserve its private output; rerun bootstrap now fails rather than silently replacing active keys. For subsequent starts:

```powershell
. ./scripts/session14-env.ps1
docker compose -f compose.yaml -f compose.secure.yaml up -d --build
examples/agent-demo/.venv/Scripts/python.exe scripts/session14/smoke.py --outages
```

The last command stops/restarts the **local** databases for controlled failure testing and should only be run without unrelated work using them. It writes synthetic fixture data and sanitized reports. Run attack evaluations:

```powershell
. ./scripts/session14-env.ps1
$env:TRACEFORGE_JOB_KEY=$private.keys.alpha.manage
Push-Location examples/agent-demo
.venv/Scripts/python.exe -m security_eval.runner --tenant alpha --output ../../artifacts/session14/adversarial-alpha.json
Pop-Location
$env:TRACEFORGE_JOB_KEY=$private.keys.beta.manage
Push-Location examples/agent-demo
.venv/Scripts/python.exe -m security_eval.runner --tenant beta --output ../../artifacts/session14/adversarial-beta.json
Pop-Location
examples/agent-demo/.venv/Scripts/python.exe scripts/session14/verify_links.py
```

For secure Next, set `TRACEFORGE_REQUIRE_AUTH=1`, private `TRACEFORGE_SESSION_SECRET` to the generated `session_secret`, `TRACEFORGE_API_URL=https://127.0.0.1:8444`, and `NODE_EXTRA_CA_CERTS` to the generated certificate path **on the server only**, then build/start `web`. Login at `/login` using a dedicated read key. Never use `NEXT_PUBLIC_*` for secrets or disable certificate validation. `/security` shows the actual owner's immutable reports and trace links. For the Python HTTP demo set `TRACEFORGE_AGENT_REQUIRE_AUTH=1` after dot-sourcing the environment helper and run `uvicorn agent_demo.app:app --host 127.0.0.1 --port 8084` from the example directory; use a matching read credential for requests. Secure mode uses per-tenant fixture agents; paid/live execution is not enabled.

To return to explicit Legacy compatibility mode, stop `secure-gateway collector-alpha collector-beta tenant-alpha tenant-beta` using both Compose files, then `docker compose up -d --build --wait`. Restart Next without secure-mode environment. Do not run both public unauthenticated host backends and the secure boundary and call that deployment protected. The session ended in Legacy mode for compatibility checks; secure services are stopped but their namespaced data is retained.
