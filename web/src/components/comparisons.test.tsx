import { render, screen } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { deltaLabel, GateDecision, Comparisons } from "./comparisons";
afterEach(() => vi.unstubAllGlobals());
it("uses percentage points and honest undefined deltas", () => {
  expect(
    deltaLabel({
      absolute_delta: -0.1,
      percentage_point_delta: -10,
      unit: "fraction",
    }),
  ).toBe("-10.00 pp");
  expect(
    deltaLabel({
      absolute_delta: null,
      percentage_point_delta: null,
      unit: "nano-USD",
    }),
  ).toContain("Unknown");
  expect(
    deltaLabel({
      absolute_delta: "100",
      percentage_point_delta: null,
      unit: "nano-USD",
    }),
  ).toBe("100 nano-USD");
});
it("renders offline decisions and individual missing evidence", async () => {
  vi.stubGlobal(
    "fetch",
    vi
      .fn()
      .mockResolvedValue(
        Response.json({
          decision_id: "a".repeat(32),
          outcome: "INCONCLUSIVE",
          result: {
            scope: "offline fixture only",
            reason: "missing usage",
            policy_version: "v1",
            policy_checksum: "hash",
            rules: [
              {
                id: "cost",
                mandatory: true,
                outcome: "INCONCLUSIVE",
                reason: "required evidence unavailable",
              },
            ],
          },
        }),
      ),
  );
  render(<GateDecision id={"a".repeat(32)} />);
  expect(await screen.findByText("Gate decision: INCONCLUSIVE")).toBeVisible();
  expect(screen.getByText("offline fixture only")).toBeVisible();
  expect(screen.getByText(/cost: INCONCLUSIVE/)).toBeVisible();
});
it("keeps empty and unavailable comparisons distinct", async () => {
  vi.stubGlobal(
    "fetch",
    vi
      .fn()
      .mockResolvedValue(Response.json({ comparisons: [], next_cursor: "" })),
  );
  render(<Comparisons />);
  expect(await screen.findByText("No comparisons yet.")).toBeVisible();
});
