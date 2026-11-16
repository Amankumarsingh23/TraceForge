"""Real Go API timings, concurrent OTLP ACKs, paired SQL inserts and backfill.

Run after benchmark.py. Keeps all raw rows and previous local evidence.
"""

import argparse
import concurrent.futures
import hashlib
import json
import os
import statistics
import subprocess
import time
import urllib.parse
import urllib.request
from pathlib import Path

from benchmark import OUT, ddl, save, sql


def fetch(path):
    start = time.perf_counter()
    try:
        with urllib.request.urlopen(
            "http://127.0.0.1:8085" + path, timeout=6
        ) as response:
            raw = response.read(16 << 20)
            return {
                "status": response.status,
                "ms": (time.perf_counter() - start) * 1000,
                "checksum": hashlib.sha256(raw).hexdigest(),
            }
    except (OSError, ValueError) as exc:
        return {"error": str(exc), "ms": (time.perf_counter() - start) * 1000}


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument(
        "--revision-base",
        type=int,
        default=22,
        help="fresh SLO revision1..99; next revision is used for isolated comparison",
    )
    revision_base = parser.parse_args().revision_base
    if not 1 <= revision_base <= 99:
        raise SystemExit("revision base must be1..99")
    baseline = json.loads((OUT / "benchmark.json").read_text())
    db = baseline["datasets"][-1]["db"]
    report = {"database": db, "paired_inserts": {}, "api": {}}
    for name, bloom in (("write_plain", False), ("write_bloom", True)):
        table = name + "_" + str(time.time_ns())
        ddl(db, table, "logs", bloom=bloom)
        times = []
        for i in range(5):
            start = time.perf_counter()
            sql(
                f"INSERT INTO {db}.{table} SELECT * FROM {db}.logs ORDER BY log_id LIMIT 10000",
                f"paired-{name}",
                fmt=None,
            )
            times.append((time.perf_counter() - start) * 1000)
        report["paired_inserts"][name] = {
            "samples": 5,
            "batch_rows": 10000,
            "p50_ack_ms": statistics.median(times),
            "p95_ack_ms": max(times),
            "counts": sql(
                f"SELECT count() physical,groupBitXor(cityHash64(tuple(*))) checksum FROM {db}.{table}"
            ),
        }
    assert (
        report["paired_inserts"]["write_plain"]["counts"]
        == report["paired_inserts"]["write_bloom"]["counts"]
    )
    report["write_storage"] = sql(
        f"SELECT table,count() parts,sum(rows) rows,sum(bytes_on_disk) disk_bytes FROM system.parts WHERE active AND database='{db}' AND table LIKE 'write_%' GROUP BY table ORDER BY table"
    )
    # Fill the benchmark metric table with the COMPLETE required historical scope.
    # Added exact duplicates are intentional and must not change logical results.
    predicate = "service_namespace LIKE 'session12-%' AND end_ns>=1794634000000000000 AND end_ns<=1794638000000000000"
    n = int(
        sql(f"SELECT count() n FROM traceforge.metric_points WHERE {predicate}")[0]["n"]
    )
    assert n <= 50000, "bounded full-scope copy exceeded; no silent LIMIT"
    sql(
        f"INSERT INTO {db}.metric_points SELECT * FROM traceforge.metric_points WHERE {predicate}",
        "complete-metric-copy",
        fmt=None,
    )
    report["complete_metric_scope_rows"] = n
    env = dict(os.environ, CLICKHOUSE_DATABASE=db, TRACEFORGE_SLO_ACTIVE="")
    log = (OUT / "api-server.log").open("w")
    p = subprocess.Popen(
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
    try:
        for _ in range(60):
            if fetch("/readyz").get("status") == 200:
                break
            time.sleep(0.2)
        else:
            raise RuntimeError("isolated API readiness failed")
        trace = baseline["datasets"][-1]["trace_id"]
        window = urllib.parse.urlencode(
            dict(**{"from": "2026-09-30T23:59:59Z", "to": "2026-09-30T23:59:59.1Z"})
        )
        metric = urllib.parse.urlencode(
            dict(
                **{
                    "from": "2026-11-14T06:28:19Z",
                    "to": "2026-11-14T06:28:39Z",
                    "service": "payment",
                    "namespace": "session12-20261114062758",
                }
            )
        )
        paths = {
            "trace": f"/api/v1/traces/{trace}",
            "search": "/api/v1/traces?" + window,
            "correlated-logs": "/api/v1/logs?from=2026-09-30T23:59:59Z&to=2026-10-01T00:05:01Z&trace_id="
            + trace,
            "recent-logs": "/api/v1/logs?" + window,
            "service-red": "/api/v1/services?" + window + "&bucket_seconds=60",
            "graph": "/api/v1/service-graph?" + window + "&bucket_seconds=60",
            "metric": "/api/v1/metrics/query?" + metric,
        }
        for label, path in paths.items():
            fetch(path)
            runs = [fetch(path) for _ in range(10)]
            report["api"][label] = {"path": path, "runs": runs}
            assert all(x.get("status") == 200 for x in runs), (label, runs)
        sql(
            f"ALTER TABLE {db}.logs MATERIALIZE INDEX s13_trace_bloom SETTINGS mutations_sync=2",
            "api-index-backfill",
            fmt=None,
        )
        indexed = [fetch(paths["correlated-logs"]) for _ in range(10)]
        report["api"]["correlated-logs-indexed"] = {
            "path": paths["correlated-logs"],
            "runs": indexed,
        }
        assert {x["checksum"] for x in indexed} == {
            x["checksum"] for x in report["api"]["correlated-logs"]["runs"]
        }
        # Same full metric population through actual SLO adapter/CLI, revisions
        # preserve earlier operational decisions and cannot move same-cutoff state.
        key = json.loads(Path("artifacts/session12/evidence.json").read_text())[
            "slo_key"
        ]
        for name, revision, scope in (
            ("reference", revision_base, dict(env, CLICKHOUSE_DATABASE="traceforge")),
            ("isolated", revision_base + 1, env),
        ):
            start = time.perf_counter()
            output = subprocess.check_output(
                [
                    "bin/slo-evaluate.exe",
                    "--file",
                    "artifacts/session12/definitions.json",
                    "--key",
                    key,
                    "--cutoff",
                    "2026-11-14T06:28:39Z",
                    "--revision",
                    str(revision),
                ],
                env=scope,
                timeout=30,
            )
            report["slo_" + name] = {
                "ms": (time.perf_counter() - start) * 1000,
                "result": json.loads(output)[0],
            }
        assert (
            report["slo_reference"]["result"]["overall"]["budget"]
            == report["slo_isolated"]["result"]["overall"]["budget"]
        )

        # Ingestion ACK benchmark runs concurrently with actual bounded API reads.
        # Loadgen reuses Session4's synchronous persistence-confirmed OTLP path.
        def queries():
            return [fetch(paths["trace"]) for _ in range(30)]

        with concurrent.futures.ThreadPoolExecutor(max_workers=1) as pool:
            future = pool.submit(queries)
            subprocess.run(
                [
                    "bin/otel-loadgen.exe",
                    "--endpoint",
                    "127.0.0.1:4325",
                    "--http-endpoint",
                    "http://127.0.0.1:8085",
                    "--spans",
                    "10000",
                    "--spans-per-request",
                    "100",
                    "--concurrency",
                    "4",
                    "--verify-storage=false",
                    "--output",
                    "artifacts/session13/concurrent-ingestion.json",
                ],
                env=env,
                check=True,
                timeout=60,
                capture_output=True,
            )
            report["concurrent_api"] = future.result()
        assert all(x.get("status") == 200 for x in report["concurrent_api"])
        report["concurrent_parts"] = sql(
            f"SELECT table,count() parts,sum(rows) rows,sum(bytes_on_disk) disk_bytes FROM system.parts WHERE active AND database='{db}' GROUP BY table ORDER BY table"
        )
    finally:
        p.terminate()
        p.wait(timeout=10)
        log.close()
        (OUT / "api-overhead.json").write_text(json.dumps(report, indent=2))
        save()
    # Adopted migration's backfill is additive; measure same records/index off/on.
    before = sql(
        "SELECT count() physical,groupBitXor(cityHash64(tuple(*))) checksum FROM traceforge.logs"
    )
    sql(
        Path("internal/storage/migrations/0004_log_trace_index.sql").read_text(),
        "migration",
        fmt=None,
    )
    sql(
        "ALTER TABLE traceforge.logs MATERIALIZE INDEX s13_trace_bloom SETTINGS mutations_sync=2",
        "backfill",
        fmt=None,
    )
    after = sql(
        "SELECT count() physical,groupBitXor(cityHash64(tuple(*))) checksum FROM traceforge.logs"
    )
    assert before == after, "backfill changed raw records"
    report["main_backfill"] = {"before": before, "after": after}
    sql("SYSTEM FLUSH LOGS", fmt=None)
    report["query_log"] = sql(
        "SELECT query_id,query_duration_ms,read_rows,read_bytes,memory_usage FROM system.query_log WHERE type='QueryFinish' AND (query_id LIKE 's13-%paired-%' OR query_id LIKE 's13-%backfill-%') ORDER BY event_time_microseconds"
    )
    (OUT / "api-overhead.json").write_text(json.dumps(report, indent=2))
    save()


if __name__ == "__main__":
    main()
