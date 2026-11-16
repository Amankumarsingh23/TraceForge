package security

import (
	"context"
	"encoding/json"
	collector "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	common "go.opentelemetry.io/proto/otlp/common/v1"
	resource "go.opentelemetry.io/proto/otlp/resource/v1"
	trace "go.opentelemetry.io/proto/otlp/trace/v1"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

type authFixture struct{ down bool }

func (a authFixture) Authenticate(_ context.Context, key string) (Identity, error) {
	if a.down {
		return Identity{}, ErrUnavailable
	}
	if key == "alpha" {
		return Identity{Tenant: "alpha", Scopes: []string{"read"}}, nil
	}
	if key == "beta" {
		return Identity{Tenant: "beta", Scopes: []string{"read", "manage"}}, nil
	}
	return Identity{}, ErrInvalid
}
func TestGatewayAuthenticationPermissionsAndTrustedRouting(t *testing.T) {
	alpha := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Error("credential forwarded")
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"owner": "alpha"})
	}))
	defer alpha.Close()
	beta := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"owner": "beta"})
	}))
	defer beta.Close()
	g := NewGateway(authFixture{}, map[string]Backend{"alpha": {HTTP: alpha.URL}, "beta": {HTTP: beta.URL}})
	for _, c := range []struct {
		key, method, path, claim string
		code                     int
	}{{"", "GET", "/api/v1/traces/x", "", 401}, {"alpha", "GET", "/api/v1/traces/x", "beta", 403}, {"alpha", "POST", "/api/v1/manage/slos", "", 403}, {"alpha", "GET", "/debug/ingestion", "", 404}, {"alpha", "GET", "/api/v1/traces/x", "", 200}, {"beta", "GET", "/api/v1/traces/x", "", 200}} {
		r := httptest.NewRequest(c.method, c.path, nil)
		if c.key != "" {
			r.Header.Set("Authorization", "Bearer "+c.key)
		}
		r.Header.Set("X-Tenant-ID", c.claim)
		w := httptest.NewRecorder()
		g.ServeHTTP(w, r)
		if w.Code != c.code {
			t.Fatal(c, w.Code)
		}
	}
	g.Auth = authFixture{down: true}
	r := httptest.NewRequest("GET", "/api/v1/traces/x", nil)
	r.Header.Set("Authorization", "Bearer alpha")
	w := httptest.NewRecorder()
	g.ServeHTTP(w, r)
	if w.Code != 503 {
		t.Fatal("auth outage allowed access")
	}
}
func TestTenantClaimsAndIndependentAdmission(t *testing.T) {
	req := &collector.ExportTraceServiceRequest{ResourceSpans: []*trace.ResourceSpans{{Resource: &resource.Resource{Attributes: []*common.KeyValue{{Key: "tenant_id", Value: &common.AnyValue{Value: &common.AnyValue_StringValue{StringValue: "beta"}}}}}}}}
	if !tenantSpoof(req, "alpha") || tenantSpoof(req, "beta") {
		t.Fatal("payload claim not checked")
	}
	g := NewGateway(authFixture{}, nil)
	done1, ok := g.admit("alpha", true)
	if !ok {
		t.Fatal("first")
	}
	done2, ok := g.admit("alpha", true)
	if !ok {
		t.Fatal("second")
	}
	if _, ok = g.admit("alpha", true); ok {
		t.Fatal("unbounded alpha")
	}
	beta, ok := g.admit("beta", true)
	if !ok {
		t.Fatal("beta starved by alpha limit")
	}
	done1()
	done2()
	beta()
	var wg sync.WaitGroup
	for n := 0; n < 20; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if done, ok := g.admit("beta", false); ok {
				done()
			}
		}()
	}
	wg.Wait()
}
