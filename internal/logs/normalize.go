package logs

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"regexp"

	collector "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	common "go.opentelemetry.io/proto/otlp/common/v1"
	logpb "go.opentelemetry.io/proto/otlp/logs/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"time"
	"traceforge/internal/ingest"
	"unicode/utf8"
)

var secretPattern = regexp.MustCompile(`(?i)(bearer\s+\S+|basic\s+\S+|(?:password|passwd|api[_-]?key|token|secret)\s*[:=]\s*\S+|-----BEGIN[^\n]*PRIVATE KEY-----)`)

func safeText(s string) string {
	if secretPattern.MatchString(s) {
		return "[REDACTED]"
	}
	return ingest.SanitizeText(s)
}
func text(s string, max int) bool  { return len(s) <= max && utf8.ValidString(s) }
func validID(b []byte, n int) bool { return len(b) == n && !bytes.Equal(b, make([]byte, n)) }

// Check depth/item bounds iteratively BEFORE proto.Size/Clone can recurse. A
// malformed individual record remains a counted rejection, not an unsafe clone.
func bounded(values ...*common.AnyValue) bool {
	type entry struct {
		value *common.AnyValue
		depth int
	}
	stack := []entry{}
	for _, v := range values {
		if v != nil {
			stack = append(stack, entry{v, 0})
		}
	}
	count := 0
	for len(stack) > 0 {
		item := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		count++
		if count > 256 || item.depth > 8 {
			return false
		}
		if item.value == nil || item.value.Value == nil {
			return false
		}
		switch v := item.value.Value.(type) {
		case *common.AnyValue_KvlistValue:
			if v.KvlistValue == nil {
				return false
			}
			if len(v.KvlistValue.Values) > 256-len(stack) {
				return false
			}
			for _, kv := range v.KvlistValue.Values {
				if kv == nil {
					return false
				}
				stack = append(stack, entry{kv.Value, item.depth + 1})
			}
		case *common.AnyValue_ArrayValue:
			if v.ArrayValue == nil {
				return false
			}
			if len(v.ArrayValue.Values) > 256-len(stack) {
				return false
			}
			for _, child := range v.ArrayValue.Values {
				stack = append(stack, entry{child, item.depth + 1})
			}
		}
		if len(stack) > 256 {
			return false
		}
	}
	return true
}
func attributesBounded(attrs []*common.KeyValue) bool {
	if len(attrs) > 256 {
		return false
	}
	vs := make([]*common.AnyValue, 0, len(attrs))
	for _, a := range attrs {
		if a == nil || a.Value == nil || a.Value.Value == nil {
			return false
		}
		vs = append(vs, a.Value)
	}
	return bounded(vs...)
}
func scrubValue(value *common.AnyValue) {
	if value == nil {
		return
	}
	switch v := value.Value.(type) {
	case *common.AnyValue_StringValue:
		v.StringValue = safeText(v.StringValue)
	case *common.AnyValue_ArrayValue:
		for _, child := range v.ArrayValue.Values {
			scrubValue(child)
		}
	case *common.AnyValue_KvlistValue:
		for _, kv := range v.KvlistValue.Values {
			scrubValue(kv.Value)
		}
	}
}
func scrubAttrs(attrs []*common.KeyValue) {
	for _, kv := range attrs {
		scrubValue(kv.Value)
	}
}
func marshal(m proto.Message) (json.RawMessage, error) { b, e := protojson.Marshal(m); return b, e }
func marshalAttrs(attrs []*common.KeyValue) (json.RawMessage, error) {
	out := make([]json.RawMessage, 0, len(attrs))
	for _, a := range attrs {
		b, e := marshal(a)
		if e != nil {
			return nil, e
		}
		out = append(out, b)
	}
	return json.Marshal(out)
}
func Inspect(req *collector.ExportLogsServiceRequest) (int, int64, error) {
	if req == nil || len(req.ResourceLogs) > MaxRecords {
		return 0, 0, errors.New("invalid log envelope")
	}
	count, scopes, wire := 0, 0, 0
	allBounded := true
	var reservation int64
	for _, rs := range req.ResourceLogs {
		if rs == nil {
			return 0, 0, errors.New("nil log resource")
		}
		scopes += len(rs.ScopeLogs)
		if scopes > MaxRecords {
			return 0, 0, errors.New("scope limit")
		}
		resourceSize := 0
		if rs.Resource == nil || attributesBounded(rs.Resource.Attributes) {
			resourceSize = proto.Size(rs.Resource)
		} else {
			allBounded = false
		}
		wire += resourceSize + len(rs.SchemaUrl)
		for _, ss := range rs.ScopeLogs {
			if ss == nil {
				return 0, 0, errors.New("nil log scope")
			}
			count += len(ss.LogRecords)
			if count > MaxRecords {
				return 0, 0, errors.New("log record limit")
			}
			scopeSize := 0
			if ss.Scope == nil || attributesBounded(ss.Scope.Attributes) {
				scopeSize = proto.Size(ss.Scope)
			} else {
				allBounded = false
			}
			wire += scopeSize + len(ss.SchemaUrl)
			for _, lr := range ss.LogRecords {
				size := 0
				if lr != nil && bounded(lr.Body) && attributesBounded(lr.Attributes) {
					size = proto.Size(lr)
				} else {
					allBounded = false
				}
				wire += size
				reservation += 64*int64(resourceSize+scopeSize+size+len(rs.SchemaUrl)+len(ss.SchemaUrl)+512) + 512
			}
		}
	}
	if wire > MaxMessageBytes || allBounded && proto.Size(req) > MaxMessageBytes {
		return 0, 0, errors.New("log message size limit")
	}
	return count, reservation, nil
}
func Normalize(ctx context.Context, req *collector.ExportLogsServiceRequest, now time.Time) ([]Row, int64, error) {
	if _, _, e := Inspect(req); e != nil {
		return nil, 0, e
	}
	if now.Before(time.Unix(0, 1)) || now.After(time.Unix(0, math.MaxInt64)) {
		return nil, 0, errors.New("invalid ingestion time")
	}
	rows := []Row{}
	var rejected int64
	for _, rs := range req.ResourceLogs {
		for _, ss := range rs.ScopeLogs {
			for _, record := range ss.LogRecords {
				if e := ctx.Err(); e != nil {
					return nil, rejected, e
				}
				if record == nil || !bounded(record.Body) || !attributesBounded(record.Attributes) || (rs.Resource != nil && !attributesBounded(rs.Resource.Attributes)) || (ss.Scope != nil && !attributesBounded(ss.Scope.Attributes)) {
					rejected++
					continue
				}
				if proto.Size(record) > MaxRecordBytes || proto.Size(rs.Resource) > 16<<10 || proto.Size(ss.Scope) > 16<<10 || !text(rs.SchemaUrl, 2048) || !text(ss.SchemaUrl, 2048) {
					rejected++
					continue
				}
				lr := proto.Clone(record).(*logpb.LogRecord)
				if len(lr.TraceId) > 0 && !validID(lr.TraceId, 16) || len(lr.SpanId) > 0 && (!validID(lr.SpanId, 8) || len(lr.TraceId) == 0) || lr.TimeUnixNano > math.MaxInt64 || lr.ObservedTimeUnixNano > math.MaxInt64 || lr.SeverityNumber < 0 || lr.SeverityNumber > 24 || !text(lr.SeverityText, 128) || !text(lr.EventName, 256) {
					rejected++
					continue
				}
				if ingest.SanitizeAttributes(lr.Attributes) != nil || lr.Body != nil && ingest.SanitizeValue(lr.Body) != nil {
					rejected++
					continue
				}
				scrubAttrs(lr.Attributes)
				scrubValue(lr.Body)
				lr.SeverityText = safeText(lr.SeverityText)
				lr.EventName = safeText(lr.EventName)
				r := Row{TraceID: hex.EncodeToString(lr.TraceId), SpanID: hex.EncodeToString(lr.SpanId), TraceFlags: lr.Flags, ServiceName: "unknown_service", SeverityNumber: uint8(lr.SeverityNumber), SeverityText: lr.SeverityText, EventName: lr.EventName, TimeSource: "ingestion", EffectiveTime: now.UTC(), IngestedAt: now.UTC(), ResourceSchemaURL: safeText(rs.SchemaUrl), ScopeSchemaURL: safeText(ss.SchemaUrl), ResourceJSON: json.RawMessage("{}"), ScopeJSON: json.RawMessage("{}"), ResourceAttributesJSON: json.RawMessage("[]"), AttributesJSON: json.RawMessage("[]"), BodyJSON: json.RawMessage("null")}
				if lr.ObservedTimeUnixNano != 0 {
					t := time.Unix(0, int64(lr.ObservedTimeUnixNano)).UTC()
					r.ObservedTime = &t
					r.EffectiveTime = t
					r.TimeSource = "observed"
				}
				if lr.TimeUnixNano != 0 {
					t := time.Unix(0, int64(lr.TimeUnixNano)).UTC()
					r.EventTime = &t
					r.EffectiveTime = t
					r.TimeSource = "event"
				}
				valid := true
				if rs.Resource != nil {
					// Use the generated concrete type through the original clone's accessor.
					copyRS := proto.Clone(&logpb.ResourceLogs{Resource: rs.Resource}).(*logpb.ResourceLogs).Resource
					if ingest.SanitizeAttributes(copyRS.Attributes) != nil {
						valid = false
					} else {
						scrubAttrs(copyRS.Attributes)
						var resourceErr, attrsErr error
						r.ResourceJSON, resourceErr = marshal(copyRS)
						r.ResourceAttributesJSON, attrsErr = marshalAttrs(copyRS.Attributes)
						if resourceErr != nil || attrsErr != nil {
							valid = false
						}
						for _, kv := range copyRS.Attributes {
							if kv.Key == "service.name" {
								if v, ok := kv.Value.Value.(*common.AnyValue_StringValue); ok && text(v.StringValue, 128) && v.StringValue != "" {
									r.ServiceName = v.StringValue
								} else {
									valid = false
								}
							}
						}
					}
				}
				if ss.Scope != nil {
					scope := proto.Clone(&logpb.ScopeLogs{Scope: ss.Scope}).(*logpb.ScopeLogs).Scope
					if !text(scope.Name, 256) || !text(scope.Version, 128) || ingest.SanitizeAttributes(scope.Attributes) != nil {
						valid = false
					} else {
						scrubAttrs(scope.Attributes)
						scope.Name = safeText(scope.Name)
						scope.Version = safeText(scope.Version)
						r.ScopeName = scope.Name
						r.ScopeVersion = scope.Version
						var scopeErr error
						r.ScopeJSON, scopeErr = marshal(scope)
						if scopeErr != nil {
							valid = false
						}
					}
				}
				if !valid {
					rejected++
					continue
				}
				var e error
				r.AttributesJSON, e = marshalAttrs(lr.Attributes)
				if e != nil {
					rejected++
					continue
				}
				for _, a := range lr.Attributes {
					if a.Key == "log.record.uid" {
						r.SourceEventID = a.Value.GetStringValue()
					}
				}
				if lr.Body != nil {
					r.BodyJSON, e = marshal(lr.Body)
					if e != nil {
						rejected++
						continue
					}
					if v, ok := lr.Body.Value.(*common.AnyValue_StringValue); ok {
						r.BodyText = v.StringValue
					} else {
						r.BodyText = string(r.BodyJSON)
					}
				}
				r.RecordJSON, e = marshal(lr)
				if e != nil {
					rejected++
					continue
				}
				identity := make([]byte, 16)
				if _, e = rand.Read(identity); e != nil {
					return nil, rejected, errors.New("log identity allocation failed")
				}
				r.LogID = hex.EncodeToString(identity)
				rows = append(rows, r)
			}
		}
	}
	return rows, rejected, nil
}
func retainedBytes(rows []Row) int64 {
	n := int64(512 + cap(rows)*1024)
	for _, r := range rows {
		n += int64(len(r.BodyText) + len(r.BodyJSON) + len(r.RecordJSON) + len(r.ResourceJSON) + len(r.ScopeJSON) + len(r.AttributesJSON) + len(r.ResourceAttributesJSON) + len(r.SourceEventID) + len(r.ServiceName) + len(r.SeverityText) + len(r.ScopeName) + len(r.ScopeVersion) + len(r.EventName) + len(r.ResourceSchemaURL) + len(r.ScopeSchemaURL) + 128)
	}
	return n
}
