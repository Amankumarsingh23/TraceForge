"use client";
import { useState } from "react";
import { defaultLogFilters, logSearchPath } from "../lib/logs";
import { LogResults } from "./log-results";
export function CorrelatedLogs({
  traceId,
  spanId,
  origin,
}: {
  traceId: string;
  spanId?: string;
  origin?: string;
}) {
  const [mode, setMode] = useState(spanId ? "span" : "trace"),
    [window, setWindow] = useState(() => defaultLogFilters(24)),
    [cursor, setCursor] = useState(""),
    [history, setHistory] = useState<string[]>([]),
    [revision, setRevision] = useState(0);
  function refresh() {
    setWindow(defaultLogFilters(24));
    setCursor("");
    setHistory([]);
    setRevision((n) => n + 1);
  }
  const filters = {
    ...window,
    trace_id: traceId,
    span_id: mode === "span" ? (spanId ?? "") : "",
  };
  const path = logSearchPath(filters, cursor, traceId);
  return (
    <section id="correlated-logs" className="panel log-panel" aria-label="Correlated logs">
      <div className="panel-heading">
        <div>
          <h2>Correlated logs</h2>
          <p className="subtle">
            Exact identifiers · Last 24 hours of effective time · Refresh for
            late-arriving records
          </p>
        </div>
        <div className="button-group">
          <button
            aria-pressed={mode === "trace"}
            onClick={() => {
              setMode("trace");
              setCursor("");
              setHistory([]);
            }}
          >
            All trace logs
          </button>
          <button
            aria-pressed={mode === "span"}
            disabled={!spanId}
            onClick={() => {
              setMode("span");
              setCursor("");
              setHistory([]);
            }}
          >
            Selected span logs
          </button>
          <button onClick={refresh}>↻ Refresh correlated logs</button>
        </div>
      </div>
      <p className="correlation-context mono">
        Trace {traceId}
        {mode === "span" && spanId
          ? ` · Span ${spanId}`
          : " · Includes trace-only records with no span ID"}
      </p>
      <LogResults
        key={`${path}:${revision}`}
        path={path}
        retry={refresh}
        page={history.length + 1}
        hasPrevious={!!history.length}
        origin={origin}
        previous={() => {
          setCursor(history.at(-1) ?? "");
          setHistory((h) => h.slice(0, -1));
        }}
        next={(value) => {
          setHistory((h) => [...h, cursor]);
          setCursor(value);
        }}
      />
    </section>
  );
}
