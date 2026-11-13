//go:build integration

package integration

import (
	"context"
	"encoding/hex"
	"fmt"
	collector "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	common "go.opentelemetry.io/proto/otlp/common/v1"
	resource "go.opentelemetry.io/proto/otlp/resource/v1"
	trace "go.opentelemetry.io/proto/otlp/trace/v1"
	"net/url"
	"testing"
	"time"
	"traceforge/internal/analytics"
)

func TestAnalyticsCrossWindowBulkAndBudget(t *testing.T) {
	// Historical isolated windows prevent fixtures from mixing with actual demo evidence.
	base := time.Date(2026, 1, 10, 10, 0, 0, 0, time.UTC)
	tid := id(t, 16)
	clientID := id(t, 8)
	req := &collector.ExportTraceServiceRequest{ResourceSpans: []*trace.ResourceSpans{
		{Resource: &resource.Resource{Attributes: []*common.KeyValue{attr("service.name", "fixture-caller"), attr("traceforge.workload.kind", "fixture")}}, ScopeSpans: []*trace.ScopeSpans{{Spans: []*trace.Span{{TraceId: tid, SpanId: clientID, Kind: trace.Span_SPAN_KIND_CLIENT, Name: "call", StartTimeUnixNano: uint64(base.Add(-time.Millisecond).UnixNano()), EndTimeUnixNano: uint64(base.Add(time.Second).UnixNano())}}}}},
		{Resource: &resource.Resource{Attributes: []*common.KeyValue{attr("service.name", "fixture-server"), attr("traceforge.workload.kind", "fixture")}}, ScopeSpans: []*trace.ScopeSpans{{Spans: []*trace.Span{{TraceId: tid, SpanId: id(t, 8), ParentSpanId: clientID, Kind: trace.Span_SPAN_KIND_SERVER, Name: "receive", StartTimeUnixNano: uint64(base.Add(time.Millisecond).UnixNano()), EndTimeUnixNano: uint64(base.Add(time.Second).UnixNano())}}}}},
	}}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	c := grpcClient(t)
	if _, e := c.Export(ctx, req); e != nil {
		t.Fatal(e)
	}
	// Inspect this trace through the storage bulk reader, while retaining any prior runs.
	store := database(t)
	q := analytics.Query{From: base, To: base.Add(time.Minute), BucketSeconds: 60, Workload: "fixture"}
	rows, e := store.AnalyticsRows(ctx, q)
	if e != nil {
		t.Fatal(e)
	}
	found := 0
	for _, r := range rows {
		if r.TraceID == hex.EncodeToString(tid) {
			found++
		}
	}
	if found != 2 {
		t.Fatal("full trace hydration lost prior-window client", found)
	}
	r, e := analytics.Compute(ctx, q, rows)
	if e != nil || len(r.Edges) != 0 {
		t.Fatal("edge incorrectly attributed to SERVER start", e)
	}
	q.From = base.Add(-time.Minute)
	q.To = base
	rows, e = store.AnalyticsRows(ctx, q)
	if e != nil {
		t.Fatal(e)
	}
	r, e = analytics.Compute(ctx, q, rows)
	if e != nil || len(r.Edges) != 1 {
		t.Fatal("cross-window confirmed edge lost", e)
	}
	// One request, 129 distinct trace IDs, explicit all-or-error candidate budget.
	base = base.Add(2 * time.Hour)
	req.ResourceSpans = req.ResourceSpans[:1]
	ss := req.ResourceSpans[0].ScopeSpans[0]
	ss.Spans = nil
	for i := 0; i < analytics.MaxTraces+1; i++ {
		ss.Spans = append(ss.Spans, &trace.Span{TraceId: id(t, 16), SpanId: id(t, 8), Kind: trace.Span_SPAN_KIND_SERVER, Name: "budget", StartTimeUnixNano: uint64(base.UnixNano()), EndTimeUnixNano: uint64(base.Add(time.Millisecond).UnixNano())})
	}
	if _, e = c.Export(ctx, req); e != nil {
		t.Fatal(e)
	}
	v := url.Values{"from": {base.Format(time.RFC3339Nano)}, "to": {base.Add(time.Minute).Format(time.RFC3339Nano)}}
	if code := api(t, "/api/v1/services?"+v.Encode(), nil); code != 413 {
		t.Fatal("candidate overflow must not be a partial success", code)
	}
	t.Log(fmt.Sprintf("bulk cross-window hydration and 129-candidate rejection verified; trace=%s", hex.EncodeToString(tid)))
}
