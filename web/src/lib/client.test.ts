import { expect, it, vi } from "vitest";
import { APIError, getJSON, LatestRequest } from "./client";
import { applyFilters, nextPage, previousPage, searchPath } from "./search";
it("discards older successful responses even if fetch ignores abort", async () => {
  const latest = new LatestRequest();
  const values: string[] = [];
  let resolveOld!: (value: string) => void;
  const older = latest.run(
    () =>
      new Promise<string>((resolve) => {
        resolveOld = resolve;
      }),
    (value) => values.push(value),
    () => {},
  );
  await latest.run(
    async () => "new",
    (value) => values.push(value),
    () => {},
  );
  resolveOld("old");
  await older;
  expect(values).toEqual(["new"]);
});
it("discards stale failures and cancels on cleanup", async () => {
  const latest = new LatestRequest();
  const failure = vi.fn();
  let reject!: (e: Error) => void;
  const work = latest.run(
    () =>
      new Promise((_, r) => {
        reject = r;
      }),
    vi.fn(),
    failure,
  );
  latest.cancel();
  reject(new Error("old"));
  await work;
  expect(failure).not.toHaveBeenCalled();
});
it("binds cursors to applied filters and resets on a new search", () => {
  const filters = {
    from: "2026-11-12T00:00:00Z",
    to: "2026-11-12T01:00:00Z",
    service: "payment",
    status: "error",
    limit: "20",
  };
  const first = applyFilters(filters),
    second = nextPage(first, "opaque"),
    third = nextPage(second, "third");
  expect(searchPath(second.filters, second.cursor)).toContain(
    "service=payment",
  );
  expect(previousPage(third)).toEqual(second);
  expect(previousPage(second)).toEqual(first);
  expect(applyFilters({ ...filters, service: "gateway" })).toMatchObject({
    cursor: "",
    previous: [],
  });
  expect(filters.service).toBe("payment");
});
it("preserves error status and never treats a 503 as empty data", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(
      async () =>
        new Response(
          JSON.stringify({
            error: { code: "backend_unavailable", message: "offline" },
          }),
          { status: 503 },
        ),
    ),
  );
  await expect(
    getJSON("/api/traces", new AbortController().signal),
  ).rejects.toMatchObject({ status: 503, code: "backend_unavailable" });
  vi.unstubAllGlobals();
});
it("turns a network timeout into a retryable visible error", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => {
      throw new DOMException("timeout", "TimeoutError");
    }),
  );
  await expect(
    getJSON("/api/traces", new AbortController().signal),
  ).rejects.toBeInstanceOf(APIError);
  vi.unstubAllGlobals();
});
