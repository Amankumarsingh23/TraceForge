// @vitest-environment node
import { expect, it } from "vitest";
import { percentage, proxyReliability } from "./reliability";
it("formats exact policy ratios for display without rounding away negative/fractional budget values", () => {
  expect(percentage("9993/10000")).toBe("99.93%");
  expect(percentage("6/5")).toBe("120%");
  expect(percentage("0")).toBe("0%");
});
it("restricts reliability proxy paths and metadata pagination", async () => {
  for (const [path, q] of [
    ["../healthz", ""],
    ["slos", "?url=http://example.com"],
    ["alerts", "?limit=51"],
    ["slos/test-v1", "?limit=1"],
    ["slos", "?after=a&after=b"],
  ]) {
    expect(
      (
        await proxyReliability(
          new Request(`http://local/api/reliability/${path}${q}`),
          path.split("/"),
        )
      ).status,
    ).toBe(400);
  }
});
