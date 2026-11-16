package ingest

import (
	"context"
	"encoding/json"
	common "go.opentelemetry.io/proto/otlp/common/v1"
	trace "go.opentelemetry.io/proto/otlp/trace/v1"
	"strings"
	"testing"
)

func TestSession14KnownPIIAndMetadataPrivacy(t *testing.T) {
	req := fixture()
	marker := "S14_PRIVACY_MARKER"
	s := first(req)
	s.Name = "password=" + marker
	s.Status = &trace.Status{Message: "Bearer " + marker}
	s.Attributes = []*common.KeyValue{str("user.email", marker), str("prompt", marker), str("gen_ai.input.messages", marker)}
	s.Events = []*trace.Span_Event{{Name: "token=" + marker, TimeUnixNano: s.StartTimeUnixNano, Attributes: []*common.KeyValue{str("phone", marker)}}}
	scope := req.ResourceSpans[0].ScopeSpans[0].Scope
	scope.Name = "api_key=" + marker
	scope.Attributes = []*common.KeyValue{str("credential", marker)}
	req.ResourceSpans[0].SchemaUrl = "https://example.invalid/?token=" + marker
	rows, rejected, e := Normalize(context.Background(), req)
	if e != nil || rejected != 0 || len(rows) != 1 {
		t.Fatal(e, rejected)
	}
	encoded, _ := json.Marshal(rows)
	if strings.Contains(string(encoded), marker) {
		t.Fatal("known sensitive marker escaped privacy projection")
	}
	if !strings.Contains(first(req).Name, marker) {
		t.Fatal("caller-owned request mutated")
	}
}
