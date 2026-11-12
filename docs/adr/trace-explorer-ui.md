# ADR 0005 — A thin, local trace explorer

Date: 2026-11-12. Status: accepted for Session 5.

## Decision and boundary

Use a small Next.js App Router/React/TypeScript app in `web/`. The browser requests only same-origin `/api/traces` and `/api/traces/{id}`. Node route handlers forward GET to the existing Go `/api/v1/traces` routes. Go remains the sole owner of ClickHouse queries, deduplication, canonical conflict selection, graph repair, query limits and error semantics. No Go contract, database schema, ingestion or Collector change was needed.

The server-only `TRACEFORGE_API_URL` defaults to `http://127.0.0.1:8082`. It must be an HTTP(S) origin without credentials, path, query or fragment. It is deployment configuration, never a browser-supplied destination. The proxy constructs fixed paths; rejects noncanonical trace IDs, unknown/repeated parameters, invalid UTC windows, invalid service/status/page/cursor syntax; and leaves opaque cursor decoding/filter binding to Go. It follows no redirects, forwards no browser cookies/authorization headers and exposes no environment credentials. It preserves upstream statuses and response bytes with `no-store`, bounds responses at 16MiB, and combines cancellation with a five-second upstream deadline. Client fetches have a seven-second deadline. Transport/configuration failures use sanitized structured errors.

Development and production-start scripts bind `127.0.0.1:3000`. The verified arrangement is Windows-host Node → loopback Go API → Compose ClickHouse. No frontend Docker service, wildcard CORS, arbitrary URL proxy, direct database access, production authentication or publishing is introduced.

## Actual contract

Search accepts `from`, `to` (UTC RFC3339Nano, maximum 24 hours), `service`, `status=error`, `limit` (UI 10/20/50/100) and opaque `cursor`. The Go default is the last hour/20 results. Responses include `traces`, `next_cursor`, absolute window, `summary_scope=raw_observations_starting_in_window`, `consistency=non_snapshot`. Summaries contain trace ID, first/last/latest observed timestamps, observed services, unique span count and `has_error_span`. They describe observations starting in the discovery window, not a complete trace. Service/error filters qualify an observed span; detail is loaded independently and retains other services.

Detail exposes `root_spans`, `orphan_spans`, `disconnected_spans`, observed services/envelope and diagnostics. Node properties mirror `internal/traces/model.go`: identity, timestamps, kind/status, attributes, scope, events, links, detached edge and children. Diagnostics report physical rows, unique logical spans, exact duplicates, conflicting groups/variants, root/orphan/missing-parent/cycle counts and IDs, timing warnings and unknown completeness. Errors use `{ "error": { "code": "…", "message": "…" } }`. The UI distinguishes 400/404/413/503/504 and network failures from empty successful search. Unsupported filters, logs and automatic/live refresh remain future work.

## Precision and geometry

Real timestamps are RFC3339 strings with up to nine fractional digits. The existing numeric `duration_ns` and `relative_start_ns` may exceed safe integer precision, so the frontend does not consume them. Parse only whole UTC seconds with `Date`, multiply that integer milliseconds value into BigInt nanoseconds, then append the untouched fractional digits. Reject invalid dates/fractions/zones. All subtraction, duration formatting and ratios use BigInt first. No additive API fields or lossless JSON dependency were needed. The proxy preserves the original byte representation for other consumers.

The common axis uses the backend envelope (earliest observed start/latest observed end). Each bar uses `(start − origin) / envelope` for its position and `(end − start) / envelope` for width. Convert only the bounded integer ratio to a percentage number, at 0.0001 percentage-point precision. Numeric duration labels retain sub-microsecond values; larger units show three truncated decimals, with exact raw start/end strings in the inspector. A three-pixel visual marker keeps zero/tiny spans selectable without changing numeric duration. Out-of-envelope geometry is clipped and flagged; reversed timing is marked, with the recorded negative interval retained in text. No child duration summation, exclusive CPU attribution or critical-path/root-cause inference is performed.

## Graph, state and rendering

Iterative traversal visits the supplied forest in backend order. Span-ID guards terminate accidental cycles/duplicates; the frontend does not rebuild a second parent graph. Roots, direct orphans and detached components remain visible. Collapse hides descendants only, with expand-all and a visible count. Depth indentation is visually capped at 12 levels and deeper depth is labeled. A scroll container keeps the supported trace bounded; 1000 real canonical rows were browser-tested and a 2000-deep malformed traversal is unit-tested. No virtualization dependency was needed.

Service names hash to a fixed color palette. Buttons, tree toggles and inspection work with a keyboard; error spans have text/status and an icon as well as color. React escapes all telemetry. The inspector shows stored backend-redacted attributes/events/links as text; it cannot strengthen the existing redaction policy.

Applied filters and cursor history travel together. Search/clear reset pagination; previous/next preserve exact bounds. Draft fields do not mutate applied requests. AbortController plus generation identity rejects stale successes and failures, even when transport ignores abort. Manual retry/refresh clears old results so backend failures do not appear as successful empty traces. Detail request state remounts on ID/refresh to clear selection/loading state. There is no snapshot-consistency or automatic-update promise.

## Dependency and verification choices

Pinned Next 16.4.6, React/React DOM 19.3.1, TypeScript 5.9.3 and npm 12.2.2; verified on installed Node 24.22.1 LTS. Consulted official [Next installation](https://nextjs.org/docs/app/getting-started/installation), [Node releases](https://nodejs.org/en/about/previous-releases), [Vitest guide](https://vitest.dev/guide/) and [Playwright installation](https://playwright.dev/docs/intro), plus exact npm registry engines/peer metadata. The committed lockfile records transitive resolutions. TypeScript 5.9.3 is within the selected parser's supported range; latest TypeScript 7 was deliberately not selected.

Use ESLint 10 with compatible official Next, React Hooks and typescript-eslint plugins directly. The aggregate `eslint-config-next` currently pulls React/JSX plugins whose peers stop at ESLint 9; keeping it would require unsupported peer overrides or a deprecated ESLint 9. The small explicit flat config avoids both. Vitest/jsdom/Testing Library cover logic/components and Playwright exercises the actual production server/API. Fixtures exist only in tests and explicit synthetic OTLP verification; the running app has no fixture/fallback dataset. Exact executions, setbacks and screenshots are recorded in the Session 5 journal.
