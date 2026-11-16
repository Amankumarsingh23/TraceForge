import { afterEach, expect, it, vi } from "vitest";
import { webcrypto } from "node:crypto";
import { sealSession, sessionCredential, SESSION_COOKIE } from "./session";
afterEach(() => {
  vi.unstubAllEnvs();
  vi.unstubAllGlobals();
});
it("seals a credential without exposing it and rejects tampering/expiry", async () => {
  vi.stubGlobal("crypto", webcrypto);
  vi.stubEnv("TRACEFORGE_SESSION_SECRET", "a".repeat(64));
  const credential = "tf_" + "b".repeat(64);
  const token = await sealSession(credential);
  expect(token).not.toContain(credential);
  const request = new Request("http://localhost", {
    headers: { cookie: `${SESSION_COOKIE}=${token}` },
  });
  expect(await sessionCredential(request)).toBe(credential);
  expect(
    await sessionCredential(
      new Request("http://localhost", {
        headers: { cookie: `${SESSION_COOKIE}=bad.${token}` },
      }),
    ),
  ).toBeNull();
  const now = Date.now();
  vi.spyOn(Date, "now").mockReturnValue(now + 16 * 60 * 1000);
  expect(await sessionCredential(request)).toBeNull();
  vi.restoreAllMocks();
});
