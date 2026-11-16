import { test, expect } from "@playwright/test";
import { readFileSync, mkdirSync } from "node:fs";
import path from "node:path";
const root = path.resolve(import.meta.dirname, "../..");
const evidence = JSON.parse(
  readFileSync(path.join(root, "artifacts/session13/sampling.json"), "utf8"),
);
test("an actually sampled-out root remains a missing trace with no success inference", async ({
  page,
}) => {
  const selected = new Set(
    evidence.head.stored.map((x: { trace_id: string }) => x.trace_id),
  );
  const dropped = evidence.head.offered.find(
    (x: { trace_id: string }) => !selected.has(x.trace_id),
  );
  await page.goto(`/traces/${dropped.trace_id}`);
  await expect(
    page.getByRole("heading", { name: "Trace not found" }),
  ).toBeVisible();
  await expect(page.locator(".error-panel")).toContainText(
    "sampled out, expired",
  );
  await expect(page.locator(".error-panel")).toContainText("zero model usage");
  mkdirSync(path.join(root, "artifacts/session13/screenshots"), {
    recursive: true,
  });
  await page.screenshot({
    path: path.join(
      root,
      "artifacts/session13/screenshots/missing-sampled-trace.png",
    ),
    fullPage: true,
  });
});
test("retained tail error opens the actual full gateway/payment trace", async ({
  page,
}) => {
  const failure = evidence.tail.offered.find(
    (x: { kind: string }) => x.kind === "failed",
  );
  await page.goto(`/traces/${failure.trace_id}`);
  await expect(
    page.getByRole("heading", { name: "Trace not found" }),
  ).toHaveCount(0);
  await expect(
    page.getByText("payment.process", { exact: true }).first(),
  ).toBeVisible();
});

test("retained evaluation links honestly to actually expired isolated raw evidence", async ({
  page,
  request,
}) => {
  const expiry = JSON.parse(
    readFileSync(path.join(root, "artifacts/session13/expiry.json"), "utf8"),
  );
  const retained = await request.get(
    `${expiry.backend}/api/v1/slo-evaluations/${expiry.evaluation.id}`,
  );
  expect(retained.status()).toBe(200);
  // Forward this one trace read to the real isolated TTL backend. No synthetic
  // response body/status and no deletion of the original project evidence.
  await page.route(`**/api/traces/${expiry.old_trace}`, async (route) => {
    const response = await route.fetch({
      url: `${expiry.backend}/api/v1/traces/${expiry.old_trace}`,
    });
    expect(response.status()).toBe(404);
    await route.fulfill({ response });
  });
  await page.goto(
    `/slos/${expiry.evaluation.slo_key}?evaluation=${expiry.evaluation.id}`,
  );
  await expect(
    page.getByRole("heading", { name: "Evaluation MEASURED", exact: true }),
  ).toBeVisible();
  await page
    .getByRole("link", {
      name: `Observed trace ${expiry.old_trace}`,
      exact: true,
    })
    .click();
  await expect(
    page.getByRole("heading", { name: "Trace not found" }),
  ).toBeVisible();
  await expect(page.locator(".error-panel")).toContainText(
    "history can outlive raw telemetry",
  );
  await page.screenshot({
    path: path.join(
      root,
      "artifacts/session13/screenshots/expired-evidence.png",
    ),
    fullPage: true,
  });
});
