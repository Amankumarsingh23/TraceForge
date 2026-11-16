# ADR 0014: Credential-selected private tenant namespaces

Status: accepted for restricted local Session 14 development.

Problem: existing canonical IDs, ClickHouse selectors and PostgreSQL metadata were single-owner. A resource attribute is attacker-controlled; adding a tenant search filter would leave secondary reads/joins exposed. A mixed Collector export credential could erase sender identity.

Decision: resolve an opaque scoped credential online at a TLS gateway and route only to a fixed private owner backend. Use private ClickHouse databases and PostgreSQL schemas with a single-schema search path. Preserve historical data as Legacy. Include the trusted namespace in logical identity; keep existing normalization/fingerprints within it. Each collector has one authenticated owner and separate queue. Separate read/ingest/manage authority, require ownership before tools and trusted nonlegacy jobs, and fail closed when authorization storage is unavailable. Default legacy Compose remains a visibly labeled compatibility profile.

Consequences: every existing query executes against an owning namespace without adding fragile optional predicates to dozens of selectors. Replay, cumulative streams, immutable comparison and SLO semantics remain unchanged within a namespace. Separate processes contain tenant cardinality and batching state. Migrations and configuration must consistently bind schema/database/process; direct backends must never be exposed. The local operator principal can access all owners, so this is not defense against host/database-admin compromise. More tenants require provisioned namespaces/processes; production IAM, database roles/RLS, distributed quotas and an external review are separate decisions.

Evidence: two equal trace/span IDs produce 100ms and 900ms respectively; foreign detail requests return 404; cross-owner Collector credentials fail; per-owner SLO evidence and outage/recovery/replay checks passed. See the Session 14 journal and threat model for measured scope and unresolved checks.
