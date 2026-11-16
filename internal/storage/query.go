package storage

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/ClickHouse/clickhouse-go/v2"
	"time"
	"traceforge/internal/traces"
)

const MaxDetailBytes = 8 << 20

func queryContext(ctx context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	return clickhouse.Context(ctx, clickhouse.WithSettings(clickhouse.Settings{
		"max_execution_time": 2, "max_rows_to_read": 1000000, "max_bytes_to_read": 128 << 20, "max_memory_usage": 256 << 20,
		"read_overflow_mode": "throw", "result_overflow_mode": "throw", "max_result_bytes": 32 << 20,
	})), cancel
}
func queryError(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	// A driver's own dial timeout can also wrap DeadlineExceeded while the
	// request context is still live. That is backend unavailability, not an
	// expired API query deadline.
	var ex *clickhouse.Exception
	if errors.As(err, &ex) && ex.Code == 159 {
		return context.DeadlineExceeded
	}
	return traces.ErrBackend
}

func (s *Store) GetTraceSpans(ctx context.Context, traceID string, maxRows int) ([]traces.Record, error) {
	if !traces.ValidID(traceID, 16) {
		return nil, traces.ErrCorrupt
	}
	if maxRows < 1 || maxRows > traces.HardMaxRows {
		return nil, traces.ErrTooLarge
	}
	ctx, cancel := queryContext(ctx)
	defer cancel()
	rows, err := s.query(ctx, `SELECT trace_id,span_id,parent_span_id,service_name,span_name,span_kind,start_time,end_time,duration_ns,status_code,status_message,
resource_attributes_json,span_attributes_json,scope_name,scope_version,events_json,links_json,resource_json,scope_json,span_json,resource_schema_url,scope_schema_url,ingested_at
FROM traceforge.spans WHERE trace_id = ? ORDER BY start_time,span_id,ingested_at LIMIT ?`, traceID, maxRows+1)
	if err != nil {
		return nil, queryError(ctx, err)
	}
	defer rows.Close()
	result := make([]traces.Record, 0)
	size := 0
	for rows.Next() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		// The extra row is a limit sentinel, never returned as a partial trace.
		if len(result) == maxRows {
			return nil, traces.ErrTooLarge
		}
		var r traces.Record
		var ra, sa, events, links, res, scope, span string
		if err := rows.Scan(&r.TraceID, &r.SpanID, &r.ParentSpanID, &r.ServiceName, &r.SpanName, &r.SpanKind, &r.StartTime, &r.EndTime, &r.DurationNS, &r.StatusCode, &r.StatusMessage, &ra, &sa, &r.ScopeName, &r.ScopeVersion, &events, &links, &res, &scope, &span, &r.ResourceSchemaURL, &r.ScopeSchemaURL, &r.IngestedAt); err != nil {
			return nil, traces.ErrCorrupt
		}
		size += len(ra) + len(sa) + len(events) + len(links) + len(res) + len(scope) + len(span) + len(r.SpanName) + len(r.ServiceName) + len(r.StatusMessage) + len(r.ScopeName) + len(r.ScopeVersion) + len(r.ResourceSchemaURL) + len(r.ScopeSchemaURL) + 128
		if size > MaxDetailBytes {
			return nil, traces.ErrTooLarge
		}
		for _, raw := range []string{ra, sa, events, links, res, scope, span} {
			if !json.Valid([]byte(raw)) {
				return nil, traces.ErrCorrupt
			}
		}
		r.ResourceAttributes = json.RawMessage(ra)
		r.SpanAttributes = json.RawMessage(sa)
		r.Events = json.RawMessage(events)
		r.Links = json.RawMessage(links)
		r.Resource = json.RawMessage(res)
		r.Scope = json.RawMessage(scope)
		r.Span = json.RawMessage(span)
		result = append(result, r)
	}
	if err := rows.Err(); err != nil {
		return nil, queryError(ctx, err)
	}
	return result, nil
}

// Search aggregates all observations starting in the window. Service/error
// filters qualify trace IDs using the SAME row, while summaries retain other
// services in that window. Detail hydration has no service/time filter.
func (s *Store) SearchTraces(ctx context.Context, q traces.SearchQuery) ([]traces.Summary, error) {
	if !q.From.Before(q.To) || q.To.Sub(q.From) > 24*time.Hour || q.Limit < 1 || q.Limit > 200 {
		return nil, traces.ErrCorrupt
	}
	if q.From.Before(time.Unix(0, 0)) || q.To.After(time.Unix(0, 1<<63-1)) || len(q.Service) > 128 || (q.Status != "" && q.Status != "error") {
		return nil, traces.ErrCorrupt
	}
	if q.After != nil && (!traces.ValidID(q.After.TraceID, 16) || q.After.LatestStart.Before(q.From) || !q.After.LatestStart.Before(q.To)) {
		return nil, traces.ErrCorrupt
	}
	ctx, cancel := queryContext(ctx)
	defer cancel()
	sql := `SELECT trace_id,min(start_time),max(end_time),max(start_time) AS latest_start,
arraySort(groupUniqArray(service_name)),uniqExact(span_id),max(toUInt8(status_code=2))
FROM traceforge.spans WHERE start_time >= fromUnixTimestamp64Nano(?, 'UTC') AND start_time < fromUnixTimestamp64Nano(?, 'UTC') GROUP BY trace_id
HAVING countIf((service_name = ? OR ? = '') AND (status_code = 2 OR ? = '')) > 0`
	args := []any{q.From.UnixNano(), q.To.UnixNano(), q.Service, q.Service, q.Status}
	if q.After != nil {
		sql += ` AND (latest_start < fromUnixTimestamp64Nano(?, 'UTC') OR (latest_start = fromUnixTimestamp64Nano(?, 'UTC') AND trace_id > ?))`
		args = append(args, q.After.LatestStart.UnixNano(), q.After.LatestStart.UnixNano(), q.After.TraceID)
	}
	sql += ` ORDER BY latest_start DESC,trace_id ASC LIMIT ?`
	args = append(args, q.Limit+1)
	rows, err := s.query(ctx, sql, args...)
	if err != nil {
		return nil, queryError(ctx, err)
	}
	defer rows.Close()
	result := make([]traces.Summary, 0)
	for rows.Next() {
		if len(result) >= q.Limit+1 {
			return nil, traces.ErrCorrupt
		}
		var r traces.Summary
		var hasError uint8
		if err := rows.Scan(&r.TraceID, &r.FirstStart, &r.LastEnd, &r.LatestStart, &r.Services, &r.UniqueSpans, &hasError); err != nil {
			return nil, traces.ErrCorrupt
		}
		if !traces.ValidID(r.TraceID, 16) {
			return nil, traces.ErrCorrupt
		}
		r.HasError = hasError != 0
		r.FirstStart = r.FirstStart.UTC()
		r.LastEnd = r.LastEnd.UTC()
		r.LatestStart = r.LatestStart.UTC()
		result = append(result, r)
	}
	if err := rows.Err(); err != nil {
		return nil, queryError(ctx, err)
	}
	return result, nil
}
