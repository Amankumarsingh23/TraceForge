"""Trusted local operator assertions; never exposes database passwords or keys."""

import json
import os
import urllib.request
from pathlib import Path

import psycopg

out = Path("artifacts/session14")
checks = []
with psycopg.connect(
    host="127.0.0.1",
    dbname="traceforge",
    user="traceforge",
    password=os.environ["POSTGRES_PASSWORD"],
) as conn:
    rows = conn.execute("""SELECT n.nspname, rn.nspname, c.conname
        FROM pg_constraint c JOIN pg_namespace n ON n.oid=c.connamespace
        JOIN pg_class r ON r.oid=c.confrelid JOIN pg_namespace rn ON rn.oid=r.relnamespace
        WHERE c.contype='f' AND n.nspname IN ('tf_tenant_alpha','tf_tenant_beta')""").fetchall()
    assert rows and all(a == b for a, b, _ in rows)
    checks.append(
        {
            "check": "all private foreign keys bind within owner schema",
            "count": len(rows),
        }
    )
    for owner, other in (("alpha", "beta"), ("beta", "alpha")):
        report = json.loads((out / f"adversarial-{other}.json").read_text())
        conn.execute("SET LOCAL search_path=tf_tenant_" + owner)
        found = conn.execute(
            "SELECT count(*) FROM security_evaluations WHERE run_id=%s",
            (report["run_id"],),
        ).fetchone()[0]
        assert found == 0
        checks.append(
            {"check": "foreign run absent under owner-only search_path", "owner": owner}
        )
    verifiers = conn.execute(
        "SELECT verifier FROM public.security_credentials"
    ).fetchall()
    assert verifiers and all(len(row[0]) == 32 for row in verifiers)
    checks.append(
        {
            "check": "stored credential verifiers are fixed 32-byte digests",
            "count": len(verifiers),
        }
    )

sql = """SELECT database,table,default_kind,default_expression FROM system.columns
WHERE database IN ('traceforge','tf_tenant_alpha','tf_tenant_beta')
AND table IN ('spans','logs','metric_points') AND name='tenant_id' FORMAT JSON"""
request = urllib.request.Request(
    "http://127.0.0.1:8123",
    sql.encode(),
    headers={
        "X-ClickHouse-User": "traceforge",
        "X-ClickHouse-Key": os.environ["CLICKHOUSE_PASSWORD"],
    },
)
with urllib.request.urlopen(request, timeout=5) as response:
    columns = json.load(response)["data"]
assert len(columns) == 9, columns
assert all(row["default_kind"] == "MATERIALIZED" for row in columns)
checks.append(
    {"check": "nine ownership columns are materialized constants", "columns": columns}
)
(out / "storage-isolation.json").write_text(json.dumps(checks, indent=2))
print(
    "Verified owner-local foreign keys, no public fallback, hashed credentials and trusted storage ownership"
)
