import { proxyComparison } from "@/lib/comparison-proxy";
export const runtime = "nodejs";
export async function GET(
  request: Request,
  context: { params: Promise<{ path?: string[] }> },
) {
  return proxyComparison(request, "comparisons", (await context.params).path);
}
