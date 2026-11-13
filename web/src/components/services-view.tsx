"use client";
import Link from "next/link";
import { useEffect, useState } from "react";
import {
  validateAnalytics,
  sortServices,
  type Analytics,
  type Service,
  type Stats,
} from "@/lib/analytics";

const n = (value: number | null) =>
  value === null
    ? "No samples"
    : value.toLocaleString(undefined, { maximumFractionDigits: 3 });
const pct = (value: number | null) =>
  value === null ? "No observations" : `${n(value * 100)}%`;
export function Trend({
  service,
  metric,
  title,
}: {
  service: Service;
  metric: "rate_per_second" | "error_fraction" | "p95_ms";
  title: string;
}) {
  const values = service.buckets.map((b) => b.stats[metric]),
    max = Math.max(...values.filter((v): v is number => v !== null), 0.001);
  return (
    <figure>
      <figcaption>{title}</figcaption>
      <svg
        viewBox="0 0 600 90"
        role="img"
        aria-label={`${title}; values in the table below`}
        style={{ width: "100%", maxWidth: 600, height: 90 }}
      >
        {values.map((v, i) =>
          v === null ? null : (
            <rect
              key={i}
              x={(i * 600) / values.length}
              y={85 - (v / max) * 80}
              width={Math.max(1, 600 / values.length - 2)}
              height={Math.max(1, (v / max) * 80)}
              fill="currentColor"
            >
              <title>
                {service.buckets[i].from}: {n(v)}
              </title>
            </rect>
          ),
        )}
      </svg>
    </figure>
  );
}
function Summary({ stats }: { stats: Stats }) {
  return (
    <dl className="analytics-summary">
      <dt>Observed SERVER requests</dt>
      <dd>{stats.count}</dd>
      <dt>Requests / second</dt>
      <dd>{n(stats.rate_per_second)}</dd>
      <dt>Explicit ERROR fraction</dt>
      <dd>
        {pct(stats.error_fraction)} ({stats.errors} ERROR)
      </dd>
      <dt>Mean / p50 / p95 / p99 (ms)</dt>
      <dd>
        {[stats.mean_ms, stats.p50_ms, stats.p95_ms, stats.p99_ms]
          .map(n)
          .join(" / ")}{" "}
        · {stats.duration_samples} duration samples
      </dd>
      <dt>Span status (UNSET is not proven success)</dt>
      <dd>{JSON.stringify(stats.status)}</dd>
      <dt>HTTP response status (separate evidence)</dt>
      <dd>{JSON.stringify(stats.http_status)}</dd>
    </dl>
  );
}
export function ServicesView({
  id,
  graph = false,
  initialQuery,
}: {
  id?: string;
  graph?: boolean;
  initialQuery: string;
}) {
  const [query, setQuery] = useState(initialQuery);
  const [sort, setSort] = useState<"name" | "rate" | "errors" | "p95">("name");
  const [data, setData] = useState<Analytics | null>(null);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(false);
  useEffect(() => {
    if (!query) return;
    const controller = new AbortController();
    async function load() {
      setLoading(true);
      setError("");
      setData(null);
      try {
        const p = new URLSearchParams(query);
        validateAnalytics(p);
        if (id && !/^[a-f0-9]{32}$/.test(id))
          throw new Error("Invalid service identity.");
        const response = await fetch(
          `/api/${graph ? "service-graph" : id ? `services/${id}/metrics` : "services"}?${p}`,
          { signal: controller.signal, cache: "no-store" },
        );
        const body = await response.json();
        if (!response.ok)
          throw new Error(
            `${response.status}: ${body.error?.message ?? "Analytics unavailable"}`,
          );
        if (
          body.version !== "trace-red/v1" ||
          !Array.isArray(body.services) ||
          !Array.isArray(body.edges)
        )
          throw new Error("Unsupported analytics response.");
        setData(body);
      } catch (e) {
        if (!controller.signal.aborted) setError((e as Error).message);
      } finally {
        if (!controller.signal.aborted) setLoading(false);
      }
    }
    void load();
    return () => controller.abort();
  }, [query, id, graph]);
  const params = new URLSearchParams(query),
    suffix = `?${query}`;
  const selected = data?.services.find((s) => s.identity.id === id);
  function submit(form: FormData) {
    const p = new URLSearchParams();
    for (const [key, value] of form.entries())
      if (String(value)) p.set(key, String(value));
    setQuery(p.toString());
    window.history.replaceState(null, "", `${window.location.pathname}?${p}`);
  }
  return (
    <section className="analytics-page">
      <p className="eyebrow">Trace-derived observations · trace-red/v1</p>
      <h1>
        {graph
          ? "Observed service graph"
          : id
            ? (selected?.identity.name ?? "Service detail")
            : "Services"}
      </h1>
      <p>
        One canonical SERVER span counts as one inbound request. Edges require
        an observed CLIENT → SERVER parent pair. Sampling and late telemetry can
        change these results.
      </p>
      <nav aria-label="Analytics views">
        <Link href={`/services${suffix}`}>Services overview</Link>
        {" · "}
        <Link href={`/service-graph${suffix}`}>Directed dependency graph</Link>
      </nav>
      <form action={submit} key={query} className="analytics-filters">
        <label>
          From UTC
          <input
            name="from"
            defaultValue={params.get("from") ?? ""}
            required
            placeholder="2026-11-13T10:00:00Z"
          />
        </label>
        <label>
          To UTC
          <input name="to" defaultValue={params.get("to") ?? ""} required />
        </label>
        <label>
          Bucket seconds
          <select
            name="bucket_seconds"
            defaultValue={params.get("bucket_seconds") ?? "60"}
          >
            {[60, 300, 900].map((s) => (
              <option key={s}>{s}</option>
            ))}
          </select>
        </label>
        <label>
          Namespace
          <input
            name="namespace"
            maxLength={128}
            defaultValue={params.get("namespace") ?? ""}
          />
        </label>
        <label>
          Environment
          <input
            name="environment"
            maxLength={128}
            defaultValue={params.get("environment") ?? ""}
          />
        </label>
        <label>
          Workload
          <select name="workload" defaultValue={params.get("workload") ?? ""}>
            {["", "synthetic", "fixture", "live", "unknown"].map((w) => (
              <option key={w} value={w}>
                {w || "All (separate identities)"}
              </option>
            ))}
          </select>
        </label>
        <button type="submit" disabled={loading}>
          Refresh analytics
        </button>
      </form>
      {loading && <p role="status">Loading bounded analytics…</p>}
      {error && <p role="alert">{error}</p>}
      {data && (
        <>
          {!id && (
            <label>
              Sort services
              <select
                value={sort}
                onChange={(e) => setSort(e.target.value as typeof sort)}
              >
                <option value="name">Name A–Z</option>
                <option value="rate">Request rate, highest first</option>
                <option value="errors">ERROR fraction, highest first</option>
                <option value="p95">p95 latency, highest first</option>
              </select>
            </label>
          )}
          <p>
            {data.query.from} ≤ start &lt; {data.query.to} ·{" "}
            {n(data.window_seconds)} seconds · Selection complete within
            budgets; traffic completeness unknown. Two reads do not form a
            snapshot.
          </p>
          {data.services.length === 0 && (
            <p>
              No observed services in this scope. Request count and rate are
              zero; error fractions and latency percentiles have no samples.
            </p>
          )}
          {!id && (
            <table>
              <caption>Observed inbound RED metrics</caption>
              <thead>
                <tr>
                  <th>Service identity</th>
                  <th>Count</th>
                  <th>Requests/s</th>
                  <th>ERROR fraction</th>
                  <th>p50 / p95 / p99 ms</th>
                  <th>Duration samples</th>
                </tr>
              </thead>
              <tbody>
                {sortServices(data.services, sort).map((s) => (
                  <tr key={s.identity.id}>
                    <td>
                      <Link href={`/services/${s.identity.id}${suffix}`}>
                        {s.identity.name}
                      </Link>
                      <br />
                      {s.identity.namespace || "No namespace"} ·{" "}
                      {s.identity.environment || "No environment"} ·{" "}
                      {s.identity.workload}
                    </td>
                    <td>{s.stats.count}</td>
                    <td>{n(s.stats.rate_per_second)}</td>
                    <td>{pct(s.stats.error_fraction)}</td>
                    <td>
                      {[s.stats.p50_ms, s.stats.p95_ms, s.stats.p99_ms]
                        .map(n)
                        .join(" / ")}
                    </td>
                    <td>{s.stats.duration_samples}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
          {id && !selected && (
            <p>
              No inbound or paired-call evidence for this service identity in
              the selected scope. Count and rate: 0; error fraction and
              durations: no samples.
            </p>
          )}
          {selected && (
            <>
              <p>
                {selected.identity.namespace || "No namespace"} /{" "}
                {selected.identity.environment || "No environment"} /{" "}
                {selected.identity.workload}
              </p>
              <Summary stats={selected.stats} />
              <Trend
                service={selected}
                metric="rate_per_second"
                title="Observed request rate (requests/s)"
              />
              <Trend
                service={selected}
                metric="error_fraction"
                title="Explicit ERROR fraction (0–1)"
              />
              <Trend
                service={selected}
                metric="p95_ms"
                title="Observed latency p95 (ms)"
              />
              <details>
                <summary>Accessible bucket values</summary>
                <table>
                  <thead>
                    <tr>
                      <th>UTC start</th>
                      <th>Seconds</th>
                      <th>Count</th>
                      <th>Requests/s</th>
                      <th>ERROR fraction</th>
                      <th>p95 ms</th>
                    </tr>
                  </thead>
                  <tbody>
                    {selected.buckets.map((b) => (
                      <tr key={b.from}>
                        <td>{b.from}</td>
                        <td>{b.seconds}</td>
                        <td>{b.stats.count}</td>
                        <td>{n(b.stats.rate_per_second)}</td>
                        <td>{pct(b.stats.error_fraction)}</td>
                        <td>{n(b.stats.p95_ms)}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </details>
              <h2>Representative observed requests</h2>
              <p>
                Representative errors and slow requests. At most five trace
                examples.
              </p>
              <Evidence ids={selected.trace_ids} />
            </>
          )}
          {(graph || id) && (
            <>
              <h2>Confirmed directed dependencies</h2>
              <ServiceGraph
                services={data.services}
                edges={data.edges}
                suffix={suffix}
              />
              <p>
                Counts and rate use CLIENT-start attribution, including matching
                SERVER observations outside the window. No inferred
                network-latency subtraction.
              </p>
              {data.edges.length === 0 && (
                <p>No confirmed cross-service pairs in this scope.</p>
              )}
              {data.edges.map((e, i) => (
                <article
                  key={e.from + e.to}
                  id={`edge-${i}`}
                  className="analytics-edge"
                >
                  <h3>
                    {
                      data.services.find((s) => s.identity.id === e.from)
                        ?.identity.name
                    }{" "}
                    →{" "}
                    {
                      data.services.find((s) => s.identity.id === e.to)
                        ?.identity.name
                    }
                  </h3>
                  <p>
                    {e.client.count} paired calls ·{" "}
                    {n(e.client.rate_per_second)} calls/s
                  </p>
                  <p>
                    CLIENT ERROR {pct(e.client.error_fraction)} · SERVER ERROR{" "}
                    {pct(e.server.error_fraction)}
                  </p>
                  <p>
                    CLIENT p50/p95/p99 ms:{" "}
                    {[e.client.p50_ms, e.client.p95_ms, e.client.p99_ms]
                      .map(n)
                      .join(" / ")}{" "}
                    ({e.client.duration_samples} samples)
                    <br />
                    SERVER p50/p95/p99 ms:{" "}
                    {[e.server.p50_ms, e.server.p95_ms, e.server.p99_ms]
                      .map(n)
                      .join(" / ")}{" "}
                    ({e.server.duration_samples} samples)
                  </p>
                  <Evidence ids={e.trace_ids} />
                </article>
              ))}
            </>
          )}
          <details>
            <summary>Coverage and evidence diagnostics</summary>
            <p>{data.completeness}</p>
            <p>
              {data.units}. {data.attribution}.
            </p>
            <dl>
              {Object.entries(data.diagnostics).map(([key, value]) => (
                <div key={key}>
                  <dt>{key.replaceAll("_", " ")}</dt>
                  <dd>{value}</dd>
                </div>
              ))}
            </dl>
            <p>
              Budgets: 128 candidate traces, 20,000 physical rows, 2,000 rows
              per trace, 16 MiB serialized rows/response, 64 service identities,
              128 edges, 4,096 service bucket points, 4-second API deadline. Any
              exceeded budget fails the entire query. Filters apply after
              bounded discovery.
            </p>
          </details>
        </>
      )}
    </section>
  );
}
function Evidence({ ids }: { ids: string[] }) {
  return (
    <ul>
      {ids.map((id) => (
        <li key={id}>
          <Link href={`/traces/${id}`}>Trace {id}</Link>
          {" · "}
          <Link href={`/traces/${id}#correlated-logs`}>Correlated logs</Link>
        </li>
      ))}
    </ul>
  );
}
export function ServiceGraph({
  services,
  edges,
  suffix,
}: {
  services: Service[];
  edges: Analytics["edges"];
  suffix: string;
}) {
  const columns = Math.max(1, Math.min(4, services.length));
  const width = columns * 195 + 20,
    height = Math.max(220, Math.ceil(services.length / columns) * 150 + 60);
  const positions = new Map(
    services.map((s, i) => [
      s.identity.id,
      { x: 100 + (i % columns) * 195, y: 60 + Math.floor(i / columns) * 150 },
    ]),
  );
  return (
    <div className="service-graph">
      <svg
        viewBox={`0 0 ${width} ${height}`}
        role="img"
        aria-label="Observed directed service dependencies. Nodes open service details; arrows open paired-call evidence."
      >
        <defs>
          <marker
            id="analytics-arrow"
            viewBox="0 0 10 10"
            refX="9"
            refY="5"
            markerWidth="7"
            markerHeight="7"
            orient="auto-start-reverse"
          >
            <path d="M 0 0 L 10 5 L 0 10 z" fill="currentColor" />
          </marker>
        </defs>
        {edges.map((e, i) => {
          const a = positions.get(e.from),
            b = positions.get(e.to);
          if (!a || !b) return null;
          return (
            <a
              key={e.from + e.to}
              href={`#edge-${i}`}
              aria-label={`${services.find((s) => s.identity.id === e.from)?.identity.name} to ${services.find((s) => s.identity.id === e.to)?.identity.name}, ${e.client.count} paired calls`}
            >
              <path
                d={`M ${a.x} ${a.y + 25} Q ${(a.x + b.x) / 2} ${(a.y + b.y) / 2 + 90} ${b.x} ${b.y + 27}`}
                stroke="currentColor"
                fill="none"
                strokeWidth="3"
                markerEnd="url(#analytics-arrow)"
              />
              <title>{e.client.count} confirmed paired calls</title>
            </a>
          );
        })}
        {services.map((s) => {
          const p = positions.get(s.identity.id)!;
          return (
            <a
              key={s.identity.id}
              href={`/services/${s.identity.id}${suffix}`}
              aria-label={`${s.identity.name}, ${s.identity.namespace || "no namespace"}, ${s.identity.workload}`}
            >
              <rect
                x={p.x - 85}
                y={p.y - 25}
                width="170"
                height="50"
                rx="9"
                fill="var(--bg, #101827)"
                stroke="currentColor"
              />
              <text
                x={p.x}
                y={p.y - 2}
                textAnchor="middle"
                fill="currentColor"
                fontSize="14"
              >
                {s.identity.name.slice(0, 22)}
              </text>
              <text
                x={p.x}
                y={p.y + 17}
                textAnchor="middle"
                fill="currentColor"
                fontSize="10"
              >
                {s.identity.workload} · {s.stats.count} inbound
              </text>
              <title>
                {s.identity.name} / {s.identity.namespace} /{" "}
                {s.identity.environment} / {s.identity.workload}
              </title>
            </a>
          );
        })}
      </svg>
      <ul aria-label="Graph node links">
        {services.map((s) => (
          <li key={s.identity.id}>
            <Link href={`/services/${s.identity.id}${suffix}`}>
              {s.identity.name} · {s.identity.namespace || "no namespace"} ·{" "}
              {s.identity.environment || "no environment"} ·{" "}
              {s.identity.workload}
            </Link>
          </li>
        ))}
      </ul>
    </div>
  );
}
