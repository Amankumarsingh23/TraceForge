CREATE TABLE IF NOT EXISTS traceforge.logs
(
    log_id FixedString(32),
    source_event_id String,
    trace_id String,
    span_id String,
    trace_flags UInt32,
    service_name LowCardinality(String),
    severity_number UInt8,
    severity_text String,
    event_time Nullable(DateTime64(9, 'UTC')),
    observed_time Nullable(DateTime64(9, 'UTC')),
    effective_time DateTime64(9, 'UTC'),
    time_source LowCardinality(String),
    body_text String,
    body_json String,
    attributes_json String,
    resource_attributes_json String,
    scope_name String,
    scope_version String,
    event_name String,
    resource_json String,
    scope_json String,
    record_json String,
    resource_schema_url String,
    scope_schema_url String,
    ingested_at DateTime64(9, 'UTC')
)
ENGINE = MergeTree
PARTITION BY toYYYYMM(effective_time)
ORDER BY (effective_time, log_id);
