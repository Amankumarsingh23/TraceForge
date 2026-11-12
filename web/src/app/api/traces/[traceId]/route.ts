import { proxyTrace } from "@/lib/proxy";
export const runtime = "nodejs";
export async function GET(
  request: Request,
  context: { params: Promise<{ traceId: string }> },
) {
  const { traceId } = await context.params;
  return proxyTrace(request, traceId);
}
