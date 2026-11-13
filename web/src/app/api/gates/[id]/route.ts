import { proxyComparison } from "@/lib/comparison-proxy";
export const runtime = "nodejs";
export async function GET(
  request: Request,
  context: { params: Promise<{ id: string }> },
) {
  return proxyComparison(request, "gates", [(await context.params).id]);
}
