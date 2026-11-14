package reliability

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
)

type brokenReader struct{}

func (brokenReader) Read(context.Context, string, string, int, string) (any, error) {
	return nil, errors.New("private database credentials")
}
func TestReliabilityHTTPBoundsAndSanitizedUnavailable(t *testing.T) {
	for _, c := range []struct {
		method, path string
		status       int
	}{
		{"POST", "/api/v1/slos", 405},
		{"GET", "/api/v1/slos?limit=51", 400},
		{"GET", "/api/v1/slos?limit=2&limit=3", 400},
		{"GET", "/api/v1/slos?url=http://example.com", 400},
		{"GET", "/api/v1/slos", 503},
	} {
		w := httptest.NewRecorder()
		NewHandler(brokenReader{}).ServeHTTP(w, httptest.NewRequest(c.method, c.path, nil))
		if w.Code != c.status || strings.Contains(w.Body.String(), "credentials") {
			t.Fatal(c, w.Code, w.Body.String())
		}
	}
}
