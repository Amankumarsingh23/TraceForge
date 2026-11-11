# ADR 0002 — Synchronous OTLP trace persistence

Status: Accepted for Session 2.

## Decision

Implement the generated OTLP/gRPC TraceServiceServer and traverse all resource/scope/span groups. Register gzip decompression to interoperate with the pinned Collector's default export compression. The Collector remains the SDK-facing endpoint; it forwards to the Go receiver over its existing Docker network, with a bounded in-memory retry queue. Retain debug export for synthetic diagnostics and independent identity comparison.

Normalize into bounded typed rows and redact known sensitive attribute keys before persistence. Store important search/aggregation fields in columns plus typed Protobuf JSON for variable attributes and full known OTLP metadata. This avoids an unbounded set of schema columns while retaining scope, events, links, flags and dropped counts. `docs/normalization.md` specifies accepted ranges and deliberate omissions.

Use the official ClickHouse Go v2 driver and one synchronous prepared multi-row insert per accepted export. No application-side asynchronous queue is introduced. Startup verifies authenticated connectivity and applies embedded repeatable migration 0001. HTTP liveness is independent of storage; readiness checks ClickHouse dynamically. gRPC concurrency and message size are bounded, processing is context-limited, and shutdown drains before closing the database pool.

## Acknowledgment boundary

Only a successful batch Send permits a successful export response. Empty requests need no persistence. Invalid spans are reported in exact OTLP partial-success counts after inserting the valid subset; clients must not retry partial success. Oversized/malformed envelopes fail at request level. Temporary storage failure returns Unavailable, with sanitized details. Context cancellation/deadlines preserve gRPC status. The distinction follows the [OTLP specification](https://opentelemetry.io/docs/specs/otlp/).

This means normal synchronous ClickHouse acceptance, not guaranteed power-loss durability, replication, atomicity across parts/partitions, or exactly-once delivery. If insertion succeeds but the response is lost, a retry can create duplicates. Ambiguous failures may also leave data before retry. `insert_deduplicate=0` makes this explicit; a controlled replay test demonstrates it. No durable receiver spool exists.

## Initial schema trade-offs

MergeTree suits append-heavy raw telemetry. Monthly partitions organize time ranges. `ORDER BY (trace_id, start_time, span_id)` groups one trace naturally for our first direct SQL inspection; its sparse index is not a uniqueness constraint. Service/time queries without trace ID may scan more data. The JSON String columns are pragmatic variable metadata storage; dedicated attribute indexes, materialized views, rollups, TTL and service-centric layouts require later workload evidence. No optimization benchmarks are claimed.

PostgreSQL remains reserved for later transactional metadata. No Kafka, replicas, distributed table, durable queue, REST trace API, reconstruction algorithm or AgentOps feature is added.

## Operational consequences

The existing `.env`, volumes and demo endpoints are preserved. A Collector restart is necessary to reload its bind-mounted file; changing file content alone does not trigger a Compose recreation. The receiver is a nonroot minimal image with only loopback host publications. Plaintext unauthenticated ingestion remains suitable for a controlled local environment only. Storage redaction does not protect the upstream debug branch.
