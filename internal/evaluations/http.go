// Package evaluations exposes bounded read-only transactional evaluation metadata.
package evaluations

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"time"
)

var ErrNotFound = errors.New("evaluation not found")

type Repository interface {
	Read(context.Context, string, string, string, int, string) (any, error)
}

var runPattern = regexp.MustCompile(`^[a-f0-9]{32}$`)
var casePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

func NewHandler(repo Repository) http.Handler {
	mux := http.NewServeMux()
	slots := make(chan struct{}, 4)
	handle := func(kind string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			send := func(status int, v any) {
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Cache-Control", "no-store")
				w.WriteHeader(status)
				_ = json.NewEncoder(w).Encode(v)
			}
			fail := func(status int, code string) {
				send(status, map[string]any{"error": map[string]string{"code": code, "message": code}})
			}
			if r.Method != "GET" {
				w.Header().Set("Allow", "GET")
				fail(405, "method_not_allowed")
				return
			}
			run, caseID := r.PathValue("run_id"), r.PathValue("case_id")
			q, e := url.ParseQuery(r.URL.RawQuery)
			malformed := e != nil
			limit := 25
			if v := q.Get("limit"); v != "" {
				limit, e = strconv.Atoi(v)
			}
			after := q.Get("after")
			invalid := malformed || e != nil || limit < 1 || limit > 50 || run != "" && !runPattern.MatchString(run) || caseID != "" && !casePattern.MatchString(caseID)
			for k, vs := range q {
				if (k != "limit" && k != "after") || len(vs) != 1 || vs[0] == "" || kind == "run" || kind == "case" || kind == "comparison" || kind == "decision" {
					invalid = true
				}
			}
			if (kind == "runs" || kind == "comparisons") && after != "" && !runPattern.MatchString(after) || kind == "experiments" && after != "" && !regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(after) || (kind == "cases" || kind == "comparison_cases") && after != "" && !casePattern.MatchString(after) {
				invalid = true
			}
			if invalid {
				fail(400, "invalid_evaluation_query")
				return
			}
			select {
			case slots <- struct{}{}:
				defer func() { <-slots }()
			default:
				fail(503, "query_busy")
				return
			}
			ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
			defer cancel()
			if repo == nil {
				fail(503, "evaluation_backend_unavailable")
				return
			}
			value, err := repo.Read(ctx, kind, run, caseID, limit, after)
			if err != nil {
				switch {
				case errors.Is(err, ErrNotFound):
					fail(404, "evaluation_not_found")
				case errors.Is(err, context.DeadlineExceeded):
					fail(504, "query_timeout")
				case errors.Is(err, context.Canceled):
					fail(408, "query_canceled")
				default:
					fail(503, "evaluation_backend_unavailable")
				}
				return
			}
			b, err := json.Marshal(value)
			if err != nil || len(b) > 4<<20 {
				fail(413, "evaluation_response_too_large")
				return
			}
			if ctx.Err() != nil {
				fail(504, "query_timeout")
				return
			}
			send(200, json.RawMessage(b))
		}
	}
	mux.HandleFunc("/api/v1/evaluations", handle("runs"))
	mux.HandleFunc("/api/v1/evaluations/{run_id}", handle("run"))
	mux.HandleFunc("/api/v1/evaluations/{run_id}/cases", handle("cases"))
	mux.HandleFunc("/api/v1/evaluations/{run_id}/cases/{case_id}", handle("case"))
	mux.HandleFunc("/api/v1/experiments", handle("experiments"))
	mux.HandleFunc("/api/v1/comparisons", handle("comparisons"))
	mux.HandleFunc("/api/v1/comparisons/{run_id}", handle("comparison"))
	mux.HandleFunc("/api/v1/comparisons/{run_id}/cases", handle("comparison_cases"))
	mux.HandleFunc("/api/v1/gates/{run_id}", handle("decision"))
	return mux
}
