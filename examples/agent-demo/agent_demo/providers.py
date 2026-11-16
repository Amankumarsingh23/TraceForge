import asyncio
import json
import re
from dataclasses import dataclass
from typing import Protocol
from urllib.parse import urlsplit

import httpx

from .accounting import Usage, normalize_usage
from .constrained import CONTRACT, generate

IDENTITY = re.compile(r"[A-Za-z0-9_.:/-]{1,128}")
TOOL_SCHEMA = {
    "type": "function",
    "function": {
        "name": "get_order_status",
        "description": "Read a synthetic DEMO order's stored status.",
        "parameters": {
            "type": "object",
            "properties": {"order_id": {"type": "string", "pattern": "^DEMO-[0-9]{4}$"}},
            "required": ["order_id"],
            "additionalProperties": False,
        },
        "strict": True,
    },
}


class ProviderFailure(Exception):
    def __init__(self, kind: str, retryable: bool = False):
        super().__init__(kind)
        self.kind, self.retryable = kind, retryable


@dataclass(frozen=True)
class ToolCall:
    id: str
    name: str
    arguments: str


@dataclass(frozen=True)
class Result:
    provider: str
    requested_model: str
    response_model: str | None
    content: str | None
    tools: list[ToolCall]
    response_id: str | None
    usage: Usage
    finish_reason: str


class Provider(Protocol):
    name: str
    model: str
    mode: str

    async def complete(
        self, messages: list[dict], call: int, attempt: int, scenario: str
    ) -> Result: ...
    async def close(self) -> None: ...


def parse_response(raw: dict, provider: str, model: str, source: str) -> Result:
    try:
        choice = raw["choices"][0]
        message = choice["message"]
        content = message.get("content")
        if content is not None and (not isinstance(content, str) or len(content) > 8000):
            raise ValueError()
        tools = []
        if len(message.get("tool_calls") or []) > 2:
            raise ValueError()
        for tool in message.get("tool_calls") or []:
            name, arguments, identity = (
                tool["function"]["name"],
                tool["function"]["arguments"],
                tool["id"],
            )
            if (
                not all(isinstance(v, str) for v in (name, arguments, identity))
                or len(name) > 128
                or len(arguments) > 1024
                or not IDENTITY.fullmatch(identity)
            ):
                raise ValueError()
            tools.append(ToolCall(identity, name, arguments))
        actual, response_id = raw.get("model"), raw.get("id")
        if actual is not None and (not isinstance(actual, str) or not IDENTITY.fullmatch(actual)):
            raise ValueError()
        if response_id is not None and (
            not isinstance(response_id, str) or not IDENTITY.fullmatch(response_id)
        ):
            raise ValueError()
        reason = choice["finish_reason"]
        if (
            reason not in {"stop", "tool_calls", "length", "content_filter"}
            or not tools
            and not content
        ):
            raise ValueError()
        return Result(
            provider,
            model,
            actual,
            content,
            tools,
            response_id,
            normalize_usage(raw.get("usage"), source),
            reason,
        )
    except (KeyError, TypeError, ValueError, IndexError, AttributeError):
        raise ProviderFailure("malformed_provider_response") from None


class FixtureProvider:
    name, model, mode = "traceforge_fixture", "fixture-support-v1", "fixture"

    async def close(self):
        return None

    async def complete(self, messages, call, attempt, scenario):
        if scenario == "timeout":
            raise ProviderFailure("provider_timeout", True)
        if scenario == "permanent_error":
            raise ProviderFailure("provider_permanent_error")
        if scenario == "retry" and call == 1 and attempt == 1:
            raise ProviderFailure("provider_timeout", True)
        await asyncio.sleep(0)
        question = next(m["content"] for m in messages if m["role"] == "user")
        tool_result = next((m["content"] for m in reversed(messages) if m["role"] == "tool"), None)
        needs_tool = bool(
            re.search(r"DEMO-\d{4}|order.*status|status.*order", question, re.I)
        ) or scenario in {
            "bad_arguments",
            "unknown_tool",
            "tool_timeout",
            "unknown_order",
            "max_steps",
        }
        tools, content, finish = [], None, "stop"
        if needs_tool and tool_result is None or scenario == "max_steps":
            order = re.search(r"DEMO-\d{4}", question)
            order_id = (
                "DEMO-9999"
                if scenario == "unknown_order"
                else order.group()
                if order
                else "DEMO-1001"
            )
            tools = [
                {
                    "id": f"fixture-call-{call}",
                    "type": "function",
                    "function": {
                        "name": "unapproved" if scenario == "unknown_tool" else "get_order_status",
                        "arguments": "{bad"
                        if scenario == "bad_arguments"
                        else json.dumps({"order_id": order_id}),
                    },
                }
            ]
            finish = "tool_calls"
        elif tool_result is not None:
            result = json.loads(tool_result)
            content = (
                f"Fixture adapter: synthetic order {result['order_id']} is {result['status']}."
            )
        else:
            untrusted = next(
                (
                    m["content"]
                    for m in messages
                    if m["content"].startswith("UNTRUSTED_CONTEXT_JSON:\n")
                ),
                None,
            )
            context = json.loads(
                untrusted.split("\n", 1)[1]
                if untrusted
                else messages[0]["content"].split("\nCONTEXT_JSON:\n", 1)[1]
            )
            content = (
                json.dumps(generate(question, context))
                if CONTRACT in messages[0]["content"]
                else "Fixture adapter (no live inference): "
                + (
                    context[0]["text"]
                    if context
                    else "No matching synthetic support documents were retrieved."
                )
            )
        usage = (
            None
            if scenario == "missing_usage"
            else {
                "prompt_tokens": 1200 if call == 1 else 1800,
                "completion_tokens": 100 if call == 1 else 300,
                "prompt_tokens_details": {"cached_tokens": 0},
                "completion_tokens_details": {"reasoning_tokens": 0},
            }
        )
        return parse_response(
            {
                "id": f"fixture-response-{call}-{attempt}",
                "model": self.model,
                "choices": [
                    {"message": {"content": content, "tool_calls": tools}, "finish_reason": finish}
                ],
                "usage": usage,
            },
            self.name,
            self.model,
            "synthetic_fixture",
        )


class CompatibleProvider:
    mode = "live"

    def __init__(self, endpoint: str, key: str, model: str, name: str = "openai", transport=None):
        url = urlsplit(endpoint)
        if (
            url.scheme != "https"
            or not url.hostname
            or url.username
            or url.password
            or url.query
            or url.fragment
            or not endpoint.endswith("/v1")
        ):
            raise ValueError(
                "live endpoint must be an HTTPS /v1 base URL without credentials/query"
            )
        if not key or not IDENTITY.fullmatch(model) or not IDENTITY.fullmatch(name):
            raise ValueError("live credentials and explicit provider/model identity are required")
        self.name, self.model = name, model
        self.client = httpx.AsyncClient(
            base_url=endpoint + "/",
            headers={"Authorization": "Bearer " + key},
            timeout=httpx.Timeout(5, connect=2),
            limits=httpx.Limits(max_connections=4, max_keepalive_connections=2),
            follow_redirects=False,
            transport=transport,
            trust_env=False,
        )

    async def close(self):
        await self.client.aclose()

    async def complete(self, messages, call, attempt, scenario):
        if scenario != "normal":
            raise ProviderFailure("fixture_scenario_in_live_mode")
        try:
            async with self.client.stream(
                "POST",
                "chat/completions",
                json={
                    "model": self.model,
                    "messages": messages,
                    "tools": [TOOL_SCHEMA],
                    "tool_choice": "auto",
                    "max_completion_tokens": 256,
                    "stream": False,
                    "store": False,
                },
            ) as response:
                if response.status_code != 200:
                    kind = (
                        "provider_rate_limited"
                        if response.status_code == 429
                        else "provider_unavailable"
                        if response.status_code >= 500
                        else "provider_permanent_error"
                    )
                    raise ProviderFailure(
                        kind, response.status_code == 429 or response.status_code >= 500
                    )
                body = bytearray()
                async for chunk in response.aiter_bytes():
                    body.extend(chunk)
                    if len(body) > 65536:
                        raise ProviderFailure("provider_response_too_large")
                try:
                    raw = json.loads(body)
                except (ValueError, UnicodeError):
                    raise ProviderFailure("malformed_provider_response") from None
                return parse_response(raw, self.name, self.model, "provider_reported")
        except httpx.TimeoutException:
            raise ProviderFailure("provider_timeout", True) from None
        except httpx.HTTPError:
            raise ProviderFailure("provider_network_error", True) from None
