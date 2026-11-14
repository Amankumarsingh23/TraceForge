import { render, screen } from "@testing-library/react";
import { expect, it } from "vitest";
import { MetricSeriesView } from "./metrics-explorer";
import type { MetricSeries } from "@/lib/metrics";
it("renders no-data without zero latency and preserves exact integer text", () => {
  const series = {
    metadata: {
      name: "requests",
      service: "payment",
      namespace: "fixture",
      instance: "writer",
      unit: "{request}",
      type: "sum",
      temporality: 1,
      monotonic: true,
      stream_id: "a".repeat(64),
      attributes: [],
    },
    points: [],
    intervals: [],
    diagnostics: { overflow: false },
    increment: "9223372036854775807",
    observed_seconds: 10,
    observed_rate_per_second: Number("922337203685477580.7"),
    coverage_fraction: 0.5,
    complete: false,
    status: "observed",
    histogram_buckets: [],
  } as unknown as MetricSeries;
  render(<MetricSeriesView series={series} />);
  expect(screen.getByText("9223372036854775807")).toBeInTheDocument();
  expect(screen.getByText(/incomplete or not applicable/)).toBeInTheDocument();
  expect(screen.getByText(/Observed-coverage rate/)).toBeInTheDocument();
});
