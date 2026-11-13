import { test, expect } from "@playwright/test";
import { readFileSync, mkdirSync } from "node:fs";
import path from "node:path";
const root = path.resolve(import.meta.dirname, "../..");
const e = JSON.parse(
  readFileSync(path.join(root, "artifacts/session09/evidence.json"), "utf8"),
);
const regression = JSON.parse(
  readFileSync(
    path.join(root, "artifacts/session09/context_regression.json"),
    "utf8",
  ),
);
const shots = path.join(root, "artifacts/session09/screenshots");
test.beforeAll(() => mkdirSync(shots, { recursive: true }));
test("real comparison shows regressions and both actual execution traces", async ({
  page,
}) => {
  const r = e.context_regression;
  await page.goto(`/comparisons/${r.comparison_id}?decision=${r.decision_id}`);
  await expect(
    page.getByRole("heading", { name: "Gate decision: FAIL" }),
  ).toBeVisible();
  await expect(page.getByText(/offline fixture engineering/)).toBeVisible();
  await page
    .getByRole("combobox", { name: "Filter case transitions" })
    .selectOption("regressed");
  const first = regression.comparison.cases.find(
    (c: { transition: string }) => c.transition === "regressed",
  );
  await expect(
    page.getByRole("heading", { name: `${first.case_id} · regressed` }),
  ).toBeVisible();
  await page.screenshot({
    path: path.join(shots, "paired-regression-desktop.png"),
    fullPage: true,
  });
  await page
    .getByRole("link", {
      name: `Baseline trace ${first.baseline_trace_id}`,
      exact: true,
    })
    .click();
  await expect(page.locator(".waterfall-row")).toHaveCount(6);
  await page.goto(`/comparisons/${r.comparison_id}?decision=${r.decision_id}`);
  await page
    .getByRole("combobox", { name: "Filter case transitions" })
    .selectOption("regressed");
  await page
    .getByRole("link", {
      name: `Candidate trace ${first.candidate_trace_id}`,
      exact: true,
    })
    .click();
  await expect(page.locator(".waterfall-row")).toHaveCount(6);
});
test("persisted PASS and live-profile INCONCLUSIVE keep their policy scope", async ({
  page,
}) => {
  for (const name of ["identical", "production_profile", "missing_cost"]) {
    const r = e[name];
    await page.goto(
      `/comparisons/${r.comparison_id}?decision=${r.decision_id}`,
    );
    await expect(
      page.getByRole("heading", { name: `Gate decision: ${r.outcome}` }),
    ).toBeVisible();
  }
  await expect(page.getByText(/required evidence unavailable/)).toBeVisible();
});
test("comparison list and mobile gate remain usable", async ({ page }) => {
  await page.goto("/comparisons");
  await expect(
    page.getByRole("heading", { name: "Persisted comparisons" }),
  ).toBeVisible();
  await page.setViewportSize({ width: 390, height: 844 });
  const r = e.identical;
  await page.goto(`/comparisons/${r.comparison_id}?decision=${r.decision_id}`);
  await expect(
    page.getByRole("heading", { name: "Gate decision: PASS" }),
  ).toBeVisible();
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= window.innerWidth,
    ),
  ).toBe(true);
  await page.screenshot({
    path: path.join(shots, "gate-mobile.png"),
    fullPage: true,
  });
});
