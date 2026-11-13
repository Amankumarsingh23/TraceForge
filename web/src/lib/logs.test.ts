import { expect, it } from "vitest";
import {
  correlationName,
  defaultLogFilters,
  logSearchPath,
  severityName,
} from "./logs";
import { timestampNS } from "./timeline";
it.each([
  [0, "UNSPECIFIED"],
  [1, "TRACE"],
  [4, "TRACE"],
  [5, "DEBUG"],
  [12, "INFO"],
  [13, "WARN"],
  [18, "ERROR"],
  [24, "FATAL"],
])("maps severity %i to %s", (value, name) =>
  expect(severityName(Number(value))).toBe(name),
);
it("distinguishes standalone, trace-only and exact span correlation", () => {
  expect(correlationName({ trace_id: "", span_id: "" })).toBe("Standalone");
  expect(correlationName({ trace_id: "a", span_id: "" })).toBe("Trace-only");
  expect(correlationName({ trace_id: "a", span_id: "b" })).toBe("Trace + span");
});
it("keeps nanosecond log offsets exact and encodes literal filters", () => {
  expect(
    timestampNS("2026-11-12T00:00:00.123456789Z") -
      timestampNS("2026-11-12T00:00:00.123456788Z"),
  ).toBe(1n);
  const filters = {
    ...defaultLogFilters(),
    trace_id: "a".repeat(32),
    span_id: "b".repeat(16),
    text: "%_? &",
  };
  const path = logSearchPath(filters, "opaque", filters.trace_id);
  const query = new URL(path, "http://localhost").searchParams;
  expect(query.get("span_id")).toBe(filters.span_id);
  expect(query.get("text")).toBe(filters.text);
  expect(query.get("cursor")).toBe("opaque");
  expect(path).toContain(`/traces/${filters.trace_id}/logs?`);
});
