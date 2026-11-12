# Session 05 — Interactive trace explorer and span investigation

Date: 2026-11-12 (Asia/Calcutta). Starting checkpoint: `57536d5`.

## Scope and exact changed files

Added a local-only Next.js App Router frontend over the existing Go/ClickHouse APIs. No backend business logic, ingestion, database schema, Collector configuration, credentials or historical notes were changed. No remote push or Session 6 implementation.

Modified: `README.md`, `ARCHITECTURE.md`, `docs/resume-evidence.md`.

Added:

```text
docs/adr/trace-explorer-ui.md
docs/engineering-journal/session-05.md
scripts/session05-demo.ps1
web/.gitignore
web/package.json
web/package-lock.json
web/tsconfig.json
web/next-env.d.ts
web/next.config.ts
web/eslint.config.mjs
web/vitest.config.ts
web/playwright.config.ts
web/src/test-setup.ts
web/src/app/layout.tsx
web/src/app/globals.css
web/src/app/page.tsx
web/src/app/traces/page.tsx
web/src/app/traces/[traceId]/page.tsx
web/src/app/api/traces/route.ts
web/src/app/api/traces/[traceId]/route.ts
web/src/components/search-explorer.tsx
web/src/components/detail-explorer.tsx
web/src/components/waterfall.tsx
web/src/components/span-inspector.tsx
web/src/components/diagnostics.tsx
web/src/components/error-panel.tsx
web/src/components/investigation.test.tsx
web/src/lib/contracts.ts
web/src/lib/timeline.ts
web/src/lib/timeline.test.ts
web/src/lib/client.ts
web/src/lib/client.test.ts
web/src/lib/search.ts
web/src/lib/proxy.ts
web/src/lib/proxy.test.ts
web/src/lib/test-fixtures.ts
web/tests/live.spec.ts
```

A new ignored `learning-notes/session-05-trace-explorer.md` teaches this implementation. It is excluded from staging, along with `.env`, artifacts, dependencies and build outputs.

## Audit, contracts and architecture

Inspected HEAD/status/AGENTS, README, architecture, Session 3/4 journals, `internal/traces/model.go`, reconstruction, actual query validation/cursors/errors and integration fixtures. Captured real historical detail and search responses under ignored `artifacts/session05/`. The initial recent search was honestly empty. Historical trace data remained available.

Installed Node is 24.22.1 LTS. Bundled Node lacked npm on PATH; downloaded npm 12.2.2 from the official registry to the existing local tools directory and verified its SHA-512 against registry integrity. Consulted official Next/Node/Vitest/Playwright docs and exact npm package engine/peer metadata. Pins and reasons are in ADR 0005. `npm ci` completed successfully with the final lockfile (256 packages). No canary or forced peer resolution is used.

Browser → same-origin Next `/api/traces` → Go `/api/v1/traces` → ClickHouse/reconstruction. `TRACEFORGE_API_URL` is server-only and defaults to loopback 8082; the verified Node process runs on the Windows host, not in Docker. Both frontend start scripts bind loopback 3000. The proxy constructs only approved paths, validates IDs/query syntax, delegates exact opaque cursor binding to Go, rejects redirects, forwards no browser credentials, preserves statuses/response bytes, caps bodies at 16MiB and applies 5s deadlines. Client requests have a 7s deadline, abort and generation guards.

Actual search fields are `from`, `to`, `service`, `status=error`, `limit`, `cursor`. A window is at most 24 hours; the UI default is one hour. Summary counts/services/timestamps concern observations starting in that window. Search filtering never prunes independently fetched full detail. Applied absolute filters travel with pagination; a new search/clear resets cursor history. Pagination remains non-snapshot. No invented operation/duration filters or automatic updates.

Detail consumes the supplied roots/orphans/disconnected forest and backend diagnostics. It does not rebuild parent relationships, deduplicate independently or infer request outcome. Iterative traversal with a visited-ID guard avoids recursive rendering failure; collapse hides descendants only. Physical rows and canonical logical bars stay distinct. Missing-parent/cycle/conflict/timing warnings and unknown completeness remain visible. React renders arbitrary telemetry as escaped text, using only already-redacted backend attributes.

## Waterfall precision and UX

Real timestamps are precise RFC3339Nano strings. Existing `duration_ns` and `relative_start_ns` JSON numbers remain unchanged for compatibility but are not consumed by frontend timing logic. Parse whole seconds with Date, append untouched fractional nanoseconds using BigInt, then subtract/format/calculate ratios with BigInt before a bounded percentage conversion. This preserves one-nanosecond differences at modern Unix epochs. No API field change was necessary.

The backend observed envelope supplies the common earliest-start/latest-end axis. Bars use relative start and recorded end-minus-start duration; overlapping child intervals are never added. Zero/tiny spans use a three-pixel minimum marker while numeric labels remain truthful. Out-of-envelope/reversed intervals are flagged and retain raw times. Display indentation caps at 12 levels with deeper depth labeled. The inspector shows trace/span/parent IDs, operation/service/kind, raw UTC start/end, duration, status/message, scope, attributes, events and links; selection can be changed/cleared. Copy has a visible success/failure message. Keyboard buttons, focus outlines, reduced-motion handling and responsive scroll layouts are included.

## Executed checks and actual results

- `npm ci`: passed against the final lockfile.
- `npm run lint`: passed with ESLint 10 and directly compatible Next/React Hooks/typescript-eslint plugins.
- `npm run typecheck`: passed; includes Next type generation so a clean checkout does not require prior dev/build output.
- `npm test`: **28 passing tests in 4 files**. Covers timing/precision/zero/tiny/parallel/anomalous intervals, stable colors, iterative/collapsed/multiple-root/orphan/cyclic/deep forests, diagnostics, safe text/error labels, stale successes/failures, cursor/reset, proxy path/query/status/body/error bounds and visible error feedback.
- `npm run build`: passed for the actual App Router pages and Node API handlers. Browser verification used `next start`, not a mock or only a development build.
- Final Playwright Chromium: **12 passed in 18.4s**, including real payment search/detail/inspector, measured normal/delay comparison, error status/exception, historical/replay counts, anomaly forest, 1000 spans, actual 400/404/413, empty/invalid search, pagination reset, mobile controls, keyboard selection/clear, real ClickHouse stop and recovery. All executed with `TRACEFORGE_BROWSER_OUTAGE=1`; none skipped in the accepted run.
- Existing `go test ./...`, `go vet ./...`, `go build ./...`: passed. Existing 119-case Go suite preserved; no new Linux race execution claimed because Go source is unchanged. The JS dependency `flatted` also contains an unrelated Go package discovered by `./...`; it reports no tests and is not tracked.
- Existing live `scripts/session03-smoke.ps1 -SkipDatabaseFailure`: passed storage/normal/delay/failure/replay/graph/search/paging/detail-limit/recovery regression. Browser suite separately exercised the actual database outage.
- PowerShell script parsing and `docker compose config --quiet`: passed. Final backend readiness recovered; historical five-span trace remains available.
- `npm audit --omit=dev`: **0 vulnerabilities**. Full `npm audit` exits 1 with **10 moderate dependency findings** rooted in the development-only minimatch glob amplification advisory [GHSA-g3hv-75wg-m249](https://github.com/advisories/GHSA-g3hv-75wg-m249), via lint/parser tooling. No forced downgrade/major override was applied. This is a known tooling limitation, not a clean full-audit claim.

## Real demo evidence

Final preparation reran real HTTP checkouts and waited for persisted five-span details:

| Scenario | Trace ID | Gateway HTTP | payment.process interval | Stored status/events |
| --- | --- | ---: | ---: | --- |
| Normal | `fbec132642b85173ee314ba52e2cb2b5` | 200 | 4263000 ns (4.263ms) | UNSET; 0 events |
| Delay 500ms | `fb24dd5139ea6ef72ecd4654fdcd1433` | 200 | 500617500 ns (500.6175ms) | UNSET; 0 events |
| Failed payment | `eab99d116ecf262fd9871c2c522aad45` | 502 | 0 ns | ERROR; `synthetic payment failure`; 1 exception event |

Observed envelopes were 37641500 ns, 502229400 ns and 1665700 ns respectively. The browser compared the actual payment intervals and verified inspector timestamps/IDs/durations against the API. These are individual local traces, not capacity or causality measurements. An earlier normal trace recorded zero duration; zero markers/labels stayed honest.

Other real persisted identities used:

- Historical `656f80607b005aa65927df202991a837`: five physical/logical spans; gateway/payment preserved.
- Replay `cc7a3f866780f0c87af521eb135a685e`: ten physical rows, five logical bars, five exact duplicates.
- Anomaly `879f68357175e32e0e0b9404dbc2f8e1`: eight physical rows, six canonical bars, one exact duplicate, one conflicting group, two roots, one missing parent/orphan, one cycle and one timing warning. All six remain visible.
- Oversized `3832d124f226fab3a03e7a588d574039`: 1001 physical rows; actual Go 413 shown without a partial waterfall.
- Supported large trace `c380441d89fb1e21000000000000002b`: 1000 logical spans independently counted by Go detail and rendered/selected/collapsed in Chromium. The generator report deliberately disables SQL accounting; this is API-backed count verification, not an independent unique SQL benchmark.
- Pagination setup prefix `90e737919f7418a5`: twelve one-span traces acknowledged through actual OTLP, ensuring enough identities on a clean database. No production UI fixture path exists.

## Browser artifacts and reproduction

Raw responses/IDs/logs are ignored local artifacts. Accepted browser log: `artifacts/session05/browser-accepted.log`. Screenshots inspected for desktop/mobile quality:

```text
artifacts/session05/screenshots/search-payment.png
artifacts/session05/screenshots/delayed-waterfall-inspector.png
artifacts/session05/screenshots/failed-span.png
artifacts/session05/screenshots/anomaly-forest.png
artifacts/session05/screenshots/oversized-trace.png
artifacts/session05/screenshots/database-unavailable.png
artifacts/session05/screenshots/mobile-detail.png
```

These are actual Chromium pages with persisted API data. The local preview URL was sent to the Codex browser panel; its open request returned queued, so panel rendering is not additional verification. Screenshots/Playwright establish the observed browser behavior. README provides exact PowerShell run/install/check/demo/browser commands, including the portable npm fallback. Browser test searches use the demos' saved absolute UTC window so later reruns do not depend on their remaining in the one-hour default.

## Setbacks resolved and remaining limits

The initial lint pass rejected synchronous effect resets; moved search resets to event handlers, supplied initial server filters, and keyed detail requests on identity/refresh. Initial browser selectors matched Next's separate route-announcer alert; narrowed them to the actual error section. A mobile sticky-column improvement initially covered the timeline button; adjusted mobile grid widths, then the real pointer and keyboard tests passed. After a long pause, demos aged out of the one-hour search default; bound test searches to saved absolute times, refreshed demos and added twelve explicit pagination traces. The product's correct recent-window behavior was preserved.

Initial dependency selection exposed legacy aggregate Next lint-plugin peers limited to ESLint 9. Briefly tried compatible 9, observed its deprecation, then selected direct compatible plugins with ESLint 10. Rebuilt the lockfile without force/legacy-peer flags and verified clean `npm ci`. A mistaken npm invocation from repository root failed harmlessly; no root package file was created. An initial combined server-start command and optional generated-cache deletion were rejected by tool policy; ordinary local startup and a reversible ignored-cache rename sufficed. No user credentials were rewritten or printed.

Production authentication/TLS/tenant isolation remain future work; local-only is not a security boundary for hostile local processes. Redaction remains the existing backend policy, not a comprehensive privacy guarantee. Completeness is unknown, summaries are window-scoped, late data can move cursor pages, and rendering a long span does not prove critical path or root cause. No cross-browser, screen-reader certification, remote CI, long rendering soak or sustained-performance claim. Runtime audit is clean but full development-tool audit has the advisory above. The preview process is stopped after final local QA; use the documented command to run it again. Base backend/demo services are preserved.

Session 6 may start structured logs/correlation after user review. No logs, metric dashboard, service map, alerting, AgentOps/RAG, authentication or SSE/WebSocket work was started. Suggested local commit: `feat: add precise trace explorer and span investigation UI`.
