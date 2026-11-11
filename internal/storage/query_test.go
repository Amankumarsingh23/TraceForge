package storage

import (
	"context"
	"errors"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"reflect"
	"strings"
	"testing"
	"time"
	"traceforge/internal/traces"
)

type queryRows struct {
	driver.Rows
	values           [][]any
	at               int
	closed           bool
	err, errorOnScan error
}

func (r *queryRows) Next() bool { r.at++; return r.at <= len(r.values) }
func (r *queryRows) Scan(dest ...any) error {
	if r.errorOnScan != nil {
		return r.errorOnScan
	}
	for i, v := range r.values[r.at-1] {
		reflect.ValueOf(dest[i]).Elem().Set(reflect.ValueOf(v))
	}
	return nil
}
func (r *queryRows) Close() error { r.closed = true; return nil }
func (r *queryRows) Err() error   { return r.err }

type queryConn struct {
	driver.Conn
	rows *queryRows
	err  error
	sql  string
	args []any
}

func (c *queryConn) Query(ctx context.Context, sql string, args ...any) (driver.Rows, error) {
	c.sql = sql
	c.args = args
	return c.rows, c.err
}

const queryTrace = "11111111111111111111111111111111"

func values() []any {
	now := time.Unix(1700000000, 123).UTC()
	return []any{queryTrace, "1111111111111111", "", "gateway", "request", uint8(2), now, now.Add(time.Second), uint64(time.Second), uint8(0), "", "[]", "[]", "scope", "v1", "[]", "[]", "{}", "{}", "{}", "", "", now}
}

func TestDetailQueryLimitsDecodeAndCleanup(t *testing.T) {
	for _, tc := range []struct {
		name            string
		count           int
		rowerr, scanerr bool
		code            error
	}{{"success", 1, false, false, nil}, {"sentinel", 3, false, false, traces.ErrTooLarge}, {"iteration_error", 0, true, false, traces.ErrBackend}, {"scan_error", 1, false, true, traces.ErrCorrupt}} {
		t.Run(tc.name, func(t *testing.T) {
			r := &queryRows{}
			for i := 0; i < tc.count; i++ {
				r.values = append(r.values, values())
			}
			if tc.rowerr {
				r.err = errors.New("private details")
			}
			if tc.scanerr {
				r.errorOnScan = errors.New("private details")
			}
			c := &queryConn{rows: r}
			rows, e := (&Store{Conn: c}).GetTraceSpans(context.Background(), queryTrace, 2)
			if !errors.Is(e, tc.code) || !r.closed {
				t.Fatalf("outcome/cleanup %v %v", e, r.closed)
			}
			if tc.code == nil && (len(rows) != 1 || rows[0].StartTime.Nanosecond() != 123) {
				t.Fatal("lost fields")
			}
			if !strings.Contains(c.sql, "LIMIT ?") || c.args[0] != queryTrace || c.args[1] != 3 {
				t.Fatal("unbounded or interpolated lookup")
			}
		})
	}
	r := &queryRows{values: [][]any{values()}}
	r.values[0][11] = "broken json"
	if _, e := (&Store{Conn: &queryConn{rows: r}}).GetTraceSpans(context.Background(), queryTrace, 2); !errors.Is(e, traces.ErrCorrupt) || !r.closed {
		t.Fatal("invalid JSON escaped repository")
	}
}

func TestRepositoryQueryCancellationAndParameters(t *testing.T) {
	c := &queryConn{err: errors.New("password=do-not-leak")}
	s := &Store{Conn: c}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := s.GetTraceSpans(ctx, queryTrace, 2); !errors.Is(e, context.Canceled) {
		t.Fatal("lost cancellation")
	}
	if _, e := s.GetTraceSpans(context.Background(), queryTrace, 2); !errors.Is(e, traces.ErrBackend) {
		t.Fatal("leaked backend failure")
	}
	c.err = context.DeadlineExceeded
	if _, e := s.GetTraceSpans(context.Background(), queryTrace, 2); !errors.Is(e, traces.ErrBackend) {
		t.Fatal("driver dial timeout mislabeled as request timeout")
	}
	c.err = nil
	c.rows = &queryRows{}
	now := time.Unix(1700000000, 123456789).UTC()
	q := traces.SearchQuery{From: now.Add(-time.Hour), To: now, Limit: 10, Service: "payment' OR 1=1--", Status: "error", After: &traces.Position{LatestStart: now.Add(-time.Minute), TraceID: queryTrace}}
	if _, e := s.SearchTraces(context.Background(), q); e != nil {
		t.Fatal(e)
	}
	if strings.Contains(c.sql, q.Service) || !strings.Contains(c.sql, "fromUnixTimestamp64Nano") || c.args[0] != q.From.UnixNano() || c.args[5] != q.After.LatestStart.UnixNano() || !c.rows.closed {
		t.Fatal("unsafe binding or lost precision")
	}
	if len(c.args) != 9 || c.args[8] != 11 {
		t.Fatal("search page not bounded")
	}
}
