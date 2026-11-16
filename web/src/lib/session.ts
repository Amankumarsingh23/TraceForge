export const SESSION_COOKIE = "traceforge_session";
function encode(bytes: Uint8Array) {
  return btoa(String.fromCharCode(...bytes))
    .replaceAll("+", "-")
    .replaceAll("/", "_")
    .replaceAll("=", "");
}
function decode(value: string) {
  return Uint8Array.from(
    atob(value.replaceAll("-", "+").replaceAll("_", "/")),
    (c) => c.charCodeAt(0),
  );
}
async function key() {
  const value = process.env.TRACEFORGE_SESSION_SECRET ?? "";
  if (!/^[a-f0-9]{64}$/.test(value))
    throw new Error("Session configuration unavailable");
  return crypto.subtle.importKey(
    "raw",
    Uint8Array.from(value.match(/../g)!, (v) => parseInt(v, 16)),
    "AES-GCM",
    false,
    ["encrypt", "decrypt"],
  );
}
export async function sealSession(credential: string) {
  const iv = crypto.getRandomValues(new Uint8Array(12));
  const payload = new TextEncoder().encode(
    JSON.stringify({ credential, expires: Date.now() + 15 * 60 * 1000 }),
  );
  const cipher = await crypto.subtle.encrypt(
    { name: "AES-GCM", iv },
    await key(),
    payload,
  );
  return encode(iv) + "." + encode(new Uint8Array(cipher));
}
// Server handlers only: credentials never enter client props or storage.
export async function sessionCredential(
  request: Request,
): Promise<string | null> {
  try {
    const matching = (request.headers.get("cookie") ?? "")
      .split(";")
      .map((v) => v.trim())
      .filter((v) => v.startsWith(SESSION_COOKIE + "="));
    if (matching.length !== 1) return null;
    const token = matching[0].slice(SESSION_COOKIE.length + 1);
    if (token.length > 1024) return null;
    const parts = token.split(".");
    if (parts.length !== 2) return null;
    const raw = await crypto.subtle.decrypt(
      { name: "AES-GCM", iv: decode(parts[0]) },
      await key(),
      decode(parts[1]),
    );
    const value = JSON.parse(new TextDecoder().decode(raw));
    return value.expires > Date.now() &&
      /^tf_[a-f0-9]{64}$/.test(value.credential)
      ? value.credential
      : null;
  } catch {
    return null;
  }
}
