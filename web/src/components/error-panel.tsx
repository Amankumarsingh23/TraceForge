import { APIError } from "../lib/client";
export function ErrorPanel({
  error,
  retry,
  resource = "traces",
}: {
  error: unknown;
  retry: () => void;
  resource?: "traces" | "logs" | "evaluations";
}) {
  const status = error instanceof APIError ? error.status : 0;
  const title =
    resource === "evaluations"
      ? ((
          {
            400: "Invalid evaluation query",
            404: "Evaluation not found",
            413: "Evaluation response exceeds supported size",
            503: "Evaluation backend unavailable",
            504: "Evaluation request timed out",
          } as Record<number, string>
        )[status] ?? "Could not load evaluation data")
      : resource === "logs"
        ? ((
            {
              400: "Invalid log query",
              413: "Log response exceeds supported size",
              503: "Log backend unavailable",
              504: "Log request timed out",
            } as Record<number, string>
          )[status] ?? "Could not load log data")
        : ((
            {
              400: "Invalid query or trace ID",
              404: "Trace not found",
              413: "Trace exceeds the supported detail size",
              503: "Trace backend unavailable",
              504: "Trace request timed out",
            } as Record<number, string>
          )[status] ?? "Could not load trace data");
  return (
    <section className="error-panel" role="alert">
      <h2>{title}</h2>
      <p>
        {error instanceof Error
          ? error.message
          : "An unexpected response was received."}
      </p>
      <p>No successful result is available for this request.</p>
      {resource === "traces" && status === 404 && (
        <p>
          The trace may be unexported, sampled out, expired or unavailable in
          this storage scope. Its absence does not imply successful execution or
          zero model usage. Retained evaluation and incident history can outlive
          raw telemetry.
        </p>
      )}
      <button onClick={retry}>Retry request</button>
    </section>
  );
}
