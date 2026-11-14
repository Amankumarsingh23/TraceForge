import { forwardTelemetry } from "./proxy";
export type Definition = {
  id: string;
  version: number;
  description: string;
  kind: string;
  source_version: string;
  service: string;
  namespace: string;
  environment: string;
  mode: string;
  provider: string;
  route: string;
  expected_writers: string[];
  target: string;
  window_seconds: number;
  threshold_seconds?: string;
  min_count: number;
  min_coverage: string;
  rules: {
    id: string;
    version: number;
    long_seconds: number;
    short_seconds: number;
    threshold: string;
    severity: string;
    confirm: number;
    recover: number;
  }[];
};
export type Evidence = {
  status: string;
  reason: string;
  from: string;
  to: string;
  total: string;
  good: string;
  bad: string;
  coverage: number;
  diagnostics: Record<string, unknown>;
  trace_ids: string[];
  details?: Record<string, unknown>;
};
export type Budget = {
  sli: string;
  bad_fraction: string;
  allowed_bad_fraction: string;
  allowed_bad_events: string;
  budget_consumed: string;
  budget_remaining: string;
  burn_rate: string;
  meets_target: boolean;
};
export type Window = { evidence: Evidence; budget: Budget | null };
export type Evaluation = {
  id: string;
  slo_key: string;
  definition: Definition;
  status: string;
  cutoff: string;
  revision: number;
  overall: Window;
  rules: {
    rule: Definition["rules"][0];
    long: Window;
    short: Window;
    decision: string;
  }[];
};
export type Incident = {
  id: string;
  fingerprint: string;
  slo_key: string;
  rule_id: string;
  severity: string;
  state: string;
  first_fired: string;
  last_evaluation: string;
  resolved_at: string | null;
};
export type Head = {
  fingerprint: string;
  slo_key: string;
  rule_id: string;
  state: string;
  incident_id: string;
  last_cutoff: string;
  last_data_status: string;
  breaches: number;
  recoveries: number;
};
export function percentage(value: string) {
  const [a, b] = value.split("/").map(Number);
  return `${((b === undefined ? a : a / b) * 100).toLocaleString(undefined, { maximumFractionDigits: 6 })}%`;
}
export function proxyReliability(request: Request, parts: string[]) {
  const path = parts.join("/");
  let valid =
    /^(?:slos(?:\/[a-z0-9_-]{1,80}(?:\/evaluations)?)?|alerts(?:\/[a-f0-9]{64})?|slo-evaluations\/[a-f0-9]{64}|alert-heads|provider-reliability)$/.test(
      path,
    );
  const q = new URL(request.url).searchParams;
  const detail = /^(?:slos\/[^/]+|alerts\/[^/]+|slo-evaluations\/[^/]+)$/.test(
    path,
  );
  for (const [k, v] of q)
    if (
      !["limit", "after"].includes(k) ||
      q.getAll(k).length !== 1 ||
      !v ||
      detail
    )
      valid = false;
  if (q.has("limit") && !/^(?:[1-9]|[1-4][0-9]|50)$/.test(q.get("limit")!))
    valid = false;
  if (q.has("after") && !/^[a-z0-9_-]{1,80}$/.test(q.get("after")!))
    valid = false;
  if (!valid)
    return Response.json(
      {
        error: {
          code: "invalid_reliability_query",
          message: "Invalid reliability path or pagination",
        },
      },
      { status: 400 },
    );
  return forwardTelemetry(request, `/api/v1/${path}`);
}
