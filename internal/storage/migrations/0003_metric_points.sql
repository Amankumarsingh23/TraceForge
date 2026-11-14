CREATE TABLE IF NOT EXISTS traceforge.metric_points (
 stream_id String,
 metric_name String,
 service_name String,
 service_namespace String,
 end_ns UInt64,
 start_ns UInt64,
 point_json String,
 ingested_at DateTime64(9, 'UTC') DEFAULT now64(9)
) ENGINE = MergeTree
PARTITION BY toYYYYMM(fromUnixTimestamp64Nano(toInt64(end_ns), 'UTC'))
ORDER BY (metric_name,service_name,stream_id,end_ns);
