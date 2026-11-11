# OTLP normalization contract — Session 2

The receiver uses the official generated OTLP trace service (proto module 1.11.0), not a custom schema. All `ResourceSpans → ScopeSpans → Spans` groups are traversed. Each row inherits the correct resource and instrumentation scope. Missing, empty or non-string `service.name` falls back to `unknown_service`. No first-group shortcut is used.

## Representations and supported fields

Trace/span/parent IDs are lowercase hex in query columns. Trace IDs must be 16 nonzero bytes; span IDs 8 nonzero bytes. Parent is absent for a root, otherwise a valid nonzero 8-byte ID. Links receive the same ID checks. Start/end and event timestamps must be positive and at most signed-int64 nanoseconds. End must not precede start. Zero-duration spans are valid. This accepted timestamp subset maps directly to `DateTime64(9, 'UTC')`, from after the Unix epoch through the signed nanosecond maximum in April 2262. No rounding is introduced. Duration is computed only after ordering validation.

`resource_attributes_json` and `span_attributes_json` contain arrays of Protobuf JSON KeyValue messages. `events_json` and `links_json` contain arrays of complete sanitized Protobuf JSON event/link messages. Full `resource_json`, `scope_json`, and `span_json` also preserve all known fields, including scope attributes/version, trace state, span/link flags, dropped counts, status, events and links. Resource and scope schema URLs have their own columns. Resource/scope metadata is repeated per row intentionally to keep the first storage schema simple.

This is **typed Protobuf JSON**, not an object of stringified values: AnyValue variants retain keys such as `intValue`, `boolValue`, `arrayValue`, `kvlistValue`, `doubleValue` and `bytesValue`. Int64 values are JSON strings to preserve all 64 bits, bytes use base64, and protobuf non-finite floating-point encoding is retained. Parsing with official `protojson.Unmarshal` recovers known types. Tests round-trip max int64 and binary attributes and preserve nanosecond timestamps through ClickHouse.

Wire-unknown protobuf fields are intentionally omitted from JSON; this is not a byte-for-byte archival format for future protocol versions. Request-level unknown metadata, transport headers and unknown future enum values are not supported as query data. Original redacted values are intentionally unrecoverable. Status/event/scope/operation free text is retained, not semantically interpreted. Flags, trace state and dropped counts are stored but not used for reconstruction or loss estimation yet.

## Bounds and rejection

Wire/decompressed message limit is 4MiB. Request-level span/resource/total scope count cap is 1000. Nil envelope groups and excessive request-level limits return non-retryable request errors before storage. A nil/malformed span is an individual rejection. Invalid inherited resource/scope metadata rejects each affected contained span with an exact count.

Individual bounds: 64KiB encoded span, 16KiB encoded resource and scope, span/scope/event names and scope version 1024 bytes, status message/string attribute values 4096 bytes, trace state 512 bytes, schema URL 4096 bytes, key 256 bytes, bytes attribute 4096 bytes, events/links 128 each. Each attribute set has a recursive budget of 256 key/value or array items and maximum nesting depth 8. Invalid UTF-8 in validated text is rejected; protobuf JSON encoding failure also rejects the span. The original request is never mutated. No truncation silently converts an oversized span into an accepted partial record.

## Redaction boundary

Cloned resource, scope, span, event and link attributes are recursively sanitized before **any** JSON persistence field or extracted service name is produced. Case/separator-insensitive keys containing authorization, password/passwd, secret, token, API key, credential, cookie or private key are replaced by the string `[REDACTED]`. This intentionally changes the original type when redacted. Nested arrays/key-value lists are traversed. String values with common Bearer/Basic credentials, private-key markers or recognizable password/API-key/token assignments are also replaced. HTTP URL userinfo is stripped and known sensitive query keys redacted.

These are conservative local-development heuristics, not complete secret detection. They can over-redact benign names (for example token counts), miss custom encodings/secrets, and do not scrub free-form operation/event names, status messages, schema URLs or trace state. No production privacy claim is made. Only synthetic telemetry should enter this stack. Collector debug output occurs **before** this boundary and is not protected by storage redaction. Instrumentation/Collector filtering and production authentication/privacy policy remain future work.

## References

- [OTLP success, partial-success and retry semantics](https://opentelemetry.io/docs/specs/otlp/)
- [Official ClickHouse Go batch API](https://github.com/ClickHouse/clickhouse-go/blob/v2.51.0/lib/driver/driver.go)
- [ClickHouse DateTime64 range](https://clickhouse.com/docs/reference/data-types/datetime64)
