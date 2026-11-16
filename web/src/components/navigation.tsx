"use client";
import Link from "next/link";
import { usePathname } from "next/navigation";
import {useEffect,useState} from "react";
export function Navigation() {
  const path = usePathname();
  const [tenant,setTenant]=useState("");
  useEffect(()=>{const c=new AbortController();void fetch("/api/security/session",{signal:c.signal,cache:"no-store"}).then(async r=>{if(!c.signal.aborted)setTenant(r.ok?(await r.json()).tenant:"");}).catch(()=>{if(!c.signal.aborted)setTenant("");});return()=>c.abort();},[path]);
  return (
    <nav aria-label="Main navigation">
      <Link href="/security" aria-current={path.startsWith("/security")?"page":undefined}>Security</Link>
      <span aria-label="Tenant context">{tenant?`Tenant: ${tenant}`:"Legacy local demo"}</span>
      <Link
        href="/slos"
        aria-current={path.startsWith("/slos") ? "page" : undefined}
      >
        SLOs
      </Link>
      <Link
        href="/alerts"
        aria-current={path.startsWith("/alerts") ? "page" : undefined}
      >
        Alerts
      </Link>
      <Link
        href="/metrics"
        aria-current={path.startsWith("/metrics") ? "page" : undefined}
      >
        Metrics
      </Link>
      <Link
        href="/services"
        aria-current={path.startsWith("/services") ? "page" : undefined}
      >
        Services
      </Link>
      <Link
        href="/service-graph"
        aria-current={path.startsWith("/service-graph") ? "page" : undefined}
      >
        Service graph
      </Link>
      <Link
        href="/comparisons"
        aria-current={path.startsWith("/comparisons") ? "page" : undefined}
      >
        Comparisons
      </Link>
      <Link
        href="/evaluations"
        aria-current={path.startsWith("/evaluations") ? "page" : undefined}
      >
        Evaluations
      </Link>
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
