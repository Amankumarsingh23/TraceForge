import asyncio
import hashlib
import json
import re
from contextvars import ContextVar
from dataclasses import dataclass

from opentelemetry.trace import SpanKind

from .accounting import Pricing, Usage, estimate_nano
from .constrained import CONTRACT, decode
from .providers import ProviderFailure
from .retrieval import DATA, PROMPT_VERSION, RETRIEVER_VERSION, Retriever
from .telemetry import CONVENTION

EVALUATION = ContextVar("evaluation", default={})


class AgentFailure(Exception):
    def __init__(self, kind):
        super().__init__(kind)
        self.kind = kind


@dataclass(frozen=True)
class Limits:
    model_calls: int = 3
    tool_calls: int = 2
    steps: int = 6
    attempts: int = 2


class OrderTool:
    def __init__(self):
        self.orders = json.loads((DATA / "orders.json").read_text(encoding="utf-8"))
        self.lookups = 0

    async def execute(self, order_id, delay=False):
        if delay:
            await asyncio.sleep(0.25)
        self.lookups += 1
        value = self.orders.get(order_id)
        if value is None:
            raise AgentFailure("unknown_order")
        return {"order_id": order_id, **value}


class Agent:
    def __init__(self, provider, telemetry, pricing=None, retriever=None, tool=None):
        self.provider, self.telemetry = provider, telemetry
        self.pricing = pricing or Pricing()
        self.retriever = retriever or Retriever()
        self.tool = tool or OrderTool()
        self.prompt = (DATA / "prompt-v1.txt").read_text(encoding="utf-8")

    def base(self, run_id, role):
        return {
            **EVALUATION.get(),
            "traceforge.run.id": run_id,
            "traceforge.operation.kind": role,
            "traceforge.provider.mode": self.provider.mode,
            "traceforge.prompt.version": PROMPT_VERSION,
            "traceforge.corpus.version": self.retriever.version,
            "traceforge.retriever.version": RETRIEVER_VERSION,
            "traceforge.agent.version": "support-agent-v1",
            "traceforge.gen_ai.convention": CONVENTION,
            "gen_ai.agent.name": "support-agent",
            "gen_ai.prompt.name": "synthetic-support",
            "gen_ai.prompt.version": PROMPT_VERSION,
        }

    def record_usage(self, span, usage, actual_model):
        span.set_attribute("traceforge.usage.accounting", "attempt")
        span.set_attribute("traceforge.usage.source", usage.source)
        span.set_attribute("traceforge.usage.status", usage.status)
        for key, value in {
            "gen_ai.usage.input_tokens": usage.input,
            "gen_ai.usage.output_tokens": usage.output,
            "gen_ai.usage.cache_read.input_tokens": usage.cached,
            "gen_ai.usage.reasoning.output_tokens": usage.reasoning,
        }.items():
            if value is not None:
                span.set_attribute(key, value)
        price = self.pricing.lookup(self.provider.name, actual_model)
        if price:
            for key, value in {
                "currency": price.currency,
                "input_per_million": price.input_per_million,
                "output_per_million": price.output_per_million,
                "cached_input_per_million": price.cached_input_per_million,
                "version": price.version,
                "source": price.source,
                "hypothetical": price.hypothetical,
            }.items():
                if value is not None:
                    span.set_attribute("traceforge.pricing." + key, value)
        cost = estimate_nano(usage, price)
        if cost is not None:
            span.set_attribute("traceforge.cost.nano_units", cost)
        return cost

    async def run(
        self,
        question,
        run_id,
        top_k=4,
        scenario="normal",
        limits=Limits(),
        context_limit=2,
        output_format="text",
        evaluation=None,
    ):
        token = EVALUATION.set(evaluation or {})
        try:
            return await self._execute(
                question, run_id, top_k, scenario, limits, context_limit, output_format
            )
        finally:
            EVALUATION.reset(token)

    async def _execute(
        self, question, run_id, top_k, scenario, limits, context_limit, output_format
    ):
        if (
            not 1 <= limits.model_calls <= 3
            or not 0 <= limits.tool_calls <= 2
            or not 1 <= limits.steps <= 6
            or not 1 <= limits.attempts <= 2
        ):
            raise AgentFailure("invalid_limits")
        if self.provider.mode == "live" and scenario != "normal":
            raise AgentFailure("fixture_scenario_in_live_mode")
        base = self.base(run_id, "agent") | {"gen_ai.operation.name": "invoke_agent"}
        with self.telemetry.span("invoke_agent support-agent", base) as agent_span:
            try:
                return await self._run(
                    question,
                    run_id,
                    top_k,
                    scenario,
                    limits,
                    agent_span,
                    context_limit,
                    output_format,
                )
            except asyncio.CancelledError:
                self.telemetry.fail(agent_span, "request_deadline_or_canceled")
                self.telemetry.event(
                    "agent execution failed", run_id, "request_deadline_or_canceled"
                )
                raise
            except AgentFailure as error:
                self.telemetry.fail(agent_span, error.kind)
                self.telemetry.event("agent execution failed", run_id, error.kind)
                raise
            except Exception:
                self.telemetry.fail(agent_span, "internal_error")
                self.telemetry.event("agent execution failed", run_id, "internal_error")
                raise AgentFailure("internal_error") from None

    async def _run(
        self, question, run_id, top_k, scenario, limits, agent_span, context_limit, output_format
    ):
        with self.telemetry.span(
            "retrieval",
            self.base(run_id, "retrieval")
            | {
                "gen_ai.operation.name": "retrieval",
                "gen_ai.data_source.id": "support-kb",
                "traceforge.retrieval.top_k": top_k,
            },
        ) as span:
            hits = self.retriever.search(
                "zzzxqvnomatch" if scenario == "no_results" else question, top_k
            )
            documents = self.retriever.supplied(hits, context_limit)
            ids = [h.document.id for h in hits]
            context_ids = [d.id for d in documents]
            span.set_attributes(
                {
                    "traceforge.retrieval.document_ids": ids,
                    "traceforge.retrieval.ranks": list(range(1, len(hits) + 1)),
                    "traceforge.retrieval.scores": [h.score for h in hits],
                    "traceforge.context.document_ids": context_ids,
                    "traceforge.outcome": "empty" if not hits else "success",
                }
            )
            self.telemetry.event("retrieval complete", run_id)
        messages = [
            {
                "role": "system",
                "content": self.prompt
                + ("\n" + CONTRACT if output_format == "structured" else "")
                + "\nCONTEXT_JSON:\n"
                + json.dumps([{"id": d.id, "title": d.title, "text": d.text} for d in documents]),
            },
            {"role": "user", "content": question},
        ]
        steps, tool_calls = 0, 0

        def step():
            nonlocal steps
            steps += 1
            if steps > limits.steps:
                raise AgentFailure("max_steps_reached")

        for call in range(1, limits.model_calls + 1):
            step()
            model_attributes = self.base(run_id, "model") | {
                "gen_ai.operation.name": "chat",
                "gen_ai.provider.name": self.provider.name,
                "gen_ai.request.model": self.provider.model,
                "traceforge.model.call_index": call,
                "traceforge.context.document_ids": context_ids,
                "gen_ai.request.stream": False,
                "gen_ai.request.max_tokens": 256,
            }
            with self.telemetry.span(
                "chat " + self.provider.model, model_attributes, SpanKind.CLIENT
            ) as model_span:
                result = None
                for attempt in range(1, limits.attempts + 1):
                    error = None
                    attrs = self.base(run_id, "attempt") | {
                        "gen_ai.provider.name": self.provider.name,
                        "gen_ai.request.model": self.provider.model,
                        "traceforge.model.call_index": call,
                        "traceforge.provider.attempt": attempt,
                    }
                    with self.telemetry.span(
                        "provider attempt", attrs, SpanKind.CLIENT
                    ) as attempt_span:
                        try:
                            if len(json.dumps(messages).encode()) > 16000:
                                raise ProviderFailure("model_input_limit")
                            result = await asyncio.wait_for(
                                self.provider.complete(messages, call, attempt, scenario),
                                timeout=5.5,
                            )
                        except TimeoutError:
                            error = ProviderFailure("provider_timeout", True)
                        except ProviderFailure as failure:
                            error = failure
                        except asyncio.CancelledError:
                            self.record_usage(
                                attempt_span,
                                Usage(
                                    None,
                                    None,
                                    None,
                                    None,
                                    "synthetic_fixture"
                                    if self.provider.mode == "fixture"
                                    else "provider_reported",
                                    "unknown",
                                ),
                                None,
                            )
                            self.telemetry.fail(attempt_span, "request_deadline_or_canceled")
                            self.telemetry.fail(model_span, "request_deadline_or_canceled")
                            raise
                        if error:
                            self.record_usage(
                                attempt_span,
                                Usage(
                                    None,
                                    None,
                                    None,
                                    None,
                                    "synthetic_fixture"
                                    if self.provider.mode == "fixture"
                                    else "provider_reported",
                                    "unknown",
                                ),
                                None,
                            )
                            self.telemetry.fail(attempt_span, error.kind)
                            self.telemetry.event("provider failure", run_id, error.kind)
                        else:
                            if result.response_model:
                                attempt_span.set_attribute(
                                    "gen_ai.response.model", result.response_model
                                )
                                model_span.set_attribute(
                                    "gen_ai.response.model", result.response_model
                                )
                            if result.response_id:
                                attempt_span.set_attribute("gen_ai.response.id", result.response_id)
                                model_span.set_attribute("gen_ai.response.id", result.response_id)
                            attempt_span.set_attribute(
                                "gen_ai.response.finish_reasons", [result.finish_reason]
                            )
                            model_span.set_attribute(
                                "gen_ai.response.finish_reasons", [result.finish_reason]
                            )
                            self.record_usage(attempt_span, result.usage, result.response_model)
                            attempt_span.set_attribute("traceforge.outcome", "success")
                    if error:
                        if error.retryable and attempt < limits.attempts:
                            self.telemetry.event("model retry", run_id, error.kind)
                            await asyncio.sleep(0.05 * attempt)
                            continue
                        self.telemetry.fail(model_span, error.kind)
                        raise AgentFailure(error.kind)
                    break
                if result.finish_reason not in {"stop", "tool_calls"}:
                    self.telemetry.fail(model_span, "incomplete_provider_response")
                    raise AgentFailure("incomplete_provider_response")
                model_span.set_attribute("traceforge.outcome", "success")
            if result.tools:
                messages.append(
                    {
                        "role": "assistant",
                        "content": result.content,
                        "tool_calls": [
                            {
                                "id": t.id,
                                "type": "function",
                                "function": {"name": t.name, "arguments": t.arguments},
                            }
                            for t in result.tools
                        ],
                    }
                )
                for request in result.tools:
                    step()
                    if tool_calls >= limits.tool_calls:
                        raise AgentFailure("max_tool_calls_reached")
                    tool_calls += 1
                    tool_attributes = self.base(run_id, "tool") | {
                        "gen_ai.operation.name": "execute_tool",
                        "gen_ai.tool.name": "get_order_status"
                        if request.name == "get_order_status"
                        else "unapproved_tool",
                        "traceforge.tool.executed": False,
                    }
                    with self.telemetry.span(
                        "execute_tool " + tool_attributes["gen_ai.tool.name"], tool_attributes
                    ) as tool_span:
                        try:
                            if request.name != "get_order_status":
                                raise AgentFailure("unknown_tool")
                            try:
                                arguments = json.loads(request.arguments)
                            except (ValueError, TypeError):
                                raise AgentFailure("malformed_tool_arguments") from None
                            if (
                                not isinstance(arguments, dict)
                                or set(arguments) != {"order_id"}
                                or not isinstance(arguments["order_id"], str)
                                or not re.fullmatch(r"DEMO-\d{4}", arguments["order_id"])
                            ):
                                raise AgentFailure("malformed_tool_arguments")
                            tool_span.set_attribute("traceforge.tool.executed", True)
                            value = await asyncio.wait_for(
                                self.tool.execute(
                                    arguments["order_id"], delay=scenario == "tool_timeout"
                                ),
                                timeout=0.1,
                            )
                            tool_span.set_attribute("traceforge.outcome", "success")
                            self.telemetry.event("tool completed", run_id)
                        except TimeoutError:
                            self.telemetry.fail(tool_span, "tool_timeout")
                            raise AgentFailure("tool_timeout") from None
                        except AgentFailure as failure:
                            self.telemetry.fail(tool_span, failure.kind)
                            raise
                        except asyncio.CancelledError:
                            self.telemetry.fail(tool_span, "request_deadline_or_canceled")
                            raise
                    messages.append(
                        {"role": "tool", "tool_call_id": request.id, "content": json.dumps(value)}
                    )
                continue
            with self.telemetry.span("finalize answer", self.base(run_id, "finalize")) as span:
                span.set_attribute("traceforge.outcome", "success")
                self.telemetry.event("agent execution completed", run_id)
            agent_span.set_attribute("traceforge.outcome", "success")
            return {
                "answer": result.content,
                "retrieved_document_ids": ids,
                "context_document_ids": context_ids,
                "mode": self.provider.mode,
                "provider": self.provider.name,
                "requested_model": self.provider.model,
                "response_model": result.response_model,
                "structured_output": decode(result.content)
                if output_format == "structured"
                else None,
                "supplied_document_hashes": {
                    d.id: hashlib.sha256(d.text.encode()).hexdigest() for d in documents
                },
            }
        raise AgentFailure("max_model_calls_reached")
