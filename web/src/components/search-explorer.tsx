"use client";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useEffect, useRef, useState } from "react";
import type { SearchResponse } from "../lib/contracts";
import { validTraceId } from "../lib/contracts";
import { getJSON, LatestRequest } from "../lib/client";
import {
  applyFilters,
  defaultFilters,
  nextPage,
  previousPage,
  searchPath,
  type Filters,
  type SearchState,
} from "../lib/search";
import { formatNS, timestampNS, serviceColor } from "../lib/timeline";
import { ErrorPanel } from "./error-panel";

export function SearchExplorer({ initial }: { initial: Filters }) {
  const router = useRouter();
  const [draft, setDraft] = useState<Filters>(initial);
  const [state, setState] = useState<SearchState>(applyFilters(initial));
  const [data, setData] = useState<SearchResponse | null>(null);
  const [loading, setLoading] = useState(true),
    [error, setError] = useState<unknown>(null);
  const [lookup, setLookup] = useState(""),
    [lookupError, setLookupError] = useState("");
  const [revision, setRevision] = useState(0);
  const latest = useRef(new LatestRequest());
  function begin() {
    latest.current.cancel();
    setLoading(true);
    setError(null);
    setData(null);
  }
  function refresh() {
    begin();
    setRevision((n) => n + 1);
  }
  useEffect(() => {
    const request = latest.current;
    void request.run(
      (signal) =>
        getJSON<SearchResponse>(
          searchPath(state.filters, state.cursor),
          signal,
        ),
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
  }, [state, revision]);
  function update(key: keyof Filters, value: string) {
    setDraft((current) => ({ ...current, [key]: value }));
  }
  return (
    <>
      <div className="page-heading">
        <div>
          <p className="eyebrow">INVESTIGATE / TRACES</p>
          <h1>Find the story behind a request.</h1>
          <p className="subtle">
            Search observed spans. Follow a request across services.
          </p>
        </div>
        <span className="section-number">01 / EXPLORER</span>
      </div>
      <section className="panel search-panel" aria-label="Search filters">
        <form
          onSubmit={(e) => {
            e.preventDefault();
            begin();
            setState(applyFilters({ ...draft }));
          }}
        >
          <div className="filter-grid">
            <label>
              From · UTC
              <input
                aria-label="From UTC"
                value={draft.from}
                onChange={(e) => update("from", e.target.value)}
                required
                placeholder="2026-11-12T00:00:00Z"
              />
            </label>
            <label>
              To · UTC
              <input
                aria-label="To UTC"
                value={draft.to}
                onChange={(e) => update("to", e.target.value)}
                required
                placeholder="2026-11-12T01:00:00Z"
              />
            </label>
            <label>
              Service
              <input
                value={draft.service}
                onChange={(e) => update("service", e.target.value)}
                placeholder="All services"
                maxLength={128}
              />
            </label>
            <label>
              Observed status
              <select
                value={draft.status}
                onChange={(e) => update("status", e.target.value)}
              >
                <option value="">All observations</option>
                <option value="error">Contains error span</option>
              </select>
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
            <p>UTC RFC3339 timestamps · Maximum 24-hour window</p>
            <button
              type="button"
              className="quiet"
              onClick={() => {
                const value = defaultFilters();
                begin();
                setDraft(value);
                setState(applyFilters(value));
              }}
            >
              Clear filters
            </button>
            <button type="submit" className="primary">
              Search traces <span aria-hidden>→</span>
            </button>
          </div>
        </form>
      </section>
      <form
        className="lookup"
        onSubmit={(e) => {
          e.preventDefault();
          if (validTraceId(lookup.trim()))
            router.push(`/traces/${lookup.trim()}`);
          else
            setLookupError("Enter a nonzero lowercase 32-character trace ID.");
        }}
      >
        <label htmlFor="trace-lookup">Already have a trace ID?</label>
        <input
          id="trace-lookup"
          value={lookup}
          onChange={(e) => {
            setLookup(e.target.value);
            setLookupError("");
          }}
          placeholder="Paste the full trace ID"
          maxLength={32}
        />
        <button>Open trace ↗</button>
        {lookupError && <span role="alert">{lookupError}</span>}
      </form>
      <section
        className="panel results"
        aria-label="Trace results"
        aria-busy={loading}
      >
        <div className="panel-heading">
          <h2>
            Observed traces{" "}
            <span className="count">{data?.traces.length ?? "—"}</span>
          </h2>
          <button className="quiet" onClick={refresh}>
            ↻ Refresh
          </button>
        </div>
        {loading && (
          <div className="state" role="status">
            <span className="spinner" />
            Searching stored observations…
          </div>
        )}
        {!!error && <ErrorPanel error={error} retry={refresh} />}
        {data && (
          <>
            {data.traces.length === 0 ? (
              <div className="state">
                <h3>No matching traces</h3>
                <p>
                  Try a wider UTC window or clear the service and error filters.
                </p>
              </div>
            ) : (
              <div className="table-scroll">
                <table>
                  <thead>
                    <tr>
                      <th>Trace ID / observed start (UTC)</th>
                      <th>Observed services</th>
                      <th>Observed envelope</th>
                      <th>Unique spans</th>
                      <th>Observation</th>
                      <th>
                        <span className="sr-only">Open</span>
                      </th>
                    </tr>
                  </thead>
                  <tbody>
                    {data.traces.map((trace) => (
                      <tr key={trace.trace_id}>
                        <td>
                          <Link
                            className="trace-link mono"
                            href={`/traces/${trace.trace_id}`}
                          >
                            {trace.trace_id}
                          </Link>
                          <small>{trace.first_observed_start}</small>
                        </td>
                        <td>
                          <div className="service-list">
                            {trace.observed_services.map((service) => (
                              <span key={service} className="service">
                                <i
                                  style={{ background: serviceColor(service) }}
                                />
                                {service}
                              </span>
                            ))}
                          </div>
                        </td>
                        <td className="mono">
                          {formatNS(
                            timestampNS(trace.last_observed_end) -
                              timestampNS(trace.first_observed_start),
                          )}
                        </td>
                        <td>{trace.unique_span_count}</td>
                        <td>
                          <span
                            className={
                              trace.has_error_span ? "badge error" : "badge"
                            }
                          >
                            {trace.has_error_span
                              ? "Contains error span"
                              : "No observed error span"}
                          </span>
                        </td>
                        <td>
                          <Link
                            aria-label={`Open trace ${trace.trace_id}`}
                            href={`/traces/${trace.trace_id}`}
                          >
                            ↗
                          </Link>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
            <div className="pagination">
              <p>
                Page {(state?.previous.length ?? 0) + 1} · Summaries use spans
                starting in this window.
                <br />
                <span>
                  Non-snapshot pagination; late telemetry may move results.
                  Detail includes all observed services.
                </span>
              </p>
              <button
                disabled={!state?.previous.length}
                onClick={() => {
                  begin();
                  setState(previousPage(state));
                }}
              >
                ← Previous
              </button>
              <button
                disabled={!data.next_cursor}
                onClick={() => {
                  begin();
                  setState(nextPage(state, data.next_cursor));
                }}
              >
                Next page →
              </button>
            </div>
          </>
        )}
      </section>
    </>
  );
}
