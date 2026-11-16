-- Additive only: existing data is retained. Existing parts require an explicit
-- MATERIALIZE INDEX operation; do not rewrite them at every backend startup.
ALTER TABLE traceforge.logs ADD INDEX IF NOT EXISTS s13_trace_bloom trace_id TYPE bloom_filter(0.01) GRANULARITY 1;
