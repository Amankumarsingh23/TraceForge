"use client";
import { useEffect, useState } from "react";
import Link from "next/link";
import {
  metricQuery,
  metricSemantics,
  quantileRange,
  type MetricResult,
  type MetricSeries,
} from "../lib/metrics";
const format = (n: number | null) =>
  n === null
    ? "N/A"
    : n.toLocaleString(undefined, { maximumFractionDigits: 6 });
const at = (ns: string) =>
  new Date(Number(BigInt(ns) / 1000000n)).toISOString();
function Histogram({ series }: { series: MetricSeries }) {
  const p = series.metadata;
  return (
    <section aria-label="Histogram distribution">
      <p>
        Count: <strong>{series.histogram_count ?? "N/A"}</strong> · Sum:{" "}
        {format(series.histogram_sum)} {p.unit} · Mean:{" "}
        {format(series.histogram_mean)} {p.unit}
      </p>
      <p>
        p50: {quantileRange(series.p50_bucket, p.unit)}
        <br />
        p95: {quantileRange(series.p95_bucket, p.unit)}
        <br />
        p99: {quantileRange(series.p99_bucket, p.unit)}
      </p>
      <table>
        <caption>Observed incremental histogram buckets (exact counts)</caption>
        <thead>
          <tr>
            <th>Bucket in {p.unit}</th>
            <th>Observations</th>
          </tr>
        </thead>
        <tbody>
          {series.histogram_buckets.map((c, i) => (
            <tr key={i}>
              <td>
                {i === 0 ? "−∞" : p.bounds[i - 1]} &lt; value ≤{" "}
                {i === p.bounds.length ? "+∞" : p.bounds[i]}
              </td>
              <td>{c}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </section>
  );
}
export function MetricSeriesView({ series }: { series: MetricSeries }) {
  const p = series.metadata;
  const values =
    p.type === "gauge"
      ? series.points.map((p) => ({
          at: p.end_ns,
          start: p.end_ns,
          value:
            p.flags & 1
              ? null
              : (p.double_value ??
                (p.int_value === null ? null : Number(p.int_value))),
        }))
      : series.intervals.map((i) => ({
          at: i.end_ns,
          start: i.start_ns,
          value: i.rate_per_second,
        }));
  const max = Math.max(...values.map((v) => Math.abs(v.value ?? 0)), 0.000001);
  const plotFrom = BigInt(values[0]?.start ?? "0"),
    plotTo = BigInt(values.at(-1)?.at ?? "0"),
    span = Number(plotTo - plotFrom) || 1;
  return (
    <article className="metric-series">
      <h2>
        {p.name} · {p.service}
      </h2>
      <p>
        {metricSemantics(p)}
        <br />
        Unit: {p.unit || "dimensionless"} · Temporality:{" "}
        {p.type === "gauge"
          ? "not applicable"
          : p.temporality === 1
            ? "DELTA"
            : "CUMULATIVE"}
        <br />
        Instance: <code>{p.instance}</code> · Namespace:{" "}
        {p.namespace || "unspecified"} ·{" "}
        {p.environment || "unknown environment"} ·{" "}
        {p.workload || "unknown workload"}
      </p>
      <p>
        Series: <code>{p.stream_id}</code>
      </p>
      <p>
        Dimensions: <code>{JSON.stringify(p.attributes)}</code>
      </p>
      <p>
        Status: <strong>{series.status}</strong> · {series.points.length}{" "}
        in-window canonical points / {series.intervals.length} eligible
        intervals · {format(series.observed_seconds)} observed seconds ·
        coverage {format(series.coverage_fraction * 100)}%{" "}
        {series.complete
          ? "of the selected interval (not a traffic guarantee)"
          : "— incomplete or not applicable"}
      </p>
      {p.type === "gauge" ? (
        <p>
          Latest observed value: <strong>{series.latest ?? "N/A"}</strong> ·
          Observation min/max/mean:{" "}
          {[
            series.minimum_observation,
            series.maximum_observation,
            series.mean_observation,
          ]
            .map(format)
            .join(" / ")}{" "}
          (numeric summaries approximate; exact integer observations retained
          below)
        </p>
      ) : p.type === "sum" ? (
        <p>
          Observed {p.monotonic ? "increment" : "signed change"}:{" "}
          <strong>{series.increment ?? "N/A"}</strong> · Observed-coverage rate:{" "}
          {format(series.observed_rate_per_second)} {p.unit}/s. This denominator
          is observed interval seconds, not an assumed complete window.
          {!p.monotonic && p.temporality === 2 && (
            <>
              {" "}
              Latest observed cumulative state: {series.latest ?? "N/A"}. This
              is an in-window observation, not a reconstructed delta state.
            </>
          )}
        </p>
      ) : (
        <Histogram series={series} />
      )}
      {p.type !== "histogram" && values.some((v) => v.value !== null) && (
        <figure>
          <figcaption>
            {p.type === "gauge"
              ? "Gauge observations"
              : "Derived interval rates"}{" "}
            · display plot is approximate; missing values are gaps
          </figcaption>
          <svg
            viewBox="0 0 700 120"
            role="img"
            aria-label={`${p.name} ${p.type === "gauge" ? "gauge observations" : "observed interval rate"}`}
            style={{ width: "100%", maxHeight: 180 }}
          >
            <line x1="0" x2="700" y1="60" y2="60" stroke="currentColor" />
            {values.map((v, i) =>
              v.value === null || !Number.isFinite(v.value) ? null : (
                <rect
                  key={i}
                  x={(Number(BigInt(v.start) - plotFrom) / span) * 690}
                  y={v.value >= 0 ? 60 - (v.value / max) * 55 : 60}
                  width={
                    p.type === "gauge"
                      ? 4
                      : Math.max(
                          1,
                          (Number(BigInt(v.at) - BigInt(v.start)) / span) *
                            690 -
                            1,
                        )
                  }
                  height={Math.max(1, (Math.abs(v.value) / max) * 55)}
                  fill="currentColor"
                >
                  <title>
                    {at(v.at)}: {v.value}
                  </title>
                </rect>
              ),
            )}
          </svg>
        </figure>
      )}
      <details>
        <summary>Coverage, resets, replay and overflow diagnostics</summary>
        <pre>{JSON.stringify(series.diagnostics, null, 2)}</pre>
        <p>
          Any SDK overflow series means attribute-specific coverage is
          unreliable. Missing exports, boundary crossings, conflicts and resets
          require inspection. No no-data point is plotted as zero.
        </p>
      </details>
      <details>
        <summary>Raw canonical points and exact timestamps</summary>
        <pre>{JSON.stringify(series.points, null, 2)}</pre>
      </details>
      <details>
        <summary>Derived intervals</summary>
        <pre>{JSON.stringify(series.intervals, null, 2)}</pre>
      </details>
    </article>
  );
}
export function MetricsExplorer({ initialQuery }: { initialQuery: string }) {
  const [query, setQuery] = useState(initialQuery),
    [data, setData] = useState<MetricResult | null>(null),
    [error, setError] = useState(""),
    [loading, setLoading] = useState(false),
    [selected, setSelected] = useState("");
  useEffect(() => {
    const controller = new AbortController();
    async function load() {
      setLoading(true);
      setData(null);
      setError("");
      try {
        metricQuery(new URLSearchParams(query));
        const response = await fetch(`/api/metrics/query?${query}`, {
          signal: controller.signal,
          cache: "no-store",
        });
        const body = await response.json();
        if (!response.ok)
          throw new Error(
            `${response.status}: ${body.error?.message ?? "Metrics unavailable"}`,
          );
        if (body.version !== "otlp-metrics/v1" || !Array.isArray(body.series))
          throw new Error("Unsupported metric response");
        setData(body);
      } catch (e) {
        if (!controller.signal.aborted) setError((e as Error).message);
      } finally {
        if (!controller.signal.aborted) setLoading(false);
      }
    }
    void load();
    return () => controller.abort();
  }, [query]);
  const params = new URLSearchParams(query);
  function submit(form: FormData) {
    const p = new URLSearchParams();
    for (const [k, v] of form) if (String(v)) p.set(k, String(v));
    setSelected("");
    setQuery(p.toString());
    window.history.replaceState(null, "", `/metrics?${p}`);
  }
  return (
    <section className="analytics-page metrics-page">
      <p className="eyebrow">
        Direct application OTLP metrics · otlp-metrics/v1
      </p>
      <h1>Metrics explorer</h1>
      <p>
        These instruments aggregate in the application independently of trace
        sampling. <Link href="/services">Trace-derived service RED</Link> uses
        canonical SERVER spans and a different start-time denominator.
      </p>
      <form action={submit} key={query} className="analytics-filters">
        <label>
          Metric name
          <input
            name="name"
            defaultValue={params.get("name") ?? ""}
            maxLength={128}
            list="metric-names"
          />
        </label>
        <datalist id="metric-names">
          {[...new Set(data?.series.map((s) => s.metadata.name) ?? [])].map(
            (n) => (
              <option key={n}>{n}</option>
            ),
          )}
        </datalist>
        <label>
          Service
          <input
            name="service"
            defaultValue={params.get("service") ?? ""}
            maxLength={128}
          />
        </label>
        <label>
          Namespace
          <input
            name="namespace"
            defaultValue={params.get("namespace") ?? ""}
            maxLength={128}
          />
        </label>
        <label>
          From UTC
          <input name="from" defaultValue={params.get("from") ?? ""} required />
        </label>
        <label>
          To UTC
          <input name="to" defaultValue={params.get("to") ?? ""} required />
        </label>
        <button disabled={loading}>Query metrics</button>
      </form>
      {loading && <p role="status">Loading bounded metric streams…</p>}
      {error && <p role="alert">{error}</p>}
      {data && (
        <>
          <p>
            {data.query.from} &lt; point end ≤ {data.query.to}
          </p>
          <p>{data.caveat}</p>
          <p>
            Budgets: one-hour window, 32 candidate series, 12,000 physical
            points, one-hour predecessor horizon, 16MiB query/response bytes.
            Native export resolution; no silent resampling, pagination or input
            truncation.
          </p>
          {data.series.length === 0 ? (
            <p>
              No observed metric series in this scope. Values, increments and
              percentiles are N/A.
            </p>
          ) : (
            <>
              <label>
                Series / dimensions
                <select
                  value={selected}
                  onChange={(e) => setSelected(e.target.value)}
                >
                  <option value="">
                    All bounded series ({data.series.length})
                  </option>
                  {data.series.map((s) => (
                    <option
                      key={s.metadata.stream_id}
                      value={s.metadata.stream_id}
                    >
                      {s.metadata.name} / {s.metadata.service} /{" "}
                      {s.metadata.instance} /{" "}
                      {JSON.stringify(s.metadata.attributes)}
                    </option>
                  ))}
                </select>
              </label>
              {data.series
                .filter((s) => !selected || s.metadata.stream_id === selected)
                .map((s) => (
                  <MetricSeriesView key={s.metadata.stream_id} series={s} />
                ))}
            </>
          )}
          <p>Histogram merge: {data.merge_status}</p>
          <p>Sum merge: {data.sum_merge_status}</p>
          {data.combined_sum && (
            <section aria-label="Combined observed Sum">
              <h2>Combined observed increment</h2>
              <p>
                <strong>{data.combined_sum.increment}</strong> from{" "}
                {data.combined_sum.source_streams} source streams · full-window
                observed rate: {format(data.combined_sum.rate_per_second)}
              </p>
              <p>{data.combined_sum.coverage}</p>
            </section>
          )}
          {data.merged_histogram && (
            <section aria-label="Merged histogram">
              <h2>Merged observed histogram</h2>
              <Histogram series={data.merged_histogram} />
            </section>
          )}
        </>
      )}
    </section>
  );
}
