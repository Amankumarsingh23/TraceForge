"""Capture schema, CPU profiles and actual sampling-aware RED API populations."""

import json
import urllib.parse
import urllib.request

from benchmark import OUT, save, sql

b = json.loads((OUT / "benchmark.json").read_text())
s = json.loads((OUT / "sampling.json").read_text())
result = {"schema": {}, "sampling": {}}
for table in ("spans", "logs", "metric_points"):
    result["schema"][table] = sql(
        f"SHOW CREATE TABLE traceforge.{table}", fmt="TabSeparatedRaw"
    )
result["cpu_profiles"] = sql(
    f"SELECT query_id,ProfileEvents['UserTimeMicroseconds'] user_cpu_us,ProfileEvents['SystemTimeMicroseconds'] system_cpu_us FROM system.query_log WHERE type='QueryFinish' AND query_id LIKE 's13-{b['run']}-%' ORDER BY event_time_microseconds"
)
for profile in ("always", "head", "tail"):
    ids = ",".join(repr(x["trace_id"]) for x in s[profile]["offered"])
    population = sql(
        f"SELECT count() physical_rows,sum(byteSize(tuple(*))) serialized_row_bytes,formatDateTime(min(start_time),'%Y-%m-%dT%H:%i:%SZ') AS f,formatDateTime(max(end_time)+INTERVAL 1 SECOND,'%Y-%m-%dT%H:%i:%SZ') AS t FROM traceforge.spans WHERE trace_id IN ({ids})"
    )[0]
    query = urllib.parse.urlencode(
        {
            "from": population["f"],
            "to": population["t"],
            "namespace": s[profile]["namespace"],
            "bucket_seconds": 60,
        }
    )
    with urllib.request.urlopen(
        "http://127.0.0.1:8082/api/v1/services?" + query, timeout=5
    ) as response:
        analytics = json.load(response)
    for service in analytics["services"]:
        assert service["stats"]["count"] == len(s[profile]["stored"])
        assert service["stats"]["errors"] == s[profile]["retained_by_kind"]["failed"]
    assert len(analytics["services"]) == 2
    result["sampling"][profile] = {
        "population": population,
        "actual_red_api": analytics,
    }
(OUT / "audit.json").write_text(json.dumps(result, indent=2))
save()
