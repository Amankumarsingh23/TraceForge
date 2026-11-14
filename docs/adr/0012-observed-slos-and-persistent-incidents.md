# ADR0012 — Freeze reliability evidence and transact incident state

Status: accepted for Session12 local development.

Use direct matched metric intervals for service availability/latency and canonical AgentOps observations for provider attempts/logical operations. Reuse the existing temporal/reconstruction engines instead of deriving availability from sampled spans or counting transport replays as new traffic. Require explicit writer and mode scopes; missing evidence must remain UNKNOWN.

Keep exact rational budgets and immutable definitions/evaluations/events in PostgreSQL. A separate bounded write-capable pool leaves the existing evaluation read pool unchanged. Serialize lifecycle decisions by definition/head locks, enforce one active incident per fingerprint and allow only newer cutoffs to advance state. Same-cutoff revisions expose late evidence without silently changing historical decisions. UNKNOWN retains the incident and interrupts confirmation/recovery streaks.

Provide a bounded opt-in scheduler, a local configuration/evaluation CLI and read-only browser views. Store definitions locally through CLI rather than adding an unauthenticated HTTP mutation API. Existing dependencies suffice; no new protocol or package version is selected. Current PostgreSQL locking and Google SRE multiwindow documentation were checked; see alerting policy links.

Consequences: retained evidence survives backend restart and enables drilldown, while incident state no longer depends on process memory. Large/raw unsupported windows fail closed. Definition-level locking serializes rules of one objective, acceptable for eight local active objectives. Raw scans, metadata availability, retention growth, absent rejection attribution, lack of auth/tenant isolation and absence of production notifications remain limits. Session13 storage optimization/retention and Session14 security are not implemented here.
