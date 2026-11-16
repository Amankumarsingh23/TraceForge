package storage

import (
	"context"
	"encoding/json"
	"errors"
	"time"
	"traceforge/internal/metricdata"
	"traceforge/internal/traces"
)

func (s *Store) InsertMetricPoints(ctx context.Context, points []metricdata.Point) error {
	if len(points) > metricdata.MaxPoints {
		return metricdata.ErrBudget
	}
	if len(points) == 0 {
		return nil
	}
	batch, e := s.prepareBatch(ctx, `INSERT INTO traceforge.metric_points (stream_id,metric_name,service_name,service_namespace,end_ns,start_ns,point_json)`)
	if e != nil {
		return metricdata.ErrBackend
	}
	defer batch.Close()
	for _, p := range points {
		if e = ctx.Err(); e != nil {
			return e
		}
		b, e := json.Marshal(p)
		if e != nil {
			return metricdata.ErrInvalid
		}
		if e = batch.Append(p.StreamID, p.Name, p.Service, p.Namespace, p.End, p.Start, string(b)); e != nil {
			return metricdata.ErrBackend
		}
	}
	if e = batch.Send(); e != nil {
		return metricdata.ErrBackend
	}
	return nil
}
func metricError(ctx context.Context, e error) error {
	e = analyticsQueryError(ctx, e)
	if errors.Is(e, traces.ErrTooLarge) {
		return metricdata.ErrBudget
	}
	if errors.Is(e, traces.ErrBackend) {
		return metricdata.ErrBackend
	}
	return e
}

// Read both in-window and bounded predecessor history for every candidate.
// No raw-row limit is silently applied before canonicalization or aggregation.
func (s *Store) MetricPoints(ctx context.Context, q metricdata.Query) ([]metricdata.Point, error) {
	if !q.Valid() {
		return nil, metricdata.ErrInvalid
	}
	ctx, cancel := queryContext(ctx)
	defer cancel()
	from, to := uint64(q.From.UnixNano()), uint64(q.To.UnixNano())
	lower := q.From.Add(-time.Hour)
	if lower.Before(time.Unix(0, 0)) {
		lower = time.Unix(0, 0)
	}
	rows, e := s.query(ctx, `SELECT stream_id FROM traceforge.metric_points WHERE end_ns > ? AND end_ns <= ? AND (metric_name=? OR ?='') AND (service_name=? OR ?='') AND (service_namespace=? OR ?='') GROUP BY stream_id ORDER BY stream_id LIMIT ?`, from, to, q.Name, q.Name, q.Service, q.Service, q.Namespace, q.Namespace, metricdata.MaxQuerySeries+1)
	if e != nil {
		return nil, metricError(ctx, e)
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if e = rows.Scan(&id); e != nil {
			rows.Close()
			return nil, metricdata.ErrInvalid
		}
		ids = append(ids, id)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, metricError(ctx, e)
	}
	if len(ids) > metricdata.MaxQuerySeries {
		return nil, metricdata.ErrBudget
	}
	if len(ids) == 0 {
		return []metricdata.Point{}, nil
	}
	// Discovery intentionally precedes exact stream filtering, so related conflicting
	// definitions cannot be hidden by selecting a single definition's hash.
	rows, e = s.query(ctx, `SELECT point_json,ingested_at FROM traceforge.metric_points WHERE stream_id IN (?) AND end_ns >= ? AND end_ns <= ? ORDER BY stream_id,end_ns,ingested_at LIMIT ?`, ids, uint64(lower.UnixNano()), to, metricdata.MaxQueryRows+1)
	if e != nil {
		return nil, metricError(ctx, e)
	}
	defer rows.Close()
	result := []metricdata.Point{}
	size := 0
	for rows.Next() {
		if len(result) == metricdata.MaxQueryRows {
			return nil, metricdata.ErrBudget
		}
		var raw string
		var at time.Time
		if e = rows.Scan(&raw, &at); e != nil {
			return nil, metricdata.ErrInvalid
		}
		size += len(raw)
		if size > 16<<20 {
			return nil, metricdata.ErrBudget
		}
		var p metricdata.Point
		if e = json.Unmarshal([]byte(raw), &p); e != nil {
			return nil, metricdata.ErrInvalid
		}
		p.IngestedAt = at.UTC()
		result = append(result, p)
	}
	if e = rows.Err(); e != nil {
		return nil, metricError(ctx, e)
	}
	return result, nil
}
