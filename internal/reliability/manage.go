package reliability

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"time"
)

// Private backend endpoint. Secure Compose exposes it only through the gateway's
// manage authorization; never mount it on the legacy compatibility backend.
func ManageHandler(store *Store, source Source) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		fail := func(code int) {
			w.WriteHeader(code)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"message": http.StatusText(code)}})
		}
		if r.Method != "POST" {
			fail(405)
			return
		}
		if store == nil {
			fail(503)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 4*time.Second)
		defer cancel()
		raw, e := io.ReadAll(io.LimitReader(r.Body, (16<<10)+1))
		if e != nil || len(raw) > 16<<10 {
			fail(413)
			return
		}
		if r.URL.Path == "/internal/security/slos" {
			var d Definition
			if json.Unmarshal(raw, &d) != nil || d.Validate() != nil {
				fail(400)
				return
			}
			if store.Register(ctx, d) != nil {
				fail(409)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"key": d.Key()})
			return
		}
		var input struct {
			Key      string    `json:"key"`
			Cutoff   time.Time `json:"cutoff"`
			Revision int       `json:"revision"`
		}
		if json.Unmarshal(raw, &input) != nil {
			fail(400)
			return
		}
		d, e := store.Definition(ctx, input.Key)
		if e != nil {
			fail(404)
			return
		}
		if input.Revision == 0 {
			input.Revision = 1
		}
		if input.Cutoff.IsZero() {
			input.Cutoff = time.Now().UTC().Add(-3 * time.Second).Truncate(time.Second)
		}
		result, e := store.Evaluate(ctx, source, d, input.Cutoff, input.Revision)
		if e != nil {
			fail(503)
			return
		}
		_ = json.NewEncoder(w).Encode(result)
	})
}
