import { test, expect, type Page } from "@playwright/test";
import { mkdirSync, readFileSync } from "node:fs";
import { execFileSync } from "node:child_process";
import path from "node:path";
const root = path.resolve(import.meta.dirname, "../..");
const demo = JSON.parse(
  readFileSync(path.join(root, "artifacts/session06/demo.json"), "utf8"),
);
const fixture = JSON.parse(
  readFileSync(path.join(root, "artifacts/session06/fixtures.json"), "utf8"),
);
const screenshots = path.join(root, "artifacts/session06/screenshots");
test.beforeAll(() => mkdirSync(screenshots, { recursive: true }));
async function fixtureWindow(page: Page) {
  await page.getByRole("textbox", { name: "Logs from UTC" }).fill(fixture.from);
  await page.getByRole("textbox", { name: "Logs to UTC" }).fill(fixture.to);
}
test("failed payment span exposes its exact logs and links back to the real trace", async ({
  page,
}) => {
  await page.goto(`/traces/${demo.failed.trace_id}`);
  await expect(page.locator(".waterfall-row")).toHaveCount(5);
  await page.getByRole("button", { name: /^Inspect payment.process/ }).click();
  const panel = page.getByRole("region", { name: "Correlated logs" });
  await expect(panel.locator(".log-record")).toHaveCount(2);
  await expect(panel).toContainText(demo.failed.payment_span_id);
  const error = panel
    .locator(".log-record")
    .filter({ hasText: "payment failure" });
  await error.locator("summary").first().click();
  await expect(error).toContainText("ERROR");
  await expect(error).toContainText("synthetic_provider_failure");
  await expect(error).toContainText(demo.failed.trace_id);
  await page.screenshot({
    path: path.join(screenshots, "failed-span-correlated-logs.png"),
    fullPage: true,
  });
  await error.getByRole("link", { name: demo.failed.trace_id }).click();
  await expect(page.locator(".waterfall-row")).toHaveCount(5);
});
test("switching all trace versus selected span retains the correct row counts", async ({
  page,
}) => {
  await page.goto(`/traces/${demo.delayed.trace_id}`);
  const panel = page.getByRole("region", { name: "Correlated logs" });
  await expect(panel.locator(".log-record")).toHaveCount(
    demo.delayed.trace_logs,
  );
  await page.getByRole("button", { name: /^Inspect payment.process/ }).click();
  await expect(panel.locator(".log-record")).toHaveCount(3);
  await expect(panel).toContainText("controlled payment delay");
  await panel.getByRole("button", { name: "All trace logs" }).click();
  await expect(panel.locator(".log-record")).toHaveCount(
    demo.delayed.trace_logs,
  );
});
test("real logs search supports severity, standalone records and structured details", async ({
  page,
}) => {
  await page.goto("/logs");
  await page
    .getByRole("textbox", { name: "Service", exact: true })
    .fill("payment");
  await page.getByLabel("Minimum severity").selectOption("17");
  await page.getByRole("button", { name: "Search logs" }).click();
  await expect(page.locator(".log-record").first()).toContainText("ERROR");
  await expect(page.locator(".log-record").first()).toContainText(
    "payment failure",
  );
  await page.screenshot({
    path: path.join(screenshots, "error-log-search.png"),
    fullPage: true,
  });
  await page.getByLabel("Minimum severity").selectOption("0");
  await page.getByLabel("Literal body text").fill("service startup");
  await page.getByRole("button", { name: "Search logs" }).click();
  const startup = page.locator(".log-record").first();
  await startup.locator("summary").first().click();
  await expect(startup).toContainText("Standalone");
  await expect(startup).toContainText("None — standalone log");
});
test("repeated/replayed physical log rows paginate without merging events", async ({
  page,
}) => {
  await page.goto("/logs");
  await fixtureWindow(page);
  await page
    .getByRole("textbox", { name: "Service", exact: true })
    .fill(fixture.service);
  await page.getByLabel("Page size").selectOption("10");
  await page.getByRole("button", { name: "Search logs" }).click();
  await expect(page.locator(".log-record")).toHaveCount(10);
  await page.getByRole("button", { name: "Next logs" }).click();
  await expect(page.locator(".log-record")).toHaveCount(4);
  await page
    .getByRole("textbox", { name: "Trace ID", exact: true })
    .fill(fixture.trace_id);
  await page
    .getByRole("textbox", { name: "Span ID", exact: true })
    .fill(fixture.span_id);
  await page.getByRole("button", { name: "Search logs" }).click();
  await expect(page.locator(".log-record")).toHaveCount(8);
  await expect(
    page.getByRole("button", { name: "Previous logs" }),
  ).toBeDisabled();
  await expect(
    page.locator(".log-record").filter({ hasText: "session06 repeated event" }),
  ).toHaveCount(6);
});
test("trace-only logs are visible and a missing associated trace remains honest", async ({
  page,
}) => {
  await page.goto(`/traces/${fixture.trace_id}`);
  const panel = page.getByRole("region", { name: "Correlated logs" });
  await expect(panel.locator(".log-record")).toHaveCount(10);
  const traceOnly = panel
    .locator(".log-record")
    .filter({ hasText: "session06 trace-only" })
    .first();
  await traceOnly.locator("summary").first().click();
  await expect(traceOnly).toContainText("Trace-only");
  await page.goto("/logs");
  await fixtureWindow(page);
  await page
    .getByRole("textbox", { name: "Trace ID", exact: true })
    .fill(fixture.other_trace_id);
  await page.getByRole("button", { name: "Search logs" }).click();
  const missing = page.locator(".log-record").first();
  await missing.locator("summary").first().click();
  await missing.getByRole("link", { name: fixture.other_trace_id }).click();
  await expect(page.locator("section[role=alert]")).toContainText(
    "Trace not found",
  );
  await expect(page.locator(".waterfall-row")).toHaveCount(0);
});
test("mobile log records remain expandable and readable", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto(`/traces/${demo.failed.trace_id}`);
  await page.getByRole("button", { name: /^Inspect payment.process/ }).click();
  const panel = page.getByRole("region", { name: "Correlated logs" });
  await expect(panel.locator(".log-record")).toHaveCount(2);
  await panel
    .locator(".log-record")
    .filter({ hasText: "payment failure" })
    .locator("summary")
    .first()
    .click();
  await expect(panel).toContainText("synthetic_provider_failure");
  await page.screenshot({
    path: path.join(screenshots, "mobile-correlated-logs.png"),
    fullPage: true,
  });
});
test("log search and correlation report real outage and recover", async ({
  page,
}) => {
  test.skip(
    process.env.TRACEFORGE_BROWSER_OUTAGE !== "1",
    "Opt-in controlled local DB outage",
  );
  test.setTimeout(120000);
  try {
    execFileSync("docker", ["compose", "stop", "clickhouse"], {
      cwd: root,
      stdio: "pipe",
      timeout: 30000,
    });
    await page.goto("/logs");
    await expect(page.locator("section[role=alert]")).toContainText(
      "Log backend unavailable",
    );
    await expect(
      page.getByText("No matching logs", { exact: true }),
    ).toHaveCount(0);
    await page.screenshot({
      path: path.join(screenshots, "logs-unavailable.png"),
      fullPage: true,
    });
    const response = await page.request.get(
      `/api/traces/${demo.failed.trace_id}/logs`,
    );
    expect(response.status()).toBe(503);
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
  await page
    .getByRole("textbox", { name: "Trace ID", exact: true })
    .fill(demo.failed.trace_id);
  await page.getByRole("button", { name: "Search logs" }).click();
  await expect(page.locator(".log-record")).toHaveCount(demo.failed.trace_logs);
});
