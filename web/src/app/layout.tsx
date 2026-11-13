import type { Metadata } from "next";
import Link from "next/link";
import { Navigation } from "../components/navigation";
import "./globals.css";
export const metadata: Metadata = {
  title: "TraceForge · Trace explorer",
  description: "Investigate observed distributed traces",
};
export default function RootLayout({
  children,
}: Readonly<{ children: React.ReactNode }>) {
  return (
    <html lang="en">
      <body>
        <header className="topbar">
          <Link href="/traces" className="brand">
            <span className="brand-mark">TF</span>TraceForge
          </Link>
          <Navigation />
          <span className="local-tag">
            <i /> Local development
          </span>
        </header>
        <main>{children}</main>
        <footer>
          TraceForge / Observed telemetry · Manual refresh · Completeness
          unknown
        </footer>
      </body>
    </html>
  );
}
