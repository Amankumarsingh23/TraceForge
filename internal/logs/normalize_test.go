package logs

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	collector "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	common "go.opentelemetry.io/proto/otlp/common/v1"
	logpb "go.opentelemetry.io/proto/otlp/logs/v1"
	resource "go.opentelemetry.io/proto/otlp/resource/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"math"
	"strings"
	"testing"
	"time"
)

func str(key, value string) *common.KeyValue {
	return &common.KeyValue{Key: key, Value: &common.AnyValue{Value: &common.AnyValue_StringValue{StringValue: value}}}
}
func fixture() *collector.ExportLogsServiceRequest {
	return &collector.ExportLogsServiceRequest{ResourceLogs: []*logpb.ResourceLogs{{Resource: &resource.Resource{Attributes: []*common.KeyValue{str("service.name", "payment")}}, ScopeLogs: []*logpb.ScopeLogs{{Scope: &common.InstrumentationScope{Name: "test", Version: "1"}, LogRecords: []*logpb.LogRecord{{TraceId: bytes.Repeat([]byte{1}, 16), SpanId: bytes.Repeat([]byte{2}, 8), TimeUnixNano: 1700000000000000123, ObservedTimeUnixNano: 1700000000000000456, SeverityNumber: logpb.SeverityNumber_SEVERITY_NUMBER_ERROR, SeverityText: "ERROR", Body: &common.AnyValue{Value: &common.AnyValue_StringValue{StringValue: "payment failure"}}}}}}}}}
}
func first(req *collector.ExportLogsServiceRequest) *logpb.LogRecord {
	return req.ResourceLogs[0].ScopeLogs[0].LogRecords[0]
}

type fakeStore struct{ err error }

func (f *fakeStore) Ping(context.Context) error              { return nil }
func (f *fakeStore) InsertLogs(context.Context, []Row) error { return f.err }
func normalized(t *testing.T, req *collector.ExportLogsServiceRequest) []Row {
	t.Helper()
	rows, n, e := Normalize(context.Background(), req, time.Now().UTC())
	if e != nil || n != 0 {
		t.Fatalf("normalization failed rejected=%d error=%v", n, e)
	}
	return rows
}
func TestNormalizationGroupsMetadataAndTypedValues(t *testing.T) {
	r := fixture()
	lr := first(r)
	lr.Flags = 257
	lr.DroppedAttributesCount = 7
	lr.EventName = "payment.failed"
	lr.Attributes = []*common.KeyValue{{Key: "large", Value: &common.AnyValue{Value: &common.AnyValue_IntValue{IntValue: math.MaxInt64}}}, str("log.record.uid", "source-identity")}
	lr.Body = &common.AnyValue{Value: &common.AnyValue_KvlistValue{KvlistValue: &common.KeyValueList{Values: []*common.KeyValue{{Key: "binary", Value: &common.AnyValue{Value: &common.AnyValue_BytesValue{BytesValue: []byte{0, 1, 255}}}}, {Key: "flag", Value: &common.AnyValue{Value: &common.AnyValue_BoolValue{BoolValue: true}}}, {Key: "fraction", Value: &common.AnyValue{Value: &common.AnyValue_DoubleValue{DoubleValue: 1.25}}}}}}}
	r.ResourceLogs[0].SchemaUrl = "resource-schema"
	r.ResourceLogs[0].ScopeLogs[0].SchemaUrl = "scope-schema"
	r.ResourceLogs[0].ScopeLogs = append(r.ResourceLogs[0].ScopeLogs, proto.Clone(r.ResourceLogs[0].ScopeLogs[0]).(*logpb.ScopeLogs))
	r.ResourceLogs = append(r.ResourceLogs, proto.Clone(r.ResourceLogs[0]).(*logpb.ResourceLogs))
	rows := normalized(t, r)
	if len(rows) != 4 {
		t.Fatal("groups dropped")
	}
	row := rows[0]
	if row.EventTime.UnixNano() != 1700000000000000123 || row.ObservedTime.UnixNano() != 1700000000000000456 || row.TimeSource != "event" || row.TraceFlags != 257 || row.EventName != "payment.failed" || row.SourceEventID != "source-identity" || row.ServiceName != "payment" {
		t.Fatal("metadata lost", row)
	}
	decoded := &logpb.LogRecord{}
	if protojson.Unmarshal(row.RecordJSON, decoded) != nil || decoded.DroppedAttributesCount != 7 || decoded.Attributes[0].Value.GetIntValue() != math.MaxInt64 || !bytes.Equal(decoded.Body.GetKvlistValue().Values[0].Value.GetBytesValue(), []byte{0, 1, 255}) {
		t.Fatal("typed data lost")
	}
	if !strings.Contains(string(row.AttributesJSON), `"9223372036854775807"`) {
		t.Fatal("integer precision lost")
	}
}
func TestStandaloneTraceOnlyAndTimestampFallback(t *testing.T) {
	for _, kind := range []string{"standalone", "trace_only", "event_only", "observed_only", "neither"} {
		t.Run(kind, func(t *testing.T) {
			r := fixture()
			lr := first(r)
			now := time.Unix(1800000000, 789).UTC()
			switch kind {
			case "standalone":
				lr.TraceId = nil
				lr.SpanId = nil
			case "trace_only":
				lr.SpanId = nil
			case "event_only":
				lr.ObservedTimeUnixNano = 0
			case "observed_only":
				lr.TimeUnixNano = 0
			case "neither":
				lr.TimeUnixNano = 0
				lr.ObservedTimeUnixNano = 0
			}
			rows, n, e := Normalize(context.Background(), r, now)
			if e != nil || n != 0 || len(rows) != 1 {
				t.Fatal("valid optional record rejected")
			}
			row := rows[0]
			if kind == "standalone" && (row.TraceID != "" || row.SpanID != "") {
				t.Fatal("fabricated IDs")
			}
			if kind == "trace_only" && row.SpanID != "" {
				t.Fatal("fabricated span")
			}
			if kind == "observed_only" && (row.EventTime != nil || row.TimeSource != "observed") {
				t.Fatal("bad observed fallback")
			}
			if kind == "neither" && (row.EventTime != nil || row.ObservedTime != nil || row.TimeSource != "ingestion" || !row.EffectiveTime.Equal(now)) {
				t.Fatal("unknown event time disguised")
			}
		})
	}
}
func TestInvalidIndividualRecords(t *testing.T) {
	cases := map[string]func(*logpb.LogRecord){"short_trace": func(r *logpb.LogRecord) { r.TraceId = []byte{1} }, "zero_trace": func(r *logpb.LogRecord) { r.TraceId = make([]byte, 16) }, "short_span": func(r *logpb.LogRecord) { r.SpanId = []byte{1} }, "zero_span": func(r *logpb.LogRecord) { r.SpanId = make([]byte, 8) }, "span_only": func(r *logpb.LogRecord) { r.TraceId = nil }, "time": func(r *logpb.LogRecord) { r.TimeUnixNano = math.MaxUint64 }, "observed_time": func(r *logpb.LogRecord) { r.ObservedTimeUnixNano = math.MaxUint64 }, "severity": func(r *logpb.LogRecord) { r.SeverityNumber = 25 }, "negative_severity": func(r *logpb.LogRecord) { r.SeverityNumber = -1 }, "large": func(r *logpb.LogRecord) {
		r.Body = &common.AnyValue{Value: &common.AnyValue_StringValue{StringValue: strings.Repeat("x", MaxRecordBytes+1)}}
	}, "deep": func(r *logpb.LogRecord) {
		v := r.Body
		for range 100 {
			v = &common.AnyValue{Value: &common.AnyValue_ArrayValue{ArrayValue: &common.ArrayValue{Values: []*common.AnyValue{v}}}}
		}
		r.Body = v
	}}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			r := fixture()
			bad := proto.Clone(first(r)).(*logpb.LogRecord)
			change(bad)
			r.ResourceLogs[0].ScopeLogs[0].LogRecords = append(r.ResourceLogs[0].ScopeLogs[0].LogRecords, bad)
			rows, n, e := Normalize(context.Background(), r, time.Now().UTC())
			if e != nil || n != 1 || len(rows) != 1 {
				t.Fatalf("want valid subset and exact rejection: %v %d %d", e, n, len(rows))
			}
		})
	}
	for severity := 0; severity <= 24; severity++ {
		r := fixture()
		first(r).SeverityNumber = logpb.SeverityNumber(severity)
		if normalized(t, r)[0].SeverityNumber != uint8(severity) {
			t.Fatal("severity changed")
		}
	}
}
func TestNestedRedactionAndImmutability(t *testing.T) {
	r := fixture()
	lr := first(r)
	lr.Attributes = []*common.KeyValue{str("Authorization", "synthetic-auth"), str("message", "inline bearer synthetic-token")}
	r.ResourceLogs[0].Resource.Attributes = append(r.ResourceLogs[0].Resource.Attributes, str("api_key", "synthetic-key"))
	lr.Body = &common.AnyValue{Value: &common.AnyValue_KvlistValue{KvlistValue: &common.KeyValueList{Values: []*common.KeyValue{str("password", "synthetic-password"), {Key: "nested", Value: &common.AnyValue{Value: &common.AnyValue_ArrayValue{ArrayValue: &common.ArrayValue{Values: []*common.AnyValue{{Value: &common.AnyValue_StringValue{StringValue: "inline token=synthetic-value"}}}}}}}}}}}
	before := proto.Clone(r)
	row := normalized(t, r)[0]
	raw, _ := json.Marshal(row)
	for _, secret := range []string{"synthetic-auth", "synthetic-token", "synthetic-key", "synthetic-password", "synthetic-value"} {
		if strings.Contains(string(raw), secret) {
			t.Fatal("secret escaped normalization")
		}
	}
	if !proto.Equal(before, r) {
		t.Fatal("incoming request mutated")
	}
}
func TestLogIdentityAndEmptyEnvelope(t *testing.T) {
	r := fixture()
	r.ResourceLogs[0].ScopeLogs[0].LogRecords = append(r.ResourceLogs[0].ScopeLogs[0].LogRecords, first(r))
	rows := normalized(t, r)
	replay := normalized(t, r)
	if len(rows) != 2 || rows[0].LogID == rows[1].LogID || rows[0].LogID == replay[0].LogID {
		t.Fatal("genuine/replayed events collapsed")
	}
	if rows := normalized(t, &collector.ExportLogsServiceRequest{}); len(rows) != 0 {
		t.Fatal("empty export changed")
	}
	r.ResourceLogs[0].ScopeLogs[0].LogRecords = make([]*logpb.LogRecord, 1001)
	if _, _, e := Normalize(context.Background(), r, time.Now()); e == nil {
		t.Fatal("count bound absent")
	}
}
func TestServerDatabaseFailureAndFullSuccess(t *testing.T) {
	c := DefaultConfig()
	c.BatchTarget = 1
	c.BatchWait = 0
	f := &fakeStore{err: errors.New("private driver information")}
	s, e := NewConfiguredServer(f, c)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Shutdown(context.Background())
	res, e := s.Export(context.Background(), fixture())
	if status.Code(e) != codes.Unavailable || res != nil || strings.Contains(e.Error(), "private") {
		t.Fatal("failure falsely acknowledged or disclosed", e)
	}
	f.err = nil
	res, e = s.Export(context.Background(), fixture())
	if e != nil || res.PartialSuccess != nil {
		t.Fatal("full success has partial response")
	}
}

func TestWideValuesRejectBeforeGrowingWorkStack(t *testing.T) {
	value := &common.AnyValue{Value: &common.AnyValue_ArrayValue{ArrayValue: &common.ArrayValue{Values: make([]*common.AnyValue, 100000)}}}
	allocations := testing.AllocsPerRun(5, func() {
		if bounded(value) {
			t.Fatal("wide array accepted")
		}
	})
	if allocations > 4 {
		t.Fatal("rejection allocated a growing work stack", allocations)
	}
}
