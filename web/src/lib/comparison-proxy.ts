import { forwardTelemetry } from "./proxy";
export function proxyComparison(
  request: Request,
  prefix: "comparisons" | "gates" | "experiments",
  parts: string[] = [],
) {
  const query = new URL(request.url).searchParams;
  let valid =
    prefix === "experiments"
      ? parts.length === 0
      : prefix === "gates"
        ? parts.length === 1
        : parts.length <= 2;
  if (parts.length && !/^[a-f0-9]{32}$/.test(parts[0])) valid = false;
  if (parts.length === 2 && parts[1] !== "cases") valid = false;
  const detail =
    prefix === "gates" || (prefix === "comparisons" && parts.length === 1);
  for (const [key, value] of query) {
    if (
      !["limit", "after"].includes(key) ||
      query.getAll(key).length !== 1 ||
      !value ||
      detail
    )
      valid = false;
  }
  if (
    query.has("limit") &&
    !/^(?:[1-9]|[1-4][0-9]|50)$/.test(query.get("limit")!)
  )
    valid = false;
  if (
    query.has("after") &&
    !(
      prefix === "experiments"
        ? /^[a-f0-9]{64}$/
        : parts.length === 2
          ? /^[A-Za-z0-9_-]{1,64}$/
          : /^[a-f0-9]{32}$/
    ).test(query.get("after")!)
  )
    valid = false;
  if (!valid)
    return Response.json(
      {
        error: {
          code: "invalid_comparison_query",
          message: "Invalid comparison path or filters",
        },
      },
      { status: 400 },
    );
  return forwardTelemetry(
    request,
    `/api/v1/${prefix}${parts.length ? "/" + parts.join("/") : ""}`,
  );
}
