# ADR 0001 — Standard OpenTelemetry and a Collector

Status: Accepted for Session 1.

## Context and decision

TraceForge needs a real distributed trace before implementing its own ingestion/storage backend. Use standard Go OTel API/SDK, otelhttp instrumentation and OTLP/gRPC to an official Collector. For now, export to detailed debug logs. Create two actual HTTP processes rather than emitting unrelated synthetic span records.

Go provides standard HTTP/context primitives, concurrency support and a compact deployable binary. Standard instrumentation avoids inventing an SDK and gives interoperable trace context. The Collector separates application instrumentation from backend routing and offers a recognizable integration boundary. This adds a local process/configuration to operate, and a Collector receipt does not establish backend durability.

ClickHouse is provisioned for future append-heavy telemetry and analytical queries that scan selected columns and time ranges. Its ordering and compression must be evaluated with the actual workload. PostgreSQL is provisioned for future transactional configuration and metadata. Neither has application tables today, and no relative performance claim has been measured.

Kafka is excluded: there is no demonstrated queue/durability requirement that justifies another distributed subsystem in this foundation session. Later ingestion must document bounded buffering and actual loss/retry guarantees.

## Consequences

Resource attributes identify service/version/local environment. W3C context and Baggage propagation are explicit. Separate providers make component tests independent of global SDK state. Batch export reduces request-path work but can lose buffered spans on crashes. Always-sampling is intentional for tiny demo workloads; no capacity or sampling claims follow from it.

Local-only plaintext transport and generated database credentials constrain the demo environment. Future remote operation requires an explicit authentication/TLS and privacy design. No custom SDK, trace storage, UI or evaluation feature is part of this decision.

## References

- [Official Go instrumentation](https://opentelemetry.io/docs/languages/go/getting-started/)
- [Official Go exporters](https://opentelemetry.io/docs/languages/go/exporters/)
- [Collector configuration](https://opentelemetry.io/docs/collector/configuration/)
- [Go releases](https://go.dev/dl/)
