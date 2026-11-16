package logs

import (
	"context"
	"encoding/json"
	collector "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	common "go.opentelemetry.io/proto/otlp/common/v1"
	logpb "go.opentelemetry.io/proto/otlp/logs/v1"
	resource "go.opentelemetry.io/proto/otlp/resource/v1"
	"strings"
	"testing"
	"time"
)

func TestSession14NestedLogAndScopePrivacy(t *testing.T) {
	marker := "S14_LOG_PRIVATE_MARKER"
	nested := &common.AnyValue{Value: &common.AnyValue_KvlistValue{KvlistValue: &common.KeyValueList{Values: []*common.KeyValue{str("password", marker)}}}}
	record := &logpb.LogRecord{Body: nested, Attributes: []*common.KeyValue{str("prompt", marker)}}
	scope := &logpb.ScopeLogs{Scope: &common.InstrumentationScope{Name: "token=" + marker, Attributes: []*common.KeyValue{str("cookie", marker)}}, LogRecords: []*logpb.LogRecord{record}}
	group := &logpb.ResourceLogs{Resource: &resource.Resource{Attributes: []*common.KeyValue{str("service.name", "privacy"), str("user.email", marker)}}, ScopeLogs: []*logpb.ScopeLogs{scope}}
	req := &collector.ExportLogsServiceRequest{ResourceLogs: []*logpb.ResourceLogs{group}}
	rows, reject, e := Normalize(context.Background(), req, time.Now().UTC())
	if e != nil || reject != 0 || len(rows) != 1 {
		t.Fatal(e, reject)
	}
	raw, _ := json.Marshal(rows)
	if strings.Contains(string(raw), marker) {
		t.Fatal("known nested log marker not redacted")
	}
}
