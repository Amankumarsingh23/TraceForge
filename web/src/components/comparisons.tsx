"use client";
import Link from "next/link";
import { useEffect, useRef, useState } from "react";
import { getJSON, LatestRequest } from "../lib/client";
import { ErrorPanel } from "./error-panel";
type Pair = {
  case_id: string;
  transition: string;
  execution_transition: string;
  baseline_trace_id: string | null;
  candidate_trace_id: string | null;
  baseline_trace_visibility: string;
  candidate_trace_visibility: string;
  baseline_run_id: string;
  candidate_run_id: string;
  baseline_supplied: string[];
  candidate_supplied: string[];
};
type Metric = {
  baseline: number | string | null;
  candidate: number | string | null;
  absolute_delta: number | string | null;
  relative_delta: number | string | null;
  percentage_point_delta: number | null;
  unit: string;
  direction: string;
  status: string;
  matched: number;
  reasons: string[];
  candidate_counts: {
    eligible: number;
    scored: number;
    coverage: number | null;
    errors: number;
  };
  bootstrap: unknown;
};
type Comparison = {
  comparison_id: string;
  baseline_run_id: string;
  candidate_run_id: string;
  result: {
    compatible: boolean;
    compatibility_errors: string[];
    mode: string;
    release_scope: string;
    configuration_diff: unknown;
    baseline_configuration: unknown;
    candidate_configuration: unknown;
    metrics: Record<string, Metric>;
    transitions: Record<string, number>;
    cases: Pair[];
    latency: unknown;
  };
};
type Decision = {
  decision_id: string;
  outcome: string;
  result: {
    scope: string;
    reason: string;
    policy_version: string;
    policy_checksum: string;
    rules: {
      id: string;
      outcome: string;
      reason: string;
      mandatory: boolean;
    }[];
  };
};
function useData<T>(path: string) {
  const [data, setData] = useState<T | null>(null),
    [error, setError] = useState<unknown>(null),
    [rev, setRev] = useState(0);
  const latest = useRef(new LatestRequest());
  useEffect(() => {
    const request = latest.current;
    void request.run((signal) => getJSON<T>(path, signal), setData, setError);
    return () => request.cancel();
  }, [path, rev]);
  return {
    data,
    error,
    retry: () => {
      setError(null);
      setRev((n) => n + 1);
    },
  };
}
export function deltaLabel(
  m: Pick<Metric, "absolute_delta" | "percentage_point_delta" | "unit">,
) {
  return m.absolute_delta === null
    ? "Unknown / incomparable"
    : m.percentage_point_delta !== null
      ? `${m.percentage_point_delta >= 0 ? "+" : ""}${m.percentage_point_delta.toFixed(2)} pp`
      : `${m.absolute_delta} ${m.unit}`;
}
export function Comparisons() {
  const [after, setAfter] = useState("");
  const { data, error, retry } = useData<{
    comparisons: {
      comparison_id: string;
      baseline_run_id: string;
      candidate_run_id: string;
      decisions: { decision_id: string; outcome: string }[];
    }[];
    next_cursor: string;
  }>(`/api/comparisons?limit=10${after ? `&after=${after}` : ""}`);
  return (
    <>
      <p className="eyebrow">AGENTOPS / PAIRED EXPERIMENTS</p>
      <h1>Compare evidence. Inspect release rules.</h1>
      <p>
        Offline PASS satisfies only its declared fixture policy. No model is
        deployed or promoted.
      </p>
      {!!error && (
        <ErrorPanel error={error} retry={retry} resource="evaluations" />
      )}
      {!data && !error && <p role="status">Loading comparisons…</p>}
      {data && (
        <section className="panel evaluation-panel">
          <h2>Persisted comparisons</h2>
          {data.comparisons.length === 0 && <p>No comparisons yet.</p>}
          {data.comparisons.map((c) => (
            <article key={c.comparison_id}>
              <Link href={`/comparisons/${c.comparison_id}`}>
                {c.comparison_id}
              </Link>
              <p>
                Baseline {c.baseline_run_id} → Candidate {c.candidate_run_id}
              </p>
              {c.decisions.map((d) => (
                <p key={d.decision_id}>
                  <Link
                    href={`/comparisons/${c.comparison_id}?decision=${d.decision_id}`}
                  >
                    Gate {d.outcome} · {d.decision_id}
                  </Link>
                </p>
              ))}
            </article>
          ))}
          <button disabled={!after} onClick={() => setAfter("")}>
            First comparisons
          </button>
          <button
            disabled={!data.next_cursor}
            onClick={() => setAfter(data.next_cursor)}
          >
            Next comparisons
          </button>
        </section>
      )}
    </>
  );
}
export function ComparisonDetail({
  id,
  decisionId,
}: {
  id: string;
  decisionId?: string;
}) {
  const { data, error, retry } = useData<Comparison>(`/api/comparisons/${id}`);
  const [filter, setFilter] = useState("all");
  return (
    <>
      <Link href="/comparisons">← Comparisons</Link>
      <h1>Paired comparison</h1>
      <code>{id}</code>
      {!!error && (
        <ErrorPanel error={error} retry={retry} resource="evaluations" />
      )}
      {!data && !error && <p role="status">Loading paired evidence…</p>}
      {data && (
        <>
          <section className="panel evaluation-panel">
            <h2>
              {data.result.mode} ·{" "}
              {data.result.compatible
                ? "Compatible evidence"
                : "Incompatible evidence"}
            </h2>
            <p>{data.result.release_scope}</p>
            <p>{data.result.compatibility_errors.join(", ")}</p>
            <p>
              <Link href={`/evaluations/${data.baseline_run_id}`}>
                Baseline evaluation
              </Link>{" "}
              ·{" "}
              <Link href={`/evaluations/${data.candidate_run_id}`}>
                Candidate evaluation
              </Link>
            </p>
            <details>
              <summary>Frozen configurations and actual differences</summary>
              <pre>
                {JSON.stringify(
                  {
                    baseline: data.result.baseline_configuration,
                    candidate: data.result.candidate_configuration,
                    diff: data.result.configuration_diff,
                  },
                  null,
                  2,
                )}
              </pre>
            </details>
            <div className="table-scroll">
              <table className="evaluation-table">
                <thead>
                  <tr>
                    <th>Metric / direction</th>
                    <th>Paired baseline → candidate</th>
                    <th>Absolute change</th>
                    <th>Matched / candidate coverage</th>
                  </tr>
                </thead>
                <tbody>
                  {Object.entries(data.result.metrics).map(([name, m]) => (
                    <tr key={name}>
                      <td>
                        {name}
                        <small>
                          {m.direction} is better · {m.unit}
                        </small>
                      </td>
                      <td>
                        {m.baseline ?? "Unknown"} → {m.candidate ?? "Unknown"}
                        <small>{m.status}</small>
                      </td>
                      <td>
                        {deltaLabel(m)}
                        <small>
                          Relative: {m.relative_delta ?? "undefined"}
                        </small>
                        {m.reasons.join(", ")}
                      </td>
                      <td>
                        {m.matched} matched · {m.candidate_counts.scored}/
                        {m.candidate_counts.eligible} eligible scored ·{" "}
                        {m.candidate_counts.errors} errors
                        <details>
                          <summary>
                            Full-run, pricing and bootstrap evidence
                          </summary>
                          <pre>{JSON.stringify(m, null, 2)}</pre>
                        </details>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
            <details>
              <summary>Successful / failed HTTP latency samples</summary>
              <pre>{JSON.stringify(data.result.latency, null, 2)}</pre>
            </details>
          </section>
          <section className="panel evaluation-panel">
            <h2>Case transitions</h2>
            <p>
              {Object.entries(data.result.transitions)
                .map(([k, v]) => `${k}: ${v}`)
                .join(" · ")}
            </p>
            <label>
              Filter case transitions
              <select
                value={filter}
                onChange={(e) => setFilter(e.target.value)}
              >
                <option value="all">All</option>
                <option value="regressed">Regressed</option>
                <option value="improved">Improved</option>
                <option value="failed">Execution failed</option>
              </select>
            </label>
            {data.result.cases
              .filter(
                (c) =>
                  filter === "all" ||
                  c.transition === filter ||
                  (filter === "failed" &&
                    ["new_failure", "both_failed"].includes(
                      c.execution_transition,
                    )),
              )
              .map((c) => (
                <article key={c.case_id}>
                  <h3>
                    {c.case_id} · {c.transition}
                  </h3>
                  <p>{c.execution_transition}</p>
                  <p>
                    Baseline supplied: {c.baseline_supplied.join(", ")} →
                    Candidate supplied: {c.candidate_supplied.join(", ")}
                  </p>
                  <p>
                    <Link
                      href={`/evaluations/${c.baseline_run_id}/cases/${c.case_id}`}
                    >
                      Baseline case
                    </Link>{" "}
                    ·{" "}
                    <Link
                      href={`/evaluations/${c.candidate_run_id}/cases/${c.case_id}`}
                    >
                      Candidate case
                    </Link>
                  </p>
                  <p>
                    {c.baseline_trace_id ? (
                      <Link href={`/traces/${c.baseline_trace_id}`}>
                        Baseline trace {c.baseline_trace_id}
                      </Link>
                    ) : (
                      "Baseline trace unknown"
                    )}{" "}
                    · {c.baseline_trace_visibility}
                  </p>
                  <p>
                    {c.candidate_trace_id ? (
                      <Link href={`/traces/${c.candidate_trace_id}`}>
                        Candidate trace {c.candidate_trace_id}
                      </Link>
                    ) : (
                      "Candidate trace unknown"
                    )}{" "}
                    · {c.candidate_trace_visibility}
                  </p>
                </article>
              ))}
          </section>
          {decisionId ? (
            <GateDecision key={decisionId} id={decisionId} />
          ) : (
            <p>
              Select a persisted gate decision from the comparison list to
              inspect its policy.
            </p>
          )}
        </>
      )}
    </>
  );
}
export function GateDecision({ id }: { id: string }) {
  const { data, error, retry } = useData<Decision>(`/api/gates/${id}`);
  return (
    <section className="panel evaluation-panel" aria-label="Gate decision">
      {!!error && (
        <ErrorPanel error={error} retry={retry} resource="evaluations" />
      )}
      {!data && !error && <p role="status">Loading gate…</p>}
      {data && (
        <>
          <h2>Gate decision: {data.outcome}</h2>
          <p>{data.result.scope}</p>
          <p>
            Policy {data.result.policy_version} · checksum{" "}
            {data.result.policy_checksum}
          </p>
          <p>{data.result.reason}</p>
          {data.result.rules.map((r) => (
            <article key={r.id}>
              <strong>
                {r.id}: {r.outcome}
              </strong>
              <p>
                {r.mandatory ? "Mandatory" : "Advisory"} · {r.reason}
              </p>
            </article>
          ))}
        </>
      )}
    </section>
  );
}
