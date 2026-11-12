// @vitest-environment node
import { expect, it, vi } from "vitest";
import { proxyTrace, validateQuery } from "./proxy";
const id = "a".repeat(32);
it("restricts paths, IDs and query parameters before contacting upstream", async () => {
  const transport = vi.fn();
  for (const path of [
    "?url=http://example.com",
    "?service=one&service=two",
    "?status=ok",
    "?limit=101",
    "?cursor=",
    "?from=2026-11-12T00:00:00Z&to=2026-11-14T00:00:00Z",
  ]) {
    expect(
      (
        await proxyTrace(
          new Request(`http://localhost/api/traces${path}`),
          undefined,
          { transport },
        )
      ).status,
    ).toBe(400);
  }
  for (const value of ["../healthz", "0".repeat(32), "A".repeat(32)])
    expect(
      (
        await proxyTrace(new Request("http://localhost/api/traces"), value, {
          transport,
        })
      ).status,
    ).toBe(400);
  expect(transport).not.toHaveBeenCalled();
});
it("leaves opaque cursor binding to Go and accepts precise UTC bounds", () => {
  expect(() =>
    validateQuery(
      new URLSearchParams({
        cursor: "opaque_A-1",
        from: "2026-11-12T00:00:00.123456789Z",
        to: "2026-11-12T01:00:00Z",
        service: "payment",
        status: "error",
      }),
    ),
  ).not.toThrow();
});
it("preserves upstream statuses, response bytes and only approved paths", async () => {
  for (const status of [200, 400, 404, 413, 503, 504]) {
    const raw =
      '{"duration_ns":999999999999999999,"start_time":"2026-11-12T00:00:00.123456789Z"}';
    const transport = vi.fn<typeof fetch>(
      async () =>
        new Response(raw, {
          status,
          headers: { "content-type": "application/json" },
        }),
    );
    const response = await proxyTrace(
      new Request("http://localhost/api/traces/" + id),
      id,
      { origin: "http://127.0.0.1:8082", transport },
    );
    expect(response.status).toBe(status);
    expect(await response.text()).toBe(raw);
    expect(String(transport.mock.calls[0][0])).toBe(
      `http://127.0.0.1:8082/api/v1/traces/${id}`,
    );
    expect(response.headers.get("cache-control")).toBe("no-store");
  }
});
it("sanitizes network failures and rejects credential-bearing origins", async () => {
  const transport = vi.fn(async () => {
    throw new Error("sensitive internal address");
  });
  const response = await proxyTrace(
    new Request("http://localhost/api/traces"),
    undefined,
    { transport },
  );
  expect(response.status).toBe(503);
  expect(await response.text()).not.toContain("sensitive");
  expect(
    (
      await proxyTrace(new Request("http://localhost/api/traces"), undefined, {
        origin: "http://user:secret@localhost/",
      })
    ).status,
  ).toBe(503);
});
it("rejects an oversized upstream body", async () => {
  const transport = vi.fn(
    async () =>
      new Response("x".repeat(16 * 1024 * 1024 + 1), {
        headers: { "content-type": "application/json" },
      }),
  );
  expect(
    (
      await proxyTrace(new Request("http://localhost/api/traces"), undefined, {
        transport,
      })
    ).status,
  ).toBe(413);
});
