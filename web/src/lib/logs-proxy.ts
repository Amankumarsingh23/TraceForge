import { timestampNS } from "./timeline";
import { validTraceId } from "./contracts";
import { forwardTelemetry } from "./proxy";
export function validateLogQuery(q: URLSearchParams, id?: string) {
  const allowed = new Set([
    "from",
    "to",
    "service",
    "min_severity",
    "trace_id",
    "span_id",
    "text",
    "limit",
    "cursor",
  ]);
  for (const key of q.keys())
    if (!allowed.has(key) || q.getAll(key).length !== 1)
      throw new Error("Unsupported or repeated log query parameter.");
  if (id !== undefined && !validTraceId(id))
    throw new Error("Invalid trace ID.");
  const trace = q.get("trace_id") || id,
    span = q.get("span_id");
  if (
    (trace && !validTraceId(trace)) ||
    (id && q.get("trace_id") && id !== q.get("trace_id"))
  )
    throw new Error("Invalid or conflicting trace ID.");
  if (span && (!trace || !/^[a-f0-9]{16}$/.test(span) || /^0+$/.test(span)))
    throw new Error(
      "Span filtering requires both a valid trace ID and span ID.",
    );
  const from = q.get("from"),
    to = q.get("to");
  for (const key of ["from", "to"]) {
    if (q.has(key)) {
      const ns = timestampNS(q.get(key)!);
      if (ns < 0n || ns > 9223372036854775807n)
        throw new Error("Invalid UTC timestamp.");
    }
  }
  if (from && to) {
    const window = timestampNS(to) - timestampNS(from);
    if (window <= 0n || window > 86400000000000n)
      throw new Error("Select a UTC window of at most 24 hours.");
  }
  const limit = q.get("limit"),
    severity = q.get("min_severity"),
    service = q.get("service"),
    cursor = q.get("cursor"),
    text = q.get("text");
  if (
    q.has("limit") &&
    (!limit || !/^\d+$/.test(limit) || +limit < 1 || +limit > 100)
  )
    throw new Error("Page size must be 1–100.");
  if (
    q.has("min_severity") &&
    (!severity || !/^\d+$/.test(severity) || +severity > 24)
  )
    throw new Error("Severity must be 0–24.");
  if (
    (service && !/^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$/.test(service)) ||
    (text && new TextEncoder().encode(text).length > 256)
  )
    throw new Error("Invalid service or oversized literal text.");
  if (
    q.has("cursor") &&
    (!cursor || cursor.length > 2048 || !/^[A-Za-z0-9_-]+$/.test(cursor))
  )
    throw new Error("Invalid cursor.");
}
export async function proxyLogs(
  request: Request,
  id?: string,
  options: { origin?: string; transport?: typeof fetch } = {},
) {
  try {
    validateLogQuery(new URL(request.url).searchParams, id);
  } catch (e) {
    return Response.json(
      {
        error: {
          code: "invalid_log_query",
          message: e instanceof Error ? e.message : "Invalid log query",
        },
      },
      { status: 400, headers: { "Cache-Control": "no-store" } },
    );
  }
  return forwardTelemetry(
    request,
    id ? `/api/v1/traces/${id}/logs` : "/api/v1/logs",
    options,
  );
}
