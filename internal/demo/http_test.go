package demo

import (
	"context"
	"encoding/json"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
	"traceforge/internal/telemetry"
)

func provider(t *testing.T, name string) (*sdktrace.TracerProvider, *tracetest.InMemoryExporter) {
	t.Helper()
	ex := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(ex), sdktrace.WithResource(resource.NewSchemaless(attribute.String("service.name", name))))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	return tp, ex
}

func TestDistributedTrace(t *testing.T) {
	for _, tc := range []struct {
		name, query string
		code        int
		failed      bool
		delay       time.Duration
	}{
		{"success", "", 200, false, 0},
		{"delay", "?delay_ms=40", 200, false, 40 * time.Millisecond},
		{"failure", "?fail=true", 502, true, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ptp, pex := provider(t, "payment")
			gtp, gex := provider(t, "gateway")
			var incoming string
			paymentHandler := Payment(ptp)
			ps := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				incoming = r.Header.Get("traceparent")
				paymentHandler.ServeHTTP(w, r)
			}))
			defer ps.Close()
			gh, err := Gateway(gtp, ps.URL, nil)
			if err != nil {
				t.Fatal(err)
			}
			gs := httptest.NewServer(gh)
			defer gs.Close()
			res, err := http.Get(gs.URL + "/checkout" + tc.query)
			if err != nil {
				t.Fatal(err)
			}
			var body map[string]string
			if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			res.Body.Close()
			if res.StatusCode != tc.code {
				t.Fatalf("status %d", res.StatusCode)
			}
			// A response can reach the client just before the server span ends.
			deadline := time.Now().Add(time.Second)
			for len(gex.GetSpans())+len(pex.GetSpans()) != 5 && time.Now().Before(deadline) {
				time.Sleep(time.Millisecond)
			}
			spans := append(gex.GetSpans(), pex.GetSpans()...)
			if len(spans) != 5 {
				t.Fatalf("expected 5 spans, got %d", len(spans))
			}
			var gateway, client, payment, validation, processing tracetest.SpanStub
			ids := map[trace.SpanID]bool{}
			for _, s := range spans {
				if !s.SpanContext.IsValid() || s.SpanContext.TraceID().String() != body["trace_id"] {
					t.Fatal("invalid or split trace")
				}
				if ids[s.SpanContext.SpanID()] {
					t.Fatal("duplicate span ID")
				}
				ids[s.SpanContext.SpanID()] = true
				switch {
				case s.SpanKind == trace.SpanKindClient:
					client = s
				case s.Name == "GET /checkout":
					gateway = s
				case s.Name == "GET /charge":
					payment = s
				case s.Name == "checkout.validate":
					validation = s
				case s.Name == "payment.process":
					processing = s
				default:
					t.Fatalf("unexpected span %s", s.Name)
				}
			}
			if gateway.SpanKind != trace.SpanKindServer || payment.SpanKind != trace.SpanKindServer || processing.SpanKind != trace.SpanKindInternal || validation.SpanKind != trace.SpanKindInternal {
				t.Fatal("wrong span kinds")
			}
			if gateway.Parent.IsValid() || client.Parent.SpanID() != gateway.SpanContext.SpanID() || validation.Parent.SpanID() != gateway.SpanContext.SpanID() || payment.Parent.SpanID() != client.SpanContext.SpanID() || processing.Parent.SpanID() != payment.SpanContext.SpanID() || !payment.Parent.IsRemote() {
				t.Fatal("incorrect parent hierarchy")
			}
			remote := trace.SpanContextFromContext(telemetry.Propagator().Extract(context.Background(), propagation.MapCarrier{"traceparent": incoming}))
			if !remote.IsValid() || remote.SpanID() != client.SpanContext.SpanID() || remote.TraceID() != client.SpanContext.TraceID() {
				t.Fatal("invalid wire traceparent")
			}
			if processing.EndTime.Sub(processing.StartTime) < tc.delay {
				t.Fatal("simulation delay absent")
			}
			if tc.failed {
				for _, s := range []tracetest.SpanStub{gateway, client, payment, processing} {
					if s.Status.Code != codes.Error {
						t.Fatalf("%s missing error", s.Name)
					}
				}
				if len(processing.Events) == 0 || len(gateway.Events) == 0 {
					t.Fatal("missing error events")
				}
			}
		})
	}
}

func TestInvalidSimulation(t *testing.T) {
	tp, _ := provider(t, "payment")
	for _, query := range []string{"?delay_ms=-1", "?delay_ms=2001", "?delay_ms=no", "?fail=maybe"} {
		w := httptest.NewRecorder()
		Payment(tp).ServeHTTP(w, httptest.NewRequest("GET", "/charge"+query, nil))
		if w.Code != 400 {
			t.Fatalf("%s: %d", query, w.Code)
		}
	}
}

func TestIncomingW3CContext(t *testing.T) {
	tp, ex := provider(t, "payment")
	r := httptest.NewRequest("GET", "/charge", nil)
	r.Header.Set("traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
	Payment(tp).ServeHTTP(httptest.NewRecorder(), r)
	for _, s := range ex.GetSpans() {
		if s.SpanContext.TraceID().String() != "4bf92f3577b34da6a3ce929d0e0e4736" {
			t.Fatal("lost incoming trace")
		}
		if s.SpanKind == trace.SpanKindServer && s.Parent.SpanID().String() != "00f067aa0ba902b7" {
			t.Fatal("lost remote parent")
		}
	}
}

func TestUninstrumentedTransportSplitsTrace(t *testing.T) {
	tp, ex := provider(t, "payment")
	ps := httptest.NewServer(Payment(tp))
	defer ps.Close()
	ctx, span := tp.Tracer("experiment").Start(context.Background(), "unpropagated caller")
	req, _ := http.NewRequestWithContext(ctx, "GET", ps.URL+"/charge", nil)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, res.Body)
	res.Body.Close()
	span.End()
	deadline := time.Now().Add(time.Second)
	for len(ex.GetSpans()) != 3 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if len(ex.GetSpans()) != 3 {
		t.Fatalf("expected 3 spans, got %d", len(ex.GetSpans()))
	}
	for _, s := range ex.GetSpans() {
		if s.Name == "GET /charge" && (s.Parent.IsValid() || s.SpanContext.TraceID() == span.SpanContext().TraceID()) {
			t.Fatal("expected separate root trace")
		}
	}
}

func TestServeShutdown(t *testing.T) {
	// Reserve an available address; the small handoff window is local to this test.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	l.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered := make(chan struct{})
	release := make(chan struct{})
	s := &http.Server{Addr: addr, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(entered); <-release; w.WriteHeader(200) }), ReadHeaderTimeout: time.Second}
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, s) }()
	clientDone := make(chan error, 1)
	go func() {
		client := &http.Client{Timeout: 2 * time.Second}
		for i := 0; i < 100; i++ {
			res, e := client.Get("http://" + addr)
			if e == nil {
				res.Body.Close()
				clientDone <- nil
				return
			}
			time.Sleep(time.Millisecond * 5)
		}
		clientDone <- context.DeadlineExceeded
	}()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("server did not start")
	}
	cancel()
	select {
	case <-done:
		t.Fatal("did not drain request")
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	select {
	case e := <-done:
		if e != nil {
			t.Fatal(e)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("shutdown hung")
	}
	if err := <-clientDone; err != nil {
		t.Fatal(err)
	}
}

func TestInvalidPaymentURL(t *testing.T) {
	tp, _ := provider(t, "gateway")
	if _, err := Gateway(tp, "https://user:secret@example.com", nil); err == nil {
		t.Fatal("accepted invalid URL")
	}
}
