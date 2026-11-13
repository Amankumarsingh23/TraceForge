import asyncio
import os
import uuid
from contextlib import asynccontextmanager
from pathlib import Path
from typing import Literal

from fastapi import FastAPI
from fastapi.exceptions import RequestValidationError
from fastapi.responses import JSONResponse
from opentelemetry.propagators.textmap import DefaultGetter
from opentelemetry.trace import SpanKind
from opentelemetry.trace.propagation.tracecontext import TraceContextTextMapPropagator
from pydantic import BaseModel, ConfigDict, Field
from starlette.requests import Request

from .accounting import Pricing
from .agent import Agent, AgentFailure, Limits
from .providers import CompatibleProvider, FixtureProvider
from .telemetry import Telemetry


class Ask(BaseModel):
    model_config = ConfigDict(extra="forbid", strict=True)
    question: str = Field(min_length=1, max_length=1000)
    top_k: int = Field(default=4, ge=1, le=5)
    context_limit: int = Field(default=2, ge=1, le=3)
    output_format: Literal["text", "structured"] = "text"
    evaluation_run_id: str | None = Field(default=None, pattern=r"^[a-f0-9]{32}$")
    evaluation_case_id: str | None = Field(default=None, pattern=r"^[A-Za-z0-9_-]{1,64}$")
    scenario: Literal[
        "normal",
        "no_results",
        "retry",
        "timeout",
        "permanent_error",
        "missing_usage",
        "bad_arguments",
        "unknown_tool",
        "tool_timeout",
        "unknown_order",
        "max_steps",
    ] = "normal"
    max_model_calls: int = Field(default=3, ge=1, le=3)
    max_tool_calls: int = Field(default=2, ge=0, le=2)
    max_steps: int = Field(default=6, ge=1, le=6)
    max_attempts: int = Field(default=2, ge=1, le=2)


class BodyLimit:
    def __init__(self, app):
        self.app = app

    async def __call__(self, scope, receive, send):
        if scope["type"] != "http" or scope["method"] != "POST":
            return await self.app(scope, receive, send)
        data = bytearray()
        deadline = asyncio.get_running_loop().time() + 2
        while True:
            try:
                message = await asyncio.wait_for(
                    receive(), max(0, deadline - asyncio.get_running_loop().time())
                )
            except TimeoutError:
                return await JSONResponse({"error": {"code": "body_timeout"}}, status_code=408)(
                    scope, receive, send
                )
            if message["type"] == "http.disconnect":
                return
            data.extend(message.get("body", b""))
            if len(data) > 8192:
                return await JSONResponse({"error": {"code": "body_too_large"}}, status_code=413)(
                    scope, receive, send
                )
            if not message.get("more_body", False):
                break
        consumed = False

        async def replay():
            nonlocal consumed
            if not consumed:
                consumed = True
                return {"type": "http.request", "body": bytes(data)}
            return await receive()

        await self.app(scope, replay, send)


def create_app(agent=None):
    @asynccontextmanager
    async def lifespan(app):
        owned = agent is None
        if owned:
            mode = os.getenv("TRACEFORGE_AGENT_MODE", "fixture")
            if mode == "fixture":
                provider = FixtureProvider()
            elif mode == "live" and os.getenv("TRACEFORGE_ALLOW_LIVE") == "1":
                provider = CompatibleProvider(
                    os.environ["TRACEFORGE_PROVIDER_URL"],
                    os.environ["TRACEFORGE_PROVIDER_API_KEY"],
                    os.environ["TRACEFORGE_PROVIDER_MODEL"],
                    os.getenv("TRACEFORGE_PROVIDER_NAME", "openai"),
                )
            else:
                raise ValueError("invalid mode or missing explicit live opt-in")
            telemetry = Telemetry(os.getenv("TRACEFORGE_OTLP_ENDPOINT", "http://127.0.0.1:4317"))
            price_path = os.getenv("TRACEFORGE_PRICING_FILE")
            app.state.agent = Agent(
                provider, telemetry, Pricing(Path(price_path)) if price_path else None
            )
        else:
            app.state.agent = agent
        app.state.slots = asyncio.Semaphore(4)
        yield
        if owned:
            await app.state.agent.provider.close()
            await asyncio.to_thread(app.state.agent.telemetry.shutdown)

    app = FastAPI(lifespan=lifespan, docs_url=None, redoc_url=None, openapi_url=None)
    app.add_middleware(BodyLimit)

    @app.exception_handler(RequestValidationError)
    async def invalid_request(request, error):
        return JSONResponse(
            {"error": {"code": "invalid_request", "message": "Invalid bounded request"}},
            status_code=422,
        )

    @app.get("/healthz")
    async def health():
        return {"status": "ready", "mode": app.state.agent.provider.mode,
                "provider":app.state.agent.provider.name,"requested_model":app.state.agent.provider.model}

    @app.post("/ask")
    async def ask(value: Ask, request: Request):
        engine = app.state.agent
        run_id = uuid.uuid4().hex
        context = TraceContextTextMapPropagator().extract(
            dict(request.headers), getter=DefaultGetter()
        )
        with engine.telemetry.span(
            "POST /ask",
            engine.base(run_id, "http") | {"http.request.method": "POST", "url.path": "/ask"},
            SpanKind.SERVER,
            context=context,
        ) as span:
            trace_id = format(span.get_span_context().trace_id, "032x")
            identity = {"run_id": run_id, "trace_id": trace_id, "mode": engine.provider.mode}
            acquired = False
            try:
                await asyncio.wait_for(app.state.slots.acquire(), timeout=0.05)
                acquired = True
                limits = Limits(
                    value.max_model_calls, value.max_tool_calls, value.max_steps, value.max_attempts
                )
                result = await asyncio.wait_for(
                    engine.run(
                        value.question,
                        run_id,
                        value.top_k,
                        value.scenario,
                        limits,
                        value.context_limit,
                        value.output_format,
                        {
                            k: v
                            for k, v in {
                                "traceforge.evaluation.run_id": value.evaluation_run_id,
                                "traceforge.evaluation.case_id": value.evaluation_case_id,
                            }.items()
                            if v
                        },
                    ),
                    timeout=20,
                )
                span.set_attribute("http.response.status_code", 200)
                return identity | result
            except TimeoutError:
                kind = "request_deadline" if acquired else "agent_busy"
                engine.telemetry.fail(span, kind)
                span.set_attribute("http.response.status_code", 504 if acquired else 503)
                return JSONResponse(
                    identity | {"error": {"code": kind}}, status_code=504 if acquired else 503
                )
            except AgentFailure as error:
                engine.telemetry.fail(span, error.kind)
                status = (
                    504
                    if "timeout" in error.kind
                    else 502
                    if error.kind.startswith("provider_")
                    or error.kind.startswith("malformed_provider")
                    else 422
                )
                span.set_attribute("http.response.status_code", status)
                return JSONResponse(identity | {"error": {"code": error.kind}}, status_code=status)
            finally:
                if acquired:
                    app.state.slots.release()

    return app


app = create_app()
