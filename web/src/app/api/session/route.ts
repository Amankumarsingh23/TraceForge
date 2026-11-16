import { sealSession, SESSION_COOKIE } from "../../../lib/session";
export async function POST(request: Request) {
  if (request.headers.get("origin") !== new URL(request.url).origin)
    return Response.json({ error: "Invalid origin" }, { status: 403 });
  if (process.env.TRACEFORGE_REQUIRE_AUTH !== "1")
    return Response.json(
      { error: "Secure session mode is disabled" },
      { status: 409 },
    );
  let raw: string;
  const reader = request.body?.getReader();
  let timer: ReturnType<typeof setTimeout> | undefined;
  try {
    const timeout = new Promise<never>((_, reject) => {
      timer = setTimeout(() => reject(new Error("body timeout")), 2000);
    });
    const chunks: Uint8Array[] = [];
    let size = 0;
    if (reader)
      while (true) {
        const { done, value } = await Promise.race([reader.read(), timeout]);
        if (done) break;
        size += value.length;
        if (size > 1024) {
          await reader.cancel();
          return Response.json({ error: "Body too large" }, { status: 413 });
        }
        chunks.push(value);
      }
    const bytes = new Uint8Array(size);
    let offset = 0;
    for (const chunk of chunks) {
      bytes.set(chunk, offset);
      offset += chunk.length;
    }
    raw = new TextDecoder().decode(bytes);
  } catch {
    await reader?.cancel();
    return Response.json({ error: "Body unavailable" }, { status: 408 });
  } finally {
    if (timer) clearTimeout(timer);
  }
  try {
    const { credential } = JSON.parse(raw);
    if (typeof credential !== "string" || !/^tf_[a-f0-9]{64}$/.test(credential))
      return Response.json({ error: "Invalid credential" }, { status: 401 });
    const origin = process.env.TRACEFORGE_API_URL;
    if (!origin || new URL(origin).protocol !== "https:") throw new Error();
    const upstream = await fetch(new URL("/api/v1/security/session", origin), {
      headers: { Authorization: `Bearer ${credential}` },
      cache: "no-store",
      redirect: "error",
      signal: AbortSignal.timeout(2500),
    });
    if (!upstream.ok)
      return Response.json(
        { error: "Authentication unavailable or denied" },
        { status: upstream.status === 503 ? 503 : 401 },
      );
    const identity = await upstream.json();
    if (identity.scopes.length !== 1 || identity.scopes[0] !== "read")
      return Response.json(
        { error: "Use a dedicated read credential" },
        { status: 403 },
      );
    const token = await sealSession(credential);
    return Response.json(
      { tenant: identity.tenant },
      {
        headers: {
          "Cache-Control": "no-store",
          "Set-Cookie": `${SESSION_COOKIE}=${token}; Path=/; HttpOnly; SameSite=Strict; Max-Age=900${process.env.TRACEFORGE_COOKIE_SECURE === "1" ? "; Secure" : ""}`,
        },
      },
    );
  } catch {
    return Response.json(
      { error: "Authentication unavailable" },
      { status: 503 },
    );
  }
}
export async function DELETE(request: Request) {
  if (request.headers.get("origin") !== new URL(request.url).origin)
    return Response.json({ error: "Invalid origin" }, { status: 403 });
  return Response.json(
    { ok: true },
    {
      headers: {
        "Set-Cookie": `${SESSION_COOKIE}=; Path=/; HttpOnly; SameSite=Strict; Max-Age=0`,
        "Cache-Control": "no-store",
      },
    },
  );
}
