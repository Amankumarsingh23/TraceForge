CREATE TABLE IF NOT EXISTS security_evaluations(
 run_id text PRIMARY KEY CHECK(run_id ~ '^[a-f0-9]{32}$'),
 result jsonb NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE OR REPLACE TRIGGER immutable_security_evaluation BEFORE UPDATE OR DELETE ON security_evaluations FOR EACH ROW EXECUTE FUNCTION prevent_experiment_rewrite();
