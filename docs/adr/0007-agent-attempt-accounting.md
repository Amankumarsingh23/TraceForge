# ADR 0007 — Bounded agent execution and canonical attempt accounting

Status: accepted for Session 7, 2026-11-13. Prior delivery/security limitations remain.

## Context

Agent execution introduces retrieval, logical model calls, retries and application-executed tools. Operational success cannot establish answer quality. Usage can be absent after a timeout, duplicated by telemetry replay, or repeated on parent spans. A fabricated zero or double-counted parent would produce misleading accounting.

## Decision

Use Python 3.13 with pinned OTel SDK/exporters 1.45.2, FastAPI 0.162.5, Uvicorn 0.54.0 and HTTPX 0.28.1. Lock resolved transitive versions. HTTPX implements the supported nonstreaming OpenAI-compatible Chat Completions protocol directly, without SDK retries. The app owns bounded retries. Default fixture mode and explicitly enabled live mode never silently replace each other. Live checks require separate billable authorization; this session performs none.

Instrument actual execution, not a prewritten span graph. BM25 ranks a stable 12-document synthetic corpus; only two retrieved documents become model context. A model proposes `get_order_status`; application code validates the allowlist and exact JSON schema before entering the synthetic read-only lookup. Bounded HTTP/body/admission/attempt/tool/execution limits and maximum calls/tools/steps constrain loops. Fixed errors avoid exporting provider bodies or user content.

Pin the development convention to [`semantic-conventions-genai@e6658f14c9d9ea0760825f029cb82e7673f48fab`](https://github.com/open-telemetry/semantic-conventions-genai/tree/e6658f14c9d9ea0760825f029cb82e7673f48fab/docs/gen-ai). Use invoke_agent/retrieval/execute_tool INTERNAL spans and logical chat CLIENT spans. Logical chat covers retries per the convention; diagnostic attempt CLIENT children omit `gen_ai.operation.name`. Deliberate adaptation: usage fields occur **only on diagnostic attempt children**, marked `traceforge.usage.accounting=attempt`. Agent and logical spans never repeat usage totals. Consumers must use this explicit accounting boundary; generic GenAI tools expecting counters only on logical inference spans may need an adapter. Preserve the revision and migration decision before changing this contract.

Provider-returned input/output/cache-read/reasoning fields map to typed GenAI usage attributes. Missing values remain absent, invalid subset claims invalidate usage. Cached input is part of input, reasoning part of output. Fixture values have `synthetic_fixture` provenance; live normalized values have `provider_reported` provenance. No tokenizer estimates masquerade as provider counts.

Pricing is a bounded versioned provider/response-model lookup. Fixture rates are hypothetical. Missing real pricing stays unknown. Store rate/currency/source/version/hypothetical metadata; Python Decimal estimates and Go independently recomputes per-attempt rational cost with positive ROUND_HALF_UP to nano-USD. A complete estimate requires known input/output and any differentiated cached count for every observed attempt. Known subtotals remain distinct from full estimates. These are estimated provider costs, never invoice totals.

Derive `/agent-summary` from the existing bounded canonical detail forest, without new ingestion services, schemas or precomputed AI tables. Deduplication happens before accounting. Group run IDs; expose only approved fields and span IDs, preserve diagnostics and unknown completeness. The thin trace-detail panel selects existing spans/log context. No evaluation scores or experiment dashboard.

Source minimization omits prompts, text, answers, tool arguments/results and credentials. The generic redactor narrowly permits four exact bounded integer usage fields; string/credential values retain existing redaction. Local unauthenticated deployment remains loopback-only; production authentication and tenant enforcement are future requirements, not implied by run IDs.

## Consequences and alternatives

Deterministic fixtures permit reproducible accounting/failure tests without billing but cannot establish live inference, answer quality or real billing. Explicit live configuration and the opt-in one-attempt smoke retain a reviewable verification path. Lexical retrieval avoids introducing a vector service solely for instrumentation. Read-time projection costs a bounded reconstruction per request and inherits the 1000-physical-row default cap, conflict selection and non-snapshot limitations. Missing telemetry can always make observed totals incomplete even when all visible attempts report usage.

References: [OpenAI function calling](https://developers.openai.com/api/docs/guides/function-calling), [Chat Completions reference](https://developers.openai.com/api/reference/python/resources/chat/subresources/completions/methods/create), pinned [client inference conventions](https://github.com/open-telemetry/semantic-conventions-genai/blob/e6658f14c9d9ea0760825f029cb82e7673f48fab/docs/gen-ai/client-inference.md). Reviewed against current primary documentation on 2026-11-13; fixture price URL is an intentional example.invalid identifier, not a real pricing source.
