import { test, expect, type Page } from "@playwright/test";
import { readFileSync, mkdirSync } from "node:fs";
import { execFileSync } from "node:child_process";
import path from "node:path";
import { flatten, formatNS, durationNS } from "../src/lib/timeline";
import type { TraceDetail } from "../src/lib/contracts";
const root = path.resolve(import.meta.dirname, "../..");
const evidence = JSON.parse(
  readFileSync(
    path.join(root, "artifacts/session05/demo-evidence.json"),
    "utf8",
  ),
);
const screenshot = path.join(root, "artifacts/session05/screenshots");
// Bind searches to the persisted demo's absolute time, so a later rerun still
// works after it leaves the product's intentionally recent one-hour default.
const delayed = JSON.parse(
  readFileSync(path.join(root, "artifacts/session05/delayed.json"), "utf8"),
) as TraceDetail;
const demoTime = Date.parse(delayed.observed_time_envelope!.start_time);
async function selectDemoWindow(page: Page) {
  await page
    .getByRole("textbox", { name: "From UTC" })
    .fill(new Date(demoTime - 300000).toISOString());
  await page
    .getByRole("textbox", { name: "To UTC" })
    .fill(new Date(demoTime + 300000).toISOString());
}
test.beforeAll(() => mkdirSync(screenshot, { recursive: true }));
test("real payment search opens a full gateway/payment trace and actual inspector", async ({
  page,
}) => {
  await page.goto("/traces");
  await selectDemoWindow(page);
  await page
    .getByRole("textbox", { name: "Service", exact: true })
    .fill("payment");
  await page.getByRole("button", { name: "Search traces" }).click();
  const table = page.getByRole("table");
  await expect(
    table.getByText(evidence.scenarios.delayed.id, { exact: true }),
  ).toBeVisible();
  await page.screenshot({
    path: path.join(screenshot, "search-payment.png"),
    fullPage: true,
  });
  await table.getByText(evidence.scenarios.delayed.id, { exact: true }).click();
  await expect(page.locator(".waterfall-row")).toHaveCount(5);
  await expect(
    page.locator(".waterfall-row").filter({ hasText: "gateway" }),
  ).toHaveCount(3);
  await expect(
    page.locator(".waterfall-row").filter({ hasText: "payment" }),
  ).toHaveCount(2);
  await page.getByRole("button", { name: /^Inspect payment.process/ }).click();
  const response = await page.request.get(
    `/api/traces/${evidence.scenarios.delayed.id}`,
  );
  const trace = (await response.json()) as TraceDetail;
  const payment = flatten(trace).find(
    (row) => row.span.span_name === "payment.process",
  )!.span;
  const inspector = page.getByRole("complementary", { name: "Span inspector" });
  await expect(inspector).toContainText(payment.span_id);
  await expect(inspector).toContainText(payment.start_time);
  await expect(inspector).toContainText(formatNS(durationNS(payment)));
  await page.screenshot({
    path: path.join(screenshot, "delayed-waterfall-inspector.png"),
    fullPage: true,
  });
});
test("normal versus delayed payment reflects the actual recorded delay", async ({
  page,
}) => {
  const payments: bigint[] = [];
  for (const name of ["normal", "delayed"]) {
    const id = evidence.scenarios[name].id;
    const trace = (await (
      await page.request.get(`/api/traces/${id}`)
    ).json()) as TraceDetail;
    const payment = flatten(trace).find(
      (row) => row.span.span_name === "payment.process",
    )!.span;
    payments.push(durationNS(payment));
    await page.goto(`/traces/${id}`);
    await page
      .getByRole("button", { name: /^Inspect payment.process/ })
      .click();
    await expect(page.getByRole("complementary")).toContainText(
      formatNS(durationNS(payment)),
    );
  }
  expect(payments[1] - payments[0]).toBeGreaterThan(400000000n);
  console.log(
    `Observed payment.process normal=${payments[0]}ns delayed=${payments[1]}ns`,
  );
});
test("failed payment exposes actual error status and exception event", async ({
  page,
}) => {
  await page.goto(`/traces/${evidence.scenarios.failed.id}`);
  await expect(
    page.getByText("Contains error span", { exact: true }),
  ).toBeVisible();
  await page.getByRole("button", { name: /^Inspect payment.process/ }).click();
  const inspector = page.getByRole("complementary");
  await expect(inspector.getByText("ERROR", { exact: true })).toBeVisible();
  await expect(inspector).toContainText("exception");
  await expect(
    page.getByText("Does not establish request outcome"),
  ).toBeVisible();
  await page.screenshot({
    path: path.join(screenshot, "failed-span.png"),
    fullPage: true,
  });
});
test("historical and replay traces retain five bars while reporting physical duplicates", async ({
  page,
}) => {
  await page.goto(`/traces/${evidence.historical}`);
  await expect(page.locator(".waterfall-row")).toHaveCount(5);
  await expect(
    page.getByText("5 physical rows", { exact: true }),
  ).toBeVisible();
  await page.goto(`/traces/${evidence.replay}`);
  await expect(page.locator(".waterfall-row")).toHaveCount(5);
  await expect(
    page.getByText("10 physical rows", { exact: true }),
  ).toBeVisible();
  await expect(
    page.getByText("5 exact duplicate rows", { exact: true }),
  ).toBeVisible();
});
test("anomaly graph exposes all canonical spans, missing parent, cycle and conflict", async ({
  page,
}) => {
  await page.goto(`/traces/${evidence.anomaly}`);
  await expect(page.locator(".waterfall-row")).toHaveCount(6);
  await expect(
    page.getByText("1 conflicting duplicate groups", { exact: true }),
  ).toBeVisible();
  await expect(
    page.getByText("1 missing parents", { exact: true }),
  ).toBeVisible();
  await expect(page.getByText("1 cycles", { exact: true })).toBeVisible();
  await expect(
    page.locator(".waterfall-row").filter({ hasText: "Missing parent" }),
  ).toHaveCount(1);
  await expect(
    page.locator(".waterfall-row").filter({ hasText: "Disconnected" }),
  ).toHaveCount(1);
  await page.screenshot({
    path: path.join(screenshot, "anomaly-forest.png"),
    fullPage: true,
  });
});
test("1000-span supported trace stays selectable, collapsible and scrollable", async ({
  page,
}) => {
  await page.goto(`/traces/${evidence.large}`);
  await expect(page.locator(".waterfall-row")).toHaveCount(1000);
  await page.getByRole("button", { name: "Collapse all" }).click();
  await expect(page.locator(".waterfall-row")).toHaveCount(1);
  await page.getByRole("button", { name: "Expand all" }).click();
  await expect(page.locator(".waterfall-row")).toHaveCount(1000);
  await page
    .locator(".waterfall-row")
    .last()
    .getByRole("button", { name: /^Inspect/ })
    .click();
  await expect(
    page.getByRole("button", { name: "Clear selection" }),
  ).toBeVisible();
});
test("actual invalid, missing and oversized traces show explicit errors", async ({
  page,
}) => {
  for (const [id, title] of [
    ["invalid", "Invalid query or trace ID"],
    ["f".repeat(32), "Trace not found"],
    [evidence.oversized, "Trace exceeds the supported detail size"],
  ]) {
    await page.goto(`/traces/${id}`);
    await expect(page.locator("section[role=alert]")).toContainText(title);
    await expect(page.locator(".waterfall-row")).toHaveCount(0);
  }
  await page.screenshot({
    path: path.join(screenshot, "oversized-trace.png"),
    fullPage: true,
  });
});
test("empty search and invalid window are distinguished", async ({ page }) => {
  await page.goto("/traces");
  await page
    .getByRole("textbox", { name: "Service", exact: true })
    .fill("no-such-service-session05");
  await page.getByRole("button", { name: "Search traces" }).click();
  await expect(
    page.getByText("No matching traces", { exact: true }),
  ).toBeVisible();
  await page.getByRole("textbox", { name: "From UTC" }).fill("invalid");
  await page.getByRole("button", { name: "Search traces" }).click();
  await expect(page.locator("section[role=alert]")).toContainText(
    "Invalid query or trace ID",
  );
});
test("cursor pagination changes pages and changing filters resets it", async ({
  page,
}) => {
  await page.goto("/traces");
  await selectDemoWindow(page);
  await page.getByLabel("Page size").selectOption("10");
  await page.getByRole("button", { name: "Search traces" }).click();
  await expect(page.getByRole("button", { name: "Next page" })).toBeEnabled();
  await page.getByRole("button", { name: "Next page" }).click();
  await expect(page.locator(".pagination")).toContainText("Page 2");
  await page
    .getByRole("textbox", { name: "Service", exact: true })
    .fill("payment");
  await page.getByRole("button", { name: "Search traces" }).click();
  await expect(page.locator(".pagination")).toContainText("Page 1");
  await expect(page.getByRole("button", { name: "Previous" })).toBeDisabled();
});
test("mobile layout preserves the investigation controls", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto(`/traces/${evidence.scenarios.delayed.id}`);
  await expect(page.locator(".waterfall-row")).toHaveCount(5);
  await page.getByRole("button", { name: /^Inspect payment.process/ }).click();
  await expect(page.getByRole("complementary")).toContainText(
    "payment.process",
  );
  await page.screenshot({
    path: path.join(screenshot, "mobile-detail.png"),
    fullPage: true,
  });
});
test("keyboard can select and clear a span", async ({ page }) => {
  await page.goto(`/traces/${evidence.scenarios.normal.id}`);
  const bar = page.getByRole("button", { name: /^Inspect payment.process/ });
  await bar.focus();
  await page.keyboard.press("Enter");
  await expect(page.getByRole("complementary")).toContainText(
    "payment.process",
  );
  const clear = page.getByRole("button", { name: "Clear selection" });
  await clear.focus();
  await page.keyboard.press("Enter");
  await expect(page.getByRole("complementary")).toContainText("A closer look");
});
test("real ClickHouse outage shows an honest error and recovers", async ({
  page,
}) => {
  test.skip(
    process.env.TRACEFORGE_BROWSER_OUTAGE !== "1",
    "Opt-in controlled local database stop; always restored in finally",
  );
  test.setTimeout(120000);
  try {
    execFileSync("docker", ["compose", "stop", "clickhouse"], {
      cwd: root,
      stdio: "pipe",
      timeout: 30000,
    });
    await page.goto(`/traces/${evidence.historical}`);
    await expect(page.locator("section[role=alert]")).toContainText(
      "Trace backend unavailable",
    );
    await expect(page.locator(".waterfall-row")).toHaveCount(0);
    await page.screenshot({
      path: path.join(screenshot, "database-unavailable.png"),
      fullPage: true,
    });
    await page.goto("/traces");
    await expect(page.locator("section[role=alert]")).toContainText(
      "Trace backend unavailable",
    );
    await expect(
      page.getByText("No matching traces", { exact: true }),
    ).toHaveCount(0);
  } finally {
    execFileSync("docker", ["compose", "start", "clickhouse"], {
      cwd: root,
      stdio: "pipe",
      timeout: 30000,
    });
    await expect
      .poll(
        async () => {
          try {
            return (
              await page.request.get("http://127.0.0.1:8082/readyz")
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
  await expect(page.locator("section[role=alert]")).toHaveCount(0);
  // Recovery can honestly return an empty recent window when demos are old.
  await selectDemoWindow(page);
  await page.getByRole("button", { name: "Search traces" }).click();
  await expect(page.getByRole("table")).toBeVisible();
});
