"use client";
import Link from "next/link";
import { useEffect, useRef, useState } from "react";
import { getJSON, LatestRequest } from "../lib/client";
import { ErrorPanel } from "./error-panel";

type Metric = {
  value: number | null;
  status?: string;
  numerator?: number;
  denominator?: number;
  eligible?: number;
  scored?: number;
  excluded?: number;
  execution_failures?: number;
  coverage?: number | null;
  macro_numerator?: number;
  macro_denominator?: number;
};
type Run = {
  run_id: string;
  dataset_version: string;
  status: string;
  started_at: string;
  attempted: number;
  succeeded: number;
  failed: number;
  scored: number;
  configuration: Record<string, unknown>;
  aggregates: Record<string, Metric>;
};
type Claim = {
  claim: { key: string; value: string; citations: string[] };
  grounded: boolean;
  contradiction: boolean;
  correct_value: boolean;
  supporting_document_ids: string[];
  citations: {
    document_id: string;
    valid: boolean;
    available: boolean;
    supports: boolean;
  }[];
};
type CaseResult = {
  output_transform?: string | null;
  case_id: string;
  status: string;
  semantic_status: string;
  trace_id: string | null;
  trace_visibility: string;
  application_run_id: string;
  error: string | null;
  case: {
    question: string;
    expected_facts: string[];
    reference_answer: string;
    review_status: string;
  };
  retrieved_document_ids: string[];
  context_document_ids: string[];
  metrics: Record<string, Metric>;
  evaluated_output: unknown;
  original_output: unknown;
  judgments: {
    claims?: Claim[];
    unsupported_claims?: number;
    contradictions?: number;
  };
  source_facts?: Record<
    string,
    { alternatives: string[]; documents: string[]; evidence_quote: string }
  >;
};

function useData<T>(path: string) {
  const [data, setData] = useState<T | null>(null),
    [error, setError] = useState<unknown>(null),
    [revision, setRevision] = useState(0);
  const latest = useRef(new LatestRequest());
  useEffect(() => {
    const request = latest.current;
    void request.run((signal) => getJSON<T>(path, signal), setData, setError);
    return () => request.cancel();
  }, [path, revision]);
  return {
    data,
    error,
    retry: () => {
      setError(null);
      setRevision((n) => n + 1);
    },
  };
}
function MetricTable({ metrics }: { metrics: Record<string, Metric> }) {
  return (
    <div className="table-scroll">
      <table className="evaluation-table">
        <thead>
          <tr>
            <th>Metric</th>
            <th>Macro score / status</th>
            <th>Numerator / denominator</th>
            <th>Coverage</th>
          </tr>
        </thead>
        <tbody>
          {Object.entries(metrics).map(([name, m]) => (
            <tr key={name}>
              <td>{name}</td>
              <td>
                {m.value === null
                  ? (m.status ?? "Pending / unknown")
                  : m.value.toFixed(4)}
                {m.status && m.value !== null && <small>{m.status}</small>}
              </td>
              <td>
                {m.macro_numerator !== undefined
                  ? `${m.macro_numerator.toFixed(4)} / ${m.macro_denominator} cases`
                  : `${m.numerator ?? "?"} / ${m.denominator ?? "?"}`}
              </td>
              <td>
                {m.eligible !== undefined
                  ? `${m.scored}/${m.eligible} eligible scored · ${m.excluded} N/A · ${m.execution_failures} execution failures`
                  : (m.status ?? "unknown")}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
function Config({ value }: { value: Record<string, unknown> }) {
  return (
    <details>
      <summary>Frozen configuration and evaluator provenance</summary>
      <pre>{JSON.stringify(value, null, 2)}</pre>
    </details>
  );
}

export function Evaluations() {
  const [after, setAfter] = useState("");
  const { data, error, retry } = useData<{ runs: Run[]; next_cursor: string }>(
    `/api/evaluations?limit=10${after ? `&after=${after}` : ""}`,
  );
  return (
    <>
      <p className="eyebrow">AGENTOPS / EVALUATIONS</p>
      <h1>Measured evidence. Visible limitations.</h1>
      <p>
        Versioned synthetic evaluation runs. Fixture quality scores do not
        establish live-model quality. No deployment gates or automatic
        promotion.
      </p>
      {!!error && (
        <ErrorPanel error={error} retry={retry} resource="evaluations" />
      )}{" "}
      {!data && !error && <p role="status">Loading evaluations…</p>}
      {data && (
        <section className="panel evaluation-panel">
          <h2>Evaluation runs</h2>
          {data.runs.length === 0 ? (
            <p>No evaluation runs yet.</p>
          ) : (
            <div className="table-scroll">
              <table className="evaluation-table">
                <thead>
                  <tr>
                    <th>Run / dataset</th>
                    <th>Mode / status</th>
                    <th>Attempted / succeeded / failed / scored</th>
                  </tr>
                </thead>
                <tbody>
                  {data.runs.map((run) => (
                    <tr key={run.run_id}>
                      <td>
                        <Link href={`/evaluations/${run.run_id}`}>
                          {run.run_id}
                        </Link>
                        <small>{run.dataset_version}</small>
                        <small>{run.started_at}</small>
                      </td>
                      <td>
                        <span className="badge">
                          {String(run.configuration.mode)}
                        </span>
                        <p>{run.status}</p>
                      </td>
                      <td>
                        {run.attempted} / {run.succeeded} / {run.failed} /{" "}
                        {run.scored}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
          <button disabled={!after} onClick={() => setAfter("")}>
            First page
          </button>
          <button
            disabled={!data.next_cursor}
            onClick={() => setAfter(data.next_cursor)}
          >
            Next runs
          </button>
        </section>
      )}
    </>
  );
}
export function EvaluationRun({ runId }: { runId: string }) {
  const run = useData<Run>(`/api/evaluations/${runId}`);
  const [after, setAfter] = useState("");
  const cases = useData<{ cases: CaseResult[]; next_cursor: string }>(
    `/api/evaluations/${runId}/cases?limit=10${after ? `&after=${after}` : ""}`,
  );
  return (
    <>
      <Link href="/evaluations">← Evaluations</Link>
      <h1>Evaluation run</h1>
      <code>{runId}</code>
      {!!run.error && (
        <ErrorPanel
          error={run.error}
          retry={run.retry}
          resource="evaluations"
        />
      )}{" "}
      {!run.data && !run.error && <p role="status">Loading run…</p>}
      {run.data && (
        <>
          <section className="panel evaluation-panel">
            <h2>
              {run.data.dataset_version}{" "}
              <span className="badge">
                {String(run.data.configuration.mode)}
              </span>
            </h2>
            <p>Status: {run.data.status}</p>
            <p>
              Attempted {run.data.attempted} · Succeeded {run.data.succeeded} ·
              Failed {run.data.failed} · Constrained scored {run.data.scored}
            </p>
            <p>
              Retrieval metrics and constrained exact-evidence judgments have
              separate applicability. Free-form semantic review is pending.
              Independent human calibration and live judge verification have not
              been performed.
            </p>
            <Config value={run.data.configuration} />
            <MetricTable metrics={run.data.aggregates} />
          </section>
          <section className="panel evaluation-panel">
            <h2>Individual cases</h2>
            {!!cases.error && (
              <ErrorPanel
                error={cases.error}
                retry={cases.retry}
                resource="evaluations"
              />
            )}{" "}
            {!cases.data && !cases.error && <p role="status">Loading cases…</p>}
            {cases.data && (
              <>
                {cases.data.cases.length === 0 && (
                  <p>No persisted case results yet.</p>
                )}
                <div className="table-scroll">
                  <table className="evaluation-table">
                    <thead>
                      <tr>
                        <th>Case / question</th>
                        <th>Execution / scoring</th>
                        <th>Actual trace</th>
                      </tr>
                    </thead>
                    <tbody>
                      {cases.data.cases.map((c) => (
                        <tr key={c.case_id}>
                          <td>
                            <Link
                              href={`/evaluations/${runId}/cases/${c.case_id}`}
                            >
                              {c.case_id}
                            </Link>
                            <p>{c.case.question}</p>
                          </td>
                          <td>
                            {c.status}
                            <small>{c.semantic_status}</small>
                            {c.error && <p>{c.error}</p>}
                          </td>
                          <td>
                            {c.trace_id ? (
                              <Link href={`/traces/${c.trace_id}`}>
                                {c.trace_id}
                              </Link>
                            ) : (
                              "Unknown"
                            )}
                            <small>{c.trace_visibility}</small>
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
                <button disabled={!after} onClick={() => setAfter("")}>
                  First cases
                </button>
                <button
                  disabled={!cases.data.next_cursor}
                  onClick={() => setAfter(cases.data!.next_cursor)}
                >
                  Next cases
                </button>
              </>
            )}
          </section>
        </>
      )}
    </>
  );
}
export function EvaluationCase({
  runId,
  caseId,
}: {
  runId: string;
  caseId: string;
}) {
  const {
    data: c,
    error,
    retry,
  } = useData<CaseResult>(`/api/evaluations/${runId}/cases/${caseId}`);
  return (
    <>
      <Link href={`/evaluations/${runId}`}>← Evaluation run</Link>
      <h1>Case evidence</h1>
      {!!error && (
        <ErrorPanel error={error} retry={retry} resource="evaluations" />
      )}{" "}
      {!c && !error && <p role="status">Loading case…</p>}
      {c && (
        <>
          <section className="panel evaluation-panel">
            <h2>{c.case_id}</h2>
            <p>{c.case.question}</p>
            <p>
              {c.status} · {c.semantic_status} · Labels: {c.case.review_status}
            </p>
            {c.error && <p role="alert">Execution failure: {c.error}</p>}
            <p>
              Trace visibility: {c.trace_visibility} (snapshot; exports can
              arrive later)
            </p>
            {c.trace_id ? (
              <Link href={`/traces/${c.trace_id}`}>
                Inspect actual trace {c.trace_id}
              </Link>
            ) : (
              <p>No trace identity returned.</p>
            )}
            <p>Application run: {c.application_run_id ?? "unknown"}</p>
            <p>
              Retrieved IDs: {c.retrieved_document_ids.join(", ") || "none"}
            </p>
            <p>
              Actually supplied IDs:{" "}
              {c.context_document_ids.join(", ") || "none"}
            </p>
            <h3>Expected atomic facts</h3>
            {c.case.expected_facts.map((key) => (
              <div key={key}>
                <strong>{key}</strong>
                <p>{c.source_facts?.[key]?.alternatives.join(" / ")}</p>
                <p>
                  Evidence: {c.source_facts?.[key]?.documents.join(", ")} ·{" "}
                  {c.source_facts?.[key]?.evidence_quote}
                </p>
              </div>
            ))}
            <p>Reference rubric: {c.case.reference_answer}</p>
            <h3>Actual evaluated output</h3>
            {c.output_transform && (
              <p className="badge error">
                Controlled diagnostic transform: {c.output_transform}. The
                original application output is preserved below.
              </p>
            )}
            <pre>
              {typeof c.evaluated_output === "string"
                ? c.evaluated_output
                : JSON.stringify(c.evaluated_output, null, 2)}
            </pre>
            <details>
              <summary>
                Original application output before any diagnostic transform
              </summary>
              <pre>{JSON.stringify(c.original_output, null, 2)}</pre>
            </details>
            <MetricTable metrics={c.metrics} />
          </section>
          <section className="panel evaluation-panel">
            <h2>Claim and citation judgments</h2>
            {c.semantic_status !== "constrained_scored" && (
              <p>
                Semantic evaluation pending / unsupported. No semantic score has
                been fabricated.
              </p>
            )}
            <p>
              Unsupported claims: {c.judgments.unsupported_claims ?? "unknown"}{" "}
              · Contradictions: {c.judgments.contradictions ?? "unknown"}
            </p>
            {c.judgments.claims?.map((j, i) => (
              <article key={i}>
                <h3>
                  {j.claim.key}: {j.claim.value}
                </h3>
                <p>
                  Grounded: {String(j.grounded)} · Correct value:{" "}
                  {String(j.correct_value)} · Contradiction:{" "}
                  {String(j.contradiction)}
                </p>
                <p>
                  Independent supporting documents:{" "}
                  {j.supporting_document_ids.join(", ") || "none"}
                </p>
                {j.citations.map((d, n) => (
                  <p key={n}>
                    Citation {d.document_id}: valid {String(d.valid)} · supplied{" "}
                    {String(d.available)} · supports claim {String(d.supports)}
                  </p>
                ))}
              </article>
            ))}
          </section>
        </>
      )}
    </>
  );
}
