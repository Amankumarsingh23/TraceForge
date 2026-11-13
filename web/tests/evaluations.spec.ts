import { test, expect } from "@playwright/test";
import { readFileSync, mkdirSync } from "node:fs";
import path from "node:path";
import { execFileSync } from "node:child_process";
const root = path.resolve(import.meta.dirname, "../..");
const baseline = JSON.parse(
  readFileSync(path.join(root, "artifacts/session08/baseline.json"), "utf8"),
);
const wrong = JSON.parse(
  readFileSync(
    path.join(root, "artifacts/session08/wrong_citation.json"),
    "utf8",
  ),
);
const failure = JSON.parse(
  readFileSync(
    path.join(root, "artifacts/session08/execution_failure.json"),
    "utf8",
  ),
);
const pending = JSON.parse(
  readFileSync(
    path.join(root, "artifacts/session08/trace_pending.json"),
    "utf8",
  ),
);
const shots = path.join(root, "artifacts/session08/screenshots");
test.beforeAll(() => mkdirSync(shots, { recursive: true }));
test("persisted evaluation run exposes denominators, paginates and opens actual case trace", async ({
  page,
}) => {
  await page.goto(`/evaluations/${baseline.run_id}`);
  await expect(page.getByText(/Attempted 35/)).toContainText("Succeeded 35");
  await expect(
    page
      .getByText("30/30 eligible scored · 5 N/A · 0 execution failures")
      .first(),
  ).toBeVisible();
  await page.getByRole("button", { name: "Next cases" }).click();
  await expect(
    page.getByRole("link", { name: "support-011", exact: true }),
  ).toBeVisible();
  await page.getByRole("button", { name: "First cases" }).click();
  await page.getByRole("link", { name: "support-001", exact: true }).click();
  await expect(page.getByText("Actually supplied IDs:")).toHaveCount(0);
  await expect(page.getByText(/Actually supplied IDs: D01/)).toBeVisible();
  await expect(
    page.getByRole("heading", { name: "Claim and citation judgments" }),
  ).toBeVisible();
  await page.screenshot({
    path: path.join(shots, "case-evidence-desktop.png"),
    fullPage: true,
  });
  await page.getByRole("link", { name: /Inspect actual trace/ }).click();
  await expect(page.locator(".waterfall-row")).toHaveCount(6);
  await expect(
    page.getByRole("region", { name: "AgentOps execution summary" }),
  ).toContainText("Fixture");
  await page.getByRole("button", { name: /^Inspect retrieval/ }).click();
  await expect(page.locator(".inspector")).toContainText(
    "traceforge.evaluation.run_id",
  );
  await expect(page.locator(".inspector")).toContainText(baseline.run_id);
});
test("wrong citation is distinct from groundedness and pending trace is honest", async ({
  page,
}) => {
  await page.goto(`/evaluations/${wrong.run_id}/cases/support-027`);
  await expect(page.getByText(/Citation D12: valid true/)).toContainText(
    "supports claim false",
  );
  await expect(page.getByText(/Grounded: true/).first()).toBeVisible();
  await page.goto(`/evaluations/${pending.run_id}/cases/support-001`);
  await expect(page.getByText(/Trace visibility: pending/)).toBeVisible();
  await expect(
    page.getByRole("link", { name: /Inspect actual trace/ }),
  ).toBeVisible();
});
test("execution failures stay visible and mobile layout does not overflow", async ({
  page,
}) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto(`/evaluations/${failure.run_id}`);
  await expect(page.getByText(/Attempted 1/)).toContainText("Failed 1");
  await expect(page.getByText("Pending / unknown").first()).toBeVisible();
  await page.getByRole("link", { name: "support-001", exact: true }).click();
  await expect(
    page.getByText("Execution failure: provider_timeout", { exact: true }),
  ).toContainText("provider_timeout");
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= window.innerWidth,
    ),
  ).toBe(true);
  await page.screenshot({
    path: path.join(shots, "failure-mobile.png"),
    fullPage: true,
  });
});

test("PostgreSQL outage leaves trace queries available and evaluations recover", async ({
  page,
}) => {
  test.skip(
    process.env.TRACEFORGE_BROWSER_OUTAGE !== "1",
    "explicit controlled database outage only",
  );
  try {
    execFileSync("docker", ["compose", "stop", "postgres"], {
      cwd: root,
      stdio: "pipe",
      timeout: 30000,
    });
    await page.goto("/evaluations");
    await expect(
      page.getByRole("heading", {
        name: /Evaluation backend unavailable|Evaluation request timed out/,
      }),
    ).toBeVisible();
    expect(
      (
        await page.request.get(
          `http://127.0.0.1:8082/api/v1/traces/${baseline.results[0].trace_id}`,
        )
      ).status(),
    ).toBe(200);
  } finally {
    execFileSync("docker", ["compose", "start", "postgres"], {
      cwd: root,
      stdio: "pipe",
      timeout: 30000,
    });
    await expect
      .poll(
        async () => {
          try {
            return (
              await page.request.get("http://127.0.0.1:8082/api/v1/evaluations")
            ).status();
          } catch {
            return 0;
          }
        },
        { timeout: 60000 },
      )
      .toBe(200);
  }
  await page.getByRole("button", { name: "Retry request" }).click();
  await expect(
    page.getByRole("heading", { name: "Evaluation runs" }),
  ).toBeVisible();
});
