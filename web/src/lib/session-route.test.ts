import { afterEach, expect, it, vi } from "vitest";
import { webcrypto } from "node:crypto";
import { POST, DELETE } from "../app/api/session/route";
import { sessionCredential } from "./session";

afterEach(() => { vi.unstubAllGlobals(); vi.unstubAllEnvs(); });
const key = "tf_" + "c".repeat(64);
const request = (origin = "http://localhost", body = JSON.stringify({ credential: key })) =>
  new Request("http://localhost/api/session", { method: "POST", headers: { origin }, body });

it("requires same origin, secure mode and a bounded valid credential", async () => {
  expect((await POST(request("http://foreign"))).status).toBe(403);
  vi.stubEnv("TRACEFORGE_REQUIRE_AUTH", "1");
  expect((await POST(request(undefined, "x".repeat(1025)))).status).toBe(413);
  expect((await POST(request(undefined, JSON.stringify({ credential: "invalid" })))).status).toBe(401);
  expect((await DELETE(request("http://foreign"))).status).toBe(403);
});

it("seals only an online dedicated read identity and closes on auth failure", async () => {
  vi.stubGlobal("crypto", webcrypto);
  vi.stubEnv("TRACEFORGE_REQUIRE_AUTH", "1");
  vi.stubEnv("TRACEFORGE_SESSION_SECRET", "d".repeat(64));
  vi.stubEnv("TRACEFORGE_API_URL", "https://localhost:8444");
  const fetcher = vi.fn().mockResolvedValue(Response.json({ tenant: "alpha", scopes: ["read"] }));
  vi.stubGlobal("fetch", fetcher);
  const response = await POST(request());
  expect(response.status).toBe(200);
  expect(await response.json()).toEqual({ tenant: "alpha" });
  const cookie = response.headers.get("set-cookie")!;
  expect(cookie).toContain("HttpOnly");
  expect(cookie).toContain("SameSite=Strict");
  expect(cookie).not.toContain(key);
  expect(await sessionCredential(new Request("http://localhost", { headers: { cookie } }))).toBe(key);
  fetcher.mockResolvedValueOnce(Response.json({ tenant: "alpha", scopes: ["manage"] }));
  expect((await POST(request())).status).toBe(403);
  fetcher.mockResolvedValueOnce(Response.json({}, { status: 503 }));
  expect((await POST(request())).status).toBe(503);
  expect((await DELETE(request())).headers.get("set-cookie")).toContain("Max-Age=0");
});
