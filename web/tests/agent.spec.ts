import { test, expect } from "@playwright/test";
import { mkdirSync, readFileSync } from "node:fs";
import path from "node:path";
const root = path.resolve(import.meta.dirname, "../..");
const evidence = JSON.parse(
  readFileSync(path.join(root, "artifacts/session07/evidence.json"), "utf8"),
);
const screenshots = path.join(root, "artifacts/session07/screenshots");
test.beforeAll(() => mkdirSync(screenshots, { recursive: true }));
test("real stored agent execution links retrieval, attempts and tool to the waterfall", async ({
  page,
}) => {
  await page.goto(`/traces/${evidence.tool.trace_id}`);
  const panel = page.getByRole("region", {
    name: "AgentOps execution summary",
  });
  await expect(panel).toContainText("Fixture · synthetic usage");
  await expect(panel).toContainText("$0.002300000");
  await expect(panel).toContainText("Retrieved IDs:");
  await expect(panel).toContainText("Supplied IDs:");
  await expect(panel).toContainText("get_order_status");
  await expect(page.locator(".waterfall-row")).toHaveCount(
    evidence.tool.unique_spans,
  );
  await panel.getByRole("button", { name: /^tool ·/ }).click();
  await expect(page.locator(".inspector")).toContainText(
    "execute_tool get_order_status",
  );
  await expect(
    page.getByRole("region", { name: "Correlated logs" }),
  ).toContainText("tool completed");
  await page.screenshot({
    path: path.join(screenshots, "agent-tool-desktop.png"),
    fullPage: true,
  });
});
test("retry and missing usage retain unknown cost and failed attempts", async ({
  page,
}) => {
  await page.goto(`/traces/${evidence.retry.trace_id}`);
  let panel = page.getByRole("region", { name: "AgentOps execution summary" });
  await expect(panel).toContainText("provider_timeout");
  await expect(panel).toContainText("Unknown");
  await expect(panel).toContainText("1/2 complete attempts");
  await page.goto(`/traces/${evidence.missing_usage.trace_id}`);
  panel = page.getByRole("region", { name: "AgentOps execution summary" });
  await expect(panel).toContainText("0/1 complete attempts");
  await expect(panel).toContainText("Unknown");
});
test("mobile agent panel remains bounded and selectable", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto(`/traces/${evidence.tool.trace_id}`);
  const panel = page.getByRole("region", {
    name: "AgentOps execution summary",
  });
  await expect(panel).toContainText("$0.002300000");
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= window.innerWidth,
    ),
  ).toBe(true);
  await page.screenshot({
    path: path.join(screenshots, "agent-tool-mobile.png"),
    fullPage: true,
  });
});
