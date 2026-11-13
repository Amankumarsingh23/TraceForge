CREATE TABLE IF NOT EXISTS eval_schema_versions (version integer PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now());
CREATE TABLE IF NOT EXISTS eval_datasets (
 dataset_id text NOT NULL, version text NOT NULL, checksum char(64) NOT NULL,
 corpus_checksum char(64) NOT NULL, content jsonb NOT NULL,
 PRIMARY KEY(dataset_id,version), UNIQUE(dataset_id,version,checksum)
);
CREATE TABLE IF NOT EXISTS eval_cases (
 dataset_id text NOT NULL, dataset_version text NOT NULL, case_id text NOT NULL,
 content jsonb NOT NULL, PRIMARY KEY(dataset_id,dataset_version,case_id),
 FOREIGN KEY(dataset_id,dataset_version) REFERENCES eval_datasets(dataset_id,version)
);
CREATE TABLE IF NOT EXISTS eval_runs (
 run_id char(32) PRIMARY KEY CHECK(run_id ~ '^[a-f0-9]{32}$'),
 dataset_id text NOT NULL, dataset_version text NOT NULL, dataset_checksum char(64) NOT NULL,
 configuration jsonb NOT NULL, status text NOT NULL CHECK(status IN ('running','completed','failed','interrupted')),
 started_at timestamptz NOT NULL DEFAULT now(), completed_at timestamptz,
 attempted integer NOT NULL DEFAULT 0 CHECK(attempted>=0),
 succeeded integer NOT NULL DEFAULT 0 CHECK(succeeded>=0),
 failed integer NOT NULL DEFAULT 0 CHECK(failed>=0),
 scored integer NOT NULL DEFAULT 0 CHECK(scored>=0),
 aggregates jsonb NOT NULL DEFAULT '{}',
 FOREIGN KEY(dataset_id,dataset_version,dataset_checksum) REFERENCES eval_datasets(dataset_id,version,checksum),
 CHECK(succeeded+failed=attempted), CHECK(scored<=succeeded),
 UNIQUE(run_id,dataset_id,dataset_version)
);
CREATE TABLE IF NOT EXISTS eval_case_results (
 run_id char(32) NOT NULL, case_id text NOT NULL, dataset_id text NOT NULL, dataset_version text NOT NULL,
 status text NOT NULL CHECK(status IN ('succeeded','execution_failed')),
 trace_id varchar(32) CHECK(trace_id IS NULL OR trace_id ~ '^[a-f0-9]{32}$'),
 application_run_id varchar(32) CHECK(application_run_id IS NULL OR application_run_id ~ '^[a-f0-9]{32}$'),
 result jsonb NOT NULL CHECK(octet_length(result::text)<=131072),
 PRIMARY KEY(run_id,case_id),
 FOREIGN KEY(run_id,dataset_id,dataset_version) REFERENCES eval_runs(run_id,dataset_id,dataset_version),
 FOREIGN KEY(dataset_id,dataset_version,case_id) REFERENCES eval_cases(dataset_id,dataset_version,case_id)
);
CREATE INDEX IF NOT EXISTS eval_run_order ON eval_runs(started_at DESC,run_id DESC);
INSERT INTO eval_schema_versions(version) VALUES(1) ON CONFLICT DO NOTHING;
