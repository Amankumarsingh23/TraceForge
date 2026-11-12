# Session 4 — Short local batching experiments

These are actual observations of one bounded workload, not sustainable-capacity or production sizing claims. All repetitions are included in [session-04.csv](session-04.csv). Raw JSON reports remain locally in ignored `artifacts/session04-benchmarks/` and include run prefixes for independent SQL accounting.

## Environment and method

- Windows host: Intel Core i5-12500H, 12 cores/16 logical processors, 16,781,885,440 bytes physical RAM (about 15.6GiB).
- Docker Desktop Linux engine 29.9.2: 16 CPUs visible, 8,130,523,136 bytes memory available to the engine. Compose 5.6.1.
- Receiver: Go 1.27.4 Linux amd64, 2 CPU limit, 256MiB container memory limit, GOMEMLIMIT=192MiB. ClickHouse 26.8.36.3 on the existing shared volume without new per-DB CPU/memory limits. PostgreSQL/Collector/main receiver remained up.
- Windows Go 1.27.4 load generator, four shared gRPC connections, 32 concurrent exports, 50 spans/export, 600 requests = 30000 spans per measured repetition. Seed 42, three synthetic services, a unique recorded prefix and base timestamp per run, no automatic retries.
- Configured offered rate 10000 spans/sec. Actual offered/acknowledged rates are separate report fields: when callers block, this closed-loop generator can fall behind the configured schedule. Do not substitute configured rate for achieved throughput.
- One worker for every configuration; queue 64, in-flight 32, owned jobs 128, span budget 16384, conservative byte budget 128MiB. Per-request control uses target 1/wait 0 with the same queue/worker/admission settings, not the previous binary's exact four-parallel-insert topology.
- Each configuration received a 20-request/1000-span warm-up with eight clients, then two measured repetitions. Configurations ran in the documented fixed order. No compiler/race-build workload ran concurrently with the final matrix. Runs lasted roughly 3.0–4.2 seconds; the short duration, order, shared database/background merges and laptop variability limit conclusions.
- All ten runs independently queried physical rows and distinct `(trace_id,span_id)` counts after pending work settled. All generated/acknowledged/stored/unique counts were 30000. No final-run admission rejections or physical duplicates occurred.

## Results — ranges across the two repetitions

| Configuration | Target/wait | Confirmed rows/sec | p95 successful acknowledgment, ms | Mean actual batch spans |
| --- | --- | ---: | ---: | ---: |
| Per-request control | 1 / 0ms | 7206–8012 | 242.12–375.59 | 50 |
| Small | 100 / 20ms | 9952–9973 | 62.74–92.75 | 100 |
| Medium | 500 / 50ms | 9809–9971 | 66.53–73.25 | 491.80–500 |
| Large | 1000 / 100ms | 9641–9687 | 111.48–120.89 | 967.74 |
| Heavy | 5000 / 100ms | 9691–9729 | 127.59–132.30 | 1034.48 |

Ranges are not pooled percentiles. The CSV records every run's acknowledgment p50/p95/p99, insert p50/p95, actual offered/acknowledged rates, durations, batch averages, queue samples, memory, reservation high-water marks and part snapshots. Quantiles use nearest rank; acknowledgment quantiles cover successful/partial responses, while all-outcome p95 is also in raw JSON. Zero successful samples means no successful-ack percentile, not zero-latency success.

Confirmed rows/sec uses independently counted physical rows divided by the load-plus-settlement window; acknowledgment throughput uses the load interval. The independent SQL query's execution time is excluded from the storage window. Batch insert samples are exact bounded recent-sample observations, not bucket-interpolated histogram estimates. Sample coverage was complete for every final run (fewer than 2048 relevant samples).

| Configuration | Maximum sampled Go heap | Maximum sampled process RSS | Maximum sampled queue depth | Reservation high-water |
| --- | ---: | ---: | ---: | ---: |
| Per-request | 8.05MiB | 29.58MiB | 31 | 68.24MiB |
| 100 | 5.98MiB | 27.57MiB | 18 | 53.31MiB |
| 500 | 7.70MiB | 31.16MiB | 10 | 55.45MiB |
| 1000 | 10.46MiB | 31.93MiB | 2 | 68.24MiB |
| 5000 | 12.30MiB | 34.75MiB | 13 | 68.24MiB |

Heap/RSS/queue are 100ms samples and may miss short peaks. Reservation high-water is tracked on every resource change, but is a conservative payload accounting unit, not measured heap. Runtime goroutine samples peaked at 57 across the final matrix. All final reports ended with zero pending jobs/reserved bytes. Active database parts were 4–8 at post-run snapshots; background merging and shared-table history mean these snapshots do not establish per-run part growth or creation counts.

## Interpretation and excluded pilot

At this offered rate, 100/500-span targets served the workload near its configured rate with lower acknowledgment latency than the larger targets. The 5000 target averaged about 1034 spans because its timer fired before the target filled; a larger target is not an achieved batch size. The workload does not prove how far throughput can rise beyond 10000 offered spans/sec or how long it remains stable. The initial learning default of 1000/100ms is retained; this matrix is insufficient for a universal default recommendation.

An earlier 300-request pilot overlapped a Linux compilation/race build, so its performance numbers are not mixed into this matrix. It also exposed a backlog issue: already-expired jobs were flushed individually instead of combining already-ready work. The final implementation drains ready jobs without additional waiting, and a deterministic regression test covers this case. Pilot reports remain under ignored `artifacts/session04-pilot-benchmarks/`; one pilot returned three temporary rejections and stored exactly its 14850 acknowledged spans. No pilot failure is represented as a final-run pass.

## Reproduce and inspect

```powershell
$env:PATH = "$env:LOCALAPPDATA\TraceForge-tools\go\bin;$env:PATH"
docker compose up -d --build --wait --wait-timeout 180
.\scripts\benchmark-session04.ps1 -Requests 600 -Repetitions 2 -Rate 10000
```

The script builds the load generator, recreates only the optional receiver on 4320/8083, warms each configuration, records JSON/CSV and stops that receiver in finally. Keep other offered traffic/builds idle during comparisons. Actual `.env` is read without printing/rewriting it. The main pipeline remains on 4317/4319/8082.

Example independently verifiable final run:

```sql
SELECT count(), uniqExact(tuple(trace_id, span_id))
FROM traceforge.spans
WHERE startsWith(trace_id, '97494d8c2a8a686d');
-- observed: 30000 physical rows, 30000 unique spans
```

Every measured prefix is in the CSV. Benchmark rows are retained. Optional cleanup is restricted to a report's validated prefix plus synthetic service/tag markers:

```powershell
.\scripts\cleanup-load.ps1 -ReportPath artifacts/session04-benchmarks/target-500-1.json
```

This deletes that run's synthetic rows; it does not remove the historical gateway/payment demonstration. Cleanup was separately exercised against the initial 800-span probe, not the final matrix. Larger/longer saturation sweeps, CPU profiling, cold-cache studies and statistically stronger repetitions remain unperformed; no results are invented for them.
