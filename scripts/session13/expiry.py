"""Real isolated span TTL with retained PostgreSQL evaluation and HTTP links.

Leaves only the owned loopback8085 test backend running for the browser check;
its PID is recorded for cleanup. Never expires project raw tables.
"""

import hashlib
import json
import os
import subprocess
import time
import urllib.error
import urllib.request
from pathlib import Path

from benchmark import OUT, RUN, ddl, save, sql


def get(path):
    try:
        with urllib.request.urlopen(
            "http://127.0.0.1:8085" + path, timeout=5
        ) as response:
            raw = response.read()
            try:
                body = json.loads(raw)
            except ValueError:
                body = raw.decode()
            return {"status": response.status, "body": body}
    except urllib.error.HTTPError as exc:
        return {"status": exc.code, "body": json.load(exc)}


def main():
    db = "tf_bench_expiry_" + RUN
    sql(f"CREATE DATABASE {db}", fmt=None)
    for table in ("spans", "logs", "metric_points"):
        ddl(db, table, table)
    sql(
        f"ALTER TABLE {db}.spans MODIFY TTL toDateTime(start_time)+INTERVAL 1 HOUR",
        fmt=None,
    )
    sql(f"SYSTEM STOP TTL MERGES {db}.spans", fmt=None)
    source = json.loads(Path("artifacts/session12/evidence.json").read_text())
    old = next(
        x["trace_id"]
        for x in source["provider_requests"]
        if x["scenario"] == "permanent_error"
    )
    recent = hashlib.sha256((db + "recent").encode()).hexdigest()[:32]
    sql(
        f"INSERT INTO {db}.spans SELECT * FROM traceforge.spans WHERE trace_id='{old}'",
        fmt=None,
    )
    sql(
        f"INSERT INTO {db}.spans SELECT * REPLACE('{recent}' AS trace_id,now64(9) AS start_time,now64(9)+toIntervalNanosecond(duration_ns) AS end_time) FROM (SELECT * FROM traceforge.spans WHERE trace_id='{old}')",
        fmt=None,
    )
    env = dict(os.environ, CLICKHOUSE_DATABASE=db, TRACEFORGE_SLO_ACTIVE="")
    log = (OUT / "expiry-backend.log").open("w")
    process = subprocess.Popen(
        [
            str(Path("bin/traceforge-server-session13.exe").resolve()),
            "--grpc-address=127.0.0.1:4325",
            "--health-address=127.0.0.1:8085",
        ],
        env=env,
        stdout=log,
        stderr=log,
        creationflags=subprocess.CREATE_NO_WINDOW,
    )
    (OUT / "expiry-pid.txt").write_text(str(process.pid))
    for _ in range(40):
        try:
            if get("/readyz")["status"] == 200:
                break
        except OSError:
            time.sleep(0.2)
    definitions = json.loads(Path("artifacts/session12/definitions.json").read_text())
    definition = next(
        d
        for d in definitions
        if d["kind"] == "provider_attempt" and d["mode"] == "fixture"
    )
    definition["id"] = "s13-ttl-" + RUN
    definition["description"] = (
        "Isolated copied fixture evidence; raw TTL link experiment"
    )
    file = OUT / "expiry-definition.json"
    file.write_text(json.dumps([definition]))
    output = subprocess.check_output(
        [
            "bin/slo-evaluate.exe",
            "--file",
            str(file),
            "--cutoff",
            "2026-11-14T06:30:10Z",
        ],
        env=env,
        timeout=30,
    )
    evaluation = json.loads(output)[0]
    assert evaluation["overall"]["evidence"]["trace_ids"] == [old]
    before = get("/api/v1/traces/" + old)
    assert before["status"] == 200
    sql(f"SYSTEM START TTL MERGES {db}.spans", fmt=None)
    sql(f"ALTER TABLE {db}.spans MATERIALIZE TTL SETTINGS mutations_sync=2", fmt=None)
    # Controlled, tiny isolated TTL fixture only; never a main-table merge.
    sql(f"OPTIMIZE TABLE {db}.spans FINAL", "isolated-ttl-merge", fmt=None)
    after = get("/api/v1/traces/" + old)
    kept = get("/api/v1/traces/" + recent)
    history = get("/api/v1/slo-evaluations/" + evaluation["id"])
    print(
        "expiry statuses",
        after["status"],
        kept["status"],
        history["status"],
        flush=True,
    )
    save()
    assert after["status"] == 404 and kept["status"] == 200 and history["status"] == 200
    assert history["body"]["overall"]["evidence"]["trace_ids"] == [old]
    report = {
        "database": db,
        "old_trace": old,
        "recent_trace": recent,
        "evaluation": evaluation,
        "before_status": before["status"],
        "after_status": after["status"],
        "recent_status": kept["status"],
        "retained_history_status": history["status"],
        "backend": "http://127.0.0.1:8085",
    }
    (OUT / "expiry.json").write_text(json.dumps(report, indent=2))
    save()


if __name__ == "__main__":
    main()
