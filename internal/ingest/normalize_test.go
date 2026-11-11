package ingest

import (
	"bytes"
	"context"
	collector "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	common "go.opentelemetry.io/proto/otlp/common/v1"
	resource "go.opentelemetry.io/proto/otlp/resource/v1"
	trace "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"math"
	"strings"
	"testing"
	"time"
)

func str(key, value string) *common.KeyValue {
	return &common.KeyValue{Key: key, Value: &common.AnyValue{Value: &common.AnyValue_StringValue{StringValue: value}}}
}
func fixture() *collector.ExportTraceServiceRequest {
	return &collector.ExportTraceServiceRequest{ResourceSpans: []*trace.ResourceSpans{{Resource: &resource.Resource{Attributes: []*common.KeyValue{str("service.name", "gateway")}}, ScopeSpans: []*trace.ScopeSpans{{Scope: &common.InstrumentationScope{Name: "test", Version: "1"}, Spans: []*trace.Span{{TraceId: bytes.Repeat([]byte{1}, 16), SpanId: bytes.Repeat([]byte{2}, 8), Name: "operation", Kind: trace.Span_SPAN_KIND_SERVER, StartTimeUnixNano: 1700000000000000123, EndTimeUnixNano: 1700000000000000456}}}}}}}
}
func first(r *collector.ExportTraceServiceRequest) *trace.Span {
	return r.ResourceSpans[0].ScopeSpans[0].Spans[0]
}

func TestNormalizeConversion(t *testing.T) {
	req := fixture()
	s := first(req)
	s.ParentSpanId = bytes.Repeat([]byte{3}, 8)
	s.Flags = 257
	s.DroppedAttributesCount = 7
	req.ResourceSpans[0].SchemaUrl = "resource-schema"
	req.ResourceSpans[0].ScopeSpans[0].SchemaUrl = "scope-schema"
	rows, rejected, err := Normalize(context.Background(), req)
	if err != nil || rejected != 0 || len(rows) != 1 {
		t.Fatalf("%v %d %d", err, rejected, len(rows))
	}
	r := rows[0]
	if r.TraceID != strings.Repeat("01", 16) || r.SpanID != strings.Repeat("02", 8) || r.ParentSpanID != strings.Repeat("03", 8) || r.DurationNS != 333 || r.StartTime.UnixNano() != int64(s.StartTimeUnixNano) || r.StartTime.Location() != time.UTC || r.ServiceName != "gateway" || r.ScopeName != "test" || r.ScopeVersion != "1" || r.ResourceSchemaURL != "resource-schema" || r.ScopeSchemaURL != "scope-schema" {
		t.Fatalf("incorrect row: %+v", r)
	}
	decoded := &trace.Span{}
	if err := protojson.Unmarshal([]byte(r.SpanJSON), decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Flags != 257 || decoded.DroppedAttributesCount != 7 {
		t.Fatal("lost additional OTLP metadata")
	}
}

func TestURLSanitization(t *testing.T) {
	ordinary := "http://localhost:8081/charge?z=hello%20world&a=1"
	if safeString(ordinary) != ordinary {
		t.Fatal("changed a non-sensitive attribute value")
	}
	for _, input := range []string{"http://synthetic-user:synthetic-secret@localhost/path", "https://localhost/path?api-key=synthetic-secret", "Bearer synthetic-secret"} {
		if strings.Contains(safeString(input), "synthetic-secret") {
			t.Fatal("recognizable URL or credential value leaked")
		}
	}
}

func TestSpanValidation(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*trace.Span)
	}{
		{"trace_length", func(s *trace.Span) { s.TraceId = []byte{1} }},
		{"trace_zero", func(s *trace.Span) { s.TraceId = make([]byte, 16) }},
		{"span_length", func(s *trace.Span) { s.SpanId = nil }},
		{"span_zero", func(s *trace.Span) { s.SpanId = make([]byte, 8) }},
		{"parent_length", func(s *trace.Span) { s.ParentSpanId = []byte{1} }},
		{"parent_zero", func(s *trace.Span) { s.ParentSpanId = make([]byte, 8) }},
		{"zero_time", func(s *trace.Span) { s.StartTimeUnixNano = 0 }},
		{"overflow_time", func(s *trace.Span) { s.EndTimeUnixNano = math.MaxUint64 }},
		{"reversed_time", func(s *trace.Span) { s.EndTimeUnixNano = s.StartTimeUnixNano - 1 }},
		{"kind", func(s *trace.Span) { s.Kind = 999 }},
		{"status", func(s *trace.Span) { s.Status = &trace.Status{Code: 999} }},
		{"name", func(s *trace.Span) { s.Name = strings.Repeat("x", 1025) }},
		{"nil_event", func(s *trace.Span) { s.Events = []*trace.Span_Event{nil} }},
		{"event_time", func(s *trace.Span) { s.Events = []*trace.Span_Event{{Name: "event"}} }},
		{"nil_link", func(s *trace.Span) { s.Links = []*trace.Span_Link{nil} }},
		{"link_id", func(s *trace.Span) { s.Links = []*trace.Span_Link{{TraceId: []byte{1}}} }},
		{"nil_attribute", func(s *trace.Span) { s.Attributes = []*common.KeyValue{nil} }},
		{"nil_value", func(s *trace.Span) { s.Attributes = []*common.KeyValue{{Key: "a"}} }},
		{"large_attribute", func(s *trace.Span) { s.Attributes = []*common.KeyValue{str("a", strings.Repeat("x", 4097))} }},
		{"span_bytes", func(s *trace.Span) { s.Name = strings.Repeat("x", MaxSpanBytes+1) }},
		{"events_count", func(s *trace.Span) { s.Events = make([]*trace.Span_Event, 129) }},
		{"links_count", func(s *trace.Span) { s.Links = make([]*trace.Span_Link, 129) }},
		{"attributes_count", func(s *trace.Span) {
			for i := 0; i < 257; i++ {
				s.Attributes = append(s.Attributes, str("a", "b"))
			}
		}},
		{"nesting", func(s *trace.Span) {
			v := str("a", "b").Value
			for i := 0; i < 10; i++ {
				v = &common.AnyValue{Value: &common.AnyValue_ArrayValue{ArrayValue: &common.ArrayValue{Values: []*common.AnyValue{v}}}}
			}
			s.Attributes = []*common.KeyValue{{Key: "nested", Value: v}}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := fixture()
			tc.mutate(first(r))
			rows, rejected, err := Normalize(context.Background(), r)
			if err != nil || len(rows) != 0 || rejected != 1 {
				t.Fatalf("expected one rejection: %v %d %d", err, len(rows), rejected)
			}
		})
	}
}

func TestGroupMappingAndFallback(t *testing.T) {
	r := fixture()
	r.ResourceSpans[0].ScopeSpans = append(r.ResourceSpans[0].ScopeSpans, &trace.ScopeSpans{Spans: []*trace.Span{first(fixture())}})
	r.ResourceSpans = append(r.ResourceSpans, fixture().ResourceSpans[0])
	r.ResourceSpans[1].Resource = nil
	rows, n, e := Normalize(context.Background(), r)
	if e != nil || n != 0 || len(rows) != 3 || rows[0].ServiceName != "gateway" || rows[1].ServiceName != "gateway" || rows[2].ServiceName != "unknown_service" || rows[1].ScopeName != "" {
		t.Fatal("incorrect group inheritance/fallback")
	}
}

func TestTypedAttributesAndRedaction(t *testing.T) {
	r := fixture()
	s := first(r)
	s.Attributes = []*common.KeyValue{
		str("http.request.header.authorization", "synthetic-secret"), str("plain", "Bearer synthetic-secret"),
		{Key: "large_integer", Value: &common.AnyValue{Value: &common.AnyValue_IntValue{IntValue: math.MaxInt64}}},
		{Key: "boolean", Value: &common.AnyValue{Value: &common.AnyValue_BoolValue{BoolValue: true}}},
		{Key: "float", Value: &common.AnyValue{Value: &common.AnyValue_DoubleValue{DoubleValue: 1.25}}},
		{Key: "bytes", Value: &common.AnyValue{Value: &common.AnyValue_BytesValue{BytesValue: []byte{0, 1, 255}}}},
		{Key: "nested", Value: &common.AnyValue{Value: &common.AnyValue_KvlistValue{KvlistValue: &common.KeyValueList{Values: []*common.KeyValue{str("api-key", "synthetic-secret")}}}}},
		{Key: "array", Value: &common.AnyValue{Value: &common.AnyValue_ArrayValue{ArrayValue: &common.ArrayValue{Values: []*common.AnyValue{str("x", "hello").Value}}}}},
	}
	r.ResourceSpans[0].Resource.Attributes = append(r.ResourceSpans[0].Resource.Attributes, str("password", "synthetic-secret"))
	r.ResourceSpans[0].ScopeSpans[0].Scope.Attributes = []*common.KeyValue{str("secret", "synthetic-secret")}
	s.Events = []*trace.Span_Event{{TimeUnixNano: s.StartTimeUnixNano, Name: "event", DroppedAttributesCount: 2, Attributes: []*common.KeyValue{str("cookie", "synthetic-secret")}}}
	s.Links = []*trace.Span_Link{{TraceId: bytes.Repeat([]byte{4}, 16), SpanId: bytes.Repeat([]byte{5}, 8), Flags: 1, Attributes: []*common.KeyValue{str("credentials", "synthetic-secret")}}}
	rows, n, e := Normalize(context.Background(), r)
	if e != nil || n != 0 || len(rows) != 1 {
		t.Fatalf("%v %d", e, n)
	}
	row := rows[0]
	for _, body := range []string{row.ResourceJSON, row.ScopeJSON, row.SpanJSON, row.ResourceAttributesJSON, row.SpanAttributesJSON, row.EventsJSON, row.LinksJSON} {
		if strings.Contains(body, "synthetic-secret") {
			t.Fatal("secret persisted")
		}
	}
	decoded := &trace.Span{}
	if err := protojson.Unmarshal([]byte(row.SpanJSON), decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Attributes[2].Value.GetIntValue() != math.MaxInt64 || !decoded.Attributes[3].Value.GetBoolValue() || decoded.Attributes[4].Value.GetDoubleValue() != 1.25 || !bytes.Equal(decoded.Attributes[5].Value.GetBytesValue(), []byte{0, 1, 255}) || decoded.Events[0].DroppedAttributesCount != 2 || decoded.Links[0].Flags != 1 {
		t.Fatal("lost types or event/link metadata")
	}
	if s.Attributes[0].Value.GetStringValue() != "synthetic-secret" {
		t.Fatal("mutated caller's telemetry")
	}
}

func TestEnvelopeAndCancellation(t *testing.T) {
	for _, r := range []*collector.ExportTraceServiceRequest{nil, {ResourceSpans: []*trace.ResourceSpans{nil}}, {ResourceSpans: []*trace.ResourceSpans{{ScopeSpans: []*trace.ScopeSpans{nil}}}}} {
		if _, _, e := Normalize(context.Background(), r); e == nil {
			t.Fatal("accepted malformed envelope")
		}
	}
	r := fixture()
	r.ResourceSpans[0].ScopeSpans[0].Spans = make([]*trace.Span, MaxSpans+1)
	if _, _, e := Normalize(context.Background(), r); e == nil {
		t.Fatal("accepted excessive count")
	}
	r = fixture()
	first(r).Name = strings.Repeat("x", MaxMessageBytes+1)
	if _, _, e := Normalize(context.Background(), r); e == nil {
		t.Fatal("accepted excessive message")
	}
	r = fixture()
	r.ResourceSpans[0].ScopeSpans[0].Spans = []*trace.Span{nil}
	if _, n, e := Normalize(context.Background(), r); e != nil || n != 1 {
		t.Fatal("nil span not rejected safely")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, e := Normalize(ctx, fixture()); e != context.Canceled {
		t.Fatal("cancellation lost")
	}
}
