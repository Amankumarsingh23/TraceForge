export interface LogRow {
  log_id: string;
  source_event_id: string;
  trace_id: string;
  span_id: string;
  trace_flags: number;
  service_name: string;
  severity_number: number;
  severity_text: string;
  event_time: string | null;
  observed_time: string | null;
  effective_time: string;
  time_source: "event" | "observed" | "ingestion";
  body_text: string;
  body: unknown;
  attributes: unknown[];
  resource_attributes: unknown[];
  scope_name: string;
  scope_version: string;
  scope: unknown;
  resource: unknown;
  record: unknown;
  event_name: string;
  ingested_at: string;
  resource_schema_url: string;
  scope_schema_url: string;
}
export interface LogResponse {
  logs: LogRow[];
  next_cursor: string;
  from: string;
  to: string;
  consistency: string;
}
export interface LogFilters {
  from: string;
  to: string;
  service: string;
  min_severity: string;
  trace_id: string;
  span_id: string;
  text: string;
  limit: string;
}
export function defaultLogFilters(hours = 1): LogFilters {
  const now = new Date();
  return {
    from: new Date(now.getTime() - hours * 3600000).toISOString(),
    to: now.toISOString(),
    service: "",
    min_severity: "0",
    trace_id: "",
    span_id: "",
    text: "",
    limit: "20",
  };
}
export function severityName(n: number) {
  return n === 0
    ? "UNSPECIFIED"
    : n <= 4
      ? "TRACE"
      : n <= 8
        ? "DEBUG"
        : n <= 12
          ? "INFO"
          : n <= 16
            ? "WARN"
            : n <= 20
              ? "ERROR"
              : n <= 24
                ? "FATAL"
                : "UNKNOWN";
}
export function correlationName(row: Pick<LogRow, "trace_id" | "span_id">) {
  return !row.trace_id
    ? "Standalone"
    : !row.span_id
      ? "Trace-only"
      : "Trace + span";
}
export function logSearchPath(
  filters: LogFilters,
  cursor = "",
  traceId?: string,
) {
  const q = new URLSearchParams();
  for (const [key, value] of Object.entries(filters)) {
    if (value && !(traceId && key === "trace_id")) q.set(key, value);
  }
  if (cursor) q.set("cursor", cursor);
  return (traceId ? `/api/traces/${traceId}/logs` : "/api/logs") + `?${q}`;
}
