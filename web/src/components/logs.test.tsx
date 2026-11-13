import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { LogResults } from "./log-results";
import { LogsExplorer } from "./logs-explorer";
import { CorrelatedLogs } from "./correlated-logs";
import { defaultLogFilters, type LogRow } from "../lib/logs";
afterEach(() => vi.unstubAllGlobals());
const row: LogRow = {
  log_id: "c".repeat(32),
  source_event_id: "",
  trace_id: "a".repeat(32),
  span_id: "b".repeat(16),
  trace_flags: 1,
  service_name: "payment",
  severity_number: 17,
  severity_text: "ERROR",
  event_time: "2026-11-12T00:00:00.123456789Z",
  observed_time: null,
  effective_time: "2026-11-12T00:00:00.123456789Z",
  time_source: "event",
  body_text: "<script>unsafe()</script>",
  body: { stringValue: "<script>unsafe()</script>" },
  attributes: [],
  resource_attributes: [],
  scope_name: "test",
  scope_version: "1",
  scope: {},
  resource: {},
  record: {},
  event_name: "",
  ingested_at: "2026-11-12T00:00:01Z",
  resource_schema_url: "",
  scope_schema_url: "",
};
const response = (logs: LogRow[] = [], next_cursor = "") => ({
  logs,
  next_cursor,
  from: "2026-11-12T00:00:00Z",
  to: "2026-11-12T01:00:00Z",
  consistency: "non_snapshot_raw_rows",
});
const props = {
  path: "/api/logs",
  retry: () => {},
  page: 1,
  previous: () => {},
  next: () => {},
  hasPrevious: false,
};
it("renders log severity, exact timestamps, safe text and trace link", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => Response.json(response([row]))),
  );
  const { container } = render(
    <LogResults {...props} origin="2026-11-12T00:00:00.123456788Z" />,
  );
  await screen.findByText(row.body_text, { selector: ".log-message" });
  fireEvent.click(
    screen.getByText(row.body_text, { selector: ".log-message" }),
  );
  expect(container.querySelector("script")).toBeNull();
  expect(screen.getByText("Recorded relative offset: 1 ns")).toBeVisible();
  expect(screen.getByRole("link", { name: row.trace_id })).toHaveAttribute(
    "href",
    `/traces/${row.trace_id}`,
  );
  expect(screen.getByText("ERROR", { exact: true })).toBeVisible();
});
it("shows honest empty and unavailable states", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => Response.json(response())),
  );
  const view = render(<LogResults {...props} />);
  await screen.findByText("No matching logs");
  view.unmount();
  vi.stubGlobal(
    "fetch",
    vi.fn(async () =>
      Response.json(
        { error: { code: "backend_unavailable", message: "offline" } },
        { status: 503 },
      ),
    ),
  );
  render(<LogResults {...props} />);
  await screen.findByText("Log backend unavailable");
  expect(screen.queryByText("No matching logs")).toBeNull();
});
it("shows loading and discards an older log response", async () => {
  let finishOld!: (v: Response) => void;
  vi.stubGlobal(
    "fetch",
    vi.fn((path: string) =>
      path.includes("old")
        ? new Promise<Response>((resolve) => {
            finishOld = resolve;
          })
        : Promise.resolve(
            Response.json(response([{ ...row, body_text: "new log" }])),
          ),
    ),
  );
  const view = render(<LogResults {...props} path="/api/logs?text=old" />);
  expect(screen.getByRole("status")).toHaveTextContent("Loading stored logs");
  view.rerender(<LogResults {...props} path="/api/logs?text=new" />);
  await screen.findByText("new log");
  finishOld(Response.json(response([{ ...row, body_text: "old log" }])));
  await waitFor(() => expect(screen.queryByText("old log")).toBeNull());
});
it("changes span/trace correlation explicitly and retains trace-only rows", async () => {
  const calls: string[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path: string) => {
      calls.push(path);
      return Response.json(
        response([
          {
            ...row,
            span_id: path.includes("span_id=") ? row.span_id : "",
            body_text: "correlated",
          },
        ]),
      );
    }),
  );
  render(<CorrelatedLogs traceId={row.trace_id} spanId={row.span_id} />);
  await screen.findByText("correlated");
  expect(calls[0]).toContain(`span_id=${row.span_id}`);
  fireEvent.click(screen.getByRole("button", { name: "All trace logs" }));
  await waitFor(() => expect(calls).toHaveLength(2));
  expect(calls[1]).not.toContain("span_id=");
  fireEvent.click(await screen.findByText("correlated"));
  expect(screen.getByText("Trace-only", { exact: true })).toBeVisible();
});
it("keeps cursor with applied log filters and resets on new search", async () => {
  const calls: string[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path: string) => {
      calls.push(path);
      return Response.json(response([row], "nexttoken"));
    }),
  );
  render(<LogsExplorer initial={defaultLogFilters()} />);
  await screen.findByText(row.body_text, { selector: ".log-message" });
  fireEvent.click(screen.getByRole("button", { name: /Next logs/ }));
  await waitFor(() => expect(calls.at(-1)).toContain("cursor=nexttoken"));
  fireEvent.change(screen.getByLabelText("Service", { exact: true }), {
    target: { value: "payment" },
  });
  fireEvent.click(screen.getByRole("button", { name: /Search logs/ }));
  await waitFor(() => expect(calls.at(-1)).toContain("service=payment"));
  expect(calls.at(-1)).not.toContain("cursor=");
});
it("labels missing source timestamps and ingestion fallback explicitly", async () => {
  const fallback = {
    ...row,
    event_time: null,
    observed_time: null,
    time_source: "ingestion" as const,
    trace_id: "",
    span_id: "",
    body_text: "fallback",
  };
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => Response.json(response([fallback]))),
  );
  render(<LogResults {...props} />);
  fireEvent.click(await screen.findByText("fallback"));
  expect(screen.getByText("Standalone", { exact: true })).toBeVisible();
  expect(
    screen.getByText("Time source: ingestion (event time unknown)"),
  ).toBeVisible();
  expect(screen.getAllByText("Unknown / absent")).toHaveLength(2);
});
