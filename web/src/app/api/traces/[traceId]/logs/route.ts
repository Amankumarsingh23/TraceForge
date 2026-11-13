import { proxyLogs } from "@/lib/logs-proxy";
export const runtime = "nodejs";
export async function GET(
  request: Request,
  context: { params: Promise<{ traceId: string }> },
) {
  return proxyLogs(request, (await context.params).traceId);
}
