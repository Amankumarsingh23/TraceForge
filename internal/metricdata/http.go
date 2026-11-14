package metricdata

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"time"
)

type Repository interface {
	MetricPoints(context.Context, Query) ([]Point, error)
}
type Handler struct {
	repo  Repository
	slots chan struct{}
}

func NewHandler(repo Repository) *Handler { return &Handler{repo: repo, slots: make(chan struct{}, 2)} }
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	fail := func(code int, message string) {
		w.WriteHeader(code)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "metric_query_failed", "message": message}})
	}
	if r.Method != "GET" {
		w.Header().Set("Allow", "GET")
		fail(405, "Use GET")
		return
	}
	if r.URL.Path != "/api/v1/metrics" && r.URL.Path != "/api/v1/metrics/series" && r.URL.Path != "/api/v1/metrics/query" {
		fail(404, "Unknown metrics path")
		return
	}
	v := r.URL.Query()
	now := time.Now().UTC()
	q := Query{From: now.Add(-15 * time.Minute), To: now}
	for k, a := range v {
		if len(a) != 1 || a[0] == "" {
			fail(400, "Unsupported or repeated filter")
			return
		}
		switch k {
		case "from", "to", "name", "service", "namespace", "stream_id":
		default:
			fail(400, "Unsupported filter")
			return
		}
	}
	for _, key := range []string{"from", "to"} {
		if value := v.Get(key); value != "" {
			if value[len(value)-1] != 'Z' {
				fail(400, "Use UTC Z timestamps")
				return
			}
			t, e := time.Parse(time.RFC3339Nano, value)
			if e != nil {
				fail(400, "Invalid UTC time")
				return
			}
			if key == "from" {
				q.From = t
			} else {
				q.To = t
			}
		}
	}
	q.Name = v.Get("name")
	q.Service = v.Get("service")
	q.Namespace = v.Get("namespace")
	q.StreamID = v.Get("stream_id")
	_, idErr := hex.DecodeString(q.StreamID)
	if !q.Valid() || idErr != nil {
		fail(400, "Use a valid UTC window <=1 hour and bounded filters")
		return
	}
	select {
	case h.slots <- struct{}{}:
		defer func() { <-h.slots }()
	default:
		fail(429, "Metric query slots full")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 4*time.Second)
	defer cancel()
	rows, e := h.repo.MetricPoints(ctx, q)
	var result *Result
	if e == nil {
		result, e = Aggregate(ctx, q, rows)
	}
	if e != nil {
		switch {
		case errors.Is(e, ErrBudget):
			fail(413, "Metric budget exceeded: no partial aggregate; narrow the window/name/service")
		case errors.Is(e, context.DeadlineExceeded):
			fail(504, "Metric query timed out")
		case errors.Is(e, context.Canceled):
			fail(408, "Metric query canceled")
		case errors.Is(e, ErrInvalid):
			fail(500, "Invalid stored metric data")
		default:
			fail(503, "Metric backend unavailable")
		}
		return
	}
	if q.StreamID != "" {
		selected := []Series{}
		for _, s := range result.Series {
			if s.Metadata.StreamID == q.StreamID {
				selected = append(selected, s)
			}
		}
		result.Series = selected
		result.MergedHistogram = nil
		result.MergeStatus = "single stream selection; cross-stream merge omitted"
	}
	b, e := json.Marshal(result)
	if e != nil {
		fail(500, "Invalid numeric result")
		return
	}
	if len(b) > 16<<20 {
		fail(413, "Metric response exceeds byte limit")
		return
	}
	_, _ = w.Write(b)
}
