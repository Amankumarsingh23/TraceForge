"""Bounded SQL layout experiment. Stdlib only; never mutates project telemetry.

Run from repository root. Every run creates new tf_bench_* databases and retains
raw queries/results/failures. SQL workloads are not claimed to be complete APIs.
"""

import argparse
import hashlib
import json
import math
import os
import statistics
import time
import urllib.parse
import urllib.request
import uuid
from pathlib import Path

OUT = Path("artifacts/session13")
OUT.mkdir(parents=True, exist_ok=True)
PASSWORD = os.environ.get("CLICKHOUSE_PASSWORD", "")
RUN = time.strftime("%Y%m%d%H%M%S", time.gmtime())
RAW = []


def sql(query, label="admin", fmt="JSONEachRow", **settings):
    if not PASSWORD:
        raise RuntimeError(
            "Configure CLICKHOUSE_PASSWORD privately before database work"
        )
    qid = f"s13-{RUN}-{label}-{uuid.uuid4().hex[:8]}"
    params = dict(
        query_id=qid,
        max_execution_time=20,
        max_threads=2,
        max_memory_usage=536870912,
        max_rows_to_read=3000000,
        max_bytes_to_read=536870912,
        read_overflow_mode="throw",
        use_query_cache=0,
        **settings,
    )
    if fmt:
        query += f" FORMAT {fmt}"
    start = time.perf_counter()
    record = {"query_id": qid, "label": label, "sql": query}
    try:
        req = urllib.request.Request(
            "http://127.0.0.1:8123/?" + urllib.parse.urlencode(params),
            query.encode(),
            headers={"X-ClickHouse-User": "traceforge", "X-ClickHouse-Key": PASSWORD},
        )
        with urllib.request.urlopen(req, timeout=35) as response:
            data = response.read(32 << 20)
        record.update(
            wall_ms=(time.perf_counter() - start) * 1000,
            checksum=hashlib.sha256(data).hexdigest(),
        )
        RAW.append(record)
        return (
            [json.loads(x) for x in data.splitlines()]
            if fmt == "JSONEachRow"
            else data.decode()
        )
    except Exception as exc:
        if hasattr(exc, "read"):
            record["server_error"] = exc.read(4096).decode(errors="replace")
        record.update(error=str(exc), wall_ms=(time.perf_counter() - start) * 1000)
        RAW.append(record)
        save()
        raise


def save():
    (OUT / f"queries-{RUN}.json").write_text(json.dumps(RAW, indent=2), encoding="utf8")


def ddl(db, table, source, order=None, codec=False, bloom=False):
    import re

    q = sql(f"SHOW CREATE TABLE traceforge.{source}", fmt="TabSeparatedRaw").strip()
    q = q.replace(f"traceforge.{source}", f"{db}.{table}", 1)
    # Keep the reference layout index-free even after the additive migration.
    q = re.sub(r",\n\s*INDEX s13_trace_bloom[^\n]*", "", q)
    if order:
        q = re.sub(r"ORDER BY \([^\n]+\)", f"ORDER BY ({order})", q)
    if codec:
        q = q.replace(
            "`span_attributes_json` String",
            "`span_attributes_json` String CODEC(ZSTD(3))",
        )
    if bloom:
        q = q.replace(
            "\n)",
            ",\n INDEX s13_trace_bloom trace_id TYPE bloom_filter(0.01) GRANULARITY 1\n)",
            1,
        )
    sql(q, fmt=None)
    return q


def measure(label, query, repeats):
    sql(query, label + "-warmup")
    values, checksums = [], set()
    for _ in range(repeats):
        result = sql(query, label)
        checksums.add(
            hashlib.sha256(json.dumps(result, sort_keys=True).encode()).hexdigest()
        )
        values.append(RAW[-1]["wall_ms"])
    assert len(checksums) == 1, label
    return {
        "p50_ms": statistics.median(values),
        "p95_ms": sorted(values)[math.ceil(0.95 * len(values)) - 1],
        "samples": repeats,
        "checksum": checksums.pop(),
    }


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--sizes", nargs="+", type=int, default=[100000, 300000])
    parser.add_argument("--repeats", type=int, default=10)
    args = parser.parse_args()
    if (
        any(n not in (100000, 300000) for n in args.sizes)
        or not 5 <= args.repeats <= 30
    ):
        raise SystemExit("bounded100k/300k sizes,5..30 repetitions")
    report = {
        "run": RUN,
        "seed": 42,
        "settings": "threads2; warm OS caches not flushed; query cache disabled",
        "environment": sql("SELECT version() version,timezone() timezone"),
        "datasets": [],
    }
    (OUT / "environment.json").write_text(json.dumps(report, indent=2))
    for n in args.sizes:
        db = f"tf_bench_s13_{RUN}_{n}"
        sql(f"CREATE DATABASE {db}", fmt=None)
        definitions = {
            "spans": ddl(db, "spans", "spans"),
            "service_first": ddl(
                db,
                "service_first",
                "spans",
                "service_name, start_time, trace_id, span_id",
            ),
            "codec": ddl(db, "codec", "spans", codec=True),
            "logs": ddl(db, "logs", "logs"),
            "logs_bloom": ddl(db, "logs_bloom", "logs", bloom=True),
            "metric_points": ddl(db, "metric_points", "metric_points"),
        }
        # Fixed UTC time spans two monthly partitions, no dependency on wall clock.
        # Every5 rows form a trace, statuses and payloads are deterministic.
        insert = f"""INSERT INTO {db}.spans SELECT
 lower(hex(MD5(concat('s13-seed42-',toString(intDiv(number,5)))))) trace_id,
 lower(hex(toFixedString(unhex(leftPad(hex(number%5+1),16,'0')),8))) span_id,
 if(number%5=0,'',leftPad(toString(number%5),16,'0')) parent_span_id,
 ['gateway','gateway','payment','payment','worker'][number%5+1] service_name,
 ['GET /checkout','HTTP GET','GET /charge','payment.process','work'][number%5+1] span_name,
 toUInt8([2,3,2,1,1][number%5+1]) span_kind,
 fromUnixTimestamp64Nano(toInt64(1790812799000000000+number*1000000),'UTC') start_time,
 start_time+toIntervalNanosecond(([10000000,8000000,6000000,4000000,1000000][number%5+1]+if(intDiv(number,5)%20=0,100000000,0))) end_time,
 toUInt64(([10000000,8000000,6000000,4000000,1000000][number%5+1]+if(intDiv(number,5)%20=0,100000000,0))) duration_ns,
 toUInt8(if(intDiv(number,5)%100=0,2,0)) status_code,'' status_message,
 concat('[{{"key":"service.name","value":{{"stringValue":"',service_name,'"}}}},{{"key":"traceforge.workload.kind","value":{{"stringValue":"synthetic"}}}}]') resource_attributes_json,concat('[{{"key":"payload","value":{{"stringValue":"',repeat('a',128),'"}}}}]') span_attributes_json,
 'traceforge/s13' scope_name,'1' scope_version,'[]' events_json,'[]' links_json,
 '{{}}' resource_json,'{{}}' scope_json,'{{}}' span_json,'' resource_schema_url,'' scope_schema_url,
 toDateTime64('2026-11-01 00:00:00',3,'UTC') ingested_at FROM numbers({n})"""
        # Bounded batches avoid materializing300k expanded JSON rows in one insert.
        for offset in range(0, n, 50000):
            sql(
                insert.replace(
                    f"numbers({n})", f"numbers({offset},{min(50000, n - offset)})"
                ),
                "generate",
                fmt=None,
            )
        sql(
            f"INSERT INTO {db}.spans SELECT * FROM {db}.spans WHERE span_id='0000000000000001' AND cityHash64(trace_id)%1000=0",
            "replay",
            fmt=None,
        )
        sql(
            f"INSERT INTO {db}.spans SELECT * REPLACE(toUInt8(2) AS status_code,ingested_at+INTERVAL 1 SECOND AS ingested_at) FROM {db}.spans WHERE span_id='0000000000000001' AND cityHash64(trace_id)%2000=1",
            "conflicts",
            fmt=None,
        )
        counts = sql(
            f"SELECT count() physical,uniqExact(tuple(trace_id,span_id)) logical,groupBitXor(cityHash64(tuple(*))) checksum FROM {db}.spans"
        )
        for table in ("service_first", "codec"):
            for offset in range(0, n, 50000):
                low = 1790812799000000000 + offset * 1000000
                high = 1790812799000000000 + min(offset + 50000, n) * 1000000
                sql(
                    f"INSERT INTO {db}.{table} SELECT * FROM {db}.spans WHERE start_time>=fromUnixTimestamp64Nano({low},'UTC') AND start_time<fromUnixTimestamp64Nano({high},'UTC')",
                    f"insert-{table}",
                    fmt=None,
                )
            assert (
                sql(
                    f"SELECT count() physical,uniqExact(tuple(trace_id,span_id)) logical,groupBitXor(cityHash64(tuple(*))) checksum FROM {db}.{table}"
                )
                == counts
            )
        sql(
            f"""INSERT INTO {db}.logs SELECT
 lower(hex(MD5(concat(trace_id,span_id,toString(ingested_at))))) log_id,'' source_event_id,toString(trace_id),toString(span_id),toUInt32(1),service_name,
 toUInt8(if(status_code=2,17,9)),'INFO',start_time,start_time,start_time,'event','bounded synthetic log','{{}}','[]','[]','s13','1','', '{{}}','{{}}','{{}}','','',toDateTime64(ingested_at,9) FROM {db}.spans""",
            "logs-generate",
            fmt=None,
        )
        sql(
            f"INSERT INTO {db}.logs_bloom SELECT * FROM {db}.logs",
            "insert-bloom",
            fmt=None,
        )
        # Copy actual Session11/12 normalized streams, preserving full point identity,
        # resets, temporality, explicit buckets and physical replay rows.
        sql(
            f"INSERT INTO {db}.metric_points SELECT * FROM traceforge.metric_points WHERE (service_namespace LIKE 'session11-%' OR service_namespace LIKE 'session12-%') AND end_ns>=1794634000000000000 AND end_ns<=1794638000000000000 ORDER BY stream_id,end_ns,ingested_at,point_json",
            "metric-copy",
            fmt=None,
        )
        trace = sql(f"SELECT trace_id FROM {db}.spans ORDER BY trace_id LIMIT 1")[0][
            "trace_id"
        ]
        base = {
            "db": db,
            "rows": n,
            "ddl": definitions,
            "counts": counts,
            "trace_id": trace,
        }
        base["storage"] = sql(
            f"SELECT table,count() parts,sum(rows) rows,sum(bytes_on_disk) disk_bytes,sum(data_uncompressed_bytes) uncompressed_bytes FROM system.parts WHERE active AND database='{db}' GROUP BY table ORDER BY table"
        )
        base["merges"] = sql(
            f"SELECT table,elapsed,progress FROM system.merges WHERE database='{db}'"
        )
        queries = {
            "trace": f"SELECT * FROM {{table}} WHERE trace_id='{trace}' ORDER BY start_time,span_id,ingested_at",
            "search": "SELECT trace_id,min(start_time),max(end_time),uniqExact(span_id) FROM {table} WHERE start_time >= toDateTime64('2026-09-30 23:59:59',9) AND start_time < toDateTime64('2026-10-01 00:00:02',9) GROUP BY trace_id HAVING countIf(service_name='payment' AND status_code=2)>0 ORDER BY trace_id",
            "red-discovery": "SELECT trace_id FROM {table} WHERE start_time>=toDateTime64('2026-09-30 23:59:59',9) AND start_time<toDateTime64('2026-10-01 00:00:00',9) AND span_kind IN (2,3) GROUP BY trace_id ORDER BY trace_id LIMIT 129",
            "dependency-hydration": f"SELECT * FROM {{table}} WHERE trace_id='{trace}' ORDER BY trace_id,span_id,ingested_at",
            "service-filter": "SELECT count(),sum(duration_ns) FROM {table} WHERE service_name='payment' AND start_time>=toDateTime64('2026-09-30 23:59:59',9) AND start_time<toDateTime64('2026-10-01 00:00:02',9)",
        }
        base["measurements"] = {}
        for name, query in queries.items():
            for table in ("spans", "service_first", "codec"):
                q = query.format(table=f"{db}.{table}")
                base["measurements"][name + "/" + table] = measure(
                    name + "-" + table, q, args.repeats
                )
                base.setdefault("explain", {})[name + "/" + table] = sql(
                    "EXPLAIN indexes=1 " + q, fmt="TabSeparatedRaw"
                )
            assert (
                len(
                    {
                        base["measurements"][name + "/" + t]["checksum"]
                        for t in ("spans", "service_first", "codec")
                    }
                )
                == 1
            )
        for name, query in {
            "correlated-logs": f"SELECT * FROM {{table}} WHERE trace_id='{trace}' ORDER BY effective_time,log_id",
            "recent-logs": "SELECT * FROM {table} WHERE effective_time>=toDateTime64('2026-10-01 00:00:00',9) ORDER BY effective_time,log_id LIMIT 50",
        }.items():
            for table in ("logs", "logs_bloom"):
                q = query.format(table=f"{db}.{table}")
                base["measurements"][name + "/" + table] = measure(
                    name + "-" + table, q, args.repeats
                )
                base["explain"][name + "/" + table] = sql(
                    "EXPLAIN indexes=1 " + q, fmt="TabSeparatedRaw"
                )
            assert (
                base["measurements"][name + "/logs"]["checksum"]
                == base["measurements"][name + "/logs_bloom"]["checksum"]
            )
        for name, q in {
            "metric-stream": "SELECT stream_id FROM {db}.metric_points WHERE end_ns>1794637699000000000 AND end_ns<=1794637719000000000 AND service_name='payment' GROUP BY stream_id ORDER BY stream_id LIMIT 25",
            "slo-points": "SELECT point_json,ingested_at FROM {db}.metric_points WHERE service_namespace='session12-20261114062758' AND end_ns>1794634099000000000 AND end_ns<=1794637719000000000 ORDER BY ingested_at",
        }.items():
            base["measurements"][name] = measure(name, q.format(db=db), args.repeats)
        # Snapshot prototype, not an incremental MV: distinct identities are rebuilt.
        # Only nonconflicting SERVER identities enter this intentionally limited prototype.
        prototype = f"SELECT canonical_service AS service_name,count() requests FROM (SELECT trace_id,span_id,any(service_name) canonical_service FROM {db}.spans WHERE span_kind=2 GROUP BY trace_id,span_id HAVING uniqExact(tuple(service_name,status_code,duration_ns))=1) GROUP BY canonical_service ORDER BY service_name"
        base["preaggregation_reference"] = measure(
            "snapshot-reference", prototype, args.repeats
        )
        sql(
            f"CREATE TABLE {db}.request_snapshot ENGINE=MergeTree ORDER BY service_name AS {prototype}",
            "snapshot-build",
            fmt=None,
        )
        base["preaggregation_snapshot"] = measure(
            "snapshot-read",
            f"SELECT * FROM {db}.request_snapshot ORDER BY service_name",
            args.repeats,
        )
        assert (
            base["preaggregation_reference"]["checksum"]
            == base["preaggregation_snapshot"]["checksum"]
        )
        # Isolated event-time TTL: old/current records, explicit controlled TTL merge.
        sql(
            f"CREATE TABLE {db}.ttl_probe(id UInt8,t DateTime) ENGINE=MergeTree ORDER BY id TTL t+INTERVAL 1 DAY SETTINGS merge_with_ttl_timeout=1",
            "ttl-create",
            fmt=None,
        )
        sql(f"SYSTEM STOP TTL MERGES {db}.ttl_probe", fmt=None)
        sql(
            f"INSERT INTO {db}.ttl_probe VALUES (1,now()-INTERVAL 2 DAY),(2,now())",
            fmt=None,
        )
        base["ttl_before"] = sql(f"SELECT id FROM {db}.ttl_probe ORDER BY id")
        sql(f"SYSTEM START TTL MERGES {db}.ttl_probe", fmt=None)
        sql(
            f"ALTER TABLE {db}.ttl_probe MATERIALIZE TTL SETTINGS mutations_sync=2",
            "ttl-materialize",
            fmt=None,
        )
        base["ttl_after"] = sql(f"SELECT id FROM {db}.ttl_probe ORDER BY id")
        assert base["ttl_after"] == [{"id": 2}]
        report["datasets"].append(base)
        (OUT / "benchmark.json").write_text(
            json.dumps(report, indent=2), encoding="utf8"
        )
        save()
        print(f"completed {n} spans in {db}", flush=True)
    sql("SYSTEM FLUSH LOGS", fmt=None)
    report["query_log"] = sql(
        f"SELECT query_id,query_duration_ms,read_rows,read_bytes,memory_usage,ProfileEvents['SelectedParts'] selected_parts,ProfileEvents['SelectedMarks'] selected_marks FROM system.query_log WHERE type='QueryFinish' AND query_id LIKE 's13-{RUN}-%' ORDER BY event_time_microseconds"
    )
    (OUT / "benchmark.json").write_text(json.dumps(report, indent=2), encoding="utf8")
    save()


if __name__ == "__main__":
    try:
        main()
    finally:
        save()
