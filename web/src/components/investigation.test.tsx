import { fireEvent, render, screen } from "@testing-library/react";
import { expect, it } from "vitest";
import { Diagnostics } from "./diagnostics";
import { SpanInspector } from "./span-inspector";
import { Waterfall } from "./waterfall";
import { ErrorPanel } from "./error-panel";
import { APIError } from "../lib/client";
import { detail, span } from "../lib/test-fixtures";
it("shows counts, unknown completeness and every anomaly warning", () => {
  const trace = detail();
  Object.assign(trace.diagnostics, {
    physical_rows: 12,
    unique_spans: 8,
    exact_duplicate_rows: 3,
    conflicting_duplicate_groups: 1,
    conflicts: [{ span_id: "conflict", variants: 2 }],
    missing_parent_count: 1,
    missing_parent_ids: ["missing"],
    cycle_count: 1,
    cycles: [["one", "two"]],
    has_detected_gaps: true,
    timing_warnings: [{ span_id: "one", code: "outside_parent" }],
  });
  render(<Diagnostics data={trace.diagnostics} />);
  expect(screen.getByText("unknown")).toBeVisible();
  expect(screen.getByText("3 exact duplicate rows")).toBeVisible();
  expect(screen.getByText(/Conflicting span/)).toBeVisible();
  expect(screen.getByText(/Missing parent/, { selector: "li" })).toBeVisible();
  expect(screen.getByText(/Cycle:/)).toBeVisible();
  expect(screen.getByText(/outside_parent/)).toBeVisible();
});
it("renders untrusted telemetry as text and labels individual error status", () => {
  const selected = span("one");
  selected.span_name = "<img src=x onerror=alert(1)>";
  selected.status_code = 2;
  selected.events = [{ name: "exception", message: "synthetic failure" }];
  const { container } = render(
    <SpanInspector span={selected} clear={() => {}} />,
  );
  expect(screen.getByText("ERROR")).toBeVisible();
  expect(screen.getByText(selected.span_name)).toBeVisible();
  expect(container.querySelector("img")).toBeNull();
  expect(screen.getByText(/synthetic failure/)).toBeVisible();
});
it("selects tiny spans and collapses children without losing other roots", () => {
  const root = span("root"),
    child = span("child", "0", "0"),
    other = span("other");
  root.children = [child];
  const selected: string[] = [];
  render(
    <Waterfall
      trace={detail([root, other])}
      selected={null}
      onSelect={(value) => selected.push(value.span_id)}
    />,
  );
  fireEvent.click(
    screen.getByRole("button", { name: /Inspect checkout.*0 ns/ }),
  );
  expect(selected).toEqual(["child"]);
  fireEvent.click(
    screen.getByRole("button", { name: "Collapse checkout root" }),
  );
  expect(screen.getAllByRole("button", { name: /^Inspect/ })).toHaveLength(2);
  fireEvent.click(screen.getByRole("button", { name: "Expand all" }));
  expect(screen.getAllByRole("button", { name: /^Inspect/ })).toHaveLength(3);
});
it.each([400, 404, 413, 503, 504])(
  "renders actionable error status %i",
  (status) => {
    render(
      <ErrorPanel
        error={new APIError(status, "test", "Actual backend error")}
        retry={() => {}}
      />,
    );
    expect(screen.getByRole("alert")).toHaveTextContent("Actual backend error");
    expect(screen.getByRole("button", { name: "Retry request" })).toBeVisible();
  },
);
