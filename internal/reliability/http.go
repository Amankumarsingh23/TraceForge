package reliability

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type Reader interface {
	Read(context.Context, string, string, int, string) (any, error)
}

var apiKey = regexp.MustCompile(`^[a-z0-9_-]{1,80}$`)

func NewHandler(repo Reader) http.Handler {
	slots := make(chan struct{}, 4)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		fail := func(n int, msg string) {
			w.WriteHeader(n)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"code": "reliability_query_failed", "message": msg}})
		}
		if r.Method != "GET" {
			w.Header().Set("Allow", "GET")
			fail(405, "Use GET")
			return
		}
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/v1/"), "/")
		kind, key := "", ""
		switch parts[0] {
		case "slos":
			kind = "slos"
			if len(parts) == 2 {
				kind = "definition"
				key = parts[1]
			} else if len(parts) == 3 && parts[2] == "evaluations" {
				kind = "evaluations"
				key = parts[1]
			} else if len(parts) != 1 {
				fail(404, "Unknown SLO path")
				return
			}
		case "alerts":
			kind = "alerts"
			if len(parts) == 2 {
				kind = "incident"
				key = parts[1]
			} else if len(parts) != 1 {
				fail(404, "Unknown alert path")
				return
			}
		case "slo-evaluations":
			if len(parts) != 2 {
				fail(404, "Use evaluation ID")
				return
			}
			kind = "evaluation"
			key = parts[1]
		case "alert-heads":
			kind = "heads"
		case "provider-reliability":
			kind = "provider"
		default:
			fail(404, "Unknown reliability path")
			return
		}
		q, err := r.URL.Query(), error(nil)
		limit := 25
		if q.Has("limit") {
			limit, err = strconv.Atoi(q.Get("limit"))
		}
		after := q.Get("after")
		bad := err != nil || limit < 1 || limit > 50 || key != "" && !apiKey.MatchString(key) || after != "" && !apiKey.MatchString(after)
		for k, v := range q {
			if k != "limit" && k != "after" || len(v) != 1 || v[0] == "" || kind == "definition" || kind == "incident" || kind == "evaluation" {
				bad = true
			}
		}
		if bad {
			fail(400, "Invalid reliability filters")
			return
		}
		if repo == nil {
			fail(503, "Reliability metadata unavailable")
			return
		}
		select {
		case slots <- struct{}{}:
			defer func() { <-slots }()
		default:
			fail(429, "Reliability query capacity reached")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		result, err := repo.Read(ctx, kind, key, limit, after)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				fail(404, "Reliability record not found")
			} else if errors.Is(err, ErrTooLarge) {
				fail(413, "Reliability metadata read exceeds byte budget")
			} else {
				fail(503, "Reliability metadata unavailable")
			}
			return
		}
		raw, err := json.Marshal(result)
		if err != nil {
			fail(500, "Invalid reliability record")
			return
		}
		if len(raw) > 8<<20 {
			fail(413, "Reliability response exceeds byte limit")
			return
		}
		_, _ = w.Write(raw)
	})
}
