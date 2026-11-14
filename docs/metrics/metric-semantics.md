# OTLP metrics contract — otlp-metrics/v1

This is direct application metric ingestion, separate from Session 10's canonical SERVER-span analytics and the receiver's existing Prometheus self-monitoring endpoints. Metric observations also have sampling/export/configuration/loss limitations; no complete production traffic or SLO guarantee is implied.

## Supported wire model and application instruments

Official OTLP/gRPC MetricsService traverses all ResourceMetrics → ScopeMetrics → Metrics → DataPoints. V1 accepts Gauge, monotonic/non-monotonic Sum with DELTA(1) or CUMULATIVE(2), and explicit-bucket Histogram with either temporality. Unsupported ExponentialHistogram/Summary points are counted individually as rejected. Unsupported exemplars, metric metadata, resource EntityRefs, dropped resource/scope identity attributes, nested/bytes attribute values and invalid points are rejected explicitly rather than acknowledged as stored. Empty groups contain no rejectable data points.

The Go demo creates a real MeterProvider, periodic reader (default2s; `--metric-interval` 200ms–1m), OTLP metric exporter and independent instruments:

| Name | Instrument / wire type | Unit | Recording rule |
|---|---|---|---|
| traceforge.requests.completed | Counter / monotonic Sum | {request} | completed fixed-route GET handler |
| traceforge.requests.errors | Counter / monotonic Sum | {request} | same completion, HTTP response ≥500 |
| traceforge.request.duration | Histogram | s | wall duration of that handler |
| traceforge.requests.active | UpDownCounter / non-monotonic Sum | {request} | +1 on entry, −1 on completion |
| traceforge.process.goroutines | observable Gauge | {goroutine} | actual runtime.NumGoroutine at collection |

Only `/checkout` and `/charge` templates with method GET are measured. No raw paths, IDs, user input or prompts are dimensions. Views allow route/method/outcome and configure explicit duration boundaries `[.001,.005,.01,.025,.05,.1,.25,.5,1,2,5]` seconds. SDK cardinality limit32 includes its overflow behavior; exemplars are disabled because V1 rejects them. One view applies per instrument to avoid duplicate streams. otelhttp does not receive this MeterProvider, preventing accidental duplicate custom HTTP instrumentation. Metric recording is independent of trace sampling; no trace sampler was changed in this session.

One random `service.instance.id` is stable for the process lifetime and shared by trace/log/metric resources; restart changes it. Existing service name, optional namespace, local environment and explicit synthetic origin remain. A completed metric operation and a SERVER span's start are different measurement events. Duration middleware placement can also differ slightly from the span boundary. Shutdown calls ForceFlush and Shutdown with bounded contexts and reports unconfirmed export failures. SDK/Collector admission alone is not proof of final database persistence.

## Validation, privacy and exact representations

End timestamp must be positive and fit signed nanosecond time. Start≤end. DELTA requires a known positive start strictly before end. CUMULATIVE permits unknown start0 or start=end as baseline evidence. Gauges have no Sum-style temporality. Finite numeric doubles only; monotonic Sum values cannot be negative. Signed non-monotonic values are retained.

Attributes are ordered deterministically by unique key. V1 accepts string/int/double/bool scalars, at most32 attributes/set, keys≤128 bytes, string values≤256, encoded set≤8KiB. Metric name≤128, unit≤64, description≤512; scope name/version≤128 and schema URLs≤256. Recognized sensitive identity keys and unsafe strings are **rejected**, using existing text sanitation checks and a conservative sensitive-key/value policy. They are not redacted into a common identity. This is limited local protection, not comprehensive DLP or authorization.

Histograms have at most64 finite strictly increasing boundaries, exactly bounds+1 bucket counts when a distribution is supplied, overflow-safe UInt64 count sum equal to Count, finite optional sum/min/max and min≤max. Empty histogram sum must be zero; empty min/max are rejected. No-recorded-value histogram points may omit the distribution. Unknown flags are rejected. A no-recorded-value point is never a zero sample. For Gauge it clears the latest value; for cumulative conversion it does not advance/reset the last valid baseline, allowing a later compatible cumulative point to recover a combined increment while preserving the missing-value diagnostic.

Wire Int64 values, UInt64 histogram counts/buckets and Unix nanoseconds remain exact decimal **strings** in API JSON. Floating values remain doubles. Integral arithmetic uses big integers/rationals before display; non-integral derived decimal text uses significant-digit formatting rather than fixed decimals that could turn small positive values into zero. Rates/means/plots are approximate floating summaries. Raw exact integers and full nanosecond timestamps remain inspectable. Absent optional values remain null.

## Stream, point and conflict identities

Canonical stream key includes the sorted typed resource attribute set (including writer ID), instrumentation scope name/version/typed attributes, resource/scope schema URLs, metric name, unit, type, temporality, monotonicity and sorted typed point attributes. Description is preserved, but is not a stream-defining property. A base family key identifies the same resource/scope/name to detect conflicting intrinsic definitions. Number representation is preserved in each point; int1 and double1 observations at one logical point are not silently declared exact replay.

Stream ID is SHA-256 of the **full canonical key**, which is also stored and compared during admission/aggregation. Hash/key disagreements or collisions are errors, never automatic merges. Service name/writer IDs must be supplied as strings; namespace/environment/workload identity attributes must also be strings when present. Missing or incorrectly typed source identity is rejected. This cannot identify two physical writers incorrectly configured with identical identities, or universally deduplicate underlying source events.

Point identity is stream+end for Gauge/CUMULATIVE, and stream+start+end for DELTA. Start is retained as cumulative sequence metadata, so contradictory starts at one cumulative timestamp are conflicts. Exact replay remains physical evidence but counts once. Canonical conflicts select earliest database ingestion, then lexicographically smallest full normalized point content excluding ingestion time. All variants contribute replay/conflict diagnostics; arrival ordering does not change results. Affected series remain unreliable and combined aggregates are withheld. Conflicting type/unit/temporality/monotonic definitions are also flagged; exact-stream selection occurs after this detection.

## Time and temporal arithmetic

Query point ends use UTC **(from,to]**, unlike Session 10's span-start `[from,to)`. Only fully covered incremental intervals with start≥from and end≤to contribute. A crossing interval is not proportionally split and called exact. Right-crossing/not-yet-ended intervals are outside the available point selection; incomplete coverage or no observations remains visible. Choose bounds around completed exports when comparing signals.

CUMULATIVE points are sorted within each source stream and compatible sequence. Adjacent observations with equal sequence start are differenced. A first point with reliable nonzero sequence start inside the selected window can contribute from the known zero accumulation origin; otherwise the first observation is a baseline, not a historical request count. Unknown start0 permits conditional adjacent differences, but marks the series unreliable and withholds combined/full-window rates. Missing intermediate cumulative exports can preserve a combined increment when uninterrupted compatible metadata supports it; finer timing is lost.

A changed positive sequence start at/after the preceding end is a confirmed new zero accumulation sequence. Its current cumulative value belongs to the new interval; any intervening gap remains unknown. A monotonic decrease without that evidence is unreliable, never a negative rate or an invented reset. Distinct writer IDs normalize independently; raw readings from different instances are never subtracted from each other.

DELTA points already describe intervals. Exact replay is removed first. Every member of an overlapping interval component is excluded from the trusted subtotal, with overlap diagnostics; it is not arbitrarily summed. Disjoint intervals contribute signed/unsigned changes as appropriate. Gaps remain gaps. A non-monotonic decrease is a legitimate signed change, not automatically a reset. DELTA changes do not reconstruct absolute current state. CUMULATIVE non-monotonic raw/latest observed state is retained separately from derived signed changes.

Gauge latest/min/max/arithmetic mean summarize valid observations in the selected window. They are not event totals, time-weighted averages or indefinitely carried current state. No in-window valid observation means null. Explicit no-value after a valid observation clears latest. Zero is still a legitimate observed value.

Per-series observed rate is increment / sum of eligible disjoint interval seconds. Coverage fraction compares these seconds with selected seconds; it does not estimate traffic capture. `complete` means supported interval/diagnostic coverage within the selected observations, **not** complete traffic. Gauges have no interval coverage claim. Compatible attributed Sum streams combine only after writer normalization; no combined full-window rate is provided unless every included source fully covers that window. Unknown/conflicting/overflow populations cannot manufacture a trusted combined result.

## Histogram arithmetic and bounds

CUMULATIVE histogram count/bucket counts are differenced only with equal boundary definitions and compatible sequences. Sums are differenced only when both are present/finite. Cumulative min/max are retained raw and never subtracted into interval extrema. Different boundaries are incompatible; V1 performs no rebinning. Negative bucket differences are unreliable. Sum/count mean is null when unavailable or count0.

Merge only compatible incremental distributions: same name/service/namespace/environment/workload/scope/unit/boundaries and exact point dimensions, with independently identified writers and no conflicting/overlapping source definitions. Temporal conversion happens first. Merge adds counts, not individual percentiles. Source coverage remains per-series; a merged observed population is not a complete-traffic claim.

For p50/p95/p99 find exact nearest-rank position `ceil(p*n)` in the merged count distribution and report its **bucket range**, not an interpolated exact value. First/last bucket endpoints may be unbounded (null). A percentile in the +∞ bucket has no invented finite upper bound. No observations means null percentile. Raw trace percentiles can differ from these ranges because raw samples contain information the histogram discarded.

## Storage, admission and query limits

Migration0003 creates raw MergeTree `metric_points`: stream/name/service/namespace indices, exact UInt64 start/end nanoseconds, lossless normalized point JSON and DateTime64(9) database ingestion time. JSON retains all supported typed fields, not a floating-only value column. Partition is end-time month; order is metric/service/stream/end. No projections/materialized aggregates or optimization redesign. Inserts are parameterized multi-row batches with synchronous ClickHouse confirmation and raw replay retained.

Metrics have two independent persistence slots,100ms admission wait,2048 points/export and4MiB normalized data; shared gRPC wire limit is4MiB. Each export is one immediate request batch (cross-request queue capacity0 and batch wait0); database context3s. Temporary overload/database failure returns retryable UNAVAILABLE, never an early queue ACK. Partly invalid exports persist valid points and return exact rejected_data_points. Failed/ambiguous writes return failure even when rows might exist, so retry may replay them. Shutdown closes metric admission and waits before closing the pool. Trace/log batch budgets remain independent; ClickHouse/pool/CPU remain shared resources, not strict physical isolation.

Concurrency-safe lifetime cardinality admission: global4096 streams,256/resource,128 resource identities,128 writers; canonical key≤32KiB. No unbounded seen-series map or silent attribute deletion. Reservations survive failed/ambiguous writes. These limits reset on **receiver restart**; stored historical cardinality does not. They are local development protection, not a durable fleet-wide tenant quota. SDK overflow (`otel.metric.overflow=true` boolean) is preserved and explicitly marks dimension coverage unreliable. V1 permits exact stream selection but no arbitrary dimension-predicate aggregate that could silently omit overflow.

Read API uses two query slots,4s API/3s storage deadline, ≤1h selected window,32 candidate streams,12,000 physical points and16MiB input/response. Discovery uses raw in-window end timestamps and name/service/namespace filters. One bulk hydration reads all rows for selected streams from one hour before `from` through `to`, so compatible predecessors can be canonicalized rather than chosen from raw replay. An older predecessor outside that horizon is unavailable, not guessed. Query result limits use overflow sentinels and errors, not silent aggregate-input truncation. Native export-point resolution is retained; no pagination/top-N/resampling in V1. Broad default windows may exceed budgets; narrow name/service/time.

Existing ClickHouse read guards apply per SELECT:1M rows read,128MiB uncompressed read,256MiB query memory,32MiB result and2s execution with throw modes. These guards and structural application limits do not constitute a measured process-heap maximum. Two reads are not a snapshot; late telemetry may alter future queries.

GET `/api/v1/metrics`, `/api/v1/metrics/series`, `/api/v1/metrics/query` share one versioned envelope. Supported filters: from/to UTC Z timestamps, exact name/service/namespace and optional stream_id. Unknown/repeated/empty filters are rejected. Applied bounds, actual point metadata, source-normalized intervals, coverage, gaps/resets/overlaps/replay/conflicts/overflow and incompatible/N/A states are returned. Invalid filter400, exceeded budget413, concurrency429, timeout504, cancellation408, corrupt data500, unavailable backend503. No SQL/credentials in errors.

Next `/metrics` uses fixed same-origin GET proxies, bounded response/abort/no-store behavior and no browser database access. It provides name/service/time/native-series selection, exact raw points, rate and gauge plots with elapsed-time spacing/gaps, signed Sum state/change labels and histogram ranges/distribution tables. Service detail links distinguish direct completion metrics from trace-derived SERVER-start RED. Local loopback restrictions remain; namespace is not authentication/tenant authorization. Production IAM/TLS/tenant isolation, comprehensive DLP, durable admission, Summary/ExponentialHistogram, arbitrary PromQL and SLOs remain out of scope.

## Dependency references

Kept existing Go OTel1.47.0/protobuf1.11.0/Collector0.165.0 compatibility; added matching sdk/metric and otlpmetricgrpc1.47.0 without upgrading the existing stack. Checked installed APIs and [versioned SDK documentation](https://pkg.go.dev/go.opentelemetry.io/otel/sdk/metric@v1.47.0), [versioned exporter](https://pkg.go.dev/go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc@v1.47.0) and [official data model](https://opentelemetry.io/docs/specs/otel/metrics/data-model/). The implemented subset and conservative policies above govern V1.
