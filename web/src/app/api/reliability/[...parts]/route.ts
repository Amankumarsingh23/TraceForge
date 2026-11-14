import { proxyReliability } from "@/lib/reliability";
export const runtime = "nodejs";
export async function GET(
  request: Request,
  { params }: { params: Promise<{ parts: string[] }> },
) {
  return proxyReliability(request, (await params).parts);
}
