//go:build integration

package integration

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	collector "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	common "go.opentelemetry.io/proto/otlp/common/v1"
	resource "go.opentelemetry.io/proto/otlp/resource/v1"
	trace "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"testing"
	"time"
	"traceforge/internal/traces"
)

const session2Trace = "656f80607b005aa65927df202991a837"

func api(t *testing.T, path string, target any) int {
	t.Helper()
	client := &http.Client{Timeout: 8 * time.Second}
	res, e := client.Get("http://localhost:8082" + path)
	if e != nil {
		t.Fatal(e)
	}
	defer res.Body.Close()
	b, e := io.ReadAll(io.LimitReader(res.Body, 16<<20))
	if e != nil {
		t.Fatal(e)
	}
	if target != nil {
		if e := json.Unmarshal(b, target); e != nil {
			t.Fatalf("invalid API JSON (%d): %s", res.StatusCode, b)
		}
	}
	return res.StatusCode
}
func detail(t *testing.T, tid string) *traces.Detail {
	t.Helper()
	var d traces.Detail
	if code := api(t, "/api/v1/traces/"+tid, &d); code != 200 {
		t.Fatalf("detail HTTP %d", code)
	}
	return &d
}
func flatten(d *traces.Detail) map[string]*traces.Node {
	stack := append(append(append([]*traces.Node{}, d.RootSpans...), d.OrphanSpans...), d.DisconnectedSpans...)
	all := map[string]*traces.Node{}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if all[n.SpanID] != nil {
			panic("cyclic/duplicate API output")
		}
		all[n.SpanID] = n
		stack = append(stack, n.Children...)
	}
	return all
}
func assertDemo(t *testing.T, d *traces.Detail) {
	t.Helper()
	all := flatten(d)
	if len(all) != 5 || len(d.RootSpans) != 1 || d.RootSpans[0].SpanName != "GET /checkout" || d.Diagnostics.Completeness != "unknown" || d.Diagnostics.HasDetectedGaps {
		t.Fatal("incorrect real trace shape", d.Diagnostics)
	}
	var g, c, p, v, i *traces.Node
	services := map[string]int{}
	for _, n := range all {
		services[n.ServiceName]++
		switch n.SpanName {
		case "GET /checkout":
			g = n
		case "HTTP GET":
			c = n
		case "GET /charge":
			p = n
		case "checkout.validate":
			v = n
		case "payment.process":
			i = n
		}
	}
	if g == nil || c == nil || p == nil || v == nil || i == nil || services["gateway"] != 3 || services["payment"] != 2 || c.ParentSpanID != g.SpanID || v.ParentSpanID != g.SpanID || p.ParentSpanID != c.SpanID || i.ParentSpanID != p.SpanID {
		t.Fatal("real parent hierarchy was not preserved")
	}
}

func TestKnownSession2TraceAPI(t *testing.T) {
	d := detail(t, session2Trace)
	assertDemo(t, d)
	s := database(t)
	rows, e := s.GetTraceSpans(context.Background(), session2Trace, 1000)
	if e != nil {
		t.Fatal(e)
	}
	nodes := flatten(d)
	for _, r := range rows {
		n := nodes[r.SpanID]
		if n == nil || n.ParentSpanID != r.ParentSpanID || !n.StartTime.Equal(r.StartTime) || n.DurationNS != r.DurationNS {
			t.Fatal("API differs from stored raw fields")
		}
	}
	t.Logf("Session 2 trace=%s physical=%d unique=%d roots=%d gateway=3 payment=2 completeness=%s", d.TraceID, d.Diagnostics.PhysicalRows, d.Diagnostics.UniqueSpans, d.Diagnostics.ObservedRootCount, d.Diagnostics.Completeness)
}

func TestTraceQueryReplay(t *testing.T) {
	client := &http.Client{Timeout: 8 * time.Second}
	res, e := client.Get("http://localhost:8080/checkout")
	if e != nil {
		t.Fatal(e)
	}
	var response map[string]string
	e = json.NewDecoder(res.Body).Decode(&response)
	res.Body.Close()
	if e != nil || res.StatusCode != 200 {
		t.Fatal("demo failed", e)
	}
	tid := response["trace_id"]
	deadline := time.Now().Add(25 * time.Second)
	for time.Now().Before(deadline) {
		var d traces.Detail
		if code := api(t, "/api/v1/traces/"+tid, &d); code == 200 && d.Diagnostics.UniqueSpans == 5 {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	d := detail(t, tid)
	assertDemo(t, d)
	s := database(t)
	rows, e := s.GetTraceSpans(context.Background(), tid, 100)
	if e != nil {
		t.Fatal(e)
	}
	req := &collector.ExportTraceServiceRequest{}
	for _, r := range rows {
		sp := &trace.Span{}
		rs := &resource.Resource{}
		scope := &common.InstrumentationScope{}
		for _, pair := range []struct {
			raw []byte
			msg proto.Message
		}{{r.Span, sp}, {r.Resource, rs}, {r.Scope, scope}} {
			if e := protojson.Unmarshal(pair.raw, pair.msg); e != nil {
				t.Fatal(e)
			}
		}
		req.ResourceSpans = append(req.ResourceSpans, &trace.ResourceSpans{Resource: rs, SchemaUrl: r.ResourceSchemaURL, ScopeSpans: []*trace.ScopeSpans{{Scope: scope, SchemaUrl: r.ScopeSchemaURL, Spans: []*trace.Span{sp}}}})
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	ack, e := grpcClient(t).Export(ctx, req)
	if e != nil || ack.PartialSuccess != nil {
		t.Fatal("replay not acknowledged", e)
	}
	after := detail(t, tid)
	assertDemo(t, after)
	if after.Diagnostics.PhysicalRows != 10 || after.Diagnostics.UniqueSpans != 5 || after.Diagnostics.ExactDuplicateRows != 5 || after.Diagnostics.ConflictingDuplicateGroups != 0 {
		t.Fatal("read-time replay deduplication failed", after.Diagnostics)
	}
	// Discover payment, then hydrate independently of its service filter.
	q := url.Values{"service": {"payment"}, "from": {d.Envelope.StartTime.Add(-time.Second).Format(time.RFC3339Nano)}, "to": {d.Envelope.EndTime.Add(time.Second).Format(time.RFC3339Nano)}, "limit": {"100"}}
	var page struct {
		Traces []traces.Summary `json:"traces"`
	}
	if api(t, "/api/v1/traces?"+q.Encode(), &page) != 200 {
		t.Fatal("payment search failed")
	}
	found := false
	for _, r := range page.Traces {
		if r.TraceID == tid {
			found = true
			if r.UniqueSpans != 5 || len(r.Services) != 2 {
				t.Fatal("search lost other services or counted replay twice")
			}
		}
	}
	if !found {
		t.Fatal("payment search missed real trace")
	}
	t.Logf("replayed real trace=%s physical_rows=10 unique_spans=5 exact_duplicate_rows=5; payment search/detail retain gateway", tid)
}

func TestSyntheticGraphAPI(t *testing.T) {
	req, tid := request(t)
	base := uint64(time.Now().UnixNano())
	ss := req.ResourceSpans[0].ScopeSpans[0]
	ss.Spans = nil
	smallID := func(n byte) []byte { b := make([]byte, 8); b[7] = n; return b }
	for _, pair := range [][2]byte{{6, 5}, {2, 1}, {4, 3}, {1, 0}, {5, 6}, {7, 0}} {
		sp := &trace.Span{TraceId: mustHex(t, tid), SpanId: smallID(pair[0]), Name: fmt.Sprintf("fixture-%d", pair[0]), StartTimeUnixNano: base, EndTimeUnixNano: base + 1000}
		if pair[1] != 0 {
			sp.ParentSpanId = smallID(pair[1])
		}
		if pair[0] == 1 {
			sp.StartTimeUnixNano = base + 10
		}
		ss.Spans = append(ss.Spans, sp)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	c := grpcClient(t)
	if _, e := c.Export(ctx, req); e != nil {
		t.Fatal(e)
	}
	var child *trace.Span
	for _, sp := range ss.Spans {
		if bytes.Equal(sp.SpanId, smallID(2)) {
			child = sp
		}
	}
	conflict := proto.Clone(child).(*trace.Span)
	conflict.Name = "zz-conflicting-version"
	ss.Spans = []*trace.Span{child, conflict}
	if _, e := c.Export(ctx, req); e != nil {
		t.Fatal(e)
	}
	d := detail(t, tid)
	a := d.Diagnostics
	if a.PhysicalRows != 8 || a.UniqueSpans != 6 || a.ExactDuplicateRows != 1 || a.ConflictingDuplicateGroups != 1 || a.ObservedRootCount != 2 || a.OrphanSpanCount != 1 || a.CycleCount != 1 || a.MissingParentCount != 1 || !a.HasDetectedGaps || a.Completeness != "unknown" || len(flatten(d)) != 6 || len(a.TimingWarnings) == 0 {
		t.Fatalf("bad anomaly diagnostics %+v", a)
	}
	if flatten(d)[hex.EncodeToString(smallID(2))].SpanName != "fixture-2" {
		t.Fatal("canonical conflict selection changed")
	}
	t.Logf("synthetic trace=%s physical=8 unique=6 exact=1 conflicting_groups=1 roots=2 orphans=1 cycles=1 missing_parent=%s timing_warnings=%d", tid, a.MissingParentIDs[0], len(a.TimingWarnings))
}
func mustHex(t *testing.T, s string) []byte {
	t.Helper()
	b, e := hex.DecodeString(s)
	if e != nil {
		t.Fatal(e)
	}
	return b
}

func TestTraceSearchPaging(t *testing.T) {
	c := grpcClient(t)
	service := "s03-page-" + hex.EncodeToString(id(t, 8))
	base := time.Now().UTC().Add(-time.Minute).Truncate(time.Second).Add(123456789 * time.Nanosecond)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	expected := map[string]bool{}
	for k := 0; k < 6; k++ {
		req, tid := request(t)
		rs := req.ResourceSpans[0]
		rs.Resource.Attributes = []*common.KeyValue{attr("service.name", service)}
		root := rs.ScopeSpans[0].Spans[0]
		start := base.Add(time.Duration(k/2) * 10 * time.Nanosecond)
		if k == 5 {
			start = base.Add(100 * time.Nanosecond)
		} else {
			expected[tid] = true
		}
		root.StartTimeUnixNano = uint64(start.UnixNano())
		root.EndTimeUnixNano = root.StartTimeUnixNano + 1000
		if k%2 == 1 {
			root.Status = &trace.Status{Code: trace.Status_STATUS_CODE_ERROR}
		}
		child := &trace.Span{TraceId: root.TraceId, SpanId: id(t, 8), ParentSpanId: root.SpanId, Name: "child-service", StartTimeUnixNano: root.StartTimeUnixNano + 1, EndTimeUnixNano: root.EndTimeUnixNano, Status: &trace.Status{Code: trace.Status_STATUS_CODE_ERROR}}
		req.ResourceSpans = append(req.ResourceSpans, &trace.ResourceSpans{Resource: &resource.Resource{Attributes: []*common.KeyValue{attr("service.name", "paging-child")}}, ScopeSpans: []*trace.ScopeSpans{{Spans: []*trace.Span{child}}}})
		if _, e := c.Export(ctx, req); e != nil {
			t.Fatal(e)
		}
	}
	query := url.Values{"service": {service}, "from": {base.Add(-time.Nanosecond).Format(time.RFC3339Nano)}, "to": {base.Add(100 * time.Nanosecond).Format(time.RFC3339Nano)}, "limit": {"2"}}
	seen := map[string]bool{}
	pages := 0
	var previous *traces.Summary
	for {
		var page struct {
			Traces []traces.Summary `json:"traces"`
			Next   string           `json:"next_cursor"`
		}
		if code := api(t, "/api/v1/traces?"+query.Encode(), &page); code != 200 {
			t.Fatalf("search HTTP %d", code)
		}
		pages++
		for _, r := range page.Traces {
			if !expected[r.TraceID] || seen[r.TraceID] || r.UniqueSpans != 2 || len(r.Services) != 2 || !r.HasError {
				t.Fatal("invalid/duplicate summary", r)
			}
			if previous != nil && (r.LatestStart.After(previous.LatestStart) || (r.LatestStart.Equal(previous.LatestStart) && r.TraceID <= previous.TraceID)) {
				t.Fatal("keyset ordering unstable")
			}
			copy := r
			previous = &copy
			seen[r.TraceID] = true
		}
		if page.Next == "" {
			break
		}
		if pages > 4 {
			t.Fatal("cursor failed to terminate")
		}
		query = url.Values{"cursor": {page.Next}}
	}
	if pages != 3 || len(seen) != 5 {
		t.Fatal("nanosecond boundaries/pagination lost traces", pages, len(seen))
	}
	query = url.Values{"service": {service}, "status": {"error"}, "from": {base.Add(-time.Nanosecond).Format(time.RFC3339Nano)}, "to": {base.Add(100 * time.Nanosecond).Format(time.RFC3339Nano)}}
	var filtered struct {
		Traces []traces.Summary `json:"traces"`
	}
	if api(t, "/api/v1/traces?"+query.Encode(), &filtered) != 200 || len(filtered.Traces) != 2 {
		t.Fatal("service and error filter did not match same span")
	}
	ids := make([]string, 0, len(seen))
	for id := range seen {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	t.Logf("service=%s nanosecond-window pagination pages=3 traces=5, exclusive-to fixture excluded; same-span error matches=2; trace_ids=%s", service, strings.Join(ids, ","))
}

func TestQueryDatabaseUnavailable(t *testing.T) {
	if os.Getenv("TRACEFORGE_EXPECT_DB_DOWN") != "1" {
		t.Skip("controlled outage only")
	}
	for _, path := range []string{"/api/v1/traces/" + session2Trace, "/api/v1/traces?limit=1"} {
		var body map[string]any
		code := api(t, path, &body)
		if code != 503 {
			t.Fatalf("expected sanitized 503 during DB stop, got %d", code)
		}
		if body["error"] == nil {
			t.Fatal("missing error body")
		}
	}
	t.Log("detail/search returned structured 503 during ClickHouse downtime")
}

func TestTraceDetailLimitAPI(t *testing.T) {
	req, tid := request(t)
	ss := req.ResourceSpans[0].ScopeSpans[0]
	sp := ss.Spans[0]
	ss.Spans = make([]*trace.Span, 1000)
	for i := range ss.Spans {
		ss.Spans[i] = sp
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	c := grpcClient(t)
	if _, e := c.Export(ctx, req); e != nil {
		t.Fatal(e)
	}
	ss.Spans = ss.Spans[:1]
	if _, e := c.Export(ctx, req); e != nil {
		t.Fatal(e)
	}
	var body map[string]any
	if code := api(t, "/api/v1/traces/"+tid, &body); code != 413 || body["error"] == nil {
		t.Fatalf("oversized physical trace not rejected: %d %+v", code, body)
	}
	t.Logf("trace=%s physical_rows=1001 logical_ids=1; detail HTTP 413 before deduplication", tid)
}
