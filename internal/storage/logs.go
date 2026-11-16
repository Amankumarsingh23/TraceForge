package storage

import (
	"context"
	"encoding/json"
	"errors"
	"traceforge/internal/logs"
	"traceforge/internal/traces"
)

const logColumns = `log_id,source_event_id,trace_id,span_id,trace_flags,service_name,severity_number,severity_text,event_time,observed_time,effective_time,time_source,body_text,body_json,attributes_json,resource_attributes_json,scope_name,scope_version,event_name,resource_json,scope_json,record_json,resource_schema_url,scope_schema_url,ingested_at`

func (s *Store) InsertLogs(ctx context.Context, rows []logs.Row) error {
	if len(rows) == 0 {
		return nil
	}
	batch, e := s.prepareBatch(ctx, `INSERT INTO traceforge.logs (`+logColumns+`)`)
	if e != nil {
		return errors.New("prepare log batch failed")
	}
	defer batch.Close()
	for _, r := range rows {
		if e = ctx.Err(); e != nil {
			return e
		}
		if e = batch.Append(r.LogID, r.SourceEventID, r.TraceID, r.SpanID, r.TraceFlags, r.ServiceName, r.SeverityNumber, r.SeverityText, r.EventTime, r.ObservedTime, r.EffectiveTime, r.TimeSource, r.BodyText, string(r.BodyJSON), string(r.AttributesJSON), string(r.ResourceAttributesJSON), r.ScopeName, r.ScopeVersion, r.EventName, string(r.ResourceJSON), string(r.ScopeJSON), string(r.RecordJSON), r.ResourceSchemaURL, r.ScopeSchemaURL, r.IngestedAt); e != nil {
			return errors.New("append normalized log failed")
		}
	}
	if batch.Send() != nil {
		return errors.New("send log batch failed; outcome may be ambiguous")
	}
	return nil
}
func (s *Store) SearchLogs(ctx context.Context, q logs.Query) ([]logs.Row, error) {
	// Revalidate bounds even for internal callers; only this parser's validated
	// struct is used by HTTP, but SQL parameterization is maintained independently.
	if q.Limit < 1 || q.Limit > 100 || !q.From.Before(q.To) || q.To.Sub(q.From) > 24*60*60*1e9 || q.From.UnixNano() < 0 || q.To.UnixNano() < 0 || q.MinSeverity < 0 || q.MinSeverity > 24 || len(q.Text) > 256 || len(q.Service) > 128 || q.TraceID != "" && !traces.ValidID(q.TraceID, 16) || q.SpanID != "" && (!traces.ValidID(q.SpanID, 8) || q.TraceID == "") {
		return nil, logs.ErrCorrupt
	}
	if q.After != nil && (!traces.ValidID(q.After.LogID, 16) || q.After.Time.Before(q.From) || !q.After.Time.Before(q.To)) {
		return nil, logs.ErrCorrupt
	}
	ctx, cancel := queryContext(ctx)
	defer cancel()
	sql := `SELECT ` + logColumns + ` FROM traceforge.logs WHERE effective_time >= fromUnixTimestamp64Nano(?, 'UTC') AND effective_time < fromUnixTimestamp64Nano(?, 'UTC') AND (service_name = ? OR ? = '') AND severity_number >= ? AND (trace_id = ? OR ? = '') AND (span_id = ? OR ? = '') AND position(body_text, ?) > 0`
	args := []any{q.From.UnixNano(), q.To.UnixNano(), q.Service, q.Service, q.MinSeverity, q.TraceID, q.TraceID, q.SpanID, q.SpanID, q.Text}
	if q.After != nil {
		sql += ` AND (effective_time < fromUnixTimestamp64Nano(?, 'UTC') OR (effective_time = fromUnixTimestamp64Nano(?, 'UTC') AND log_id < ?))`
		args = append(args, q.After.Time.UnixNano(), q.After.Time.UnixNano(), q.After.LogID)
	}
	sql += ` ORDER BY effective_time DESC, log_id DESC LIMIT ?`
	args = append(args, q.Limit+1)
	rows, e := s.query(ctx, sql, args...)
	if e != nil {
		return nil, logQueryError(ctx, e)
	}
	defer rows.Close()
	result := []logs.Row{}
	size := 0
	for rows.Next() {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		var r logs.Row
		var body, attrs, resattrs, res, scope, record string
		if rows.Scan(&r.LogID, &r.SourceEventID, &r.TraceID, &r.SpanID, &r.TraceFlags, &r.ServiceName, &r.SeverityNumber, &r.SeverityText, &r.EventTime, &r.ObservedTime, &r.EffectiveTime, &r.TimeSource, &r.BodyText, &body, &attrs, &resattrs, &r.ScopeName, &r.ScopeVersion, &r.EventName, &res, &scope, &record, &r.ResourceSchemaURL, &r.ScopeSchemaURL, &r.IngestedAt) != nil {
			return nil, logs.ErrCorrupt
		}
		if !traces.ValidID(r.LogID, 16) {
			return nil, logs.ErrCorrupt
		}
		for _, s := range []string{body, attrs, resattrs, res, scope, record} {
			if !json.Valid([]byte(s)) {
				return nil, logs.ErrCorrupt
			}
		}
		r.BodyJSON = json.RawMessage(body)
		r.AttributesJSON = json.RawMessage(attrs)
		r.ResourceAttributesJSON = json.RawMessage(resattrs)
		r.ResourceJSON = json.RawMessage(res)
		r.ScopeJSON = json.RawMessage(scope)
		r.RecordJSON = json.RawMessage(record)
		size += len(body) + len(attrs) + len(resattrs) + len(res) + len(scope) + len(record) + len(r.BodyText) + len(r.SourceEventID) + len(r.ServiceName) + len(r.ScopeName) + len(r.ScopeVersion) + len(r.SeverityText) + len(r.EventName) + len(r.ResourceSchemaURL) + len(r.ScopeSchemaURL) + 512
		if size > 4<<20 {
			return nil, logs.ErrTooLarge
		}
		r.EffectiveTime = r.EffectiveTime.UTC()
		r.IngestedAt = r.IngestedAt.UTC()
		if r.EventTime != nil {
			t := r.EventTime.UTC()
			r.EventTime = &t
		}
		if r.ObservedTime != nil {
			t := r.ObservedTime.UTC()
			r.ObservedTime = &t
		}
		result = append(result, r)
	}
	if e = rows.Err(); e != nil {
		return nil, logQueryError(ctx, e)
	}
	return result, nil
}
func logQueryError(ctx context.Context, e error) error {
	err := queryError(ctx, e)
	if errors.Is(err, traces.ErrBackend) {
		return logs.ErrBackend
	}
	return err
}
