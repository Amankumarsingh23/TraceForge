// @vitest-environment node
import { expect, it } from "vitest";
import {
  metricQuery,
  proxyMetrics,
  quantileRange,
  metricSemantics,
  type MetricPoint,
} from "./metrics";
it("bounds metrics paths, filters and UTC ranges", async () => {
  for (const q of [
    "url=http://example.com",
    "name=a&name=b",
    "from=bad",
    "from=2026-11-14T00:00:00Z&to=2026-11-14T02:00:00Z",
    "stream_id=bad",
  ]) {
    expect(() => metricQuery(new URLSearchParams(q))).toThrow();
    expect(
      (await proxyMetrics(new Request(`http://local/api/metrics?${q}`))).status,
    ).toBe(400);
  }
  expect(
    (
      await proxyMetrics(new Request("http://local/api/metrics"), [
        "../healthz",
      ])
    ).status,
  ).toBe(400);
});
it("labels unknown histogram ranges without inventing exact finite quantiles", () => {
  expect(quantileRange(null, "s")).toContain("N/A");
  expect(
    quantileRange({ lower: 0.5, upper: null, meaning: "range" }, "s"),
  ).toContain("+∞");
  expect(
    quantileRange({ lower: 0.1, upper: 0.5, meaning: "range" }, "s"),
  ).toContain("not exact");
});
it("distinguishes gauges, counters and signed non-monotonic changes", () => {
  expect(metricSemantics({ type: "gauge" } as MetricPoint)).toContain(
    "no carry-forward",
  );
  expect(
    metricSemantics({ type: "sum", monotonic: false } as MetricPoint),
  ).toContain("not absolute state");
  expect(
    metricSemantics({ type: "sum", monotonic: true } as MetricPoint),
  ).toContain("Counter");
});
