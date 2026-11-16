package evaluations

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"net/http"
	"regexp"
	"strings"
	"time"
)

var securityID = regexp.MustCompile(`^[a-f0-9]{32}$`)

func SecurityHandler(store Repository) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		fail := func(code int) {
			w.WriteHeader(code)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"message": http.StatusText(code)}})
		}
		if r.Method != "GET" {
			fail(405)
			return
		}
		if r.URL.RawQuery != "" {
			fail(400)
			return
		}
		id := strings.TrimPrefix(r.URL.Path, "/api/v1/security/evaluations")
		if id != "" {
			id = strings.TrimPrefix(id, "/")
			if !securityID.MatchString(id) {
				fail(400)
				return
			}
		}
		actual, ok := store.(*Store)
		if !ok || actual == nil {
			fail(503)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		expression := `CASE WHEN octet_length(result::text)<=262144 THEN result ELSE jsonb_build_object('status','UNAVAILABLE','reason','metadata byte budget') END`
		if id != "" {
			var raw []byte
			e := actual.pool.QueryRow(ctx, `SELECT `+expression+` FROM security_evaluations WHERE run_id=$1`, id).Scan(&raw)
			if errors.Is(e, pgx.ErrNoRows) {
				fail(404)
				return
			}
			if e != nil {
				fail(503)
				return
			}
			_, _ = w.Write(raw)
			return
		}
		rows, e := actual.pool.Query(ctx, `SELECT `+expression+` FROM security_evaluations ORDER BY created_at DESC,run_id LIMIT 10`)
		if e != nil {
			fail(503)
			return
		}
		defer rows.Close()
		values := []json.RawMessage{}
		for rows.Next() {
			var raw []byte
			if rows.Scan(&raw) != nil {
				fail(503)
				return
			}
			values = append(values, raw)
		}
		if rows.Err() != nil {
			fail(503)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"runs": values, "limit": 10, "consistency": "latest ten immutable local runs"})
	})
}
