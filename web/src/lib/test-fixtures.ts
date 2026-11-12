import type { SpanNode, TraceDetail } from "./contracts";
export const at = (ns: string) => `2026-11-12T00:00:00.${ns.padStart(9, "0")}Z`;
export function span(id: string, start = "0", end = "1000"): SpanNode {
  return {
    span_id: id,
    trace_id: "a".repeat(32),
    parent_span_id: "",
    service_name: "gateway",
    span_name: "checkout",
    span_kind: 2,
    start_time: at(start),
    end_time: at(end),
    status_code: 0,
    status_message: "",
    resource_attributes: [],
    span_attributes: [],
    events: [],
    links: [],
    scope: {},
    scope_name: "demo",
    scope_version: "1",
    detached_parent_edge: false,
    children: [],
  };
}
export function detail(roots: SpanNode[] = []): TraceDetail {
  return {
    trace_id: "a".repeat(32),
    root_spans: roots,
    orphan_spans: [],
    disconnected_spans: [],
    observed_services: ["gateway"],
    observed_time_envelope: { start_time: at("0"), end_time: at("1000") },
    diagnostics: {
      physical_rows: roots.length,
      unique_spans: roots.length,
      exact_duplicate_rows: 0,
      conflicting_duplicate_groups: 0,
      conflicts: [],
      observed_root_count: roots.length,
      orphan_span_count: 0,
      missing_parent_count: 0,
      missing_parent_ids: [],
      cycle_count: 0,
      cycles: [],
      multiple_roots: false,
      has_detected_gaps: false,
      timing_warnings: [],
      completeness: "unknown",
    },
  };
}
