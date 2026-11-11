CREATE TABLE IF NOT EXISTS traceforge.spans
(
    trace_id FixedString(32),
    span_id FixedString(16),
    parent_span_id String,
    service_name LowCardinality(String),
    span_name String,
    span_kind UInt8,
    start_time DateTime64(9, 'UTC'),
    end_time DateTime64(9, 'UTC'),
    duration_ns UInt64,
    status_code UInt8,
    status_message String,
    resource_attributes_json String,
    span_attributes_json String,
    scope_name String,
    scope_version String,
    events_json String,
    links_json String,
    resource_json String,
    scope_json String,
    span_json String,
    resource_schema_url String,
    scope_schema_url String,
    ingested_at DateTime64(3, 'UTC') DEFAULT now64(3)
)
ENGINE = MergeTree
PARTITION BY toYYYYMM(start_time)
ORDER BY (trace_id, start_time, span_id);
