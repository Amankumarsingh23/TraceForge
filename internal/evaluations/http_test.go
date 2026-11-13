package evaluations

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type fakeRepo struct {
	err                      error
	kind, run, caseID, after string
	limit                    int
}

func (f *fakeRepo) Read(ctx context.Context, kind, run, caseID string, limit int, after string) (any, error) {
	f.kind = kind
	f.run = run
	f.caseID = caseID
	f.after = after
	f.limit = limit
	return map[string]any{"trace_id": "unobserved-but-real-identity", "trace_visibility": "pending"}, f.err
}
func TestEvaluationRoutesAndBounds(t *testing.T) {
	id := strings.Repeat("a", 32)
	for _, tc := range []struct {
		path, kind string
		status     int
	}{
		{"?limit=3&broken=%", "", 400},
		{"", "runs", 200}, {"?limit=1&after=" + id, "runs", 200}, {"/" + id, "run", 200}, {"/" + id + "/cases?limit=50&after=support-001", "cases", 200}, {"/" + id + "/cases/support-001", "case", 200},
		{"?limit=0", "", 400}, {"?limit=51", "", 400}, {"?limit=1&limit=2", "", 400}, {"?unknown=x", "", 400}, {"?after=bad!", "", 400}, {"/bad", "", 400}, {"/" + id + "?limit=1", "", 400}, {"/" + id + "/cases/support-001?after=x", "", 400}, {"?limit=", "", 400},
	} {
		t.Run(tc.path, func(t *testing.T) {
			repo := &fakeRepo{}
			w := httptest.NewRecorder()
			NewHandler(repo).ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/evaluations"+tc.path, nil))
			if w.Code != tc.status {
				t.Fatalf("%d %s", w.Code, w.Body)
			}
			if tc.status == 200 && repo.kind != tc.kind {
				t.Fatal(repo.kind)
			}
			if tc.status == 400 && repo.kind != "" {
				t.Fatal("queried invalid request")
			}
		})
	}
}
func TestEvaluationSafeErrorsAndMethod(t *testing.T) {
	for _, tc := range []struct {
		err  error
		code int
	}{{ErrNotFound, 404}, {context.DeadlineExceeded, 504}, {context.Canceled, 408}, {errors.New("password=SECRET"), 503}} {
		w := httptest.NewRecorder()
		NewHandler(&fakeRepo{err: tc.err}).ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/evaluations", nil))
		if w.Code != tc.code || strings.Contains(w.Body.String(), "SECRET") {
			t.Fatalf("%d %s", w.Code, w.Body)
		}
	}
	for _, method := range []string{"POST", "DELETE"} {
		w := httptest.NewRecorder()
		NewHandler(&fakeRepo{}).ServeHTTP(w, httptest.NewRequest(method, "/api/v1/evaluations", nil))
		if w.Code != http.StatusMethodNotAllowed {
			t.Fatal(w.Code)
		}
	}
	w := httptest.NewRecorder()
	NewHandler(nil).ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/evaluations", nil))
	if w.Code != 503 {
		t.Fatal(w.Code)
	}
}
