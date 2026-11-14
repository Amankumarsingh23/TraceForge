import { render, screen, cleanup } from "@testing-library/react";
import { expect, it } from "vitest";
import { WindowView } from "./reliability-view";
import type { Window } from "../lib/reliability";
it("keeps UNKNOWN evidence N/A and negative error budgets visible", () => {
  const w = {
    evidence: {
      status: "BACKEND_UNAVAILABLE",
      reason: "metric backend unavailable",
      from: "2026-11-14T10:00:00Z",
      to: "2026-11-14T10:00:20Z",
      total: "",
      good: "",
      bad: "",
      coverage: 0,
      diagnostics: {},
      trace_ids: [],
    },
    budget: null,
  } satisfies Window;
  render(<WindowView w={w} title="Short window" />);
  expect(screen.getByText(/no healthy result inferred/)).toBeInTheDocument();
  expect(screen.getByText(/Eligible: N\/A/)).toBeInTheDocument();
  cleanup();
  render(
    <WindowView
      w={{
        ...w,
        evidence: {
          ...w.evidence,
          status: "MEASURED",
          total: "1000",
          good: "940",
          bad: "60",
        },
        budget: {
          sli: "47/50",
          bad_fraction: "3/50",
          allowed_bad_fraction: "1/20",
          allowed_bad_events: "50",
          budget_consumed: "6/5",
          budget_remaining: "-10",
          burn_rate: "6/5",
          meets_target: false,
        },
      }}
      title="Objective"
    />,
  );
  expect(screen.getByText("-10")).toBeInTheDocument();
  expect(screen.getByText(/120%/)).toBeInTheDocument();
});
