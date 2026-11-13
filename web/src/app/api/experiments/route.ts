import { proxyComparison } from "@/lib/comparison-proxy";
export const runtime = "nodejs";
export function GET(request: Request) {
  return proxyComparison(request, "experiments");
}
