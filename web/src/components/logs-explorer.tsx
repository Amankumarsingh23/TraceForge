"use client";
import { useState } from "react";
import { defaultLogFilters, logSearchPath, type LogFilters } from "../lib/logs";
import { LogResults } from "./log-results";
export function LogsExplorer({ initial }: { initial: LogFilters }) {
  const [draft, setDraft] = useState(initial),
    [applied, setApplied] = useState(initial),
    [cursor, setCursor] = useState(""),
    [history, setHistory] = useState<string[]>([]),
    [revision, setRevision] = useState(0);
  function apply(value: LogFilters) {
    setDraft(value);
    setApplied({ ...value });
    setCursor("");
    setHistory([]);
    setRevision((n) => n + 1);
  }
  function update(key: keyof LogFilters, value: string) {
    setDraft((d) => ({ ...d, [key]: value }));
  }
  const path = logSearchPath(applied, cursor),
    retry = () => setRevision((n) => n + 1);
  return (
    <>
      <div className="page-heading">
        <div>
          <p className="eyebrow">INVESTIGATE / LOGS</p>
          <h1>Find the event behind the operation.</h1>
          <p className="subtle">
            Structured records, exact correlation, preserved repeats.
          </p>
        </div>
        <span className="section-number">02 / LOG SEARCH</span>
      </div>
      <section className="panel search-panel">
        <form
          onSubmit={(e) => {
            e.preventDefault();
            apply(draft);
          }}
        >
          <div className="log-filter-grid">
            <label>
              From · UTC
              <input
                aria-label="Logs from UTC"
                value={draft.from}
                onChange={(e) => update("from", e.target.value)}
                required
              />
            </label>
            <label>
              To · UTC
              <input
                aria-label="Logs to UTC"
                value={draft.to}
                onChange={(e) => update("to", e.target.value)}
                required
              />
            </label>
            <label>
              Service
              <input
                value={draft.service}
                onChange={(e) => update("service", e.target.value)}
                placeholder="All services"
              />
            </label>
            <label>
              Minimum severity
              <select
                value={draft.min_severity}
                onChange={(e) => update("min_severity", e.target.value)}
              >
                {[
                  [0, "All severities"],
                  [1, "TRACE+"],
                  [5, "DEBUG+"],
                  [9, "INFO+"],
                  [13, "WARN+"],
                  [17, "ERROR+"],
                  [21, "FATAL"],
                ].map(([value, label]) => (
                  <option value={value} key={value}>
                    {label}
                  </option>
                ))}
              </select>
            </label>
            <label>
              Trace ID
              <input
                value={draft.trace_id}
                onChange={(e) => update("trace_id", e.target.value)}
                placeholder="Optional, 32 lowercase hex"
                maxLength={32}
              />
            </label>
            <label>
              Span ID
              <input
                value={draft.span_id}
                onChange={(e) => update("span_id", e.target.value)}
                placeholder="Requires trace ID"
                maxLength={16}
              />
            </label>
            <label>
              Literal body text
              <input
                value={draft.text}
                onChange={(e) => update("text", e.target.value)}
                maxLength={256}
                placeholder="Case-sensitive text, no query language"
              />
            </label>
            <label>
              Page size
              <select
                value={draft.limit}
                onChange={(e) => update("limit", e.target.value)}
              >
                {[10, 20, 50, 100].map((n) => (
                  <option key={n}>{n}</option>
                ))}
              </select>
            </label>
          </div>
          <div className="filter-actions">
            <p>
              UTC window ≤24h · Trace/span filters match exact recorded
              identifiers
            </p>
            <button
              type="button"
              className="quiet"
              onClick={() => apply(defaultLogFilters())}
            >
              Clear log filters
            </button>
            <button className="primary">Search logs →</button>
          </div>
        </form>
      </section>
      <section className="panel log-panel">
        <div className="panel-heading">
          <h2>Stored logs</h2>
          <button className="quiet" onClick={retry}>
            ↻ Refresh logs
          </button>
        </div>
        <LogResults
          key={`${path}:${revision}`}
          path={path}
          retry={retry}
          page={history.length + 1}
          hasPrevious={!!history.length}
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
    </>
  );
}
