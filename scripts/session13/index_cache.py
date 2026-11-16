"""Separate index pruning from the default warm query-condition cache."""

import json
import time

from benchmark import OUT, RUN, ddl, measure, save, sql

b = json.loads((OUT / "benchmark.json").read_text())
report = {
    "settings": "use_query_condition_cache=0;use_query_cache=0;warm OS caches;threads2",
    "datasets": [],
}
for d in b["datasets"]:
    db = d["db"]
    table = "logs_uncached_" + str(time.time_ns())
    ddl(db, table, "logs")
    sql(
        f"INSERT INTO {db}.{table} SELECT * FROM {db}.logs",
        "index-baseline-copy",
        fmt=None,
    )
    item = {"database": db, "reference_table": table, "measurements": {}, "explain": {}}
    for label, name in (("reference", table), ("bloom", "logs_bloom")):
        query = f"SELECT * FROM {db}.{name} WHERE trace_id='{d['trace_id']}' ORDER BY effective_time,log_id SETTINGS use_query_condition_cache=0"
        item["measurements"][label] = measure("cache-controlled-" + label, query, 10)
        item["explain"][label] = sql(
            "EXPLAIN indexes=1 " + query, fmt="TabSeparatedRaw"
        )
    assert (
        item["measurements"]["reference"]["checksum"]
        == item["measurements"]["bloom"]["checksum"]
    )
    item["storage"] = sql(
        f"SELECT table,count() parts,sum(rows) rows,sum(bytes_on_disk) disk_bytes FROM system.parts WHERE active AND database='{db}' AND table IN ('{table}','logs_bloom') GROUP BY table ORDER BY table"
    )
    report["datasets"].append(item)
sql("SYSTEM FLUSH LOGS", fmt=None)
report["query_log"] = sql(
    f"SELECT query_id,query_duration_ms,read_rows,read_bytes,memory_usage,ProfileEvents['SelectedParts'] selected_parts,ProfileEvents['SelectedMarks'] selected_marks FROM system.query_log WHERE type='QueryFinish' AND query_id LIKE 's13-{RUN}-cache-controlled-%' ORDER BY event_time_microseconds"
)
(OUT / "index-cache.json").write_text(json.dumps(report, indent=2))
save()
