import { proxyTrace } from "@/lib/proxy";
export const runtime = "nodejs";
export async function GET(request: Request) {
  return proxyTrace(request);
}
