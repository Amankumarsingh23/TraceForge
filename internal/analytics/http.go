package analytics

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
	"traceforge/internal/traces"
)

func Parse(r *http.Request, now time.Time) (Query, error) {
	q := Query{From: now.UTC().Add(-15 * time.Minute), To: now.UTC(), BucketSeconds: 60}
	v := r.URL.Query()
	for k, values := range v {
		if len(values) != 1 || values[0] == "" {
			return q, traces.ErrCorrupt
		}
		switch k {
		case "from", "to", "bucket_seconds", "environment", "namespace", "workload", "service_id":
		default:
			return q, traces.ErrCorrupt
		}
	}
	for _, key := range []string{"from", "to"} {
		if s := v.Get(key); s != "" {
			if !strings.HasSuffix(s, "Z") {
				return q, traces.ErrCorrupt
			}
			t, e := time.Parse(time.RFC3339Nano, s)
			if e != nil {
				return q, traces.ErrCorrupt
			}
			if key == "from" {
				q.From = t
			} else {
				q.To = t
			}
		}
	}
	if s := v.Get("bucket_seconds"); s != "" {
		n, e := strconv.Atoi(s)
		if e != nil {
			return q, traces.ErrCorrupt
		}
		q.BucketSeconds = n
	}
	q.Environment = v.Get("environment")
	q.Namespace = v.Get("namespace")
	q.Workload = v.Get("workload")
	q.ServiceID = v.Get("service_id")
	if !q.Valid() {
		return q, traces.ErrCorrupt
	}
	return q, nil
}

type Handler struct {
	repo  Repository
	slots chan struct{}
}

func NewHandler(repo Repository) *Handler { return &Handler{repo: repo, slots: make(chan struct{}, 2)} }
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	fail := func(status int, code, message string) {
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"code": code, "message": message}})
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		fail(405, "method_not_allowed", "Use GET")
		return
	}
	q, e := Parse(r, time.Now())
	if e != nil {
		fail(400, "invalid_analytics_query", "Use UTC [from,to), at most one hour, bucket_seconds 60/300/900 and supported filters")
		return
	}
	p := strings.TrimPrefix(r.URL.Path, "/api/v1/")
	if p != "services" && p != "service-graph" {
		parts := strings.Split(p, "/")
		if len(parts) != 3 || parts[0] != "services" || !traces.ValidID(parts[1], 16) || (parts[2] != "metrics" && parts[2] != "dependencies") || q.ServiceID != "" {
			fail(404, "not_found", "Unknown analytics path")
			return
		}
		q.ServiceID = parts[1]
	}
	select {
	case h.slots <- struct{}{}:
		defer func() { <-h.slots }()
	default:
		fail(429, "analytics_busy", "Two analytics queries are already active; retry shortly")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 4*time.Second)
	defer cancel()
	rows, e := h.repo.AnalyticsRows(ctx, q)
	var result *Result
	if e == nil {
		result, e = Compute(ctx, q, rows)
	}
	if e != nil {
		switch {
		case errors.Is(e, traces.ErrTooLarge):
			fail(413, "analytics_budget_exceeded", "No partial result: narrow the time window or use an isolated workload")
		case errors.Is(e, context.DeadlineExceeded):
			fail(504, "query_timeout", "Analytics query deadline exceeded")
		case errors.Is(e, context.Canceled):
			fail(408, "query_canceled", "Analytics query canceled")
		case errors.Is(e, traces.ErrCorrupt):
			fail(500, "invalid_stored_data", "Stored telemetry could not be interpreted")
		default:
			fail(503, "backend_unavailable", "Analytics storage is unavailable")
		}
		return
	}
	data, e := json.Marshal(result)
	if e != nil {
		fail(500, "encoding_error", "Analytics result could not be encoded")
		return
	}
	if len(data) > MaxBytes {
		fail(413, "analytics_budget_exceeded", "Analytics response exceeds byte budget")
		return
	}
	_, _ = w.Write(data)
}
