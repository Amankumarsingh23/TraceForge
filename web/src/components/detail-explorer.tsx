"use client";
import Link from "next/link";
import { useEffect, useRef, useState } from "react";
import { APIError, getJSON, LatestRequest } from "../lib/client";
import {
  validTraceId,
  type TraceDetail,
  type SpanNode,
} from "../lib/contracts";
import { flatten, formatNS, serviceColor, timestampNS } from "../lib/timeline";
import { Diagnostics } from "./diagnostics";
import { ErrorPanel } from "./error-panel";
import { SpanInspector } from "./span-inspector";
import { Waterfall } from "./waterfall";
import { CorrelatedLogs } from "./correlated-logs";
import { AgentSummary } from "./agent-summary";
export function DetailExplorer({ traceId }: { traceId: string }) {
  const [revision, setRevision] = useState(0);
  return (
    <DetailRequest
      key={`${traceId}-${revision}`}
      traceId={traceId}
      refresh={() => setRevision((n) => n + 1)}
    />
  );
}
function DetailRequest({
  traceId,
  refresh,
}: {
  traceId: string;
  refresh: () => void;
}) {
  const valid = validTraceId(traceId);
  const [trace, setTrace] = useState<TraceDetail | null>(null),
    [error, setError] = useState<unknown>(() =>
      valid
        ? null
        : new APIError(
            400,
            "invalid_trace_id",
            "Use a nonzero lowercase 32-character trace ID.",
          ),
    );
  const [loading, setLoading] = useState(valid);
  const [selected, setSelected] = useState<SpanNode | null>(null),
    [copy, setCopy] = useState("");
  const latest = useRef(new LatestRequest());
  useEffect(() => {
    const request = latest.current;
    if (!valid) return;
    void request.run(
      (signal) => getJSON<TraceDetail>(`/api/traces/${traceId}`, signal),
      (value) => {
        setTrace(value);
        setLoading(false);
      },
      (failure) => {
        setError(failure);
        setLoading(false);
      },
    );
    return () => request.cancel();
  }, [traceId, valid]);
  const envelope = trace?.observed_time_envelope;
  const hasError =
    trace && flatten(trace).some((row) => row.span.status_code === 2);
  return (
    <>
      <Link href="/traces" className="back-link">
        ← All traces
      </Link>
      <div className="page-heading detail-heading">
        <div>
          <p className="eyebrow">INVESTIGATE / TRACE DETAIL</p>
          <h1>One request. Every observed step.</h1>
          <div className="trace-identity">
            <code>{traceId}</code>
            <button
              className="quiet"
              onClick={async () => {
                try {
                  await navigator.clipboard.writeText(traceId);
                  setCopy("Copied");
                } catch {
                  setCopy("Copy unavailable; select the ID to copy.");
                }
              }}
            >
              Copy ID
            </button>
            <span role="status">{copy}</span>
          </div>
        </div>
        <button onClick={refresh}>↻ Refresh trace</button>
      </div>
      {loading && (
        <div className="panel state" role="status">
          <span className="spinner" />
          Loading stored trace observations…
        </div>
      )}
      {!!error && <ErrorPanel error={error} retry={refresh} />}
      {trace && (
        <>
          <section className="trace-summary">
            <div>
              <p>OBSERVED ENVELOPE</p>
              <strong>
                {envelope
                  ? formatNS(
                      timestampNS(envelope.end_time) -
                        timestampNS(envelope.start_time),
                    )
                  : "Unavailable"}
              </strong>
              <small>Earliest start → latest end</small>
            </div>
            <div>
              <p>LOGICAL SPANS</p>
              <strong>{trace.diagnostics.unique_spans}</strong>
              <small>{trace.diagnostics.physical_rows} physical rows</small>
            </div>
            <div>
              <p>OBSERVED SERVICES</p>
              <div className="service-list">
                {trace.observed_services.map((service) => (
                  <span className="service" key={service}>
                    <i style={{ background: serviceColor(service) }} />
                    {service}
                  </span>
                ))}
              </div>
            </div>
            <div>
              <p>OBSERVED STATUS</p>
              <span className={hasError ? "badge error" : "badge"}>
                {hasError ? "Contains error span" : "No observed error span"}
              </span>
              <small>Does not establish request outcome</small>
            </div>
          </section>
          {envelope && (
            <p className="raw-envelope mono">
              {envelope.start_time} → {envelope.end_time}
            </p>
          )}
          <Diagnostics data={trace.diagnostics} />
          <AgentSummary traceId={traceId} selectSpan={(id) => setSelected(flatten(trace).find(row => row.span.span_id === id)?.span ?? null)} />
          <div className="investigation-grid">
            <Waterfall
              trace={trace}
              selected={selected?.span_id ?? null}
              onSelect={setSelected}
            />
            <SpanInspector span={selected} clear={() => setSelected(null)} />
          </div>
          <CorrelatedLogs
            key={`${traceId}:${selected?.span_id ?? "trace"}`}
            traceId={traceId}
            spanId={selected?.span_id}
            origin={envelope?.start_time}
          />
        </>
      )}
    </>
  );
}
