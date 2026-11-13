package logs

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestQueryFiltersAndCursor(t *testing.T) {
	now := time.Unix(1800000000, 123456789).UTC()
	values := url.Values{"from": {now.Add(-time.Hour).Format(time.RFC3339Nano)}, "to": {now.Format(time.RFC3339Nano)}, "service": {"payment"}, "min_severity": {"17"}, "trace_id": {strings.Repeat("a", 32)}, "span_id": {strings.Repeat("b", 16)}, "text": {"literal %_?"}, "limit": {"2"}}
	q, e := ParseQuery(values, "", now)
	if e != nil || q.MinSeverity != 17 || q.From.Nanosecond() != 123456789 {
		t.Fatal("valid query failed", e)
	}
	last := Row{LogID: strings.Repeat("c", 32), EffectiveTime: now.Add(-time.Second)}
	token := EncodeCursor(q, last)
	next, e := ParseQuery(url.Values{"cursor": {token}}, "", now.Add(time.Minute))
	if e != nil || !sameFilters(q, next) || next.After.LogID != last.LogID {
		t.Fatal("cursor not precise/filter-bound", e)
	}
	for key, value := range map[string]string{"service": "gateway", "min_severity": "9", "trace_id": strings.Repeat("d", 32), "span_id": strings.Repeat("e", 16), "text": "other", "limit": "3", "from": now.Add(-2 * time.Hour).Format(time.RFC3339Nano)} {
		if _, e := ParseQuery(url.Values{"cursor": {token}, key: {value}}, "", now); e == nil {
			t.Fatal("changed cursor filter accepted", key)
		}
	}
	if _, e := ParseQuery(values, strings.Repeat("d", 32), now); e == nil {
		t.Fatal("path/query trace mismatch")
	}
	for _, raw := range []string{"span_id=" + strings.Repeat("a", 16), "min_severity=25", "limit=0", "trace_id=" + strings.Repeat("0", 32), "service=a&service=b", "unknown=x", "cursor=bad", "from=2026-11-12T00:00:00%2B01:00", "from=2026-11-10T00:00:00Z&to=2026-11-12T00:00:00Z", "text=" + strings.Repeat("x", 257)} {
		v, _ := url.ParseQuery(raw)
		if _, e := ParseQuery(v, "", now); e == nil {
			t.Fatal("invalid query accepted", raw)
		}
	}
}

type queryFake struct {
	rows []Row
	err  error
	q    Query
}

func (f *queryFake) SearchLogs(_ context.Context, q Query) ([]Row, error) {
	f.q = q
	return f.rows, f.err
}
func TestLogHTTPStatusAndNoTraceForeignKey(t *testing.T) {
	f := &queryFake{}
	h := NewQueryHandler(f)
	tid := strings.Repeat("a", 32)
	for _, tc := range []struct {
		err    error
		status int
	}{{nil, 200}, {ErrBackend, 503}, {context.DeadlineExceeded, 504}, {ErrTooLarge, 413}, {ErrCorrupt, 500}, {errors.New("private password"), 503}} {
		f.err = tc.err
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/traces/"+tid+"/logs", nil))
		if w.Code != tc.status || strings.Contains(w.Body.String(), "private") {
			t.Fatal("bad HTTP failure", w.Code, w.Body.String())
		}
		if f.q.TraceID != tid {
			t.Fatal("correlation did not bind trace ID")
		}
		if tc.err == nil {
			var result struct {
				Logs []Row `json:"logs"`
			}
			if json.Unmarshal(w.Body.Bytes(), &result) != nil || len(result.Logs) != 0 {
				t.Fatal("unknown trace must allow empty independent logs")
			}
		}
	}
	f.err = nil
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/logs?span_id="+strings.Repeat("a", 16), nil))
	if w.Code != 400 {
		t.Fatal("span-only filter accepted")
	}
}
