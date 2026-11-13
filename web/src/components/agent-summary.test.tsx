import { fireEvent, render, screen } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { AgentSummary, nanoUSD, type AgentSummaryData } from "./agent-summary";
afterEach(() => vi.unstubAllGlobals());
function fixture(): AgentSummaryData {
  return {
    trace_id: "a".repeat(32),
    completeness: "unknown",
    runs: [
      {
        run_id: "b".repeat(32),
        agent_name: "support-agent",
        observed_duration_ns: "1000",
        mode: "fixture",
        outcome: "success",
        versions: {},
        model_calls: 1,
        provider_attempts: 2,
        tool_executions: 0,
        known_input_tokens: "1200",
        known_output_tokens: "100",
        known_cached_input_tokens: "0",
        known_reasoning_output_tokens: "0",
        complete_usage_attempts: 1,
        usage_coverage: "incomplete",
        priced_attempts: 1,
        cost_coverage: "incomplete",
        known_estimated_nano_usd: "800000",
        estimated_nano_usd: null,
        operations: [
          {
            span_id: "c".repeat(16),
            role: "attempt",
            duration_ns: "1000",
            status_code: 2,
            "error.type": "provider_timeout",
            "gen_ai.request.model": "<img src=x onerror=alert(1)>",
            "traceforge.usage.source": "synthetic_fixture",
          },
        ],
      },
    ],
  };
}
it("formats integer nano-USD without floating point", () => {
  expect(nanoUSD("2300000")).toBe("$0.002300000");
  expect(nanoUSD("1")).toBe("$0.000000001");
});
it("shows missing usage, retry provenance and unknown cost; selects the actual span", async () => {
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(Response.json(fixture())));
  const select = vi.fn();
  const { container } = render(
    <AgentSummary traceId={"a".repeat(32)} selectSpan={select} />,
  );
  expect(await screen.findByText("Fixture · synthetic usage")).toBeVisible();
  expect(screen.getByText("Unknown", { exact: true })).toBeVisible();
  expect(screen.getByText(/provider_timeout/)).toBeVisible();
  expect(screen.getByText(/synthetic_fixture/)).toBeVisible();
  expect(container.querySelector("img")).toBeNull();
  fireEvent.click(screen.getByRole("button", { name: /attempt ·/ }));
  expect(select).toHaveBeenCalledWith("c".repeat(16));
});
it("hides the panel for conventional traces", async () => {
  const transport = vi.fn().mockResolvedValue(Response.json({ runs: [] }));
  vi.stubGlobal("fetch", transport);
  const { container } = render(
    <AgentSummary traceId={"a".repeat(32)} selectSpan={() => {}} />,
  );
  await vi.waitFor(() => expect(transport).toHaveBeenCalled());
  expect(container).toBeEmptyDOMElement();
});
it("shows summary errors without hiding the waterfall", async () => {
  vi.stubGlobal(
    "fetch",
    vi
      .fn()
      .mockResolvedValue(
        Response.json(
          {
            error: {
              code: "backend_unavailable",
              message: "Backend unavailable",
            },
          },
          { status: 503 },
        ),
      ),
  );
  render(<AgentSummary traceId={"a".repeat(32)} selectSpan={() => {}} />);
  expect(await screen.findByText("AgentOps summary unavailable")).toBeVisible();
  expect(screen.getByRole("button", { name: "Retry request" })).toBeVisible();
});
