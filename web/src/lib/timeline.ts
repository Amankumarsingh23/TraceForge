import type { SpanNode, TraceDetail } from "./contracts";

// Parse whole seconds with Date, then append the untouched fractional ns.
// Never round absolute Unix nanoseconds through a JavaScript number.
export function timestampNS(value: string): bigint {
  const m = /^(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2})(?:\.(\d{1,9}))?Z$/.exec(
    value,
  );
  if (!m) throw new Error("Expected a UTC RFC3339 nanosecond timestamp");
  const seconds = Date.parse(m[1] + "Z");
  if (
    !Number.isFinite(seconds) ||
    new Date(seconds).toISOString().slice(0, 19) !== m[1]
  )
    throw new Error("Invalid timestamp");
  return BigInt(seconds) * 1_000_000n + BigInt((m[2] ?? "").padEnd(9, "0"));
}
export function durationNS(
  span: Pick<SpanNode, "start_time" | "end_time">,
): bigint {
  return timestampNS(span.end_time) - timestampNS(span.start_time);
}
export function formatNS(value: bigint): string {
  const sign = value < 0n ? "−" : "";
  const ns = value < 0n ? -value : value;
  if (ns < 1000n) return `${sign}${ns} ns`;
  const unit =
    ns < 1_000_000n ? 1000n : ns < 1_000_000_000n ? 1_000_000n : 1_000_000_000n;
  const suffix = unit === 1000n ? "µs" : unit === 1_000_000n ? "ms" : "s";
  const scaled = (ns * 1000n) / unit;
  return `${sign}${scaled / 1000n}.${String(scaled % 1000n).padStart(3, "0")} ${suffix}`;
}
const percent = (part: bigint, total: bigint) =>
  Number((part * 1_000_000n) / total) / 10_000;
export function geometry(span: SpanNode, start: bigint, end: bigint) {
  const rawStart = timestampNS(span.start_time),
    rawEnd = timestampNS(span.end_time);
  const total = end - start;
  const duration = rawEnd - rawStart;
  if (total <= 0n)
    return {
      left: 0,
      width: 0,
      duration,
      anomalous: duration < 0n,
      clipped: false,
    };
  const clamp = (n: bigint) => (n < start ? start : n > end ? end : n);
  const left = clamp(rawStart),
    right = clamp(rawEnd < rawStart ? rawStart : rawEnd);
  return {
    left: percent(left - start, total),
    width: percent(right - left, total),
    duration,
    anomalous: duration < 0n,
    clipped:
      rawStart < start || rawStart > end || rawEnd > end || rawEnd < start,
  };
}
const colors = [
  "#2563eb",
  "#0f766e",
  "#9333ea",
  "#b45309",
  "#be185d",
  "#4f46e5",
  "#047857",
];
export function serviceColor(service: string) {
  let hash = 2166136261;
  for (const c of service) hash = Math.imul(hash ^ c.charCodeAt(0), 16777619);
  return colors[(hash >>> 0) % colors.length];
}
export interface FlatSpan {
  span: SpanNode;
  depth: number;
  group: string;
  ancestors: string[];
}
// Iterative traversal uses the backend forest only. Duplicate/cycle guards keep
// malformed inputs finite; no client-side parent reconstruction or edge repair.
export function flatten(detail: TraceDetail): FlatSpan[] {
  const stack: FlatSpan[] = [];
  for (const [group, nodes] of [
    ["Roots", detail.root_spans],
    ["Missing parent", detail.orphan_spans],
    ["Disconnected", detail.disconnected_spans],
  ] as const) {
    for (const span of nodes)
      stack.push({ span, depth: 0, group, ancestors: [] });
  }
  stack.reverse();
  const visited = new Set<string>(),
    rows: FlatSpan[] = [];
  while (stack.length) {
    const row = stack.pop()!;
    if (visited.has(row.span.span_id)) continue;
    visited.add(row.span.span_id);
    rows.push(row);
    for (let i = row.span.children.length - 1; i >= 0; i--)
      stack.push({
        span: row.span.children[i],
        depth: row.depth + 1,
        group: row.group,
        ancestors: [...row.ancestors, row.span.span_id],
      });
  }
  return rows;
}
export function visibleRows(rows: FlatSpan[], collapsed: ReadonlySet<string>) {
  return rows.filter((row) => !row.ancestors.some((id) => collapsed.has(id)));
}
