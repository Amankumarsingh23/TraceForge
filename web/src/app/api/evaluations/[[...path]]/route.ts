import { forwardTelemetry } from "@/lib/proxy";
export const runtime = "nodejs";
export async function GET(
  request: Request,
  context: { params: Promise<{ path?: string[] }> },
) {
  const { path = [] } = await context.params;
  const q = new URL(request.url).searchParams;
  let valid =
    path.length <= 3 &&
    (path.length === 0 || /^[a-f0-9]{32}$/.test(path[0])) &&
    (path.length < 2 || path[1] === "cases") &&
    (path.length < 3 || /^[A-Za-z0-9_-]{1,64}$/.test(path[2]));
  for (const [k, v] of q)
    if (
      !["limit", "after"].includes(k) ||
      q.getAll(k).length !== 1 ||
      !v ||
      path.length === 1 ||
      path.length === 3
    )
      valid = false;
  if (q.has("limit") && !/^(?:[1-9]|[1-4][0-9]|50)$/.test(q.get("limit")!))
    valid = false;
  if (
    q.has("after") &&
    !(path.length === 0 ? /^[a-f0-9]{32}$/ : /^[A-Za-z0-9_-]{1,64}$/).test(
      q.get("after")!,
    )
  )
    valid = false;
  if (!valid)
    return Response.json(
      {
        error: {
          code: "invalid_evaluation_query",
          message: "Invalid evaluation path or pagination.",
        },
      },
      { status: 400 },
    );
  return forwardTelemetry(
    request,
    "/api/v1/evaluations" + (path.length ? "/" + path.join("/") : ""),
  );
}
