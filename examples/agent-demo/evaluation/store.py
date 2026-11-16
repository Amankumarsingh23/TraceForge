import json
import os
import ssl
import urllib.request

import psycopg
from psycopg.types.json import Jsonb

from .dataset import ROOT


class Store:
    def __init__(self):
        tenant = os.getenv("TRACEFORGE_TRUSTED_TENANT", "legacy")
        if tenant not in {"legacy", "alpha", "beta"}:
            raise ValueError("invalid trusted job tenant")
        schema = "public" if tenant == "legacy" else "tf_tenant_" + tenant
        if tenant != "legacy":
            key = os.environ["TRACEFORGE_JOB_KEY"]
            request = urllib.request.Request(
                os.environ["TRACEFORGE_AUTH_IDENTITY_URL"],
                headers={"Authorization": "Bearer " + key},
            )
            with urllib.request.urlopen(
                request,
                context=ssl.create_default_context(cafile=os.environ["TRACEFORGE_CA_FILE"]),
                timeout=2,
            ) as response:
                identity = json.load(response)
            if identity["tenant"] != tenant or "manage" not in identity["scopes"]:
                raise ValueError("trusted job not authorized for tenant metadata")
        self.connection = psycopg.connect(
            host=os.getenv("POSTGRES_HOST", "127.0.0.1"),
            port=int(os.getenv("POSTGRES_PORT", "5432")),
            dbname=os.getenv("POSTGRES_DB", "traceforge"),
            user=os.getenv("POSTGRES_USER", "traceforge"),
            password=os.environ["POSTGRES_PASSWORD"],
            connect_timeout=3,
            options=f"-c statement_timeout=3000 -c lock_timeout=1000 -c search_path={schema}",
            autocommit=True,
        )

    def close(self):
        self.connection.close()

    def migrate(self):
        with self.connection.transaction():
            self.connection.execute("SELECT pg_advisory_xact_lock(88001)")
            for migration in sorted((ROOT / "migrations").glob("*.sql")):
                self.connection.execute(migration.read_text())

    def start(self, dataset, digest, run_id, config):
        with self.connection.transaction():
            manifest = config.get("manifest")
            if manifest:
                self.connection.execute(
                    "INSERT INTO experiment_configs VALUES(%s,%s,%s,%s) ON CONFLICT DO NOTHING",
                    (
                        manifest["config_id"],
                        manifest["version"],
                        manifest["checksum"],
                        Jsonb(manifest["content"]),
                    ),
                )
                existing = self.connection.execute(
                    "SELECT checksum FROM experiment_configs WHERE config_id=%s AND version=%s",
                    (manifest["config_id"], manifest["version"]),
                ).fetchone()
                if existing[0] != manifest["checksum"]:
                    raise ValueError("immutable manifest version mismatch")
            self.connection.execute(
                "INSERT INTO eval_datasets VALUES(%s,%s,%s,%s,%s) ON CONFLICT DO NOTHING",
                (
                    dataset["dataset_id"],
                    dataset["version"],
                    digest,
                    dataset["corpus_checksum"],
                    Jsonb(dataset),
                ),
            )
            existing = self.connection.execute(
                "SELECT checksum FROM eval_datasets WHERE dataset_id=%s AND version=%s FOR UPDATE",
                (dataset["dataset_id"], dataset["version"]),
            ).fetchone()
            if existing[0] != digest:
                raise ValueError("immutable dataset version checksum mismatch")
            for case in dataset["cases"]:
                self.connection.execute(
                    "INSERT INTO eval_cases VALUES(%s,%s,%s,%s) ON CONFLICT DO NOTHING",
                    (dataset["dataset_id"], dataset["version"], case["case_id"], Jsonb(case)),
                )
            self.connection.execute(
                "INSERT INTO eval_runs(run_id,dataset_id,dataset_version,dataset_checksum,configuration,status) VALUES(%s,%s,%s,%s,%s,'running')",
                (run_id, dataset["dataset_id"], dataset["version"], digest, Jsonb(config)),
            )
            if manifest:
                self.connection.execute(
                    "UPDATE eval_runs SET manifest_checksum=%s WHERE run_id=%s",
                    (manifest["checksum"], run_id),
                )

    def save(self, run_id, dataset, case, result):
        with self.connection.transaction():
            row = self.connection.execute(
                "SELECT status FROM eval_runs WHERE run_id=%s FOR UPDATE", (run_id,)
            ).fetchone()
            if row is None or row[0] != "running":
                raise ValueError("run is not writable")
            self.connection.execute(
                "INSERT INTO eval_case_results VALUES(%s,%s,%s,%s,%s,%s,%s,%s)",
                (
                    run_id,
                    case["case_id"],
                    dataset["dataset_id"],
                    dataset["version"],
                    result["status"],
                    result.get("trace_id"),
                    result.get("application_run_id"),
                    Jsonb(result),
                ),
            )
            success = int(result["status"] == "succeeded")
            scored = int(success and result["semantic_status"] == "constrained_scored")
            self.connection.execute(
                "UPDATE eval_runs SET attempted=attempted+1,succeeded=succeeded+%s,failed=failed+%s,scored=scored+%s WHERE run_id=%s",
                (success, 1 - success, scored, run_id),
            )

    def finish(self, run_id, aggregates, status="completed"):
        self.connection.execute(
            "UPDATE eval_runs SET status=%s,completed_at=now(),aggregates=%s WHERE run_id=%s AND status='running'",
            (status, Jsonb(aggregates), run_id),
        )
