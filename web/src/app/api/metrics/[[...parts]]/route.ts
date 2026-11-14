import { proxyMetrics } from "@/lib/metrics";
export const runtime = "nodejs";
export async function GET(
  request: Request,
  { params }: { params: Promise<{ parts?: string[] }> },
) {
  return proxyMetrics(request, (await params).parts ?? []);
}
