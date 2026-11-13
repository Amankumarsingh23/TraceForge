"use client";
import Link from "next/link";
import { useEffect, useRef, useState } from "react";
import { getJSON, LatestRequest } from "../lib/client";
import { correlationName, severityName, type LogResponse } from "../lib/logs";
import { formatNS, timestampNS } from "../lib/timeline";
import { ErrorPanel } from "./error-panel";
export function LogResults({
  path,
  retry,
  page,
  previous,
  next,
  hasPrevious,
  origin,
}: {
  path: string;
  retry: () => void;
  page: number;
  previous: () => void;
  next: (cursor: string) => void;
  hasPrevious: boolean;
  origin?: string;
}) {
  const [data, setData] = useState<LogResponse | null>(null),
    [error, setError] = useState<unknown>(null),
    [loading, setLoading] = useState(true);
  const latest = useRef(new LatestRequest());
  useEffect(() => {
    const request = latest.current;
    void request.run(
      (signal) => getJSON<LogResponse>(path, signal),
      (result) => {
        setData(result);
        setLoading(false);
      },
      (failure) => {
        setError(failure);
        setLoading(false);
      },
    );
    return () => request.cancel();
  }, [path]);
  return (
    <div aria-label="Log results" aria-busy={loading}>
      {loading && (
        <div className="state" role="status">
          Loading stored logs…
        </div>
      )}
      {!!error && <ErrorPanel error={error} retry={retry} resource="logs" />}
      {data && (
        <>
          {data.logs.length === 0 ? (
            <div className="state">
              <h3>No matching logs</h3>
              <p>
                No records in this effective-time window. Logs may arrive later;
                refresh or adjust the window.
              </p>
            </div>
          ) : (
            <div className="log-list">
              {data.logs.map((log) => (
                <details
                  className="log-record"
                  key={log.log_id}
                  data-log-id={log.log_id}
                >
                  <summary>
                    <span
                      className={`badge ${log.severity_number >= 17 ? "error" : ""}`}
                    >
                      {severityName(log.severity_number)}
                    </span>
                    <span className="log-service">{log.service_name}</span>
                    <span className="log-message">
                      {log.body_text || "(no body)"}
                    </span>
                    <span className="log-time mono">{log.effective_time}</span>
                  </summary>
                  <div className="log-content">
                    <div className="log-meta">
                      <span>{correlationName(log)}</span>
                      <span>
                        Time source: {log.time_source}
                        {log.time_source === "ingestion"
                          ? " (event time unknown)"
                          : ""}
                      </span>
                      {origin && (
                        <span>
                          Recorded relative offset:{" "}
                          {formatNS(
                            timestampNS(log.effective_time) -
                              timestampNS(origin),
                          )}
                        </span>
                      )}
                    </div>
                    <dl>
                      <dt>Storage row ID</dt>
                      <dd className="mono">{log.log_id}</dd>
                      <dt>Source event identity</dt>
                      <dd>{log.source_event_id || "None supplied"}</dd>
                      <dt>Trace ID</dt>
                      <dd>
                        {log.trace_id ? (
                          <>
                            <Link href={`/traces/${log.trace_id}`}>
                              {log.trace_id}
                            </Link>
                            <small> · Opens observed trace, if available</small>
                          </>
                        ) : (
                          "None — standalone log"
                        )}
                      </dd>
                      <dt>Span ID</dt>
                      <dd className="mono">{log.span_id || "None supplied"}</dd>
                      <dt>Event timestamp</dt>
                      <dd className="mono">
                        {log.event_time ?? "Unknown / absent"}
                      </dd>
                      <dt>Observed timestamp</dt>
                      <dd className="mono">
                        {log.observed_time ?? "Unknown / absent"}
                      </dd>
                      <dt>Ingested at</dt>
                      <dd className="mono">{log.ingested_at}</dd>
                      <dt>Original severity</dt>
                      <dd>
                        {log.severity_number} ·{" "}
                        {log.severity_text || "Unspecified text"}
                      </dd>
                      <dt>Event name / scope</dt>
                      <dd>
                        {log.event_name || "No event name"} · {log.scope_name}{" "}
                        {log.scope_version}
                      </dd>
                    </dl>
                    <h4>Typed body</h4>
                    <pre>{JSON.stringify(log.body, null, 2)}</pre>
                    <h4>Attributes</h4>
                    <pre>{JSON.stringify(log.attributes, null, 2)}</pre>
                    <h4>Resource metadata</h4>
                    <pre>{JSON.stringify(log.resource, null, 2)}</pre>
                    <h4>Scope metadata</h4>
                    <pre>{JSON.stringify(log.scope, null, 2)}</pre>
                    <details>
                      <summary>Preserved OTLP record</summary>
                      <pre>{JSON.stringify(log.record, null, 2)}</pre>
                    </details>
                  </div>
                </details>
              ))}
            </div>
          )}
          <div className="pagination">
            <p>
              Page {page} · Raw physical rows; repeated events and replay rows
              are retained.
              <br />
              <span>
                Effective-time ordering · Non-snapshot pagination · No
                completeness guarantee
              </span>
            </p>
            <button disabled={!hasPrevious} onClick={previous}>
              ← Previous logs
            </button>
            <button
              disabled={!data.next_cursor}
              onClick={() => next(data.next_cursor)}
            >
              Next logs →
            </button>
          </div>
        </>
      )}
    </div>
  );
}
