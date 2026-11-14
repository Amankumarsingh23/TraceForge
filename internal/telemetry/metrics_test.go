package telemetry

import (
	"context"
	sdk "go.opentelemetry.io/otel/sdk/metric"
	data "go.opentelemetry.io/otel/sdk/metric/metricdata"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestActualInboundMetricInstruments(t *testing.T) {
	reader := sdk.NewManualReader()
	provider := sdk.NewMeterProvider(sdk.WithReader(reader))
	defer provider.Shutdown(context.Background())
	handler, e := InstrumentInbound(provider, "/checkout", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Has("fail") {
			w.WriteHeader(502)
		} else {
			_, _ = w.Write([]byte("ok"))
		}
	}))
	if e != nil {
		t.Fatal(e)
	}
	for _, url := range []string{"/checkout", "/checkout?fail=true", "/unrelated"} {
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", url, nil))
	}
	var rm data.ResourceMetrics
	if e = reader.Collect(context.Background(), &rm); e != nil {
		t.Fatal(e)
	}
	found := map[string]bool{}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			found[m.Name] = true
			switch m.Name {
			case "traceforge.requests.completed":
				if v := m.Data.(data.Sum[int64]); v.DataPoints[0].Value != 2 || !v.IsMonotonic {
					t.Fatal(v)
				}
			case "traceforge.requests.errors":
				if v := m.Data.(data.Sum[int64]); v.DataPoints[0].Value != 1 {
					t.Fatal(v)
				}
			case "traceforge.requests.active":
				if v := m.Data.(data.Sum[int64]); v.DataPoints[0].Value != 0 || v.IsMonotonic {
					t.Fatal(v)
				}
			case "traceforge.request.duration":
				if v := m.Data.(data.Histogram[float64]); v.DataPoints[0].Count != 2 || m.Unit != "s" {
					t.Fatal(v)
				}
			case "traceforge.process.goroutines":
				if v := m.Data.(data.Gauge[int64]); v.DataPoints[0].Value < 1 {
					t.Fatal(v)
				}
			}
		}
	}
	if len(found) != 5 {
		t.Fatal("missing actual instruments", found)
	}
}
