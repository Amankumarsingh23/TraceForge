-- Trusted schema ownership. MATERIALIZED columns cannot be supplied by normal
-- inserts and are excluded from SELECT * by default, preserving old fixtures.
ALTER TABLE traceforge.spans ADD COLUMN IF NOT EXISTS tenant_id LowCardinality(String) MATERIALIZED '{tenant}';
ALTER TABLE traceforge.logs ADD COLUMN IF NOT EXISTS tenant_id LowCardinality(String) MATERIALIZED '{tenant}';
ALTER TABLE traceforge.metric_points ADD COLUMN IF NOT EXISTS tenant_id LowCardinality(String) MATERIALIZED '{tenant}';
