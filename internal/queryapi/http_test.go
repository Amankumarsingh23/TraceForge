package queryapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
	"traceforge/internal/traces"
)

const traceID = "11111111111111111111111111111111"

type fakeRepo struct {
	rows     []traces.Record
	items    []traces.Summary
	err      error
	captured traces.SearchQuery
	wait     bool
	entered  chan struct{}
	release  chan struct{}
}

func (f *fakeRepo) GetTraceSpans(ctx context.Context, id string, limit int) ([]traces.Record, error) {
	if f.entered != nil {
		f.entered <- struct{}{}
	}
	if f.release != nil {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-f.release:
		}
	}
	if f.wait {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return f.rows, f.err
}
func (f *fakeRepo) SearchTraces(ctx context.Context, q traces.SearchQuery) ([]traces.Summary, error) {
	f.captured = q
	if f.wait {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return f.items, f.err
}
func handlerFor(t *testing.T, f *fakeRepo, timeout time.Duration) http.Handler {
	t.Helper()
	c := DefaultConfig()
	c.Timeout = timeout
	c.Now = func() time.Time { return time.Date(2026, 11, 12, 12, 0, 0, 0, time.UTC) }
	h, e := NewHandler(f, c)
	if e != nil {
		t.Fatal(e)
	}
	return h
}
func call(h http.Handler, path string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
	return w
}
func validRecord() traces.Record {
	return traces.Record{TraceID: traceID, SpanID: "1111111111111111", ServiceName: "gateway", SpanName: "request", StartTime: time.Unix(1700000000, 123), EndTime: time.Unix(1700000000, 456), DurationNS: 333}
}

func TestDetailResponses(t *testing.T) {
	for _, tc := range []struct {
		name, path string
		rows       []traces.Record
		err        error
		code       int
	}{
		{"valid", "/api/v1/traces/" + traceID, []traces.Record{validRecord()}, nil, 200},
		{"unknown", "/api/v1/traces/" + traceID, nil, nil, 404},
		{"uppercase", "/api/v1/traces/" + strings.Repeat("A", 32), nil, nil, 400},
		{"zero", "/api/v1/traces/" + strings.Repeat("0", 32), nil, nil, 400},
		{"short", "/api/v1/traces/abc", nil, nil, 400},
		{"extra_filter", "/api/v1/traces/" + traceID + "?service=payment", nil, nil, 400},
		{"unavailable", "/api/v1/traces/" + traceID, nil, errors.New("password=do-not-leak"), 503},
		{"oversized", "/api/v1/traces/" + traceID, nil, traces.ErrTooLarge, 413},
		{"corrupt", "/api/v1/traces/" + traceID, nil, traces.ErrCorrupt, 500},
		{"canceled", "/api/v1/traces/" + traceID, nil, context.Canceled, 408},
		{"timeout", "/api/v1/traces/" + traceID, nil, context.DeadlineExceeded, 504},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := call(handlerFor(t, &fakeRepo{rows: tc.rows, err: tc.err}, time.Second), tc.path)
			if w.Code != tc.code || !json.Valid(w.Body.Bytes()) || strings.Contains(w.Body.String(), "do-not-leak") {
				t.Fatalf("status=%d body=%s", w.Code, w.Body)
			}
			if tc.code == 200 {
				var d traces.Detail
				if e := json.Unmarshal(w.Body.Bytes(), &d); e != nil || d.Diagnostics.UniqueSpans != 1 || d.Diagnostics.Completeness != "unknown" {
					t.Fatal("bad detail", e)
				}
			}
		})
	}
}

func TestSearchInputValidation(t *testing.T) {
	for _, query := range []string{"?from=no", "?from=2026-11-11T00:00:00Z&to=2026-11-12T12:00:00Z", "?from=2026-11-12T12:00:00Z&to=2026-11-12T11:00:00Z", "?to=2026-11-12T12:00:00%2B00:00", "?limit=0", "?limit=101", "?limit=-2", "?status=success", "?service=payment%27%20OR%201%3D1--", "?unknown=x", "?limit=1&limit=2", "?cursor=bad!", "?from=%"} {
		t.Run(query, func(t *testing.T) {
			w := call(handlerFor(t, &fakeRepo{}, time.Second), "/api/v1/traces"+query)
			if w.Code != 400 {
				t.Fatalf("accepted %s (%d)", query, w.Code)
			}
		})
	}
}

func TestSearchPaginationAndFilterConsistency(t *testing.T) {
	now := time.Date(2026, 11, 12, 12, 0, 0, 123456789, time.UTC)
	f := &fakeRepo{items: []traces.Summary{{TraceID: traceID, LatestStart: now.Add(-time.Minute)}, {TraceID: strings.Repeat("2", 32), LatestStart: now.Add(-2 * time.Minute)}}}
	h := handlerFor(t, f, time.Second)
	w := call(h, "/api/v1/traces?service=payment&status=error&limit=1")
	if w.Code != 200 || f.captured.Service != "payment" || f.captured.Status != "error" {
		t.Fatal("filters not passed")
	}
	var page struct {
		Traces []traces.Summary `json:"traces"`
		Next   string           `json:"next_cursor"`
	}
	if e := json.Unmarshal(w.Body.Bytes(), &page); e != nil || page.Next == "" || len(page.Traces) != 1 {
		t.Fatal("bad next page", e)
	}
	q, e := ParseSearch(url.Values{"cursor": {page.Next}}, now.Add(time.Hour), 100)
	if e != nil || q.Service != "payment" || q.Status != "error" || q.Limit != 1 || q.After.TraceID != traceID || q.After.LatestStart.Nanosecond() != 123456789 {
		t.Fatalf("cursor lost filters/precision: %+v %v", q, e)
	}
	if !q.To.Equal(f.captured.To) {
		t.Fatal("default time window moved during pagination")
	}
	for k, v := range map[string]string{"service": "gateway", "status": "", "limit": "2", "from": "2026-11-12T10:00:00Z"} {
		if _, e := ParseSearch(url.Values{"cursor": {page.Next}, k: {v}}, now, 100); e == nil {
			t.Fatal("cursor mismatch accepted", k)
		}
	}
	bad := base64.RawURLEncoding.EncodeToString([]byte(`{"v":1,"unexpected":"field"}`))
	if _, e := ParseSearch(url.Values{"cursor": {bad}}, now, 100); e == nil {
		t.Fatal("unknown cursor structure accepted")
	}
	f.items = nil
	w = call(h, "/api/v1/traces")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"traces":[]`) {
		t.Fatal("empty search shape")
	}
}

func TestAPIContextAndConcurrency(t *testing.T) {
	h := handlerFor(t, &fakeRepo{wait: true}, 10*time.Millisecond)
	if w := call(h, "/api/v1/traces/"+traceID); w.Code != 504 {
		t.Fatal("query timeout not enforced", w.Code)
	}
	r := httptest.NewRequest("GET", "/api/v1/traces", nil)
	ctx, cancel := context.WithCancel(r.Context())
	cancel()
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r.WithContext(ctx))
	if w.Code != 408 {
		t.Fatal("cancellation not propagated")
	}
	f := &fakeRepo{rows: []traces.Record{validRecord()}, entered: make(chan struct{}, 4), release: make(chan struct{})}
	h = handlerFor(t, f, time.Second)
	done := make(chan int, 4)
	for i := 0; i < 4; i++ {
		go func() { done <- call(h, "/api/v1/traces/"+traceID).Code }()
	}
	for i := 0; i < 4; i++ {
		<-f.entered
	}
	if w := call(h, "/api/v1/traces/"+traceID); w.Code != 503 {
		t.Fatal("read concurrency unbounded")
	}
	close(f.release)
	for i := 0; i < 4; i++ {
		if code := <-done; code != 200 {
			t.Fatal(code)
		}
	}
}

func TestAPIMethodAndConfig(t *testing.T) {
	h := handlerFor(t, &fakeRepo{}, time.Second)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/api/v1/traces", nil))
	if w.Code != 405 || !json.Valid(w.Body.Bytes()) {
		t.Fatal("method error not structured")
	}
	c := DefaultConfig()
	c.MaxRows = traces.HardMaxRows + 1
	if _, e := NewHandler(&fakeRepo{}, c); e == nil {
		t.Fatal("unbounded configuration accepted")
	}
}
