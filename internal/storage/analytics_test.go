package storage

import (
	"context"
	"errors"
	"fmt"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"strings"
	"testing"
	"time"
	"traceforge/internal/analytics"
	"traceforge/internal/traces"
)

type analyticsConn struct {
	driver.Conn
	replies []*queryRows
	sql     []string
	args    [][]any
}

func (c *analyticsConn) Query(ctx context.Context, sql string, args ...any) (driver.Rows, error) {
	c.sql = append(c.sql, sql)
	c.args = append(c.args, args)
	if len(c.sql) > len(c.replies) {
		return nil, errors.New("unexpected query")
	}
	return c.replies[len(c.sql)-1], nil
}
func analyticsQuery() analytics.Query {
	from := time.Date(2026, 11, 13, 10, 0, 0, 0, time.UTC)
	return analytics.Query{From: from, To: from.Add(time.Minute), BucketSeconds: 60}
}
func TestAnalyticsBulkPlanAndSentinel(t *testing.T) {
	discovery := &queryRows{values: [][]any{{queryTrace}, {"22222222222222222222222222222222"}}}
	hydrate := &queryRows{}
	c := &analyticsConn{replies: []*queryRows{discovery, hydrate}}
	s := &Store{Conn: c}
	if _, e := s.AnalyticsRows(context.Background(), analyticsQuery()); e != nil {
		t.Fatal(e)
	}
	if len(c.sql) != 2 || !strings.Contains(c.sql[0], "span_kind IN (2,3)") || strings.Contains(c.sql[1], "start_time >=") || !strings.Contains(c.sql[1], "trace_id IN (?)") || len(c.args[1][0].([]string)) != 2 || c.args[1][1] != analytics.MaxRows+1 || !discovery.closed || !hydrate.closed {
		t.Fatal("not bounded bulk discovery/full hydration", c.sql, c.args)
	}
	rows := &queryRows{}
	for i := 1; i <= analytics.MaxTraces+1; i++ {
		rows.values = append(rows.values, []any{fmt.Sprintf("%032x", i)})
	}
	c = &analyticsConn{replies: []*queryRows{rows}}
	s.Conn = c
	if _, e := s.AnalyticsRows(context.Background(), analyticsQuery()); !errors.Is(e, traces.ErrTooLarge) || len(c.sql) != 1 || !rows.closed {
		t.Fatal("discovery silently truncated", e)
	}
}
func TestAnalyticsNoCandidatesAndDriverFailure(t *testing.T) {
	for _, row := range []*queryRows{{}, {err: errors.New("secret backend error")}, {values: [][]any{{"invalid"}}}} {
		c := &analyticsConn{replies: []*queryRows{row}}
		_, e := (&Store{Conn: c}).AnalyticsRows(context.Background(), analyticsQuery())
		if row.err != nil && !errors.Is(e, traces.ErrBackend) {
			t.Fatal(e)
		}
		if len(row.values) > 0 && !errors.Is(e, traces.ErrCorrupt) {
			t.Fatal(e)
		}
		if len(c.sql) != 1 || !row.closed {
			t.Fatal("unnecessary hydration or leaked rows")
		}
	}
}
