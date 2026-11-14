"use client";
import { useEffect, useState } from "react";
import Link from "next/link";
import {
  percentage,
  type Definition,
  type Evaluation,
  type Window,
  type Incident,
  type Head,
} from "../lib/reliability";
type Row = { key: string; definition: Definition; latest: Evaluation | null };
type Data = {
  slos?: Row[];
  evaluations?: Evaluation[];
  alerts?: Incident[];
  heads?: Head[];
  definition?: Definition;
  latest?: Evaluation;
  incident?: Incident;
  events?: {
    id: string;
    kind: string;
    cutoff: string;
    evaluation_id: string;
  }[];
  next_cursor?: string;
};
function Scope({ d }: { d: Definition }) {
  return (
    <p>
      {d.service} / {d.namespace || "no namespace"} / {d.environment} ·{" "}
      <strong>{d.mode}</strong> · {d.kind} · source {d.source_version}. Target{" "}
      {percentage(d.target)} ({d.target}) · window {d.window_seconds}s.{" "}
      {d.description}{" "}
      {d.threshold_seconds && `Latency good: ≤ ${d.threshold_seconds}s. `}
      Minimum count {d.min_count}; required supported coverage{" "}
      {percentage(d.min_coverage)}.
    </p>
  );
}
export function WindowView({ w, title }: { w: Window; title: string }) {
  const e = w.evidence,
    b = w.budget;
  return (
    <section className="slo-window">
      <h3>
        {title}: {e.status}
      </h3>
      <p>
        {e.from} → {e.to}
      </p>
      {e.reason && (
        <p>
          <strong>{e.reason}</strong>
        </p>
      )}
      <p>
        Eligible: {e.total || "N/A"} · Good: {e.good || "N/A"} · Bad:{" "}
        {e.bad || "N/A"} · supported coverage {(e.coverage * 100).toFixed(3)}%
        (not traffic capture)
      </p>
      {e.details && "attempts" in e.details && (
        <section aria-label="Provider operation breakdown">
          <h4>Provider attempts and logical operations</h4>
          <p>
            Attempts: {String(e.details.attempts)} · successful attempts:{" "}
            {String(e.details.successful_attempts)} · logical operations:{" "}
            {String(e.details.logical_operations)} · successful logical
            operations: {String(e.details.successful_logical_operations)} ·
            retry recovered: {String(e.details.retry_recovered)}
          </p>
          <p>
            Timeouts: {String(e.details.timeouts)} · rate limited:{" "}
            {String(e.details.rate_limited)} · provider errors:{" "}
            {String(e.details.provider_errors)} · usage-complete attempts:{" "}
            {String(e.details.usage_complete_attempts)} · usage coverage:{" "}
            {String(e.details.usage_coverage ?? "inspect counts")}
          </p>
          <p>
            Attempt mean/p95 ms: {String(e.details.attempt_mean_ms ?? "N/A")} /{" "}
            {String(e.details.attempt_p95_ms ?? "N/A")} · provenance{" "}
            {String(e.details.mode)} / {String(e.details.provider)}.
          </p>
          <p>
            Logical operation mean/p95 ms:{" "}
            {String(e.details.logical_mean_ms ?? "N/A")} /{" "}
            {String(e.details.logical_p95_ms ?? "N/A")}.
          </p>
        </section>
      )}
      {b ? (
        <dl className="analytics-summary">
          <dt>Measured SLI</dt>
          <dd>
            {percentage(b.sli)} ({b.sli}) ·{" "}
            {b.meets_target
              ? "Target met in observed scope"
              : "Target violated in observed scope"}
          </dd>
          <dt>Allowed bad events (fractional)</dt>
          <dd>{b.allowed_bad_events}</dd>
          <dt>Consumed budget</dt>
          <dd>
            {percentage(b.budget_consumed)} ({b.budget_consumed})
          </dd>
          <dt>Remaining bad-event budget</dt>
          <dd>{b.budget_remaining}</dd>
          <dt>Burn rate</dt>
          <dd>{b.burn_rate}×</dd>
        </dl>
      ) : (
        <p>SLI, budget and burn: N/A — no healthy result inferred.</p>
      )}
      <details>
        <summary>Measurement diagnostics and source intervals</summary>
        <pre>
          {JSON.stringify(
            { diagnostics: e.diagnostics, details: e.details },
            null,
            2,
          )}
        </pre>
      </details>
      {e.trace_ids?.length > 0 && (
        <ul>
          {e.trace_ids.map((id) => (
            <li key={id}>
              <Link href={`/traces/${id}`}>Observed trace {id}</Link>
              {" · "}
              <Link href={`/traces/${id}#correlated-logs`}>
                Correlated logs
              </Link>
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}
export function EvaluationView({ e }: { e: Evaluation }) {
  const d = e.definition;
  const metricQuery = new URLSearchParams({
    from: e.overall.evidence.from,
    to: e.cutoff,
    service: d.service,
    ...(d.namespace ? { namespace: d.namespace } : {}),
  });
  return (
    <article>
      <h2>Evaluation {e.status}</h2>
      <p>
        <code>{e.id}</code> · cutoff {e.cutoff} · revision {e.revision}. History
        is immutable; later telemetry does not rewrite this decision.
      </p>
      <Scope d={d} />
      {d.kind === "availability" || d.kind === "latency" ? (
        <Link href={`/metrics?${metricQuery}`}>
          Inspect actual source metrics
        </Link>
      ) : (
        <p>
          Provider attempts and model operations are observed execution
          outcomes, not answer-quality or live-provider guarantees.
        </p>
      )}
      <WindowView w={e.overall} title="Rolling objective" />
      {e.rules.map((r) => (
        <section key={r.rule.id}>
          <h2>
            {r.rule.id}: {r.decision}
          </h2>
          <p>
            {r.rule.severity} · version {r.rule.version} · both windows require
            burn ≥ {r.rule.threshold}× · confirm {r.rule.confirm}, recover{" "}
            {r.rule.recover}.
          </p>
          <WindowView w={r.long} title="Long window" />
          <WindowView w={r.short} title="Short window" />
        </section>
      ))}
    </article>
  );
}
export function ReliabilityView({
  kind,
  id,
  evaluation,
}: {
  kind: "slos" | "alerts" | "provider-reliability";
  id?: string;
  evaluation?: string;
}) {
  const [data, setData] = useState<Data | null>(null),
    [selected, setSelected] = useState<Evaluation | null>(null),
    [heads, setHeads] = useState<Head[]>([]),
    [error, setError] = useState(""),
    [loading, setLoading] = useState(false),
    [after, setAfter] = useState(""),
    [revision, setRevision] = useState(0);
  useEffect(() => {
    const controller = new AbortController();
    async function read(path: string) {
      const response = await fetch(`/api/reliability/${path}`, {
        signal: controller.signal,
        cache: "no-store",
      });
      const body = await response.json();
      if (!response.ok)
        throw new Error(
          `${response.status}: ${body.error?.message ?? "Reliability unavailable"}`,
        );
      return body;
    }
    async function load() {
      setLoading(true);
      setError("");
      setData(null);
      setSelected(null);
      try {
        if (
          (id && !/^[a-z0-9_-]{1,80}$/.test(id)) ||
          (evaluation && !/^[a-f0-9]{64}$/.test(evaluation))
        )
          throw new Error("Invalid reliability identity");
        if (kind === "slos" && id) {
          const [detail, history] = await Promise.all([
            read(`slos/${id}`),
            read(
              `slos/${id}/evaluations?limit=10${after ? `&after=${after}` : ""}`,
            ),
          ]);
          setData({ ...detail, ...history });
          if (evaluation) {
            const chosen = await read(`slo-evaluations/${evaluation}`);
            if (chosen.slo_key !== id)
              throw new Error("Evaluation belongs to a different SLO version");
            setSelected(chosen);
          } else setSelected(detail.latest ?? null);
        } else {
          setData(
            await read(
              `${kind}${id ? `/${id}` : `?limit=25${after ? `&after=${after}` : ""}`}`,
            ),
          );
          if (kind === "alerts" && !id) {
            const h = await read("alert-heads?limit=50");
            setHeads(h.heads);
          }
        }
      } catch (e) {
        if (!controller.signal.aborted) setError((e as Error).message);
      } finally {
        if (!controller.signal.aborted) setLoading(false);
      }
    }
    void load();
    return () => controller.abort();
  }, [kind, id, evaluation, after, revision]);
  return (
    <section className="analytics-page reliability-page">
      <p className="eyebrow">
        Observed reliability · reliability/v1 · local development
      </p>
      <h1>
        {kind === "slos"
          ? id
            ? "SLO detail"
            : "Reliability objectives"
          : kind === "alerts"
            ? id
              ? "Incident history"
              : "Alert incidents"
            : "Provider reliability"}
      </h1>
      <nav aria-label="Reliability views">
        <Link href="/slos">SLOs</Link>
        {" · "}
        <Link href="/alerts">Alerts</Link>
        {" · "}
        <Link href="/provider-reliability">Provider reliability</Link>
      </nav>
      <p>
        Measurement status is separate from incident state. UNKNOWN never
        resolves an active incident. Fixture/synthetic evidence does not
        establish production or live-provider reliability.
      </p>
      <button onClick={() => setRevision((n) => n + 1)} disabled={loading}>
        Refresh persisted results
      </button>
      {loading && <p role="status">Loading persisted reliability evidence…</p>}
      {error && <p role="alert">{error}</p>}
      {data && (
        <>
          {data.slos && (
            <table>
              <caption>Immutable reliability objectives</caption>
              <thead>
                <tr>
                  <th>Objective</th>
                  <th>Target</th>
                  <th>Latest measurement</th>
                  <th>Good / bad / eligible</th>
                  <th>Budget / burn</th>
                </tr>
              </thead>
              <tbody>
                {data.slos.map((s) => (
                  <tr key={s.key}>
                    <td>
                      <Link href={`/slos/${s.key}`}>{s.key}</Link>
                      <br />
                      {s.definition.service} · {s.definition.mode} ·{" "}
                      {s.definition.kind}
                    </td>
                    <td>{percentage(s.definition.target)}</td>
                    <td>
                      {s.latest?.status ?? "UNKNOWN — never evaluated"}
                      <br />
                      {s.latest?.overall.evidence.status}
                    </td>
                    <td>
                      {s.latest
                        ? `${s.latest.overall.evidence.good || "N/A"} / ${s.latest.overall.evidence.bad || "N/A"} / ${s.latest.overall.evidence.total || "N/A"}`
                        : "N/A"}
                    </td>
                    <td>
                      {s.latest?.overall.budget
                        ? `${s.latest.overall.budget.budget_remaining} remaining; ${s.latest.overall.budget.burn_rate}×`
                        : "N/A"}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
          {data.definition && <Scope d={data.definition} />}{" "}
          {selected && <EvaluationView e={selected} />}
          {data.evaluations && (
            <section>
              <h2>Persisted evaluations</h2>
              {data.evaluations.length === 0 && (
                <p>No persisted evaluations.</p>
              )}
              <ul>
                {data.evaluations.map((e) => (
                  <li key={e.id}>
                    <Link href={`/slos/${e.slo_key}?evaluation=${e.id}`}>
                      {e.slo_key} · {e.cutoff} · {e.status} /{" "}
                      {e.overall.evidence.status}
                    </Link>
                    {kind === "provider-reliability" && (
                      <>
                        <Scope d={e.definition} />
                        <WindowView
                          w={e.overall}
                          title="Observed provider population"
                        />
                      </>
                    )}
                  </li>
                ))}
              </ul>
            </section>
          )}
          {data.alerts && (
            <>
              <h2>Active and resolved incidents</h2>
              {data.alerts.length === 0 && <p>No persisted incidents.</p>}
              <ul>
                {data.alerts.map((a) => (
                  <li key={a.id}>
                    <Link href={`/alerts/${a.id}`}>
                      {a.state} · {a.severity} · {a.slo_key} / {a.rule_id}
                    </Link>
                    <p>
                      First fired {a.first_fired} · resolved{" "}
                      {a.resolved_at ?? "not resolved"}
                    </p>
                  </li>
                ))}
              </ul>
              <h2>Latest evaluator status (separate from alert state)</h2>
              {heads.map((h) => (
                <p key={h.fingerprint}>
                  {h.slo_key} / {h.rule_id}: incident {h.state}, evidence{" "}
                  {h.last_data_status}, cutoff {h.last_cutoff}
                </p>
              ))}
            </>
          )}
          {data.incident && (
            <>
              <h2>
                {data.incident.state} · {data.incident.severity}
              </h2>
              <p>
                <code>{data.incident.id}</code> · {data.incident.slo_key}/
                {data.incident.rule_id}
              </p>
              <p>
                First fired {data.incident.first_fired} · resolved{" "}
                {data.incident.resolved_at ?? "not resolved"}
              </p>
              <Link
                href={`/slos/${data.incident.slo_key}?evaluation=${data.incident.last_evaluation}`}
              >
                Latest incident evaluation
              </Link>
              <h2>Deduplicated event history</h2>
              <ul>
                {data.events?.map((e) => (
                  <li key={e.id}>
                    {e.kind} · {e.cutoff} ·{" "}
                    <Link
                      href={`/slos/${data.incident!.slo_key}?evaluation=${e.evaluation_id}`}
                    >
                      Contributing evaluation
                    </Link>
                  </li>
                ))}
              </ul>
            </>
          )}
          {data.next_cursor && (
            <button onClick={() => setAfter(data.next_cursor!)}>
              Next page
            </button>
          )}
        </>
      )}
      <p>
        Definitions are configured locally. No external notification, automatic
        remediation, complete-traffic guarantee or production SLA certification.
      </p>
    </section>
  );
}
