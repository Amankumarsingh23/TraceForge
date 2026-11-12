"use client";
import { useMemo, useState } from "react";
import type { SpanNode, TraceDetail } from "../lib/contracts";
import {
  flatten,
  formatNS,
  geometry,
  serviceColor,
  timestampNS,
  visibleRows,
} from "../lib/timeline";
export function Waterfall({
  trace,
  selected,
  onSelect,
}: {
  trace: TraceDetail;
  selected: string | null;
  onSelect: (span: SpanNode) => void;
}) {
  const rows = useMemo(() => flatten(trace), [trace]);
  const [collapsed, setCollapsed] = useState<Set<string>>(new Set());
  const envelope = trace.observed_time_envelope;
  if (!envelope) return <p>No observed timeline is available.</p>;
  const start = timestampNS(envelope.start_time),
    end = timestampNS(envelope.end_time);
  const visible = visibleRows(rows, collapsed);
  return (
    <section className="panel waterfall-panel" aria-label="Span waterfall">
      <div className="panel-heading">
        <div>
          <h2>
            Execution waterfall{" "}
            <span className="count">{rows.length} spans</span>
          </h2>
          <p className="subtle">
            Recorded timing on one shared axis. Parallel spans may overlap.
          </p>
        </div>
        <div className="button-group">
          <button className="quiet" onClick={() => setCollapsed(new Set())}>
            Expand all
          </button>
          <button
            className="quiet"
            onClick={() =>
              setCollapsed(
                new Set(
                  rows
                    .filter((r) => r.span.children.length)
                    .map((r) => r.span.span_id),
                ),
              )
            }
          >
            Collapse all
          </button>
        </div>
      </div>
      <div className="waterfall-scroll">
        <div className="waterfall-grid">
          <div className="waterfall-axis">
            <span>OPERATION / SERVICE</span>
            <div className="axis-ticks">
              {[0n, 1n, 2n, 3n, 4n].map((n) => (
                <span key={String(n)}>
                  {formatNS(((end - start) * n) / 4n)}
                </span>
              ))}
            </div>
            <span>DURATION</span>
          </div>
          {visible.map(({ span, depth, group }) => {
            const box = geometry(span, start, end),
              color = serviceColor(span.service_name);
            const warning =
              box.anomalous || box.clipped || span.detached_parent_edge;
            return (
              <div
                key={span.span_id}
                className={`waterfall-row ${selected === span.span_id ? "selected" : ""}`}
                data-span-id={span.span_id}
              >
                <div
                  className="operation"
                  style={{ paddingLeft: `${Math.min(depth, 12) * 16 + 10}px` }}
                >
                  {span.children.length ? (
                    <button
                      className="tree-toggle"
                      aria-label={`${collapsed.has(span.span_id) ? "Expand" : "Collapse"} ${span.span_name} ${span.span_id}`}
                      aria-expanded={!collapsed.has(span.span_id)}
                      onClick={() =>
                        setCollapsed((current) => {
                          const next = new Set(current);
                          if (next.has(span.span_id)) next.delete(span.span_id);
                          else next.add(span.span_id);
                          return next;
                        })
                      }
                    >
                      {collapsed.has(span.span_id) ? "▸" : "▾"}
                    </button>
                  ) : (
                    <span className="tree-placeholder" />
                  )}
                  <button
                    className="span-label"
                    onClick={() => onSelect(span)}
                    aria-pressed={selected === span.span_id}
                  >
                    <strong title={span.span_name}>{span.span_name}</strong>
                    <small>
                      <i style={{ background: color }} />
                      {span.service_name}
                      {depth === 0 && <> · {group}</>}
                      {depth > 12 && <> · depth {depth}</>}
                    </small>
                  </button>
                </div>
                <button
                  className="timeline-track"
                  onClick={() => onSelect(span)}
                  aria-label={`Inspect ${span.span_name} · ${span.service_name} · ${formatNS(box.duration)}${span.status_code === 2 ? " · Error span" : ""}`}
                  title={`Offset ${formatNS(timestampNS(span.start_time) - start)} · ${formatNS(box.duration)}`}
                >
                  <span
                    className={`timeline-bar ${span.status_code === 2 ? "failed" : ""}`}
                    style={{
                      left: `${box.left}%`,
                      width: `${box.width}%`,
                      background: span.status_code === 2 ? "#dc3545" : color,
                    }}
                  />
                </button>
                <div className="span-duration mono">
                  {formatNS(box.duration)}
                  {span.status_code === 2 && (
                    <span className="error-dot" title="Observed error span">
                      !
                    </span>
                  )}
                  {warning && (
                    <span title="Recorded timing or detached edge warning">
                      {" "}
                      ⚠
                    </span>
                  )}
                </div>
              </div>
            );
          })}
        </div>
      </div>
      <p className="waterfall-note">
        {visible.length} / {rows.length} rows shown · Tiny and zero-duration
        spans use a minimum visual marker. Numeric durations are unchanged. ⚠
        marks clipped timing or a detached parent edge.
      </p>
    </section>
  );
}
