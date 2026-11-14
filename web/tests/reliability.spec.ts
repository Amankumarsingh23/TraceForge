import { test, expect } from "@playwright/test";
import { readFileSync, mkdirSync } from "node:fs";
import path from "node:path";
const root = path.resolve(import.meta.dirname, "../..");
const e = JSON.parse(
  readFileSync(path.join(root, "artifacts/session12/evidence.json"), "utf8"),
);
const shots = path.join(root, "artifacts/session12/screenshots");
const durationRevision = JSON.parse(
  readFileSync(
    path.join(root, "artifacts/session12/provider-duration-revision.json"),
    "utf8",
  ),
)[0];
test.beforeAll(() => mkdirSync(shots, { recursive: true }));
test("persisted breach has exact budgets and source drilldown", async ({
  page,
}) => {
  await page.goto(`/slos/${e.slo_key}?evaluation=${e.firing}`);
  await expect(
    page.getByRole("heading", { name: "Evaluation MEASURED", exact: true }),
  ).toBeVisible();
  const overall = page.locator(".slo-window").first();
  await expect(overall).toContainText("Eligible: 23 · Good: 4 · Bad: 19");
  await expect(overall).toContainText("190/23×");
  await expect(overall).toContainText("-167/10");
  await expect(
    page.getByRole("heading", { name: "fast-demo: BREACH" }),
  ).toBeVisible();
  await page.screenshot({
    path: path.join(shots, "measured-breach.png"),
    fullPage: true,
  });
  await page
    .getByRole("link", { name: "Inspect actual source metrics" })
    .click();
  await expect(
    page.getByRole("heading", { name: "Metrics explorer" }),
  ).toBeVisible();
});
test("one resolved incident retains exactly two contributing events", async ({
  page,
}) => {
  await page.goto(`/alerts/${e.incident_id}`);
  await expect(
    page.getByRole("heading", { name: "RESOLVED · critical" }),
  ).toBeVisible();
  const links = page.getByRole("link", {
    name: "Contributing evaluation",
    exact: true,
  });
  await expect(links).toHaveCount(2);
  await page.screenshot({
    path: path.join(shots, "incident-history.png"),
    fullPage: true,
  });
  await links.first().click();
  await expect(
    page.getByRole("heading", { name: "fast-demo: BREACH" }),
  ).toBeVisible();
});
test("backend outage remains unknown with no budget", async ({ page }) => {
  await page.goto(`/slos/${e.slo_key}?evaluation=${e.unknown}`);
  await expect(
    page.getByRole("heading", { name: "Evaluation UNKNOWN", exact: true }),
  ).toBeVisible();
  await expect(page.locator(".slo-window").first()).toContainText(
    "BACKEND_UNAVAILABLE",
  );
  await expect(page.locator(".slo-window").first()).toContainText(
    "SLI, budget and burn: N/A",
  );
});
test("histogram threshold reports measured slow requests on mobile", async ({
  page,
}) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto(`/slos/${e.latency_key}?evaluation=${e.latency}`);
  await expect(page.locator(".slo-window").first()).toContainText(
    "Eligible: 21 · Good: 14 · Bad: 7",
  );
  await page.screenshot({
    path: path.join(shots, "mobile-latency.png"),
    fullPage: true,
  });
});
test("fixture attempts differ from logical operations and link actual traces", async ({
  page,
}) => {
  await page.goto(`/slos/${e.provider_key}?evaluation=${durationRevision.id}`);
  await expect(
    page.getByRole("region", { name: "Provider operation breakdown" }),
  ).toContainText(
    "Attempts: 4 · successful attempts: 2 · logical operations: 3",
  );
  await expect(
    page.getByRole("region", { name: "Provider operation breakdown" }),
  ).toContainText("retry recovered: 1");
  await expect(
    page.getByRole("region", { name: "Provider operation breakdown" }),
  ).toContainText("usage coverage: incomplete");
  await page.screenshot({
    path: path.join(shots, "fixture-provider.png"),
    fullPage: true,
  });
  await expect(
    page.getByRole("region", { name: "Provider operation breakdown" }),
  ).toContainText(
    `Logical operation mean/p95 ms: ${durationRevision.overall.evidence.details.logical_mean_ms} / ${durationRevision.overall.evidence.details.logical_p95_ms}`,
  );
  await page
    .getByRole("link", { name: /^Observed trace / })
    .first()
    .click();
  await expect(page).toHaveURL(/\/traces\/[a-f0-9]{32}$/);
});
test("absent live data stays unknown and invalid proxy filters are rejected", async ({
  page,
  request,
}) => {
  await page.goto(`/slos/${e.live_key}?evaluation=${e.live}`);
  await expect(
    page.getByRole("heading", { name: "Evaluation UNKNOWN", exact: true }),
  ).toBeVisible();
  await expect(page.locator(".slo-window").first()).toContainText(
    "NO_OBSERVATIONS",
  );
  const response = await request.get("/api/reliability/slos?limit=10000");
  expect(response.status()).toBe(400);
});
