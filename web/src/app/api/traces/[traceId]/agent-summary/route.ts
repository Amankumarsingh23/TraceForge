import { forwardTelemetry, validateQuery } from "@/lib/proxy";
export const runtime = "nodejs";
export async function GET(
  request: Request,
  context: { params: Promise<{ traceId: string }> },
) {
  const { traceId } = await context.params;
  try {
    validateQuery(new URL(request.url).searchParams, traceId);
  } catch {
    return Response.json(
      {
        error: {
          code: "invalid_trace_query",
          message: "Invalid trace ID or filters.",
        },
      },
      { status: 400 },
    );
  }
  return forwardTelemetry(request, `/api/v1/traces/${traceId}/agent-summary`);
}
