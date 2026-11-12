import type { SpanNode } from "../lib/contracts";
import { durationNS, formatNS } from "../lib/timeline";
export const kindName = (kind: number) =>
  ["UNSPECIFIED", "INTERNAL", "SERVER", "CLIENT", "PRODUCER", "CONSUMER"][
    kind
  ] ?? `UNKNOWN (${kind})`;
export const statusName = (status: number) =>
  ["UNSET", "OK", "ERROR"][status] ?? `UNKNOWN (${status})`;
function DataBlock({ title, value }: { title: string; value: unknown }) {
  return (
    <details open>
      <summary>{title}</summary>
      <pre>{JSON.stringify(value, null, 2)}</pre>
    </details>
  );
}
export function SpanInspector({
  span,
  clear,
}: {
  span: SpanNode | null;
  clear: () => void;
}) {
  return (
    <aside className="panel inspector" aria-label="Span inspector">
      <div className="panel-heading">
        <h2>Span inspector</h2>
        {span && (
          <button className="quiet" onClick={clear}>
            Clear selection
          </button>
        )}
      </div>
      {!span ? (
        <div className="state">
          <span className="inspect-icon">↖</span>
          <h3>A closer look</h3>
          <p>
            Select an operation or timeline bar to inspect its recorded details.
          </p>
        </div>
      ) : (
        <div className="inspector-content">
          <p className="eyebrow">
            {kindName(span.span_kind)} / {span.service_name}
          </p>
          <h3>{span.span_name}</h3>
          <span className={span.status_code === 2 ? "badge error" : "badge"}>
            {statusName(span.status_code)}
          </span>
          <dl>
            <dt>Duration</dt>
            <dd>{formatNS(durationNS(span))}</dd>
            <dt>Span ID</dt>
            <dd className="mono">{span.span_id}</dd>
            <dt>Parent span ID</dt>
            <dd className="mono">{span.parent_span_id || "None recorded"}</dd>
            <dt>Trace ID</dt>
            <dd className="mono">{span.trace_id}</dd>
            <dt>Raw start · UTC</dt>
            <dd className="mono">{span.start_time}</dd>
            <dt>Raw end · UTC</dt>
            <dd className="mono">{span.end_time}</dd>
            <dt>Status message</dt>
            <dd>{span.status_message || "None recorded"}</dd>
            <dt>Instrumentation scope</dt>
            <dd>
              {span.scope_name || "Unnamed"} {span.scope_version}
            </dd>
          </dl>
          <DataBlock title="Span attributes" value={span.span_attributes} />
          <DataBlock
            title="Resource attributes"
            value={span.resource_attributes}
          />
          <DataBlock title="Instrumentation scope data" value={span.scope} />
          <DataBlock
            title={`Events (${span.events.length})`}
            value={span.events}
          />
          <DataBlock
            title={`Links (${span.links.length})`}
            value={span.links}
          />
        </div>
      )}
    </aside>
  );
}
