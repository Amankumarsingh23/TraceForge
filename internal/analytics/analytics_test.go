package analytics

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"traceforge/internal/traces"
)

var start = time.Date(2026, 11, 13, 10, 0, 0, 0, time.UTC)

func query() Query { return Query{From: start, To: start.Add(time.Minute), BucketSeconds: 60} }
func record(trace, span int, name string, kind uint8, parent int, at time.Time, ms int, status uint8) traces.Record {
	p := ""
	if parent > 0 {
		p = fmt.Sprintf("%016x", parent)
	}
	return traces.Record{TraceID: fmt.Sprintf("%032x", trace), SpanID: fmt.Sprintf("%016x", span), ParentSpanID: p, ServiceName: name, SpanName: "operation", SpanKind: kind, StartTime: at, EndTime: at.Add(time.Duration(ms) * time.Millisecond), DurationNS: uint64(ms) * 1e6, StatusCode: status, ResourceAttributes: json.RawMessage(fmt.Sprintf(`[{"key":"service.name","value":{"stringValue":%q}}]`, name)), SpanAttributes: json.RawMessage(`[]`), Events: json.RawMessage(`[]`), Links: json.RawMessage(`[]`), Resource: json.RawMessage(`{}`), Scope: json.RawMessage(`{}`), Span: json.RawMessage(`{}`), IngestedAt: start}
}
func compute(t *testing.T, q Query, rows []traces.Record) *Result {
	t.Helper()
	r, e := Compute(context.Background(), q, rows)
	if e != nil {
		t.Fatal(e)
	}
	return r
}
func TestREDAndNearestRank(t *testing.T) {
	rows := []traces.Record{}
	for i, ms := range []int{10, 20, 30, 40, 50, 60, 70, 80, 90, 1000} {
		r := record(i+1, 1, "payment", 2, 0, start, ms, 0)
		if i == 9 {
			r.StatusCode = 2
		}
		rows = append(rows, r, record(i+1, 2, "payment", 1, 1, start, 2, 2))
	}
	r := compute(t, query(), rows)
	s := r.Services[0].Stats
	if s.Count != 10 || s.Errors != 1 || s.Rate != 10.0/60 || *s.ErrorFraction != .1 || *s.Mean != 145 || *s.P50 != 50 || *s.P95 != 1000 || *s.P99 != 1000 || s.Status["UNSET"] != 9 {
		t.Fatalf("wrong RED %+v", s)
	}
}
func TestPairingReplayConflictAndRetries(t *testing.T) {
	rows := []traces.Record{record(1, 1, "gateway", 2, 0, start, 30, 0), record(1, 2, "gateway", 3, 1, start, 20, 2), record(1, 3, "gateway", 3, 1, start, 10, 0), record(1, 4, "payment", 2, 3, start, 8, 0), record(1, 5, "payment", 1, 4, start, 2, 2)}
	rows = append(rows, rows...)
	conflict := rows[3]
	conflict.ServiceName = "other"
	conflict.ResourceAttributes = json.RawMessage(`[{"key":"service.name","value":{"stringValue":"other"}}]`)
	conflict.IngestedAt = start.Add(time.Second)
	rows = append(rows, conflict)
	r := compute(t, query(), rows)
	if len(r.Edges) != 1 || r.Edges[0].Client.Count != 1 || r.Diagnostics.UnmatchedClients != 1 || r.Diagnostics.DuplicateRows != 5 || r.Diagnostics.ConflictingGroups != 1 || r.Services[1].Stats.Errors != 0 {
		t.Fatalf("wrong pair diagnostics %+v", r)
	}
	// Shuffling physical observations cannot select a later conflicting variant.
	for i, j := 0, len(rows)-1; i < j; i, j = i+1, j-1 {
		rows[i], rows[j] = rows[j], rows[i]
	}
	b := compute(t, query(), rows)
	aJSON, _ := json.Marshal(r)
	bJSON, _ := json.Marshal(b)
	if string(aJSON) != string(bJSON) {
		t.Fatal("order-dependent result")
	}
}
func TestCrossWindowAndBoundary(t *testing.T) {
	rows := []traces.Record{record(1, 1, "gateway", 3, 0, start.Add(-100*time.Millisecond), 300, 0), record(1, 2, "payment", 2, 1, start.Add(100*time.Millisecond), 100, 0)}
	r := compute(t, query(), rows)
	if len(r.Edges) != 0 || len(r.Services) != 1 || r.Services[0].Stats.Count != 1 {
		t.Fatal("edge belongs to earlier client window")
	}
	q := query()
	q.From = start.Add(-time.Minute)
	q.To = start
	r = compute(t, q, rows)
	if len(r.Edges) != 1 || r.Edges[0].Server.Count != 1 {
		t.Fatal("lost cross-window server")
	}
	for _, s := range r.Services {
		if s.Stats.Count != 0 || s.Stats.P95 != nil || s.Stats.ErrorFraction != nil {
			t.Fatal("edge endpoint has fake inbound observations")
		}
	}
}
func TestGraphDiagnostics(t *testing.T) {
	cases := []struct {
		name  string
		rows  []traces.Record
		check func(Diagnostics) bool
	}{
		{"ambiguous", []traces.Record{record(1, 1, "gateway", 3, 0, start, 10, 0), record(1, 2, "payment", 2, 1, start, 5, 0), record(1, 3, "payment", 2, 1, start, 5, 0)}, func(d Diagnostics) bool { return d.AmbiguousClients == 1 }},
		{"same service", []traces.Record{record(1, 1, "gateway", 3, 0, start, 10, 0), record(1, 2, "gateway", 2, 1, start, 5, 0)}, func(d Diagnostics) bool { return d.SameServicePairs == 1 }},
		{"missing parent", []traces.Record{record(1, 2, "payment", 2, 1, start, 5, 0)}, func(d Diagnostics) bool { return d.MissingServerParents == 1 }},
		{"wrong kind", []traces.Record{record(1, 1, "gateway", 1, 0, start, 10, 0), record(1, 2, "payment", 2, 1, start, 5, 0)}, func(d Diagnostics) bool { return d.WrongParentKinds == 1 }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := compute(t, query(), c.rows)
			if len(r.Edges) != 0 || !c.check(r.Diagnostics) {
				t.Fatalf("bad evidence %+v", r.Diagnostics)
			}
		})
	}
}
func TestPartialBucketsAndEmpty(t *testing.T) {
	q := query()
	q.To = start.Add(90 * time.Second)
	r := compute(t, q, []traces.Record{record(1, 1, "payment", 2, 0, start.Add(80*time.Second), 50, 0)})
	s := r.Services[0]
	if len(s.Buckets) != 2 || s.Buckets[0].Stats.Count != 0 || s.Buckets[0].Stats.P95 != nil || s.Buckets[1].Seconds != 30 || s.Buckets[1].Stats.Rate != 1.0/30 || s.Stats.Rate != 1.0/90 {
		t.Fatal("wrong partial denominator")
	}
	empty := compute(t, q, nil)
	if len(empty.Services) != 0 || len(empty.Edges) != 0 {
		t.Fatal("fake empty observations")
	}
}
func TestIdentityAndHTTP(t *testing.T) {
	a := record(1, 1, "payment", 2, 0, start, 1, 0)
	b := record(2, 1, "payment", 2, 0, start, 1, 2)
	b.ResourceAttributes = json.RawMessage(`[{"key":"service.name","value":{"stringValue":"payment"}},{"key":"service.namespace","value":{"stringValue":"isolated"}},{"key":"traceforge.workload.kind","value":{"stringValue":"synthetic"}}]`)
	b.SpanAttributes = json.RawMessage(`[{"key":"http.response.status_code","value":{"intValue":"503"}}]`)
	r := compute(t, query(), []traces.Record{a, b})
	if len(r.Services) != 2 {
		t.Fatal("collapsed identities")
	}
	q := query()
	q.Namespace = "isolated"
	r = compute(t, q, []traces.Record{a, b})
	if len(r.Services) != 1 || r.Services[0].Stats.HTTPStatus["503"] != 1 {
		t.Fatal("identity or pinned HTTP convention")
	}
	a.ResourceAttributes = json.RawMessage(`[]`)
	r = compute(t, query(), []traces.Record{a})
	if len(r.Services) != 0 || r.Diagnostics.MissingServiceNames != 1 {
		t.Fatal("invented service identity")
	}
}
func TestBudgetsAndCancellation(t *testing.T) {
	rows := []traces.Record{}
	for i := 1; i <= MaxTraces+1; i++ {
		rows = append(rows, record(i, 1, "payment", 2, 0, start, 1, 0))
	}
	if _, e := Compute(context.Background(), query(), rows); !errors.Is(e, traces.ErrTooLarge) {
		t.Fatal(e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := Compute(ctx, query(), rows[:1]); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
}

type fakeRepo struct {
	err   error
	calls int
}

func TestCombinedDistributionAndKindEligibility(t *testing.T) {
	q := query()
	q.To = start.Add(2 * time.Minute)
	rows := []traces.Record{record(1, 1, "payment", 2, 0, start, 1000, 0)}
	for i := 2; i <= 101; i++ {
		rows = append(rows, record(i, 1, "payment", 2, 0, start.Add(time.Minute), 10, 0))
	}
	for _, kind := range []uint8{1, 3, 4, 5} {
		rows = append(rows, record(102+int(kind), 1, "payment", kind, 0, start, 2000, 2))
	}
	r := compute(t, q, rows)
	s := r.Services[0]
	if s.Stats.Count != 101 || *s.Stats.P95 != 10 || *s.Buckets[0].Stats.P95 != 1000 || *s.Buckets[1].Stats.P95 != 10 || s.Stats.Errors != 0 {
		t.Fatal("counted other span kinds or averaged bucket percentiles", s.Stats)
	}
}
func TestHTTPDoesNotOverrideOTelStatusAndInvalidDurations(t *testing.T) {
	a := record(1, 1, "payment", 2, 0, start, 10, 0)
	a.SpanAttributes = json.RawMessage(`[{"key":"http.response.status_code","value":{"intValue":"404"}}]`)
	b := record(2, 1, "payment", 2, 0, start, 10, 2)
	b.EndTime = start.Add(-time.Millisecond)
	r := compute(t, query(), []traces.Record{a, b})
	s := r.Services[0].Stats
	if s.Count != 2 || s.Errors != 1 || s.Status["UNSET"] != 1 || s.HTTPStatus["404"] != 1 || s.Samples != 1 || r.Diagnostics.InvalidDurations != 1 {
		t.Fatal("HTTP/status/duration denominators confused", s)
	}
}
func TestIdentityAndTraceBudgets(t *testing.T) {
	rows := []traces.Record{}
	for i := 1; i <= MaxServices+1; i++ {
		rows = append(rows, record(1, i, fmt.Sprintf("service-%d", i), 2, 0, start, 1, 0))
	}
	if _, e := Compute(context.Background(), query(), rows); !errors.Is(e, traces.ErrTooLarge) {
		t.Fatal("service budget", e)
	}
	rows = nil
	for i := 1; i <= traces.HardMaxRows+1; i++ {
		rows = append(rows, record(1, i, "payment", 2, 0, start, 1, 0))
	}
	if _, e := Compute(context.Background(), query(), rows); !errors.Is(e, traces.ErrTooLarge) {
		t.Fatal("trace budget", e)
	}
}

func (f *fakeRepo) AnalyticsRows(context.Context, Query) ([]traces.Record, error) {
	f.calls++
	return nil, f.err
}
func TestHTTPValidationAndSafeErrors(t *testing.T) {
	for _, tc := range []struct {
		url    string
		err    error
		status int
	}{{"/api/v1/services?bucket_seconds=1", nil, 400}, {"/api/v1/services?from=bad", nil, 400}, {"/api/v1/services?workload=synthetic&workload=live", nil, 400}, {"/api/v1/services?limit=5", nil, 400}, {"/api/v1/services", traces.ErrTooLarge, 413}, {"/api/v1/service-graph", errors.New("password=secret"), 503}, {"/api/v1/services", nil, 200}} {
		f := &fakeRepo{err: tc.err}
		w := httptest.NewRecorder()
		NewHandler(f).ServeHTTP(w, httptest.NewRequest("GET", tc.url, nil))
		if w.Code != tc.status || strings.Contains(w.Body.String(), "secret") {
			t.Fatal(w.Code, w.Body.String())
		}
		if tc.status == 400 && f.calls != 0 {
			t.Fatal("queried invalid input")
		}
	}
}
