"use client";
import { useEffect, useRef, useState } from "react";
import { getJSON, LatestRequest } from "../lib/client";
import { ErrorPanel } from "./error-panel";
import { formatNS } from "../lib/timeline";

type Operation = {
  span_id: string;
  role: string;
  duration_ns: string;
  status_code: number;
  [key: string]: unknown;
};
export type AgentSummaryData = {
  trace_id: string;
  completeness: string;
  runs: {
    run_id: string;
    agent_name: string;
    observed_duration_ns: string;
    mode: string;
    outcome: string;
    versions: Record<string, string>;
    model_calls: number;
    provider_attempts: number;
    tool_executions: number;
    known_input_tokens: string;
    known_output_tokens: string;
    known_cached_input_tokens: string;
    known_reasoning_output_tokens: string;
    complete_usage_attempts: number;
    usage_coverage: string;
    priced_attempts: number;
    cost_coverage: string;
    known_estimated_nano_usd: string;
    estimated_nano_usd: string | null;
    operations: Operation[];
  }[];
};
export function nanoUSD(value: string) {
  const n = BigInt(value);
  return `$${n / 1000000000n}.${(n % 1000000000n).toString().padStart(9, "0")}`;
}
function display(value: unknown): string {
  if (value === undefined || value === null) return "unknown";
  if (Array.isArray(value)) return value.length ? value.join(", ") : "none";
  return String(value);
}
export function AgentSummary({
  traceId,
  selectSpan,
}: {
  traceId: string;
  selectSpan: (id: string) => void;
}) {
  const [data, setData] = useState<AgentSummaryData | null>(null);
  const [error, setError] = useState<unknown>(null);
  const [revision, setRevision] = useState(0);
  const latest = useRef(new LatestRequest());
  useEffect(() => {
    const request = latest.current;
    void request.run(
      (signal) =>
        getJSON<AgentSummaryData>(
          `/api/traces/${traceId}/agent-summary`,
          signal,
        ),
      setData,
      setError,
    );
    return () => request.cancel();
  }, [traceId, revision]);
  if (error)
    return (
      <section className="panel agent-panel">
        <h2>AgentOps summary unavailable</h2>
        <ErrorPanel
          error={error}
          retry={() => {
            setError(null);
            setRevision((n) => n + 1);
          }}
        />
      </section>
    );
  if (!data || data.runs.length === 0) return null;
  return (
    <section
      className="panel agent-panel"
      aria-label="AgentOps execution summary"
    >
      <p className="eyebrow">AGENTOPS / OBSERVED EXECUTION</p>
      <h2>Retrieval, model attempts & tools</h2>
      <p>
        Trace completeness: {data.completeness}. Canonical provider attempts
        supply accounting. Estimates are not billed costs or quality scores.
      </p>
      {data.runs.map((run) => (
        <article key={run.run_id}>
          <div className="agent-run-title">
            <code>{run.run_id}</code>
            <span className={run.mode === "fixture" ? "badge" : "badge error"}>
              {run.mode === "fixture"
                ? "Fixture · synthetic usage"
                : `${run.mode} · provider usage`}
            </span>
            <strong>Outcome: {run.outcome}</strong>
          </div>
          <div className="agent-metrics">
            <div>
              <small>AGENT / OBSERVED ENVELOPE</small>
              <strong>{run.agent_name}</strong>
              <small>
                {formatNS(BigInt(run.observed_duration_ns))} · overlapping
                operations are not summed
              </small>
            </div>
            <div>
              <small>LOGICAL MODEL CALLS / ATTEMPTS</small>
              <strong>
                {run.model_calls} / {run.provider_attempts}
              </strong>
            </div>
            <div>
              <small>TOOLS ENTERED</small>
              <strong>{run.tool_executions}</strong>
            </div>
            <div>
              <small>KNOWN INPUT / OUTPUT SUBTOTAL</small>
              <strong>
                {run.known_input_tokens} / {run.known_output_tokens}
              </strong>
              <small>
                Usage: {run.complete_usage_attempts}/{run.provider_attempts}{" "}
                complete attempts · {run.usage_coverage}
              </small>
              <small>
                Known cached input {run.known_cached_input_tokens}; reasoning
                output {run.known_reasoning_output_tokens} (subsets)
              </small>
            </div>
            <div>
              <small>ESTIMATED PROVIDER COST</small>
              <strong>
                {run.estimated_nano_usd === null
                  ? "Unknown"
                  : nanoUSD(run.estimated_nano_usd)}
              </strong>
              <small>
                Known estimate subtotal: {nanoUSD(run.known_estimated_nano_usd)}
              </small>
              <small>
                {run.priced_attempts}/{run.provider_attempts} priced ·{" "}
                {run.cost_coverage}
              </small>
            </div>
          </div>
          <details>
            <summary>Version metadata</summary>
            {Object.entries(run.versions).map(([key, value]) => (
              <p className="mono" key={key}>
                {key}: {value}
              </p>
            ))}
          </details>
          <div className="table-scroll">
            <table className="agent-operations">
              <thead>
                <tr>
                  <th>Operation / span</th>
                  <th>Observed metadata</th>
                  <th>Outcome / usage</th>
                </tr>
              </thead>
              <tbody>
                {run.operations
                  .filter((op) =>
                    ["model", "attempt", "retrieval", "tool"].includes(op.role),
                  )
                  .map((op) => (
                    <tr key={op.span_id}>
                      <td>
                        <button
                          className="quiet"
                          onClick={() => selectSpan(op.span_id)}
                        >
                          {op.role} · {op.span_id.slice(0, 8)}
                        </button>
                        <small>
                          {(Number(op.duration_ns) / 1000000).toFixed(2)} ms
                        </small>
                      </td>
                      <td>
                        {op.role === "retrieval" ? (
                          <>
                            <p>
                              Retrieved IDs:{" "}
                              {display(op["traceforge.retrieval.document_ids"])}
                            </p>
                            <p>
                              Ranks: {display(op["traceforge.retrieval.ranks"])}{" "}
                              · scores:{" "}
                              {display(op["traceforge.retrieval.scores"])}
                            </p>
                            <p>
                              Supplied IDs:{" "}
                              {display(op["traceforge.context.document_ids"])}
                            </p>
                          </>
                        ) : op.role === "tool" ? (
                          <>
                            <p>{display(op["gen_ai.tool.name"])}</p>
                            <p>
                              Function entered:{" "}
                              {display(op["traceforge.tool.executed"])}
                            </p>
                          </>
                        ) : (
                          <>
                            <p>{display(op["gen_ai.provider.name"])}</p>
                            <p>
                              Requested: {display(op["gen_ai.request.model"])}
                            </p>
                            <p>
                              Response: {display(op["gen_ai.response.model"])}
                            </p>
                            {op.role === "attempt" && (
                              <p>
                                Attempt{" "}
                                {display(op["traceforge.provider.attempt"])} ·
                                response ID {display(op["gen_ai.response.id"])}
                              </p>
                            )}
                          </>
                        )}
                      </td>
                      <td>
                        <p>
                          {display(
                            op["error.type"] ?? op["traceforge.outcome"],
                          )}
                        </p>
                        {op.role === "attempt" && (
                          <>
                            <p>
                              Input / output: {display(op.input_tokens)} /{" "}
                              {display(op.output_tokens)}
                            </p>
                            <p>
                              Provenance:{" "}
                              {display(op["traceforge.usage.source"])}
                            </p>
                            <p>
                              Estimate:{" "}
                              {typeof op.estimated_nano_usd === "string"
                                ? nanoUSD(op.estimated_nano_usd)
                                : "unknown"}
                            </p>
                            <p>
                              {op["traceforge.pricing.hypothetical"] === true
                                ? "Hypothetical price"
                                : "Price"}
                              : {display(op["traceforge.pricing.version"])}
                            </p>
                            <p className="mono">
                              {display(op["traceforge.pricing.source"])}
                            </p>
                          </>
                        )}
                      </td>
                    </tr>
                  ))}
              </tbody>
            </table>
          </div>
        </article>
      ))}
    </section>
  );
}
