// Mirrors internal/traces/model.go. Large numeric ns fields are intentionally
// not consumed: RFC3339Nano strings preserve all timestamp digits on the wire.
export interface SpanNode {
  trace_id: string;
  span_id: string;
  parent_span_id: string;
  service_name: string;
  span_name: string;
  span_kind: number;
  start_time: string;
  end_time: string;
  status_code: number;
  status_message: string;
  resource_attributes: unknown[];
  span_attributes: unknown[];
  scope_name: string;
  scope_version: string;
  scope: unknown;
  events: unknown[];
  links: unknown[];
  detached_parent_edge: boolean;
  children: SpanNode[];
}
export interface Diagnostics {
  physical_rows: number;
  unique_spans: number;
  exact_duplicate_rows: number;
  conflicting_duplicate_groups: number;
  conflicts: { span_id: string; variants: number }[];
  observed_root_count: number;
  orphan_span_count: number;
  missing_parent_count: number;
  missing_parent_ids: string[];
  cycle_count: number;
  cycles: string[][];
  multiple_roots: boolean;
  has_detected_gaps: boolean;
  timing_warnings: { span_id: string; parent_span_id?: string; code: string }[];
  completeness: string;
}
export interface TraceDetail {
  trace_id: string;
  root_spans: SpanNode[];
  orphan_spans: SpanNode[];
  disconnected_spans: SpanNode[];
  observed_services: string[];
  observed_time_envelope: { start_time: string; end_time: string } | null;
  diagnostics: Diagnostics;
}
export interface TraceSummary {
  trace_id: string;
  first_observed_start: string;
  last_observed_end: string;
  latest_observed_start: string;
  observed_services: string[];
  unique_span_count: number;
  has_error_span: boolean;
}
export interface SearchResponse {
  traces: TraceSummary[];
  next_cursor: string;
  from: string;
  to: string;
  summary_scope: string;
  consistency: string;
}
export interface BackendError {
  error: { code: string; message: string };
}
export const validTraceId = (id: string) =>
  /^[0-9a-f]{32}$/.test(id) && !/^0+$/.test(id);
