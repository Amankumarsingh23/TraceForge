from opentelemetry.sdk.trace.sampling import ALWAYS_ON

from agent_demo.telemetry import Telemetry


def test_default_agent_provider_ignores_ambient_drop_sampler(monkeypatch):
    monkeypatch.setenv("OTEL_TRACES_SAMPLER", "always_off")
    telemetry = Telemetry()
    try:
        assert telemetry.provider.sampler is ALWAYS_ON
    finally:
        telemetry.provider.shutdown()
        telemetry.logs.shutdown()
