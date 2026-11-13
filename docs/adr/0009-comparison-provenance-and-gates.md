# ADR 0009 — Frozen paired evidence and fail-closed policy decisions

Accepted for Session 9, 2026-11-13. Reuse the Session 8 runner, datasets/results, PostgreSQL and canonical Session 7 summaries. No new runtime dependency, provider protocol or telemetry ingestion path is introduced.

Separate retrieval depth from evaluation cutoff. Freeze a canonical versioned manifest at run start and reference it from the existing run row. Existing correctly stored run metadata is retained; legacy snapshots have explicit provenance and known v1 semantic mapping, not fabricated metadata. Use a pure bounded comparison engine that pairs stable IDs, preserves failures/coverage, checks compatibility, records intended differences and emits server-authoritative deltas/transitions.

Keep statistics small and transparent: R7 quantiles, seeded paired percentile bootstrap, insufficient/degenerate warnings. No significance claim or mandatory statistical gate. Use exact nano-USD totals/Decimal money means and keep partial/unknown cost separate. Synthetic fixture prices/usage and local HTTP wall times cannot establish real billing/model speed.

Migration0002 adds manifests, comparisons, policies and decisions with foreign keys, unique policy/comparison identities, payload caps and immutable-history triggers. Python CLI writes parameterized transactions; content-addressed comparisons and unique comparison/policy pairs make retries idempotent. New policy versions retain new decision rows. Full ClickHouse telemetry remains in ClickHouse; metadata contains only snapshots, judgments, IDs and canonical summary observations.

Go remains read-only and bounded; Next adds a focused paired-evidence page and thin proxies. Rule validation, units, missing evidence and precedence live in Python, not competing frontend logic. PASS means only satisfied declared policy scope; no automatic deployment. Production-oriented evidence checks require live execution and independent semantic validation, which remain pending. Current deployment stays restricted local development; production authentication/tenant isolation are future work.

Controlled gate fixtures are separate artifacts/tests with no fabricated trace links and are never stored as measured application runs. Actual context/retrieval candidates execute unchanged labels/outputs through the real application; no score editing creates measured outcomes. Protected cases are designated from existing multi-document categories before the candidate runs.
