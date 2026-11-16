"use client";
import { useState } from "react";
import { useRouter } from "next/navigation";
export default function Login() {
  const router = useRouter();
  const [credential, setCredential] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  return (
    <section className="analytics-page">
      <h1>Sign in to TraceForge</h1>
      <p>
        Use your tenant’s dedicated read credential. Ingestion and management
        credentials are not accepted here.
      </p>
      <form
        onSubmit={async (event) => {
          event.preventDefault();
          setBusy(true);
          setError("");
          try {
            const response = await fetch("/api/session", {
              method: "POST",
              headers: { "Content-Type": "application/json" },
              body: JSON.stringify({ credential }),
            });
            setCredential("");
            if (response.ok) {
              router.push("/security");
              return;
            }
            setError((await response.json()).error);
          } catch {
            setError("Authentication unavailable");
          } finally {
            setBusy(false);
          }
        }}
      >
        <label>
          Read credential{" "}
          <input
            type="password"
            autoComplete="off"
            value={credential}
            maxLength={67}
            onChange={(e) => setCredential(e.target.value)}
          />
        </label>
        <button disabled={busy}>Sign in</button>
      </form>
      {error && <p role="alert">{error}</p>}
      <p>
        Local demo sessions expire after 15 minutes. Revocation is checked on
        every protected backend request.
      </p>
    </section>
  );
}
