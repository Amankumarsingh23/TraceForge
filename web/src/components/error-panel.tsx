import { APIError } from "../lib/client";
export function ErrorPanel({
  error,
  retry,
}: {
  error: unknown;
  retry: () => void;
}) {
  const status = error instanceof APIError ? error.status : 0;
  const title =
    (
      {
        400: "Invalid query or trace ID",
        404: "Trace not found",
        413: "Trace exceeds the supported detail size",
        503: "Trace backend unavailable",
        504: "Trace request timed out",
      } as Record<number, string>
    )[status] ?? "Could not load trace data";
  return (
    <section className="error-panel" role="alert">
      <h2>{title}</h2>
      <p>
        {error instanceof Error
          ? error.message
          : "An unexpected response was received."}
      </p>
      <p>No successful result is available for this request.</p>
      <button onClick={retry}>Retry request</button>
    </section>
  );
}
