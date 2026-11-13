import { proxyLogs } from "@/lib/logs-proxy";
export const runtime = "nodejs";
export async function GET(request: Request) {
  return proxyLogs(request);
}
