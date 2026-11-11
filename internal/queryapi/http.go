package queryapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"
	"traceforge/internal/traces"
)

type Repository interface {
	GetTraceSpans(context.Context, string, int) ([]traces.Record, error)
	SearchTraces(context.Context, traces.SearchQuery) ([]traces.Summary, error)
}
type Config struct {
	MaxRows, MaxPageSize int
	Timeout              time.Duration
	Now                  func() time.Time
}

func DefaultConfig() Config {
	return Config{MaxRows: 1000, MaxPageSize: 100, Timeout: 2 * time.Second, Now: time.Now}
}

type handler struct {
	repo   Repository
	config Config
	slots  chan struct{}
}

func NewHandler(repo Repository, c Config) (http.Handler, error) {
	if c.MaxRows < 1 || c.MaxRows > traces.HardMaxRows || c.MaxPageSize < 1 || c.MaxPageSize > 200 || c.Timeout <= 0 || c.Timeout > 4*time.Second {
		return nil, fmt.Errorf("query limits out of range")
	}
	if c.Now == nil {
		c.Now = time.Now
	}
	h := &handler{repo: repo, config: c, slots: make(chan struct{}, 4)}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/traces", h.search)
	mux.HandleFunc("/api/v1/traces/{trace_id}", h.detail)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { writeError(w, 404, "not_found", "API route not found") })
	return mux, nil
}
func writeError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"code": code, "message": message}})
}
func method(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != "GET" {
		w.Header().Set("Allow", "GET")
		writeError(w, 405, "method_not_allowed", "use GET")
		return false
	}
	return true
}
func (h *handler) acquire(w http.ResponseWriter, r *http.Request) (context.Context, func(), bool) {
	select {
	case h.slots <- struct{}{}:
	default:
		writeError(w, 503, "query_busy", "query concurrency limit reached")
		return nil, nil, false
	}
	ctx, cancel := context.WithTimeout(r.Context(), h.config.Timeout)
	return ctx, func() { cancel(); <-h.slots }, true
}
func failure(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, traces.ErrTooLarge):
		writeError(w, 413, "trace_too_large", "trace exceeds physical-row, payload or response limits")
	case errors.Is(err, context.DeadlineExceeded):
		writeError(w, 504, "query_timeout", "query deadline exceeded")
	case errors.Is(err, context.Canceled):
		writeError(w, 408, "query_canceled", "query was canceled")
	case errors.Is(err, traces.ErrCorrupt):
		writeError(w, 500, "stored_data_invalid", "stored trace data could not be interpreted safely")
	default:
		writeError(w, 503, "backend_unavailable", "query backend unavailable or resource limit exceeded")
	}
}
func writeResult(ctx context.Context, w http.ResponseWriter, v any) {
	b, e := json.Marshal(v)
	if e != nil {
		failure(w, traces.ErrCorrupt)
		return
	}
	if len(b) > 16<<20 {
		failure(w, traces.ErrTooLarge)
		return
	}
	if e := ctx.Err(); e != nil {
		failure(w, e)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(200)
	_, _ = w.Write(b)
}
func (h *handler) detail(w http.ResponseWriter, r *http.Request) {
	if !method(w, r) {
		return
	}
	id := r.PathValue("trace_id")
	if !traces.ValidID(id, 16) || r.URL.RawQuery != "" {
		writeError(w, 400, "invalid_trace_query", "use a canonical nonzero lowercase 32-character trace ID without filters")
		return
	}
	ctx, release, ok := h.acquire(w, r)
	if !ok {
		return
	}
	defer release()
	rows, e := h.repo.GetTraceSpans(ctx, id, h.config.MaxRows)
	if e != nil {
		failure(w, e)
		return
	}
	if len(rows) == 0 {
		writeError(w, 404, "trace_not_found", "trace has no observed spans")
		return
	}
	d, e := traces.Reconstruct(id, rows, h.config.MaxRows)
	if e != nil {
		failure(w, e)
		return
	}
	writeResult(ctx, w, d)
}
func (h *handler) search(w http.ResponseWriter, r *http.Request) {
	if !method(w, r) {
		return
	}
	values, parseErr := url.ParseQuery(r.URL.RawQuery)
	if parseErr != nil {
		writeError(w, 400, "invalid_search_query", "malformed query encoding")
		return
	}
	q, e := ParseSearch(values, h.config.Now(), h.config.MaxPageSize)
	if e != nil {
		writeError(w, 400, "invalid_search_query", "invalid filters, UTC window, page size or cursor")
		return
	}
	ctx, release, ok := h.acquire(w, r)
	if !ok {
		return
	}
	defer release()
	items, e := h.repo.SearchTraces(ctx, q)
	if e != nil {
		failure(w, e)
		return
	}
	if items == nil {
		items = []traces.Summary{}
	}
	next := ""
	if len(items) > q.Limit {
		items = items[:q.Limit]
		next = encodeCursor(q, items[len(items)-1])
	}
	writeResult(ctx, w, struct {
		Traces       []traces.Summary `json:"traces"`
		NextCursor   string           `json:"next_cursor"`
		From         time.Time        `json:"from"`
		To           time.Time        `json:"to"`
		SummaryScope string           `json:"summary_scope"`
		Consistency  string           `json:"consistency"`
	}{items, next, q.From, q.To, "raw_observations_starting_in_window", "non_snapshot"})
}
