import { test, expect } from "@playwright/test";
import { readFileSync, mkdirSync } from "node:fs";
import path from "node:path";
const root = path.resolve(import.meta.dirname, "../..");
const read = (name: string) => JSON.parse(readFileSync(path.join(root, "artifacts/session15", name), "utf8"));
const shots = path.join(root, "artifacts/session15/screenshots");
test.beforeAll(() => mkdirSync(shots, { recursive: true }));

test("final demo A opens actual service graph, failed trace, logs and incident", async ({ page }) => {
  const workload = read("demo-a/workload.json");
  const reliability = read("demo-a/reliability.json");
  await page.goto(`/services?${workload.trace_query}`);
  await expect(page.getByRole("heading", { name: "Services", exact: true })).toBeVisible();
  await expect(page.getByRole("link", { name: "payment", exact: true })).toBeVisible();
  await page.screenshot({ path: path.join(shots, "a-services.png"), fullPage: true });
  await page.goto(`/service-graph?${workload.trace_query}`);
  await expect(page.getByRole("heading", { name: "Observed service graph" })).toBeVisible();
  await page.screenshot({ path: path.join(shots, "a-graph.png"), fullPage: true });
  const failed = workload.requests.find((row: { kind: string }) => row.kind === "failed");
  await page.goto(`/traces/${failed.trace_id}`);
  await expect(page.locator(".waterfall-row")).toHaveCount(5);
  await expect(page.getByRole("region", { name: "Correlated logs" })).toContainText("payment failure");
  await page.screenshot({ path: path.join(shots, "a-failed-trace.png"), fullPage: true });
  await page.goto(`/alerts/${reliability.incident_id}`);
  await expect(page.getByRole("heading", { name: "RESOLVED · critical" })).toBeVisible();
  await page.screenshot({ path: path.join(shots, "a-incident.png"), fullPage: true });
});

test("final demo B opens persisted fixture gate regression and both real traces", async ({ page }) => {
  const evidence = read("demo-b/evidence.json");
  const regression = read("demo-b/context_regression.json");
  const gate = evidence.context_regression;
  await page.goto(`/comparisons/${gate.comparison_id}?decision=${gate.decision_id}`);
  await expect(page.getByRole("heading", { name: "Gate decision: FAIL" })).toBeVisible();
  await page.getByRole("combobox", { name: "Filter case transitions" }).selectOption("regressed");
  const row = regression.comparison.cases.find((item: { transition: string }) => item.transition === "regressed");
  await expect(page.getByRole("heading", { name: `${row.case_id} · regressed` })).toBeVisible();
  await page.screenshot({ path: path.join(shots, "b-regression.png"), fullPage: true });
  for (const trace of [row.baseline_trace_id, row.candidate_trace_id]) {
    await page.goto(`/traces/${trace}`);
    await expect(page.locator(".waterfall-row")).toHaveCount(6);
    await expect(page.getByRole("region", { name: "AgentOps execution summary" })).toContainText("Fixture");
  }
  await page.screenshot({ path: path.join(shots, "b-agent-trace.png"), fullPage: true });
});
