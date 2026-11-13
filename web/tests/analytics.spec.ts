import { test, expect } from "@playwright/test";
import { readFileSync, mkdirSync } from "node:fs";
import path from "node:path";
import { execFileSync } from "node:child_process";
const root = path.resolve(import.meta.dirname, "../..");
const e = JSON.parse(
  readFileSync(path.join(root, "artifacts/session10/evidence.json"), "utf8"),
);
const shots = path.join(root, "artifacts/session10/screenshots");
test.beforeAll(() => mkdirSync(shots, { recursive: true }));
test("actual service metrics, bucket charts and failed trace/log links", async ({
  page,
}) => {
  await page.goto(`/services?${e.query}`);
  await expect(
    page.getByRole("heading", { name: "Services", exact: true }),
  ).toBeVisible();
  await page.getByRole("combobox",{name:"Sort services"}).selectOption("p95");
  await expect(page.getByRole("columnheader",{name:"Duration samples"})).toBeVisible();
  await page.getByRole("link", { name: "payment", exact: true }).click();
  await expect(
    page.getByRole("heading", { name: "payment", exact: true }),
  ).toBeVisible();
  await expect(page.getByText("5% (5 ERROR)")).toBeVisible();
  await expect(
    page.getByRole("img", { name: /Observed latency p95/ }),
  ).toBeVisible();
  await page.screenshot({
    path: path.join(shots, "payment-detail.png"),
    fullPage: true,
  });
  const failed = new Set(
    e.requests
      .filter((r: { kind: string }) => r.kind === "failed")
      .map((r: { trace_id: string }) => r.trace_id),
  );
  const traceLink = page
    .getByRole("link", { name: /^Trace [a-f0-9]{32}$/ })
    .first();
  const text = await traceLink.innerText();
  expect(failed.has(text.slice(6))).toBeTruthy();
  await traceLink.click();
  await expect(page.locator(".waterfall-row")).toHaveCount(5);
  await page.goBack();
  await page.getByRole("link", { name: "Correlated logs" }).first().click();
  await expect(
    page.getByRole("region", { name: "Correlated logs" }),
  ).toBeVisible();
  await expect(
    page
      .getByRole("region", { name: "Correlated logs" })
      .locator(".log-record")
      .first(),
  ).toBeVisible();
  await page.goBack();
  const slow = new Set(
    e.requests
      .filter((r: { kind: string }) => r.kind === "delayed")
      .map((r: { trace_id: string }) => r.trace_id),
  );
  // Representative examples include a real slow non-error, not just failed requests.
  await expect(page.getByRole("link",{name:/^Trace [a-f0-9]{32}$/}).first()).toBeVisible();
  const links = await page
    .getByRole("link", { name: /^Trace [a-f0-9]{32}$/ })
    .all();
  let selectedSlow = false;
  for (const link of links) {
    if (slow.has((await link.innerText()).slice(6))) {
      await link.click();
      selectedSlow = true;
      break;
    }
  }
  expect(selectedSlow).toBeTruthy();
  await expect(page.locator(".waterfall-row")).toHaveCount(5);
});
test("directed graph nodes and actual paired-call evidence are navigable", async ({
  page,
}) => {
  await page.goto(`/service-graph?${e.query}`);
  await expect(
    page.getByRole("heading", { name: "Observed service graph" }),
  ).toBeVisible();
  const edge = page.getByRole("link", {
    name: "gateway to payment, 100 paired calls",
  });
  await expect(edge).toBeVisible();
  await edge.focus();
  await expect(edge).toBeFocused();
  await edge.press("Enter");
  await expect(
    page.getByRole("heading", { name: "gateway → payment" }),
  ).toBeInViewport();
  await page.screenshot({
    path: path.join(shots, "directed-graph.png"),
    fullPage: true,
  });
  await page
    .getByRole("list", { name: "Graph node links" })
    .getByRole("link", { name: /payment/ })
    .click();
  await expect(
    page.getByRole("heading", { name: "payment", exact: true }),
  ).toBeVisible();
});
test("honest empty and invalid-window states use real same-origin API", async ({
  page,
  request,
}) => {
  await page.goto(`/services?${e.query}&environment=nonexistent-session10`);
  await expect(
    page.getByText(/No observed services in this scope/),
  ).toBeVisible();
  await page.goto("/services?bucket_seconds=1");
  await expect(page.locator(".analytics-page [role=alert]")).toContainText(
    "Bucket must",
  );
  expect(
    (await request.get("/api/services?url=http://example.com")).status(),
  ).toBe(400);
});

test("small screen analytics preserves evidence and bounded-query errors", async ({
  page,
  request,
}) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto(`/services?${e.query}`);
  await page.getByRole("link", { name: "payment", exact: true }).click();
  await expect(page.getByText("5% (5 ERROR)")).toBeVisible();
  await page.screenshot({
    path: path.join(shots, "payment-mobile.png"),
    fullPage: true,
  });
  const budget = "from=2026-01-10T12%3A00%3A00Z&to=2026-01-10T12%3A01%3A00Z";
  expect((await request.get(`/api/services?${budget}`)).status()).toBe(413);
  await page.goto(`/services?${budget}`);
  await expect(page.locator(".analytics-page [role=alert]")).toContainText(
    "No partial result",
  );
});
test("real analytics storage outage returns an honest error and recovers", async ({
  page,
}) => {
  test.skip(
    process.env.TRACEFORGE_BROWSER_OUTAGE !== "1",
    "Explicit local ClickHouse outage opt-in",
  );
  test.setTimeout(120000);
  try {
    execFileSync("docker", ["compose", "stop", "clickhouse"], {
      cwd: root,
      stdio: "pipe",
    });
    await page.goto(`/services?${e.query}`);
    await expect(page.locator(".analytics-page [role=alert]")).toContainText(
      "503",
    );
    await expect(page.locator(".analytics-page table")).toHaveCount(0);
  } finally {
    execFileSync("docker", ["compose", "start", "clickhouse"], {
      cwd: root,
      stdio: "pipe",
    });
    await expect
      .poll(
        async () => {
          try {
            return (await fetch("http://127.0.0.1:8082/readyz")).status;
          } catch {
            return 0;
          }
        },
        { timeout: 60000 },
      )
      .toBe(200);
  }
  await page.goto(`/services?${e.query}`);
  await expect(
    page.getByRole("link", { name: "payment", exact: true }),
  ).toBeVisible();
});
