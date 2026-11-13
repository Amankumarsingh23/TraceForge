CREATE TABLE IF NOT EXISTS experiment_configs (
 config_id text NOT NULL, version text NOT NULL, checksum char(64) NOT NULL UNIQUE,
 manifest jsonb NOT NULL, PRIMARY KEY(config_id,version)
);
ALTER TABLE eval_runs ADD COLUMN IF NOT EXISTS manifest_checksum char(64) REFERENCES experiment_configs(checksum);
CREATE TABLE IF NOT EXISTS eval_comparisons (
 comparison_id char(32) PRIMARY KEY CHECK(comparison_id ~ '^[a-f0-9]{32}$'),
 baseline_run_id char(32) NOT NULL REFERENCES eval_runs(run_id),
 candidate_run_id char(32) NOT NULL REFERENCES eval_runs(run_id),
 engine_version text NOT NULL, result jsonb NOT NULL CHECK(octet_length(result::text)<=2097152),
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS gate_policies (
 policy_id text NOT NULL, version text NOT NULL, checksum char(64) NOT NULL UNIQUE,
 content jsonb NOT NULL, PRIMARY KEY(policy_id,version)
);
CREATE TABLE IF NOT EXISTS gate_decisions (
 decision_id char(32) PRIMARY KEY CHECK(decision_id ~ '^[a-f0-9]{32}$'),
 comparison_id char(32) NOT NULL REFERENCES eval_comparisons(comparison_id),
 policy_checksum char(64) NOT NULL REFERENCES gate_policies(checksum),
 outcome text NOT NULL CHECK(outcome IN ('PASS','FAIL','INCONCLUSIVE')),
 result jsonb NOT NULL, created_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(comparison_id,policy_checksum)
);
CREATE INDEX IF NOT EXISTS comparisons_order ON eval_comparisons(created_at DESC,comparison_id DESC);
INSERT INTO eval_schema_versions(version) VALUES(2) ON CONFLICT DO NOTHING;
CREATE OR REPLACE FUNCTION prevent_experiment_rewrite() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN RAISE EXCEPTION 'experiment history is immutable'; END $$;
CREATE OR REPLACE TRIGGER immutable_config BEFORE UPDATE OR DELETE ON experiment_configs FOR EACH ROW EXECUTE FUNCTION prevent_experiment_rewrite();
CREATE OR REPLACE TRIGGER immutable_policy BEFORE UPDATE OR DELETE ON gate_policies FOR EACH ROW EXECUTE FUNCTION prevent_experiment_rewrite();
CREATE OR REPLACE TRIGGER immutable_comparison BEFORE UPDATE OR DELETE ON eval_comparisons FOR EACH ROW EXECUTE FUNCTION prevent_experiment_rewrite();
CREATE OR REPLACE TRIGGER immutable_decision BEFORE UPDATE OR DELETE ON gate_decisions FOR EACH ROW EXECUTE FUNCTION prevent_experiment_rewrite();
