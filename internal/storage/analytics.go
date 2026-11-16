package storage

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/ClickHouse/clickhouse-go/v2"
	"traceforge/internal/analytics"
	"traceforge/internal/traces"
)

func analyticsQueryError(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	var ex *clickhouse.Exception
	if errors.As(err, &ex) {
		switch ex.Code {
		case 158, 241, 307, 396:
			return traces.ErrTooLarge
		}
	}
	return queryError(ctx, err)
}

// AnalyticsRows uses two bulk queries. Discovery is deliberately not scoped to
// one service: a filtered-out parent may be needed to confirm an edge.
func (s *Store) AnalyticsRows(ctx context.Context, q analytics.Query) ([]traces.Record, error) {
	if !q.Valid() {
		return nil, traces.ErrCorrupt
	}
	ctx, cancel := queryContext(ctx)
	defer cancel()
	rows, e := s.query(ctx, `SELECT trace_id FROM traceforge.spans WHERE start_time >= fromUnixTimestamp64Nano(?, 'UTC') AND start_time < fromUnixTimestamp64Nano(?, 'UTC') AND span_kind IN (2,3) GROUP BY trace_id ORDER BY trace_id LIMIT ?`, q.From.UnixNano(), q.To.UnixNano(), analytics.MaxTraces+1)
	if e != nil {
		return nil, analyticsQueryError(ctx, e)
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if e = rows.Scan(&id); e != nil {
			rows.Close()
			return nil, traces.ErrCorrupt
		}
		if !traces.ValidID(id, 16) {
			rows.Close()
			return nil, traces.ErrCorrupt
		}
		ids = append(ids, id)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, analyticsQueryError(ctx, e)
	}
	if len(ids) > analytics.MaxTraces {
		return nil, traces.ErrTooLarge
	}
	if len(ids) == 0 {
		return []traces.Record{}, nil
	}
	// Full trace hydration has no start-time filter; pairing may cross a boundary.
	rows, e = s.query(ctx, `SELECT trace_id,span_id,parent_span_id,service_name,span_name,span_kind,start_time,end_time,duration_ns,status_code,status_message,
 resource_attributes_json,span_attributes_json,scope_name,scope_version,events_json,links_json,resource_json,scope_json,span_json,resource_schema_url,scope_schema_url,ingested_at
 FROM traceforge.spans WHERE trace_id IN (?) ORDER BY trace_id,span_id,ingested_at LIMIT ?`, ids, analytics.MaxRows+1)
	if e != nil {
		return nil, analyticsQueryError(ctx, e)
	}
	defer rows.Close()
	result := []traces.Record{}
	size := 0
	for rows.Next() {
		if e = ctx.Err(); e != nil {
			return nil, e
		}
		if len(result) == analytics.MaxRows {
			return nil, traces.ErrTooLarge
		}
		var r traces.Record
		var ra, sa, events, links, res, scope, span string
		if e = rows.Scan(&r.TraceID, &r.SpanID, &r.ParentSpanID, &r.ServiceName, &r.SpanName, &r.SpanKind, &r.StartTime, &r.EndTime, &r.DurationNS, &r.StatusCode, &r.StatusMessage, &ra, &sa, &r.ScopeName, &r.ScopeVersion, &events, &links, &res, &scope, &span, &r.ResourceSchemaURL, &r.ScopeSchemaURL, &r.IngestedAt); e != nil {
			return nil, traces.ErrCorrupt
		}
		size += len(ra) + len(sa) + len(events) + len(links) + len(res) + len(scope) + len(span) + len(r.ServiceName) + len(r.SpanName) + len(r.StatusMessage) + len(r.ScopeName) + len(r.ScopeVersion) + len(r.ResourceSchemaURL) + len(r.ScopeSchemaURL) + 256
		if size > analytics.MaxBytes {
			return nil, traces.ErrTooLarge
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
	if e = rows.Err(); e != nil {
		return nil, analyticsQueryError(ctx, e)
	}
	return result, nil
}
