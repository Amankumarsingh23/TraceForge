import asyncio
import json
from dataclasses import replace

import httpx
import pytest
from fastapi.testclient import TestClient
from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export import SimpleSpanProcessor
from opentelemetry.sdk.trace.export.in_memory_span_exporter import InMemorySpanExporter

from agent_demo.accounting import Pricing, estimate_nano, normalize_usage
from agent_demo.agent import Agent, AgentFailure, Limits
from agent_demo.app import create_app
from agent_demo.providers import (
    CompatibleProvider,
    FixtureProvider,
    ProviderFailure,
    parse_response,
)
from agent_demo.retrieval import Retriever
from agent_demo.telemetry import Telemetry


@pytest.fixture
def engine():
    exporter = InMemorySpanExporter()
    provider = TracerProvider()
    provider.add_span_processor(SimpleSpanProcessor(exporter))
    agent = Agent(FixtureProvider(), Telemetry(provider=provider))
    yield agent, exporter
    provider.shutdown()


@pytest.mark.asyncio
async def test_actual_tool_and_canonical_usage(engine):
    agent, exporter = engine
    result = await agent.run("Order status DEMO-1002", "a" * 32)
    assert "in_transit" in result["answer"]
    assert agent.tool.lookups == 1
    spans = exporter.get_finished_spans()
    attempts = [s for s in spans if s.attributes["traceforge.operation.kind"] == "attempt"]
    assert len(attempts) == 2
    assert sum(s.attributes["gen_ai.usage.input_tokens"] for s in attempts) == 3000
    assert sum(s.attributes["traceforge.cost.nano_units"] for s in attempts) == 2300000
    assert all("gen_ai.usage.input_tokens" not in s.attributes for s in spans if s not in attempts)
    root = next(s for s in spans if s.name.startswith("invoke_agent"))
    assert all(s.parent is not None for s in spans if s != root)
    assert len({s.context.trace_id for s in spans}) == 1
    by_id = {s.context.span_id: s for s in spans}
    assert all(by_id[s.parent.span_id].name.startswith("chat ") for s in attempts)
    assert all(
        s.parent.span_id == root.context.span_id
        for s in spans
        if s.attributes["traceforge.operation.kind"] in {"tool", "model", "retrieval", "finalize"}
    )


@pytest.mark.asyncio
@pytest.mark.parametrize(
    "scenario,kind,lookups",
    [
        ("timeout", "provider_timeout", 0),
        ("permanent_error", "provider_permanent_error", 0),
        ("bad_arguments", "malformed_tool_arguments", 0),
        ("unknown_tool", "unknown_tool", 0),
        ("tool_timeout", "tool_timeout", 0),
        ("unknown_order", "unknown_order", 1),
    ],
)
async def test_controlled_failures(engine, scenario, kind, lookups):
    agent, exporter = engine
    with pytest.raises(AgentFailure, match=kind):
        await agent.run("refund policy", "b" * 32, scenario=scenario)
    assert agent.tool.lookups == lookups
    root = next(s for s in exporter.get_finished_spans() if s.name.startswith("invoke_agent"))
    assert root.attributes["error.type"] == kind


@pytest.mark.asyncio
@pytest.mark.parametrize(
    "limits,kind",
    [
        (Limits(steps=1), "max_steps_reached"),
        (Limits(tool_calls=0), "max_tool_calls_reached"),
        (Limits(model_calls=1), "max_model_calls_reached"),
    ],
)
async def test_loop_bounds(engine, limits, kind):
    with pytest.raises(AgentFailure, match=kind):
        await engine[0].run("status DEMO-1001", "c" * 32, limits=limits)


@pytest.mark.asyncio
async def test_retry_unknown_usage_not_zero(engine):
    agent, exporter = engine
    await agent.run("refund policy", "d" * 32, scenario="retry")
    attempts = [s for s in exporter.get_finished_spans() if s.name == "provider attempt"]
    assert len(attempts) == 2
    assert attempts[0].attributes["traceforge.usage.status"] == "unknown"
    assert "gen_ai.usage.input_tokens" not in attempts[0].attributes
    assert attempts[1].attributes["gen_ai.usage.input_tokens"] == 1200


@pytest.mark.asyncio
@pytest.mark.parametrize("scenario", ["normal", "no_results", "missing_usage"])
async def test_source_minimization(engine, scenario):
    agent, exporter = engine
    marker = "SECRET_SESSION07_MARKER"
    result = await agent.run("refund password=" + marker, "e" * 32, scenario=scenario)
    spans = exporter.get_finished_spans()
    serialized = json.dumps(
        [
            {"attributes": dict(s.attributes), "events": [dict(e.attributes) for e in s.events]}
            for s in spans
        ]
    )
    assert marker not in serialized
    assert result["context_document_ids"] == result["retrieved_document_ids"][:2]
    if scenario == "no_results":
        assert result["retrieved_document_ids"] == []
    if scenario == "missing_usage":
        assert all("gen_ai.usage.input_tokens" not in s.attributes for s in spans)


def test_retrieval_is_deterministic_and_ranked():
    retriever = Retriever()
    hits = retriever.search("refund damaged return", 5)
    assert hits == retriever.search("refund damaged return", 5)
    assert len(hits) > 2
    assert all(h.score > 0 for h in hits)
    assert retriever.search("zzzxqvnomatch", 4) == []
    assert len(retriever.supplied(hits)) == 2


@pytest.mark.parametrize(
    "raw",
    [None, {}, {"prompt_tokens": True}, {"prompt_tokens": -1}, {"completion_tokens": 10000001}],
)
def test_unknown_usage(raw):
    usage = normalize_usage(raw, "provider_reported")
    assert usage.status == "unknown"
    assert estimate_nano(usage, Pricing().entries[0]) is None


def test_cached_reasoning_are_subsets_and_decimal_rounding():
    usage = normalize_usage(
        {
            "prompt_tokens": 100,
            "completion_tokens": 20,
            "prompt_tokens_details": {"cached_tokens": 40},
            "completion_tokens_details": {"reasoning_tokens": 10},
        },
        "provider_reported",
    )
    assert estimate_nano(usage, Pricing().entries[0]) == 74000
    invalid = normalize_usage(
        {"prompt_tokens": 1, "completion_tokens": 2, "prompt_tokens_details": {"cached_tokens": 3}},
        "provider_reported",
    )
    assert invalid.status == "invalid"
    assert estimate_nano(invalid, Pricing().entries[0]) is None
    partial = normalize_usage({"prompt_tokens": 3}, "provider_reported")
    assert partial.status == "partial" and partial.input == 3 and partial.output is None
    assert estimate_nano(partial, Pricing().entries[0]) is None
    rounded = normalize_usage(
        {"prompt_tokens": 1, "completion_tokens": 0, "prompt_tokens_details": {"cached_tokens": 0}},
        "synthetic_fixture",
    )
    assert estimate_nano(rounded, replace(Pricing().entries[0], input_per_million="0.0005")) == 1


def test_http_bounds_and_parent_propagation(engine):
    with TestClient(create_app(engine[0])) as client:
        response = client.post(
            "/ask",
            json={"question": "refund policy"},
            headers={"traceparent": "00-" + "a" * 32 + "-" + "b" * 16 + "-01"},
        )
        assert response.status_code == 200
        assert response.json()["trace_id"] == "a" * 32
        assert len(response.json()["run_id"]) == 32
        assert client.post("/ask", json={"question": "SECRET", "max_steps": 7}).status_code == 422
        large = client.post("/ask", content=b"x" * 8193)
        assert large.status_code == 413
    http = next(s for s in engine[1].get_finished_spans() if s.name == "POST /ask")
    assert http.parent.span_id == int("b" * 16, 16)


def test_evaluation_request_metadata_context_and_no_label_input(engine):
    with TestClient(create_app(engine[0])) as client:
        response = client.post(
            "/ask",
            json={
                "question": "refund eligibility window",
                "top_k": 5,
                "context_limit": 1,
                "output_format": "structured",
                "evaluation_run_id": "a" * 32,
                "evaluation_case_id": "support-001",
            },
        )
        assert response.status_code == 200
        value = response.json()
        assert value["structured_output"]["claims"] and len(value["context_document_ids"]) == 1
        assert set(value["supplied_document_hashes"]) == set(value["context_document_ids"])
        rejected = client.post(
            "/ask", json={"question": "refund", "expected_facts": ["PRIVATE_LABEL"]}
        )
        assert rejected.status_code == 422 and "PRIVATE_LABEL" not in rejected.text
    spans = engine[1].get_finished_spans()
    retrieval = next(s for s in spans if s.name == "retrieval")
    assert retrieval.attributes["traceforge.evaluation.run_id"] == "a" * 32
    assert retrieval.attributes["traceforge.evaluation.case_id"] == "support-001"
    assert not any("PRIVATE_LABEL" in str(s.attributes) for s in spans)


@pytest.mark.asyncio
@pytest.mark.parametrize(
    "status,kind,retryable",
    [
        (401, "provider_permanent_error", False),
        (429, "provider_rate_limited", True),
        (503, "provider_unavailable", True),
    ],
)
async def test_live_adapter_mock_errors(status, kind, retryable):
    provider = CompatibleProvider(
        "https://provider.invalid/v1",
        "fake-test-key",
        "test-model",
        transport=httpx.MockTransport(lambda request: httpx.Response(status, text="SECRET")),
    )
    try:
        with pytest.raises(ProviderFailure) as failure:
            await provider.complete([], 1, 1, "normal")
        assert failure.value.kind == kind
        assert failure.value.retryable == retryable
        assert "SECRET" not in str(failure.value)
    finally:
        await provider.close()


@pytest.mark.asyncio
async def test_live_adapter_mock_payload_and_usage():
    def respond(request):
        body = json.loads(request.content)
        assert body["stream"] is False and body["store"] is False
        assert body["max_completion_tokens"] == 256
        return httpx.Response(
            200,
            json={
                "id": "test-id",
                "model": "actual-model",
                "choices": [{"message": {"content": "answer"}, "finish_reason": "stop"}],
                "usage": {"prompt_tokens": 9, "completion_tokens": 2},
            },
        )

    provider = CompatibleProvider(
        "https://provider.invalid/v1",
        "fake-test-key",
        "test-model",
        transport=httpx.MockTransport(respond),
    )
    try:
        result = await provider.complete([], 1, 1, "normal")
        assert result.response_model == "actual-model"
        assert result.usage.input == 9 and result.usage.cached is None
        assert Pricing().lookup(provider.name, result.response_model) is None
    finally:
        await provider.close()


@pytest.mark.parametrize(
    "raw", [None, [], {}, {"choices": [None]}, {"choices": [{"message": None}]}]
)
def test_malformed_response_safe(raw):
    with pytest.raises(ProviderFailure, match="malformed_provider_response"):
        parse_response(raw, "test", "model", "provider_reported")


@pytest.mark.asyncio
@pytest.mark.parametrize(
    "arguments",
    ['{"order_id":"DEMO-1001","extra":1}', '{"order_id":42}', '{"order_id":"REAL-1001"}', "[]"],
)
async def test_tool_schema_rejects_before_function_entry(engine, arguments):
    class InvalidArguments(FixtureProvider):
        async def complete(self, messages, call, attempt, scenario):
            result = await super().complete(messages, call, attempt, scenario)
            return replace(result, tools=[replace(result.tools[0], arguments=arguments)])

    engine[0].provider = InvalidArguments()
    with pytest.raises(AgentFailure, match="malformed_tool_arguments"):
        await engine[0].run("status DEMO-1001", "f" * 32)
    assert engine[0].tool.lookups == 0


@pytest.mark.asyncio
async def test_cancellation_closes_attempt_and_preserves_unknown_usage(engine):
    class Slow(FixtureProvider):
        async def complete(self, *args):
            await asyncio.sleep(30)

    engine[0].provider = Slow()
    task = asyncio.create_task(engine[0].run("refund policy", "f" * 32))
    await asyncio.sleep(0.02)
    task.cancel()
    with pytest.raises(asyncio.CancelledError):
        await task
    attempt = next(s for s in engine[1].get_finished_spans() if s.name == "provider attempt")
    assert attempt.attributes["traceforge.usage.status"] == "unknown"
    assert attempt.attributes["error.type"] == "request_deadline_or_canceled"
    assert "gen_ai.usage.input_tokens" not in attempt.attributes
