import { proxyAnalytics } from "@/lib/analytics";
export const runtime = "nodejs";
export async function GET(
  request: Request,
  { params }: { params: Promise<{ parts: string[] }> },
) {
  return proxyAnalytics(request, (await params).parts);
}
