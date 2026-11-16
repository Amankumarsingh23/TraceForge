# Sampling profiles and evidence boundaries

Default development remains unsampled. Go supports `OTEL_TRACES_SAMPLER=always_on` (or unset) and opt-in `parentbased_traceidratio` with `OTEL_TRACES_SAMPLER_ARG` strictly0..1. Invalid/unknown profiles fail startup. Parent-based sampling respects sampled and unsampled remote parents after W3C propagation; the root's random trace ID determines head selection. A fixed256-ID test at25% selects64 deterministically; the actual random-root20% experiment selected19/100, not an exact quota. No metric or log sampler was added.

Python AgentOps/evaluation's default SDK provider now explicitly uses ALWAYS_ON, preserving protected runs even if an ambient SDK environment requests always_off. Injected test providers remain supported. Use the normal unsampled Collector endpoint for these runs. Explicitly routing a protected run through a dropping Collector still loses traces; SDK ALWAYS_ON cannot override downstream tail sampling. A missing link remains missing, and known usage never becomes zero merely because a trace is unavailable.

## Separate tail profile

The pinned core Collector0.165.0 does not include tail_sampling; the matching contrib0.165.0 distribution does. Base Compose and Collector configuration remain unchanged. Start only the separate opt-in profile:

```powershell
docker compose -f compose.yaml -f compose.sampling.yaml --profile sampling up -d sampling-collector
```

It exposes loopback4321 gRPC,4322 OTLP HTTP (controlled probes) and8890 internal metrics. Container:1 CPU/192MiB; memory_limiter checks each1s at128MiB plus32MiB spike allowance. Tail policy buffers at most128 traces, decision_wait2s, expected_new_traces_per_sec100 (sizing estimate, not admission rate), sampled/non-sampled decision caches2048 each. Exporter queue32/one consumer and15s retry lifetime are bounded; memory limits and queues can still reject/drop data.

Policies retain observed ERROR traces, observed duration>=100ms traces, or a10% hash-based normal subset. These three non-inverted, non-drop top-level policies act as retain-if-any-matches under the tested processor flow. It is not an AND rule and does not retain all successful traces. The versioned [processor README](https://github.com/open-telemetry/opentelemetry-collector-contrib/blob/v0.165.0/processor/tailsamplingprocessor/README.md) was checked and actual errors/delays/normal, late spans, pressure and restart exercised. Configuration validation initially rejected retry max_elapsed_time below the default max_interval; explicitly bounded initial/max intervals fixed it.

All demo traces reach one Collector instance, so the same trace-ID buffer sees gateway/payment spans. Multiple Collector replicas require trace-ID-affine routing before this stateful stage; ordinary round-robin balancing can split traces. No distributed routing or durable sampling state was added.

## Actual measured populations

Each profile offered80 normal,10 delayed150ms and10 failed checkouts. Always-on stored100 roots/500 spans. Head20% stored19 roots/95 spans (16 normal,0 delayed,3 failed). Tail stored28 roots/140 spans (8 normal,10 delayed,10 failed). Every retained low-pressure trace had five spans and both services. This is observed structural consistency, not universal trace completeness.

Independent direct counters reported100 requests/10 errors per service in all three profiles. Actual Go RED APIs report the retained populations, respectively100/10,19/3 and28/10 per service. Tail's10/28=35.714…% observed error fraction is outcome-biased; actual direct workload error fraction is10/100=10%. Head19/3 is a finite random sample, not a precise traffic-rate estimate. Service availability/latency SLOs retain the direct metric population and Session12 coverage rules.

Under20 normal-root late-span probes,4 trace IDs were initially retained; late error children did not change the cached dropped traces into retained traces. A burst of400 error roots exceeded capacity: telemetry reported272 early buffer evictions, and downstream queue-full messages explain additional losses; only28 were stored at the finite check. These are different pipeline stages, not a claim that every rejected/retained decision is a durable stored trace. After restart, a late error child was stored without its earlier buffered parent. State is volatile, and SDK/Collector retries/ACKs do not promise complete tail evidence. No all-errors guarantee is made.

Shared compressed parts do not permit honest per-profile compressed-disk attribution. The audit records actual serialized stored row bytes per offered trace-ID population and table disk snapshots separately; serialized bytes are not compressed disk bytes. Raw query IDs, actual API RED output, root identities, counters and internal telemetry are committed with the benchmark report.

Stop only this optional service after experiments:

```powershell
docker compose -f compose.yaml -f compose.sampling.yaml stop sampling-collector
```

No paid calls, live-provider reliability claim, source-traffic extrapolation from tail samples, production routing or automatic retention was introduced.
