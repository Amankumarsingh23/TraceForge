import { timestampNS } from "./timeline";
import { validTraceId } from "./contracts";

export class InvalidQuery extends Error {}
export function validateQuery(query: URLSearchParams, id?: string) {
  if (id !== undefined) {
    if (!validTraceId(id) || query.size)
      throw new InvalidQuery(
        "Use a nonzero lowercase 32-character trace ID without filters.",
      );
    return;
  }
  const approved = new Set([
    "from",
    "to",
    "service",
    "status",
    "limit",
    "cursor",
  ]);
  for (const key of query.keys())
    if (!approved.has(key) || query.getAll(key).length !== 1)
      throw new InvalidQuery("Unsupported or repeated query parameter.");
  const from = query.get("from"),
    to = query.get("to");
  try {
    for (const value of [from, to])
      if (value) {
        const ns = timestampNS(value);
        if (ns < 0n || ns > 9223372036854775807n) throw new Error();
      }
    if ((query.has("from") && !from) || (query.has("to") && !to))
      throw new Error();
    if (from && to) {
      const window = timestampNS(to) - timestampNS(from);
      if (window <= 0n || window > 86400000000000n) throw new Error();
    }
  } catch {
    throw new InvalidQuery("Use a valid UTC window of at most 24 hours.");
  }
  const service = query.get("service"),
    status = query.get("status"),
    limit = query.get("limit"),
    cursor = query.get("cursor");
  if (service && !/^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$/.test(service))
    throw new InvalidQuery("Invalid service name.");
  if (status && status !== "error")
    throw new InvalidQuery("Only the error status filter is supported.");
  if (
    query.has("limit") &&
    (!limit || !/^\d+$/.test(limit) || +limit < 1 || +limit > 100)
  )
    throw new InvalidQuery("Page size must be 1–100.");
  if (
    query.has("cursor") &&
    (!cursor || cursor.length > 2048 || !/^[A-Za-z0-9_-]+$/.test(cursor))
  )
    throw new InvalidQuery("Invalid cursor.");
  // Go validates the versioned cursor contents and their exact filter binding.
}
function error(status: number, code: string, message: string) {
  return Response.json(
    { error: { code, message } },
    { status, headers: { "Cache-Control": "no-store" } },
  );
}
export async function proxyTrace(
  request: Request,
  id?: string,
  options: {
    origin?: string;
    transport?: typeof fetch;
  } = {},
) {
  const query = new URL(request.url).searchParams;
  try {
    validateQuery(query, id);
  } catch (e) {
    return error(400, "invalid_trace_query", (e as Error).message);
  }
  return forwardTelemetry(
    request,
    "/api/v1/traces" + (id === undefined ? "" : `/${id}`),
    options,
  );
}
export async function forwardTelemetry(
  request: Request,
  path: string,
  options: { origin?: string; transport?: typeof fetch } = {},
) {
  if (!/^\/api\/v1\/(?:logs|traces(?:\/[a-f0-9]{32}(?:\/(?:logs|agent-summary))?)?|evaluations(?:\/[a-f0-9]{32}(?:\/cases(?:\/[A-Za-z0-9_-]{1,64})?)?)?)$/.test(path))
    return error(400, "invalid_api_path", "Unsupported telemetry API path.");
  const query = new URL(request.url).searchParams;
  let origin: URL;
  try {
    origin = new URL(
      options.origin ??
        process.env.TRACEFORGE_API_URL ??
        "http://127.0.0.1:8082",
    );
    if (
      !["http:", "https:"].includes(origin.protocol) ||
      origin.username ||
      origin.password ||
      origin.pathname !== "/" ||
      origin.search ||
      origin.hash
    )
      throw new Error();
  } catch {
    return error(
      503,
      "backend_configuration",
      "The telemetry API origin is not configured correctly.",
    );
  }

  const upstream = new URL(path, origin);
  upstream.search = query.toString();
  const signal = AbortSignal.any([request.signal, AbortSignal.timeout(5000)]);
  try {
    const response = await (options.transport ?? fetch)(upstream, {
      signal,
      cache: "no-store",
      redirect: "error",
      headers: { Accept: "application/json" },
    });
    if (
      !(response.headers.get("content-type") ?? "").includes("application/json")
    )
      throw new Error("Unexpected upstream format");
    // Preserve the response bytes, including numeric fields unused by the UI.
    const reader = response.body?.getReader();
    if (!reader) throw new Error("Missing response");
    const chunks: Uint8Array[] = [];
    let length = 0;
    while (true) {
      const { done, value } = await reader.read();
      if (done) break;
      length += value.length;
      if (length > 16 * 1024 * 1024) {
        await reader.cancel();
        return error(
          413,
          "trace_too_large",
          "Telemetry exceeds the supported response size.",
        );
      }
      chunks.push(value);
    }
    const bytes = new Uint8Array(length);
    let offset = 0;
    for (const chunk of chunks) {
      bytes.set(chunk, offset);
      offset += chunk.length;
    }
    return new Response(bytes, {
      status: response.status,
      headers: {
        "Content-Type": "application/json",
        "Cache-Control": "no-store",
      },
    });
  } catch {
    return signal.aborted
      ? error(
          504,
          "query_timeout",
          "The telemetry API request timed out or was canceled.",
        )
      : error(
          503,
          "backend_unavailable",
          "The telemetry API is unavailable. Check the backend and retry.",
        );
  }
}
