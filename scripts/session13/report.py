"""Publish measured local evidence (no credentials) into project documentation."""

import hashlib
import json
import shutil
import statistics
from pathlib import Path

ROOT = Path("artifacts/session13")
DEST = Path("docs/benchmarks/session13")
DEST.mkdir(parents=True, exist_ok=True)
b = json.loads((ROOT / "benchmark.json").read_text())
a = json.loads((ROOT / "api-overhead.json").read_text())
s = json.loads((ROOT / "sampling.json").read_text())
raw = json.loads((ROOT / f"queries-{b['run']}.json").read_text())
by_id = {q["query_id"]: q for q in raw}
for source, target in (
    ("benchmark.json", "benchmark.json"),
    (f"queries-{b['run']}.json", "queries.json"),
    ("api-overhead.json", "api.json"),
    ("sampling.json", "sampling.json"),
    ("mixed.json", "mixed.json"),
    ("concurrent-ingestion.json", "concurrent-ingestion.json"),
    ("tail-telemetry.txt", "tail-telemetry.txt"),
    ("audit.json", "audit.json"),
    ("expiry.json", "expiry.json"),
    ("index-cache.json", "index-cache.json"),
    ("correctness-final.txt", "correctness.txt"),
):
    shutil.copyfile(ROOT / source, DEST / target)
index_case = json.loads((ROOT / "index-cache.json").read_text())
index_run = index_case["query_log"][0]["query_id"].split("-")[1]
shutil.copyfile(ROOT / f"queries-{index_run}.json", DEST / "index-queries.json")
environment = {
    name: json.loads((ROOT / f"{name}.json").read_text())
    for name in ("host-cpu", "host-memory", "docker-environment", "clickhouse-limits")
}
(DEST / "environment.json").write_text(json.dumps(environment, indent=2))
failures = []
for file in ROOT.glob("queries-*.json"):
    failures += [q for q in json.loads(file.read_text()) if "error" in q]
(DEST / "failed-queries.json").write_text(json.dumps(failures, indent=2))
lines = [
    "# Session13 measured local storage and sampling experiments",
    "",
    "Final SQL run: `"
    + b["run"]
    + "`. Raw DDL, query text/IDs, checksums, EXPLAIN, query-log read/memory/parts/marks and failure records are retained in [session13/](session13/). HTTP timings include client/network overhead; DB durations below come from QueryFinish records. No cold-cache claim.",
    "",
    "## Method",
    "",
    "Windows host: i5-12500H,12 physical cores/16 logical processors,16,781,885,440 bytes RAM. Docker29.9.2/WSL2 Linux exposes16 CPUs and8,130,514,944 bytes RAM. ClickHouse26.8.36.3-alpine has no additional per-container CPU/RAM limit. SQL queries use2 threads,20s,512MiB memory/read budgets,3M read rows, query cache disabled. One warmup then10 sequential repetitions per workload/layout; p95 is nearest-rank (the maximum at n=10). OS/page caches were not flushed. Other local services and background merges can affect timings; layouts run sequentially, not a randomized capacity study.",
    "",
    "Seed42 SQL fixtures contain five spans/trace, gateway/payment/worker, bounded128-byte attribute payloads, actual resource identity, delayed/error patterns, exact replay and later-ingested conflicts. SQL-generated records exercise stored-record semantics; they are not claimed as OTLP ingestion throughput. Candidate full rows and physical/logical fingerprints match. Separate real Go/API/SDK experiments cover ingestion and semantics.",
    "",
    "## Same-dataset query comparisons",
    "",
    "Workload names describe the actual SQL stages, not a substitute for Go canonicalization. RED discovery/dependency hydration are stages; service-filter is a raw physical-row query. Exact Go engine equality is tested separately.",
    "",
    "|Rows|Workload|Layout|HTTP p50/p95 ms|DB p50/p95 ms|Median rows/bytes read|Median peak query memory|",
    "|---:|---|---|---:|---:|---:|---:|",
]
for d in b["datasets"]:
    if d["rows"] != b["datasets"][0]["rows"]:
        lines += [
            "### Additional dataset query comparison",
            "",
            "|Rows|Workload|Layout|HTTP p50/p95 ms|DB p50/p95 ms|Median rows/bytes read|Median peak query memory|",
            "|---:|---|---|---:|---:|---:|---:|",
        ]
    for name, values in d["measurements"].items():
        label, _, table = name.partition("/")
        metrics = []
        for q in b["query_log"]:
            entry = by_id.get(q["query_id"], {})
            if entry.get("label") == label + ("-" + table if table else "") and d[
                "db"
            ] + "." in entry.get("sql", ""):
                metrics.append(q)
        if metrics:
            times = sorted(x["query_duration_ms"] for x in metrics)
            dbtime = f"{statistics.median(times):.2f}/{times[-1]:.2f}"
            reads = f"{statistics.median(x['read_rows'] for x in metrics):.0f}/{statistics.median(x['read_bytes'] for x in metrics):.0f}"
            memory = f"{statistics.median(x['memory_usage'] for x in metrics):.0f}"
        else:
            dbtime = reads = memory = "unavailable"
        lines.append(
            f"|{d['rows']}|{label}|{table or 'reference'}|{values['p50_ms']:.2f}/{values['p95_ms']:.2f}|{dbtime}|{reads}|{memory}|"
        )
    lines += [
        "",
        f"### {d['rows']} storage snapshot",
        "",
        "|Table|Parts|Physical rows|Disk bytes|Uncompressed bytes|Bytes/row|Compression ratio|",
        "|---|---:|---:|---:|---:|---:|---:|",
    ]
    for row in d["storage"]:
        lines.append(
            f"|{row['table']}|{row['parts']}|{row['rows']}|{row['disk_bytes']}|{row['uncompressed_bytes']}|{row['disk_bytes'] / row['rows']:.2f}|{row['uncompressed_bytes'] / row['disk_bytes']:.2f}|"
        )
    lines += [
        "",
        f"Membership fingerprint (CityHash64 tuple XOR, not a cryptographic proof): `{d['counts']}`. SHA256 of this dataset's DDL/config: `{hashlib.sha256(json.dumps({'ddl': d['ddl'], 'seed': 42, 'rows': d['rows']}, sort_keys=True).encode()).hexdigest()}`. Equality also checks sorted query-result SHA256 and production Go canonical output.",
        "",
        f"TTL probe before: `{d['ttl_before']}`; after explicit isolated MATERIALIZE TTL: `{d['ttl_after']}`. Existing project telemetry has no destructive TTL.",
        "",
        f"Snapshot prototype: reference HTTP p50/p95 {d['preaggregation_reference']['p50_ms']:.2f}/{d['preaggregation_reference']['p95_ms']:.2f}ms; snapshot read {d['preaggregation_snapshot']['p50_ms']:.2f}/{d['preaggregation_snapshot']['p95_ms']:.2f}ms. Identical result checksum. This conflict-free subset rebuild is experimental, not a production incremental materialized view.",
        "",
    ]
lines += [
    "## Actual Go API and ingestion",
    "",
    "Isolated300k database plus later controlled fixture copies. Ten observations each; complete endpoints include application work, unlike SQL stage timings. The metric population includes the full required historical S12 scope; physical replays remain canonical. No silent query truncation.",
    "",
    "|API|p50 ms|p95 ms|n|",
    "|---|---:|---:|---:|",
]
for name, row in a["api"].items():
    times = sorted(x["ms"] for x in row["runs"])
    lines.append(
        f"|{name}|{statistics.median(times):.2f}|{times[-1]:.2f}|{len(times)}|"
    )
load = json.loads((ROOT / "concurrent-ingestion.json").read_text())["load"]
lines += [
    "",
    f"Concurrent Session4 loadgen: {load['generated_spans']} offered/acknowledged spans, {load['successful_requests']} successful RPCs, {load['rejected_spans']} rejected, {load['unknown_outcomes']} unknown; {load['acknowledged_spans_per_second']:.2f} acknowledged spans/s; p50/p95 ACK {load['p50_ack_ms']:.2f}/{load['p95_ack_ms']:.2f}ms. Thirty concurrent actual trace API reads returned200. This is one local trace-ingestion run, not production capacity.",
    "",
    "A separate real mixed-signal SDK workload offered100 checkouts while60 trace/log/metric API requests ran concurrently; all reads returned200 and all100 traces/five spans plus independent100-request/10-error counters were observed. See mixed.json.",
    "",
    "Five paired10k-row log SQL insert batches per layout (same rows, persistence-confirmed HTTP completion):",
]
for name, x in a["paired_inserts"].items():
    lines.append(
        f"- {name}: p50/p95 ACK {x['p50_ack_ms']:.2f}/{x['p95_ack_ms']:.2f}ms; {x['counts']}."
    )
lines += [
    "",
    f"Existing-main log index backfill preserved physical count/content fingerprint: `{a['main_backfill']}`. This is additive index materialization, not raw-row deletion or a synchronous-retention promise.",
    "",
    "## Sampling",
    "",
    "Each profile offered80 normal,10 delayed150ms and10 failed actual requests; two services propagated one root trace identity. Observed retained counts:",
    "",
    "|Profile|Normal|Delayed|Failed|Total roots|Stored spans|Direct requests/errors per service|",
    "|---|---:|---:|---:|---:|---:|---|",
]
for profile in ("always", "head", "tail"):
    x = s[profile]
    c = x["retained_by_kind"]
    lines.append(
        f"|{profile}|{c['normal']}|{c['delayed']}|{c['failed']}|{len(x['stored'])}|{sum(v['spans'] for v in x['stored'])}|100/10|"
    )
probes = s["probes"]
lines += [
    "",
    f"Late-span probe:20 offered healthy roots, {len(probes['late_before'])} retained before late errors; {len(probes['late_after'])} trace IDs afterward. Cached drop decisions did not change to retain late errors. Pressure:400 simultaneous error roots, {len(probes['pressure_stored'])} stored at the finite check; telemetry reported272 early buffer drops and Collector logs additionally reported exporter queue-full losses. These counters refer to different pipeline stages, not disjoint end-to-end guarantees. Restart probe retained one error child without its earlier buffered parent: `{probes['restarted_trace']}`.",
    "",
    "Tail sampling kept10/10 errors only under the low-pressure test. The retained fraction10/28=35.714…% differs from actual10/100=10%; use direct metrics for availability. Head20% selected19/100 roots in this run, not a promised quota. No paid/live provider calls.",
    "",
    "## Decision and limitations",
    "",
    "Adopt only the log trace-ID bloom index: EXPLAIN pruned38 granules to1 at300k; the same result and small disk overhead support reduced scanning. HTTP/API p95 did not consistently improve, so no blanket latency speedup is claimed. Service-first traces trade exact-lookup scans/storage for some service scans and remain experimental. ZSTD payload codec measurements are confounded by part/merge state and highly repeated synthetic payloads; not adopted. Snapshot preaggregation excludes conflicting identities and is not an equivalent general incremental design; production canonical Go engines remain authoritative.",
    "",
    "The first SQL dataset lacked resource identity and was excluded from nonempty RED conclusions. Expanded300k generation and service-first backfill each hit512MiB limits; bounded50k batches resolved them. Failed databases/queries were retained. A duplicate-index harness failure after migration was fixed by stripping the adopted index from reference DDL. Invalid bucket_seconds=1 was corrected to supported60. A Session7 regression exposed independent trace/log export timing; a finite log-visibility poll replaced the assumption that trace visibility guarantees logs. No failing measurements were silently discarded.",
    "",
    "No cluster deployment, destructive retention of project evidence, traffic extrapolation, sampling completeness guarantee, production capacity or Session14 security claim. See session13 journal for all checks and reproduction commands.",
]
Path("docs/benchmarks/session-13.md").write_text(
    "\n".join(lines) + "\n", encoding="utf8"
)
with Path("docs/benchmarks/session-13.md").open("a", encoding="utf8") as doc:
    doc.write(
        "\n## Query-condition cache control and raw-span expiry\n\nThe initial warm SQL comparison left the default query-condition cache enabled. Repeated baseline correlation reads consequently fell to8192 rows too: warmed read-row savings cannot be attributed to the bloom index. A separate identical-data experiment disabled that cache for both layouts while retaining warm OS caches. Its full queries, IDs, EXPLAIN and metrics are in index-cache.json/index-queries.json. Adoption applies to uncached/new trace predicates, not guaranteed repeated-query gains.\n\n"
    )
    doc.write(
        "|Database size|Reference p50/p95 HTTP ms|Bloom p50/p95 HTTP ms|Reference/bloom rows read|\n|---|---:|---:|---:|\n"
    )
    for i, case in enumerate(index_case["datasets"]):
        reference = case["measurements"]["reference"]
        bloom = case["measurements"]["bloom"]
        reference_rows = index_case["query_log"][i * 22]["read_rows"]
        bloom_rows = index_case["query_log"][i * 22 + 11]["read_rows"]
        doc.write(
            f"|{case['database'].split('_')[-1]}|{reference['p50_ms']:.2f}/{reference['p95_ms']:.2f}|{bloom['p50_ms']:.2f}/{bloom['p95_ms']:.2f}|{reference_rows}/{bloom_rows}|\n"
        )
    expiry = json.loads((ROOT / "expiry.json").read_text())
    doc.write(
        f"\nA copied real fixture trace in `{expiry['database']}` returned200 before a1-hour event-time TTL, then404 after explicitly controlled TTL processing/one tiny isolated FINAL merge. A recent copied trace still returned200; frozen PostgreSQL evaluation `{expiry['evaluation']['id']}` remained200 with the expired trace ID. Browser verification forwarded that one trace API request to the real isolated backend, then showed the honest missing-evidence state. No fabricated response or main-table expiry. Shared-part per-profile disk attribution remains unavailable; actual serialized row bytes and CPU profiles are in audit.json.\n"
    )
