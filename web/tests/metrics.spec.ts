import { test, expect } from "@playwright/test";
import { readFileSync, mkdirSync, writeFileSync } from "node:fs";
import path from "node:path";
import { execFileSync } from "node:child_process";
const root = path.resolve(import.meta.dirname, "../..");
const evidence = JSON.parse(
  readFileSync(path.join(root, "artifacts/session11/evidence.json"), "utf8"),
);
const overflow = JSON.parse(
  readFileSync(
    path.join(root, "artifacts/session11/sdk-overflow.json"),
    "utf8",
  ),
);
const fixtures = JSON.parse(
  readFileSync(path.join(root, "artifacts/session11/fixtures.json"), "utf8"),
);
const shots = path.join(root, "artifacts/session11/screenshots");
test.beforeAll(() => mkdirSync(shots, { recursive: true }));
test("real request counter values, rates and replay diagnostics", async ({
  page,
}) => {
  await page.goto(
    `/metrics?${evidence.query}&name=traceforge.requests.completed&service=payment`,
  );
  await expect(
    page.getByRole("heading", { name: "Metrics explorer" }),
  ).toBeVisible();
  const panel = page.locator(".metric-series");
  await expect(panel).toHaveCount(1);
  await expect(panel).toContainText("CUMULATIVE");
  await expect(panel).toContainText("Observed increment: 100");
  await expect(panel).toContainText("Unit: {request}");
  await expect(panel.getByRole("img")).toBeVisible();
  await panel
    .getByText("Coverage, resets, replay and overflow diagnostics")
    .click();
  await expect(panel).toContainText('"replays":');
  await page.screenshot({
    path: path.join(shots, "counter-rates-replay.png"),
    fullPage: true,
  });
});
test("histogram range, real gauge and signed Sum keep their semantics", async ({
  page,
}) => {
  await page.goto(
    `/metrics?${evidence.query}&name=traceforge.request.duration&service=payment`,
  );
  const series = page.locator(".metric-series");
  await expect(series).toContainText("Count: 100");
  await expect(series).toContainText("not exact");
  await expect(series).toContainText("Unit: s");
  await expect(series.getByRole("table")).toBeVisible();
  await page.screenshot({
    path: path.join(shots, "histogram-ranges.png"),
    fullPage: true,
  });
  await page.goto(
    `/metrics?${evidence.query}&name=traceforge.process.goroutines&service=payment`,
  );
  await expect(page.locator(".metric-series")).toContainText("Gauge");
  await expect(page.locator(".metric-series")).toContainText(
    "Latest observed value:",
  );
  await page.goto(
    `/metrics?${evidence.query}&name=traceforge.requests.active&service=payment`,
  );
  await expect(page.locator(".metric-series")).toContainText(
    "Non-monotonic Sum",
  );
  await expect(page.locator(".metric-series")).toContainText(
    "signed change: 0",
  );
});
test("restart keeps independent writer identities and combined observed increments", async ({
  page,
}) => {
  await page.goto(`/metrics?${evidence.restart_query}`);
  await expect(page.locator(".metric-series")).toHaveCount(2);
  await expect(
    page.getByRole("region", { name: "Combined observed Sum" }),
  ).toContainText("105");
  await expect(page.locator(".metrics-page")).toContainText(
    evidence.old_instance,
  );
  await expect(page.locator(".metrics-page")).toContainText(
    evidence.new_instance,
  );
});
test("SDK overflow, budget and empty scopes remain honest on mobile", async ({
  page,
}) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto(`/metrics?${overflow.query}`);
  await expect(page.locator(".metric-series")).toHaveCount(4);
  await expect(
    page.getByText("SDK overflow: dimension coverage unreliable", {
      exact: true,
    }),
  ).toBeVisible();
  await page.screenshot({
    path: path.join(shots, "sdk-overflow-mobile.png"),
    fullPage: true,
  });
  await page.goto(`/metrics?${fixtures.query}`);
  await expect(page.locator(".metrics-page [role=alert]")).toContainText("413");
  await page.goto(`/metrics?${evidence.query}&service=nonexistent`);
  await expect(page.getByText(/No observed metric series/)).toBeVisible();
});
test("real metric backend outage rejects export and recovers", async ({
  page,
}) => {
  test.skip(
    process.env.TRACEFORGE_BROWSER_OUTAGE !== "1",
    "Explicit local ClickHouse outage",
  );
  test.setTimeout(120000);
  // Compile while dependencies are healthy. The outage tests execution, not
  // package downloads, compiler startup or a cold build cache.
  const binary = path.join(root, "bin", process.platform === "win32" ? "metric-outage-test.exe" : "metric-outage-test");
  const logDir = path.join(root, "artifacts/session15");
  mkdirSync(path.dirname(binary), { recursive: true });
  mkdirSync(logDir, { recursive: true });
  execFileSync("go", ["test", "-c", "-tags", "integration", "-o", binary, "./tests/integration"],
    { cwd: root, stdio: "pipe", timeout: 90000 });
  try {
    execFileSync("docker", ["compose", "stop", "clickhouse"], {
      cwd: root,
      stdio: "pipe",
      timeout: 20000,
    });
    await page.goto(
      `/metrics?${evidence.query}&name=traceforge.requests.completed&service=payment`,
    );
    await expect(page.locator(".metrics-page [role=alert]")).toContainText(
      "503",
    );
    let output: string;
    try {
      output = execFileSync(binary, ["-test.run=^TestMetricDatabaseUnavailable$", "-test.v", "-test.timeout=20s"], {
        cwd: root,
        env: { ...process.env, TRACEFORGE_EXPECT_DB_DOWN: "1" },
        stdio: "pipe",
        timeout: 25000,
      }).toString();
      writeFileSync(path.join(logDir, "metric-outage-child.txt"), output);
    } catch (error) {
      const failure = error as Error & { stdout?: Buffer; stderr?: Buffer; status?: number; signal?: string };
      writeFileSync(path.join(logDir, "metric-outage-child.txt"),
        `${failure.message}\nstatus=${failure.status} signal=${failure.signal}\n${failure.stdout ?? ""}\n${failure.stderr ?? ""}`);
      throw error;
    }
    expect(output).toContain("PASS");
    console.log(output.trim());
  } finally {
    execFileSync("docker", ["compose", "start", "clickhouse"], {
      cwd: root,
      stdio: "pipe",
      timeout: 20000,
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
  await page.goto(
    `/metrics?${evidence.query}&name=traceforge.requests.completed&service=payment`,
  );
  await expect(page.locator(".metric-series")).toContainText(
    "Observed increment: 100",
  );
});
