"use client";
import Link from "next/link";
import { usePathname } from "next/navigation";
export function Navigation() {
  const path = usePathname();
  return (
    <nav aria-label="Main navigation">
      <Link href="/evaluations" aria-current={path.startsWith("/evaluations") ? "page" : undefined}>Evaluations</Link>
      <Link
        href="/traces"
        aria-current={path.startsWith("/traces") ? "page" : undefined}
      >
        Trace explorer
      </Link>
      <Link
        href="/logs"
        aria-current={path.startsWith("/logs") ? "page" : undefined}
      >
        Logs
      </Link>
    </nav>
  );
}
