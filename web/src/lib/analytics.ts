import { forwardTelemetry } from "./proxy";
import { timestampNS } from "./timeline";
export type Stats = {
  count: number;
  rate_per_second: number;
  errors: number;
  error_fraction: number | null;
  status: Record<string, number>;
  http_status: Record<string, number>;
  duration_samples: number;
  mean_ms: number | null;
  p50_ms: number | null;
  p95_ms: number | null;
  p99_ms: number | null;
};
export type Service = {
  identity: {
    id: string;
    name: string;
    namespace: string;
    environment: string;
    workload: string;
  };
  stats: Stats;
  buckets: { from: string; to: string; seconds: number; stats: Stats }[];
  trace_ids: string[];
};
export type Edge = {
  from: string;
  to: string;
  client: Stats;
  server: Stats;
  trace_ids: string[];
};
export type Analytics = {
  version: string;
  query: { from: string; to: string; bucket_seconds: number };
  window_seconds: number;
  units: string;
  attribution: string;
  completeness: string;
  non_snapshot: boolean;
  selection_complete: boolean;
  services: Service[];
  edges: Edge[];
  diagnostics: Record<string, number>;
};
export function sortServices(
  services: Service[],
  sort: "name" | "rate" | "errors" | "p95",
): Service[] {
  return [...services].sort((a, b) => {
    if (sort === "name")
      return (
        a.identity.name.localeCompare(b.identity.name) ||
        a.identity.id.localeCompare(b.identity.id)
      );
    const value = (s: Service) =>
      sort === "rate"
        ? s.stats.rate_per_second
        : sort === "errors"
          ? (s.stats.error_fraction ?? -1)
          : (s.stats.p95_ms ?? -1);
    return value(b) - value(a) || a.identity.id.localeCompare(b.identity.id);
  });
}
export function validateAnalytics(query: URLSearchParams) {
  for (const [key, value] of query)
    if (
      ![
        "from",
        "to",
        "bucket_seconds",
        "environment",
        "namespace",
        "workload",
        "service_id",
      ].includes(key) ||
      query.getAll(key).length !== 1 ||
      !value
    )
      throw new Error("Unsupported or repeated analytics filter.");
  const from = query.get("from"),
    to = query.get("to");
  for (const value of [from, to])
    if (value) {
      const ns = timestampNS(value);
      if (ns < 0n || ns > 9223372036854775807n)
        throw new Error("Invalid UTC bounds.");
    }
  if (from && to) {
    const n = timestampNS(to) - timestampNS(from);
    if (n <= 0n || n > 3600000000000n)
      throw new Error("Select a UTC window of at most one hour.");
  }
  if (
    query.has("bucket_seconds") &&
    !["60", "300", "900"].includes(query.get("bucket_seconds")!)
  )
    throw new Error("Bucket must be 60, 300 or 900 seconds.");
  if (
    query.has("workload") &&
    !["synthetic", "fixture", "live", "unknown"].includes(
      query.get("workload")!,
    )
  )
    throw new Error("Invalid workload class.");
  for (const key of ["namespace", "environment"])
    if ((query.get(key)?.length ?? 0) > 128)
      throw new Error("Filter exceeds 128 characters.");
  if (
    query.has("service_id") &&
    !/^[a-f0-9]{32}$/.test(query.get("service_id")!)
  )
    throw new Error("Invalid service identity.");
}
export function proxyAnalytics(
  request: Request,
  parts: string[] = [],
  graph = false,
) {
  try {
    validateAnalytics(new URL(request.url).searchParams);
    if (
      (graph && parts.length) ||
      (parts.length !== 0 &&
        (parts.length !== 2 ||
          !/^[a-f0-9]{32}$/.test(parts[0]) ||
          !["metrics", "dependencies"].includes(parts[1])))
    )
      throw new Error("Invalid analytics path.");
  } catch (e) {
    return Response.json(
      {
        error: {
          code: "invalid_analytics_query",
          message: (e as Error).message,
        },
      },
      { status: 400, headers: { "Cache-Control": "no-store" } },
    );
  }
  return forwardTelemetry(
    request,
    `/api/v1/${graph ? "service-graph" : "services"}${parts.length ? "/" + parts.join("/") : ""}`,
  );
}
