import { forwardTelemetry } from "../../../../lib/proxy";
export async function GET(
  request: Request,
  { params }: { params: Promise<{ parts: string[] }> },
) {
  const { parts } = await params;
  const path = parts.join("/");
  if (
    !/^(?:session|evaluations(?:\/[a-f0-9]{32})?)$/.test(path) ||
    new URL(request.url).search
  )
    return Response.json(
      { error: { message: "Invalid security query" } },
      { status: 400 },
    );
  return forwardTelemetry(request, "/api/v1/security/" + path);
}
