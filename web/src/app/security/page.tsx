"use client";
import { useEffect, useState } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
type Row = {
  run_id: string;
  tenant: string;
  mode: string;
  metrics: Record<string, number | null>;
  cases: {
    case_id: string;
    outcome: string;
    reason: string;
    trace_id: string;
  }[];
};
export default function SecurityPage() {
  const router = useRouter();
  const [tenant, setTenant] = useState("");
  const [runs, setRuns] = useState<Row[]>([]);
  const [error, setError] = useState("");
  useEffect(() => {
    const controller = new AbortController();
    async function load() {
      try {
        const identity = await fetch("/api/security/session", {
          signal: controller.signal,
          cache: "no-store",
        });
        if (!identity.ok)
          throw new Error("Authorized tenant session unavailable");
        setTenant((await identity.json()).tenant);
        const data = await fetch("/api/security/evaluations", {
          signal: controller.signal,
          cache: "no-store",
        });
        if (!data.ok) throw new Error("Security evidence unavailable");
        setRuns((await data.json()).runs);
      } catch (e) {
        if (!controller.signal.aborted) setError((e as Error).message);
      }
    }
    void load();
    return () => controller.abort();
  }, []);
  return (
    <section className="analytics-page">
      <h1>Security evidence</h1>
      <p>
        Authenticated tenant: <strong>{tenant || "unavailable"}</strong>
      </p>
      <p>
        Fixture containment tests are separate from RAG answer quality. They do
        not establish live-model injection resistance or production security
        certification.
      </p>
      <button
        onClick={async () => {
          await fetch("/api/session", { method: "DELETE" });
          router.push("/login");
        }}
      >
        Sign out
      </button>
      {error && <p role="alert">{error}</p>}
      {runs.map((run) => (
        <article key={run.run_id}>
          <h2>Security run {run.run_id}</h2>
          <p>
            {run.tenant} · {run.mode}
          </p>
          <dl>
            {Object.entries(run.metrics).map(([name, value]) => (
              <div key={name}>
                <dt>{name.replaceAll("_", " ")}</dt>
                <dd>{value ?? "N/A"}</dd>
              </div>
            ))}
          </dl>
          <table>
            <caption>Actual evaluated cases</caption>
            <thead>
              <tr>
                <th>Case</th>
                <th>Outcome</th>
                <th>Reason</th>
                <th>Evidence</th>
              </tr>
            </thead>
            <tbody>
              {run.cases.map((row) => (
                <tr key={row.case_id}>
                  <td>{row.case_id}</td>
                  <td>{row.outcome}</td>
                  <td>{row.reason}</td>
                  <td>
                    <Link href={`/traces/${row.trace_id}`}>Observed trace</Link>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </article>
      ))}
    </section>
  );
}
