// @vitest-environment node
import { expect, it } from "vitest";
import {
  validateAnalytics,
  proxyAnalytics,
  sortServices,
  type Service,
} from "./analytics";
it("bounds UTC windows, workload filters and bucket sizes", () => {
  for (const s of [
    "from=bad",
    "bucket_seconds=1",
    "workload=production",
    "namespace=x&namespace=y",
    "limit=1",
    "from=2026-11-13T00:00:00Z&to=2026-11-13T02:00:00Z",
    "from=2026-11-13T00:00:00Z&to=2026-11-13T00:00:00Z",
  ]) {
    expect(() => validateAnalytics(new URLSearchParams(s))).toThrow();
  }
  expect(() =>
    validateAnalytics(
      new URLSearchParams(
        "from=2026-11-13T00:00:00Z&to=2026-11-13T01:00:00Z&workload=synthetic&bucket_seconds=300",
      ),
    ),
  ).not.toThrow();
});
it("sorts bounded services without treating missing latency as a fast sample", () => {
  const a = {
    identity: { id: "a", name: "payment" },
    stats: { rate_per_second: 2, error_fraction: 0, p95_ms: null },
  } as Service;
  const b = {
    identity: { id: "b", name: "gateway" },
    stats: { rate_per_second: 1, error_fraction: 0.5, p95_ms: 0 },
  } as Service;
  const c = {
    identity: { id: "c", name: "worker" },
    stats: { rate_per_second: 2, error_fraction: null, p95_ms: 50 },
  } as Service;
  const services = [a, b, c];
  expect(sortServices(services, "p95").map((s) => s.identity.id)).toEqual([
    "c",
    "b",
    "a",
  ]);
  expect(sortServices(services, "errors").map((s) => s.identity.id)).toEqual([
    "b",
    "a",
    "c",
  ]);
  expect(sortServices(services, "rate").map((s) => s.identity.id)).toEqual([
    "a",
    "c",
    "b",
  ]);
  expect(sortServices(services, "name").map((s) => s.identity.id)).toEqual([
    "b",
    "a",
    "c",
  ]);
  expect(services).toEqual([a, b, c]);
});
it("rejects arbitrary and malformed paths before forwarding", async () => {
  for (const parts of [
    ["../healthz"],
    ["a".repeat(32), "secret"],
    ["a".repeat(32)],
  ])
    expect(
      (
        await proxyAnalytics(
          new Request("http://localhost/api/services"),
          parts,
        )
      ).status,
    ).toBe(400);
});
