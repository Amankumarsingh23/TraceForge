import { proxyAnalytics } from "@/lib/analytics";
export const runtime = "nodejs";
export async function GET(request: Request) {
  return proxyAnalytics(request);
}
