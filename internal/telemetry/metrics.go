package telemetry

import (
	"context"
	"errors"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	api "go.opentelemetry.io/otel/metric"
	sdk "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/exemplar"
	"net/http"
	"net/url"
	"runtime"
	"strings"
	"time"
)

var DurationBounds = []float64{.001, .005, .01, .025, .05, .1, .25, .5, 1, 2, 5}

func NewMeterProvider(ctx context.Context, service, endpoint string, interval time.Duration) (*sdk.MeterProvider, error) {
	u, e := url.Parse(endpoint)
	if e != nil || u == nil || u.Scheme != "http" || u.Hostname() == "" || u.Port() == "" || u.User != nil || u.Path != "" && u.Path != "/" || u.RawQuery != "" || u.Fragment != "" || interval < 200*time.Millisecond || interval > time.Minute {
		return nil, errors.New("invalid local metrics configuration")
	}
	exporter, e := otlpmetricgrpc.New(ctx, otlpmetricgrpc.WithEndpoint(u.Host), otlpmetricgrpc.WithInsecure(), otlpmetricgrpc.WithTimeout(3*time.Second))
	if e != nil {
		return nil, e
	}
	view := func(i sdk.Instrument) (sdk.Stream, bool) {
		if !strings.HasPrefix(i.Name, "traceforge.") {
			return sdk.Stream{}, false
		}
		s := sdk.Stream{Name: i.Name, Unit: i.Unit, Description: i.Description, AttributeFilter: attribute.NewAllowKeysFilter("http.route", "http.request.method", "outcome")}
		if i.Name == "traceforge.request.duration" {
			s.Aggregation = sdk.AggregationExplicitBucketHistogram{Boundaries: DurationBounds}
		}
		return s, true
	}
	return sdk.NewMeterProvider(sdk.WithResource(serviceResource(service)), sdk.WithReader(sdk.NewPeriodicReader(exporter, sdk.WithInterval(interval), sdk.WithTimeout(4*time.Second))), sdk.WithView(view), sdk.WithCardinalityLimit(32), sdk.WithExemplarFilter(exemplar.AlwaysOffFilter)), nil
}

type responseStatus struct {
	http.ResponseWriter
	status int
}

func (w *responseStatus) WriteHeader(code int) {
	if w.status != 0 {
		return
	}
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}
func (w *responseStatus) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(200)
	}
	return w.ResponseWriter.Write(b)
}
func (w *responseStatus) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// InstrumentInbound records only the fixed demo route; no raw URL or IDs.
func InstrumentInbound(mp api.MeterProvider, route string, next http.Handler) (http.Handler, error) {
	meter := mp.Meter("traceforge.demo", api.WithInstrumentationVersion("0.1.0"))
	requests, e := meter.Int64Counter("traceforge.requests.completed", api.WithUnit("{request}"))
	if e != nil {
		return nil, e
	}
	errorsCounter, e := meter.Int64Counter("traceforge.requests.errors", api.WithUnit("{request}"))
	if e != nil {
		return nil, e
	}
	duration, e := meter.Float64Histogram("traceforge.request.duration", api.WithUnit("s"))
	if e != nil {
		return nil, e
	}
	active, e := meter.Int64UpDownCounter("traceforge.requests.active", api.WithUnit("{request}"))
	if e != nil {
		return nil, e
	}
	_, e = meter.Int64ObservableGauge("traceforge.process.goroutines", api.WithUnit("{goroutine}"), api.WithInt64Callback(func(ctx context.Context, o api.Int64Observer) error {
		o.Observe(int64(runtime.NumGoroutine()))
		return nil
	}))
	if e != nil {
		return nil, e
	}
	attrs := api.WithAttributes(attribute.String("http.route", route), attribute.String("http.request.method", "GET"))
	requests.Add(context.Background(), 0, attrs)
	errorsCounter.Add(context.Background(), 0, attrs)
	active.Add(context.Background(), 0, attrs)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != route || r.Method != "GET" {
			next.ServeHTTP(w, r)
			return
		}
		started := time.Now()
		active.Add(r.Context(), 1, attrs)
		out := &responseStatus{ResponseWriter: w}
		defer func() {
			active.Add(r.Context(), -1, attrs)
			requests.Add(r.Context(), 1, attrs)
			if out.status >= 500 {
				errorsCounter.Add(r.Context(), 1, attrs)
			}
			duration.Record(r.Context(), time.Since(started).Seconds(), attrs)
		}()
		next.ServeHTTP(out, r)
	}), nil
}
