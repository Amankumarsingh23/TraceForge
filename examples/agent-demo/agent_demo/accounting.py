import json
import re
from dataclasses import dataclass
from decimal import ROUND_HALF_UP, Decimal
from pathlib import Path
from urllib.parse import urlsplit

from .retrieval import DATA

MAX_COUNTER = 10_000_000


@dataclass(frozen=True)
class Usage:
    input: int | None
    output: int | None
    cached: int | None
    reasoning: int | None
    source: str
    status: str


def normalize_usage(raw: dict | None, source: str) -> Usage:
    if not isinstance(raw, dict):
        return Usage(None, None, None, None, source, "unknown")

    def counter(value):
        return value if type(value) is int and 0 <= value <= MAX_COUNTER else None

    incoming = counter(raw.get("prompt_tokens"))
    outgoing = counter(raw.get("completion_tokens"))
    details_in = raw.get("prompt_tokens_details")
    details_out = raw.get("completion_tokens_details")
    cached = counter(details_in.get("cached_tokens")) if isinstance(details_in, dict) else None
    reasoning = (
        counter(details_out.get("reasoning_tokens")) if isinstance(details_out, dict) else None
    )
    if (
        cached is not None
        and (incoming is None or cached > incoming)
        or reasoning is not None
        and (outgoing is None or reasoning > outgoing)
    ):
        return Usage(None, None, None, None, source, "invalid")
    status = (
        "complete"
        if incoming is not None and outgoing is not None
        else "partial"
        if incoming is not None or outgoing is not None
        else "unknown"
    )
    return Usage(incoming, outgoing, cached, reasoning, source, status)


@dataclass(frozen=True)
class Price:
    provider: str
    model: str
    currency: str
    input_per_million: str
    output_per_million: str
    cached_input_per_million: str | None
    version: str
    source: str
    hypothetical: bool


class Pricing:
    def __init__(self, path: Path = DATA / "pricing.json"):
        data = json.loads(path.read_text(encoding="utf-8"))
        self.entries = [Price(**item) for item in data["entries"]]
        if len(self.entries) > 20:
            raise ValueError("pricing entry limit")
        seen = set()
        for entry in self.entries:
            if not isinstance(entry.input_per_million, str) or not isinstance(
                entry.output_per_million, str
            ):
                raise ValueError("input and output pricing rates are required strings")
            if (entry.provider, entry.model) in seen or entry.currency != "USD":
                raise ValueError("ambiguous pricing or unsupported currency")
            seen.add((entry.provider, entry.model))
            if (
                not all(
                    isinstance(v, str) and re.fullmatch(r"[A-Za-z0-9_.:/-]{1,128}", v)
                    for v in (entry.provider, entry.model, entry.version)
                )
                or type(entry.hypothetical) is not bool
            ):
                raise ValueError("invalid pricing identity")
            if entry.provider == "traceforge_fixture" and not entry.hypothetical:
                raise ValueError("fixture pricing must be hypothetical")
            for rate in (
                entry.input_per_million,
                entry.output_per_million,
                entry.cached_input_per_million,
            ):
                if rate is not None and (
                    not isinstance(rate, str)
                    or not re.fullmatch(r"(?:0|[1-9]\d{0,4})(?:\.\d{1,9})?", rate)
                    or not Decimal(rate).is_finite()
                    or not 0 <= Decimal(rate) <= 10000
                ):
                    raise ValueError("invalid pricing rate")
            if len(entry.version) > 128 or len(entry.source) > 256:
                raise ValueError("pricing metadata limit")
            source = urlsplit(entry.source)
            if (
                source.scheme not in {"http", "https"}
                or not source.hostname
                or source.username
                or source.password
                or source.query
                or source.fragment
            ):
                raise ValueError("invalid pricing reference")

    def lookup(self, provider: str, model: str | None) -> Price | None:
        return next((p for p in self.entries if p.provider == provider and p.model == model), None)


def estimate_nano(usage: Usage, price: Price | None) -> int | None:
    if price is None or usage.input is None or usage.output is None or usage.status != "complete":
        return None
    if price.cached_input_per_million is not None and usage.cached is None:
        return None
    # Cached tokens are a subset of input; reasoning is a subset of output.
    # No extra reasoning charge is added to the output total.
    cached = usage.cached if price.cached_input_per_million is not None else 0
    amount = Decimal(usage.input - cached) * Decimal(price.input_per_million)
    amount += Decimal(cached) * Decimal(price.cached_input_per_million or "0")
    amount += Decimal(usage.output) * Decimal(price.output_per_million)
    return int((amount * 1000).quantize(Decimal("1"), rounding=ROUND_HALF_UP))
