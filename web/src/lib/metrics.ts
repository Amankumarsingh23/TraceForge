import { timestampNS } from "./timeline";
import { forwardTelemetry } from "./proxy";
export type MetricPoint = {
  stream_id: string;
  name: string;
  service: string;
  namespace: string;
  environment: string;
  workload: string;
  instance: string;
  unit: string;
  type: "sum" | "gauge" | "histogram";
  temporality: number;
  monotonic: boolean;
  attributes: unknown;
  start_ns: string;
  end_ns: string;
  int_value: string | null;
  double_value: number | null;
  bounds: number[];
  buckets: string[];
  flags: number;
  overflow: boolean;
};
export type Quantile = {
  lower: number | null;
  upper: number | null;
  meaning: string;
};
export type MetricSeries = {
  metadata: MetricPoint;
  points: MetricPoint[];
  intervals: {
    start_ns: string;
    end_ns: string;
    increment: string;
    rate_per_second: number | null;
    count: string;
    sum: number | null;
    buckets: string[];
  }[];
  diagnostics: Record<string, number | boolean>;
  increment: string | null;
  observed_seconds: number;
  observed_rate_per_second: number | null;
  coverage_fraction: number;
  complete: boolean;
  latest: string | null;
  minimum_observation: number | null;
  maximum_observation: number | null;
  mean_observation: number | null;
  histogram_count: string | null;
  histogram_sum: number | null;
  histogram_mean: number | null;
  histogram_buckets: string[];
  p50_bucket: Quantile | null;
  p95_bucket: Quantile | null;
  p99_bucket: Quantile | null;
  status: string;
};
export type MetricResult = {
  version: string;
  query: { from: string; to: string };
  series: MetricSeries[];
  merged_histogram: MetricSeries | null;
  merge_status: string;
  caveat: string;
  combined_sum: {
    increment: string;
    rate_per_second: number | null;
    source_streams: number;
    coverage: string;
  } | null;
  sum_merge_status: string;
};
export function metricQuery(q: URLSearchParams) {
  for (const [k, v] of q)
    if (
      !["from", "to", "name", "service", "namespace", "stream_id"].includes(
        k,
      ) ||
      q.getAll(k).length !== 1 ||
      !v
    )
      throw new Error("Unsupported or repeated metric filter.");
  for (const k of ["from", "to"]) {
    const v = q.get(k);
    if (v) {
      const n = timestampNS(v);
      if (n < 0n || n > 9223372036854775807n)
        throw new Error("Invalid UTC metric timestamp.");
    }
  }
  if (q.has("from") && q.has("to")) {
    const n = timestampNS(q.get("to")!) - timestampNS(q.get("from")!);
    if (n <= 0n || n > 3600000000000n)
      throw new Error("Select a UTC window of at most one hour.");
  }
  for (const k of ["name", "service", "namespace"])
    if ((q.get(k)?.length ?? 0) > 128)
      throw new Error("Metric filter is too long.");
  if (q.has("stream_id") && !/^[a-f0-9]{64}$/.test(q.get("stream_id")!))
    throw new Error("Invalid stream ID.");
}
export function quantileRange(q: Quantile | null, unit: string) {
  if (q === null) return "N/A — no observations";
  return `${q.lower === null ? "−∞" : q.lower} < percentile ≤ ${q.upper === null ? "+∞" : q.upper} ${unit} (bucket range, not exact)`;
}
export function metricSemantics(p: MetricPoint) {
  return p.type === "gauge"
    ? "Gauge · latest observation; no carry-forward"
    : p.type === "histogram"
      ? "Explicit histogram · incremental bucket distribution"
      : p.monotonic
        ? "Monotonic Sum (Counter) · increments"
        : "Non-monotonic Sum (UpDownCounter) · signed change; delta is not absolute state";
}
export function proxyMetrics(request: Request, parts: string[] = []) {
  try {
    metricQuery(new URL(request.url).searchParams);
    if (
      parts.length > 1 ||
      (parts.length === 1 && !["series", "query"].includes(parts[0]))
    )
      throw new Error("Invalid metrics API path.");
  } catch (e) {
    return Response.json(
      {
        error: { code: "invalid_metric_query", message: (e as Error).message },
      },
      { status: 400 },
    );
  }
  return forwardTelemetry(
    request,
    `/api/v1/metrics${parts.length ? "/" + parts[0] : ""}`,
  );
}
