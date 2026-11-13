// @vitest-environment node
import { expect, it, vi } from "vitest";
import { proxyLogs, validateLogQuery } from "./logs-proxy";
import { forwardTelemetry } from "./proxy";
it("narrows log routes and requires both IDs for span filters", async () => {
  const transport = vi.fn();
  for (const query of [
    "url=http://example.com",
    "span_id=" + "b".repeat(16),
    "min_severity=25",
    "service=a&service=b",
    "cursor=",
    "text=" + "x".repeat(257),
  ]) {
    expect(
      (
        await proxyLogs(
          new Request(`http://localhost/api/logs?${query}`),
          undefined,
          { transport },
        )
      ).status,
    ).toBe(400);
  }
  expect(
    (
      await forwardTelemetry(new Request("http://localhost"), "/healthz", {
        transport,
      })
    ).status,
  ).toBe(400);
  expect(transport).not.toHaveBeenCalled();
  expect(() =>
    validateLogQuery(
      new URLSearchParams({ span_id: "b".repeat(16) }),
      "a".repeat(32),
    ),
  ).not.toThrow();
});
it("forwards only the exact correlation route and preserves unavailable status", async () => {
  const transport = vi.fn<typeof fetch>(
    async () =>
      new Response(
        JSON.stringify({
          error: { code: "backend_unavailable", message: "offline" },
        }),
        { status: 503, headers: { "content-type": "application/json" } },
      ),
  );
  const response = await proxyLogs(
    new Request(
      "http://localhost/api/traces/" +
        "a".repeat(32) +
        "/logs?span_id=" +
        "b".repeat(16),
    ),
    "a".repeat(32),
    { transport },
  );
  expect(response.status).toBe(503);
  expect(String(transport.mock.calls[0][0])).toContain(
    "/api/v1/traces/" + "a".repeat(32) + "/logs?span_id=",
  );
});
