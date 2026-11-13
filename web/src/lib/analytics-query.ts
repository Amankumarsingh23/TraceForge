export function analyticsQuery(
  values: Record<string, string | string[] | undefined>,
) {
  const p = new URLSearchParams();
  for (const [key, value] of Object.entries(values)) {
    for (const item of Array.isArray(value)
      ? value
      : value === undefined
        ? []
        : [value])
      p.append(key, item);
  }
  if (!p.has("from") && !p.has("to")) {
    const to = new Date();
    p.set("from", new Date(+to - 900000).toISOString());
    p.set("to", to.toISOString());
  }
  return p.toString();
}
