import type { Diagnostics as DiagnosticData } from "../lib/contracts";
export function Diagnostics({ data }: { data: DiagnosticData }) {
  return (
    <section className="diagnostics" aria-label="Trace diagnostics">
      <div className="diagnostic-summary">
        <span>
          Completeness: <strong>{data.completeness}</strong>
        </span>
        <span>{data.exact_duplicate_rows} exact duplicate rows</span>
        <span>
          {data.conflicting_duplicate_groups} conflicting duplicate groups
        </span>
        <span>{data.missing_parent_count} missing parents</span>
        <span>{data.cycle_count} cycles</span>
        <span>{data.timing_warnings.length} timing warnings</span>
      </div>
      <p>
        Observed telemetry only. Missing observations cannot be ruled out, even
        when no gaps are detected.
      </p>
      {(data.has_detected_gaps ||
        data.multiple_roots ||
        data.timing_warnings.length > 0 ||
        data.conflicting_duplicate_groups > 0) && (
        <details open>
          <summary>Graph and timing warnings</summary>
          <ul>
            {data.multiple_roots && (
              <li>
                Multiple observed roots ({data.observed_root_count}); each
                remains visible.
              </li>
            )}
            {data.missing_parent_ids.map((id) => (
              <li key={id}>
                Missing parent <code>{id}</code>; orphan spans remain visible.
              </li>
            ))}
            {data.cycles.map((ids, i) => (
              <li key={i}>
                Cycle: <code>{ids.join(" → ")}</code>. The backend detached one
                display edge.
              </li>
            ))}
            {data.conflicts.map((c) => (
              <li key={c.span_id}>
                Conflicting span <code>{c.span_id}</code>: {c.variants}{" "}
                variants; backend canonical selection displayed.
              </li>
            ))}
            {data.timing_warnings.map((w, i) => (
              <li key={i}>
                Timing: {w.code} · span <code>{w.span_id}</code>
                {w.parent_span_id && (
                  <>
                    {" "}
                    · parent <code>{w.parent_span_id}</code>
                  </>
                )}
              </li>
            ))}
          </ul>
        </details>
      )}
    </section>
  );
}
