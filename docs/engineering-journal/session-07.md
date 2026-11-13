# Session 07 — Bounded AgentOps execution and canonical accounting

Implemented/verified 2026-11-13 (Asia/Calcutta), starting commit `66c961e`. Scope stops at execution observability. No Session 8 quality metrics, Session 9 experiments/gates, new AI ingestion/table, remote push or billable provider invocation.

## Implementation

- Python 3.13 support API: actual deterministic BM25 (`k1=1.2`, `b=0.75`, document-ID tie break) over 12 synthetic D01–D12 documents in `support-kb-v1`. Defaults retrieve top four and supply only two actual documents to the provider. Prompt/retriever/agent versions accompany metadata. No-results fixture substitutes a single unmatched query, rather than fabricating retrieval spans.
- Explicit fixture adapter and separately enabled HTTPS OpenAI-compatible nonstreaming Chat Completions adapter. The latter is implemented and mock-transport tested, with real execution pending. No fallback, automatic HTTP retries, redirects, ambient proxy use or credential discovery. Fixture answers consume actual context/tool results but are deterministic templates, not model inference.
- Bounded loop: three logical model calls, two tool entries, six orchestration steps, two attempts per call, 256 requested completion tokens/attempt, 20s execution deadline, 5.5s attempt and 100ms tool timeout. Body ≤8192 bytes/read ≤2s; question ≤1000 chars, provider body ≤65536 bytes, four active requests/50ms admission. Caller bounds can only decrease. Maximum six provider attempts. These are work bounds, not measured throughput or exact billing bounds.
- Model proposes tools; the application validates exact schema and allowlist before actual read-only lookup in three synthetic orders. Unknown/malformed tools are rejected before function entry. Order timeout actually cancels a delayed coroutine; unknown order actually consults the store. Tool results feed the next model call.
- Manual SDK spans follow real active parent context: HTTP SERVER → agent INTERNAL → retrieval / logical chat CLIENT → diagnostic attempt CLIENT; actual tool and finalization are agent children. Logical chat encloses retries; attempt-only counters define accounting. W3C parent extraction and trace-correlated safe logs are verified. Provider failure/retry logs contain fixed messages/types, never raw response bodies.
- Python Decimal and Go rational pricing independently compute per-attempt half-up integer nano-USD. Fixture rates/version/source are explicitly hypothetical; live rates absent by default. Cache-read input and reasoning output are subsets; differentiated cache pricing requires its count. Known subtotals and incomplete coverage remain distinct from full observed estimates. Failed attempts with unknown usage do not become zero.
- Go summary shares bounded repository read/reconstruction/query slots/deadlines/response cap. It traverses canonical nodes and groups run IDs; only marked provider-attempt spans contribute accounting. No parents or raw duplicate rows are re-summed. It exposes agent identity/versions, observed envelope duration, roles, outcomes, documents, tool entry, provider/model identities, provenance, usage/cost coverage and diagnostics. Completeness always remains unknown.
- A focused Next trace panel uses one narrow same-origin route, shared request cancellation and existing inspector/waterfall/log selection. React escapes metadata; no client graph reconstruction or broad AgentOps dashboard.

## Dependencies and semantic contract

Installed Python 3.13.15, pip 26.2.1; exact direct pins in `requirements.in`, resolved transitive pins in `requirements.lock`. FastAPI 0.162.5, Uvicorn 0.54.0, HTTPX 0.28.1, OTel SDK/exporter 1.45.2, pytest 9.2.0, pytest-asyncio 1.5.0, Ruff 0.17.5. `pip check` passed. Existing Go/Next/Collector/ClickHouse dependencies were preserved. Official API/convention documentation and package requirements were reviewed; installed SDK imports/export/shutdown were exercised.

GenAI semantics moved into their own repository; pin `open-telemetry/semantic-conventions-genai@e6658f14c9d9ea0760825f029cb82e7673f48fab`, marked development, instead of treating the older moved registry as current. [ADR 0007](../adr/0007-agent-attempt-accounting.md) links primary references and documents the deliberate attempt-counter adaptation. The pinned logical-inference convention normally describes aggregate logical calls; this app keeps all usage on marked diagnostic attempts to make its accounting level unambiguous. Generic GenAI consumers may need an adapter.

## Verification

| Check | Actual result |
| --- | --- |
| Windows Go unit/component suite | 179 cases passed (72 top-level + 107 subtests); `go vet` passed |
| Linux Docker verification | `go vet ./...` and `go test -race ./...` passed; runtime image rebuilt |
| Python | 36 pytest cases passed, Ruff lint/format and pip dependency check passed |
| SDK lifecycle | Trace/log force-flush and shutdown exercised against the local Collector |
| Go integration through actual Python/Collector/ClickHouse | 12 scenarios passed; SQL/API privacy and trace hierarchy/log correlation verified |
| Real physical span replay | Tool trace 9 → 18 physical rows, 9 canonical spans/9 exact duplicates, unchanged run accounting |
| Frontend | 50 tests, ESLint, TypeScript and production build passed |
| Browser | 20 regular scenarios passed; both explicitly enabled ClickHouse outage/recovery scenarios passed separately (22 distinct scenarios total) |
| New browser behavior | Actual stored AgentOps data, span selection, exact correlated tool log, retry/missing usage, desktop/mobile layout; screenshots inspected |
| Live provider | NOT RUN; authorization/credentials/budget not supplied; mock transport evidence only |

Python reports one Starlette TestClient deprecation warning for its HTTPX bridge. It does not fail tests; production provider transport uses HTTPX directly. Initial Windows temporary Go test executables twice returned Access denied; unchanged retries passed. No workaround disabled security controls. The no-results test initially exposed an underscore-separated query that tokenized into common words; corrected to one unmatched term and reran the suites. These failures are not hidden as successful first attempts.

Local reproducibility artifacts (ignored): `artifacts/session07/evidence.json`, `integration.txt`, `go-tests.jsonl`, process logs, screenshots `agent-tool-desktop.png` and `agent-tool-mobile.png`. Smoke setup refuses a non-fixture server both in PowerShell and in the Go integration test before posting any question. Full startup/check/live commands are in the repository and example READMEs. Historical session checks retain their own fixture artifacts.

## Accounting observations

Representative final fixture evidence:

| Scenario | Trace ID | Spans / logs | Observed result |
| --- | --- | --- | --- |
| Policy | `fe2645024e7766eddb87d3860bf61d79` | 6 / 2 | One model call; $0.000800000 hypothetical estimate |
| Actual order tool | `52d7c80a9c621ff6616458be17dbe3a7` | 9 / 3 | Two model calls; 3000/400 usage; $0.002300000; replay to 18 physical rows |
| Recovered retry | `6973ae7f992a65741eca5a069f5e270b` | 7 / 4 | Two attempts, known successful subtotal, unknown full estimate |
| Missing usage | `014e72a765d2a128e8d15514ceb22d50` | 6 / 2 | No fabricated counters or complete estimate |

The actual tool run's API observed envelope was 9837700 ns during verification; this is one recorded fixture execution, not a model latency benchmark. All twelve scenario identities are in the ignored local evidence artifact. Browser screenshots were regenerated against the final stored runs.

Policy: one logical call/attempt, synthetic 1200 input/100 output, 800000 nano-USD estimate ($0.000800000). Order path: two calls/attempts, actual DEMO-1002 lookup, 3000 input/400 output, 2300000 nano-USD ($0.002300000), nine spans and three logs before replay. Retry: one logical call/two attempts; known successful usage 1200/100 and known estimate $0.000800000, but the complete observed estimate is null because the first attempt has unknown usage. Missing-usage run: zero known subtotal, incomplete coverage and null estimate, never a claim of zero actual usage.

Other exercised scenarios: no results, provider timeout (injected, two attempts), permanent provider failure (one attempt), malformed arguments, unknown tool, actual tool timeout, actual unknown order, and step limit. Rejected tools have zero function entries; timeouts/unknown orders mark function entry but no successful outcome. Fixture provider failures model controlled branches; they do not prove a live provider was unavailable. Source tests and actual SQL search show the synthetic `SESSION07_PRIVACY_MARKER` question marker absent from span/log storage and hydrated detail; prompts, answers, tool arguments/results and document text are not exported by the app.

## Limitations

Operational success is not correctness. No Recall@K/MRR/groundedness/citation scoring, labeled dataset, experiment comparison or deployment gate exists. Fixture usage is synthetic, not observed tokenization or billing. Compatible adapter tests validate bounded request/response/error handling against mock HTTP transports, not live authentication, model capability or service pricing. Optional one-attempt paid smoke is implemented but unexecuted. In-memory SDK/Collector/receiver buffers remain finite and non-durable; forced process termination can lose telemetry. Canonical conflicts are deterministic selections, not proof of truth. An observed complete-usage estimate is incomplete if an entire operation was never observed. Loopback operation has no production auth/tenant isolation. No benchmarks or universal performance claims were added.

## Five assessment answers

1. **Retrieval vs supplied context:** record D1–D5 as actual retrieval output and D1–D3 as the subset actually supplied. This separates retrieval behavior from context selection and later allows evaluation of both. HTTP 200 and retrieval success establish operational completion, not factual correctness, groundedness or appropriate citations.
2. **Tool trace and authority:** HTTP → agent; agent children are retrieval, logical chat A (attempt children), execute_tool get_order_status, logical chat B (attempt children), finalization. The model proposes a function name/JSON arguments. Application code validates/authorizes and executes the function, then returns its real result to the model. Allowlist/schema block unintended functions/inputs; timeouts and maximum calls/steps bound hangs and loops. A proposal is not a completed tool execution.
3. **Usage/cost:** input 2000+3000=5000; output 500+1000=1500. Hypothetical cost = 5000/1,000,000×$0.50 + 1500/1,000,000×$2 = **$0.0055**. Parent totals must not be added to child counters. If call B usage is missing, call A gives a known subtotal of 2000 input/500 output ($0.002); expose incomplete coverage and unknown complete cost, rather than assuming B used zero tokens.
4. **Retries:** there are two actual model attempts. OTLP export retry sends telemetry again and does not itself call a model. Attempt 1 usage remains unknown after timeout; attempt 2 reports its available usage. Canonical span deduplication prevents exported/replayed rows from inflating usage. Physical duplicates and unknown completeness remain visible; delivery guarantees have not improved merely because the query deduplicates.
5. **Conventions/privacy/truthful claims:** shared GenAI vocabulary makes operation/provider/model/tool/usage metadata interpretable across tools, while pinned development semantics prevent silent field/meaning drift. Capture bounded IDs, versions, ranks, supplied context IDs, durations, statuses, reported counters, provenance and price versions. Omit raw prompts/answers/documents/tool payloads/credentials by default and retain redaction/access-control requirements. Fixture tests show deterministic engineering behavior; they cannot establish live inference, actual billing or answer quality. Those need separate live evidence and later quality evaluation.
