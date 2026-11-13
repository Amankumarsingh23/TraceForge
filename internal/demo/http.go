package demo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"time"
	"traceforge/internal/telemetry"
)

// Simulation permits synthetic, bounded inputs only. No request data is exported.
func simulation(r *http.Request) (time.Duration, bool, error) {
	q := r.URL.Query()
	delay := 0
	var err error
	if q.Has("delay_ms") {
		delay, err = strconv.Atoi(q.Get("delay_ms"))
	}
	if err != nil || delay < 0 || delay > 2000 {
		return 0, false, errors.New("delay_ms must be 0..2000")
	}
	fail := q.Get("fail")
	if fail != "" && fail != "true" && fail != "false" {
		return 0, false, errors.New("fail must be true or false")
	}
	return time.Duration(delay) * time.Millisecond, fail == "true", nil
}

func respond(w http.ResponseWriter, r *http.Request, code int, status string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": status, "trace_id": trace.SpanContextFromContext(r.Context()).TraceID().String()})
}

func options(tp trace.TracerProvider) []otelhttp.Option {
	return []otelhttp.Option{otelhttp.WithTracerProvider(tp), otelhttp.WithPropagators(telemetry.Propagator())}
}

func demoLogger(loggers []*slog.Logger) *slog.Logger {
	if len(loggers) > 0 && loggers[0] != nil {
		return loggers[0]
	}
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
func Payment(tp trace.TracerProvider, loggers ...*slog.Logger) http.Handler {
	logger := demoLogger(loggers)
	tracer := tp.Tracer("traceforge/demo")
	mux := http.NewServeMux()
	mux.Handle("GET /charge", otelhttp.NewHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		delay, fail, err := simulation(r)
		if err != nil {
			respond(w, r, 400, err.Error())
			return
		}
		ctx, span := tracer.Start(r.Context(), "payment.process", trace.WithAttributes(attribute.String("payment.provider", "synthetic")))
		defer span.End()
		logger.InfoContext(ctx, "payment started", "payment.provider", "synthetic")
		if delay > 0 {
			logger.WarnContext(ctx, "controlled payment delay", "delay_ms", delay.Milliseconds())
		}
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			logger.WarnContext(ctx, "payment canceled", "error_type", "request_canceled")
			span.RecordError(ctx.Err())
			span.SetStatus(codes.Error, "request canceled")
			return
		case <-timer.C:
		}
		if fail {
			logger.ErrorContext(ctx, "payment failure", "error_type", "synthetic_provider_failure", "payment.provider", "synthetic")
			err := errors.New("synthetic payment failure")
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			respond(w, r, http.StatusServiceUnavailable, "payment_failed")
			return
		}
		logger.InfoContext(ctx, "payment completion", "payment.provider", "synthetic")
		respond(w, r, 200, "ok")
	}), "GET /charge", options(tp)...))
	return mux
}

func Gateway(tp trace.TracerProvider, paymentURL string, transport http.RoundTripper, loggers ...*slog.Logger) (http.Handler, error) {
	logger := demoLogger(loggers)
	u, err := url.Parse(paymentURL)
	if err != nil || u == nil || u.Scheme != "http" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return nil, fmt.Errorf("payment URL must be an http://host:port origin")
	}
	if transport == nil {
		transport = http.DefaultTransport
	}
	client := &http.Client{Transport: otelhttp.NewTransport(transport, options(tp)...), Timeout: 4 * time.Second,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	tracer := tp.Tracer("traceforge/demo")
	mux := http.NewServeMux()
	mux.Handle("GET /checkout", otelhttp.NewHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		delay, fail, err := simulation(r)
		if err != nil {
			respond(w, r, 400, err.Error())
			return
		}
		logger.InfoContext(r.Context(), "checkout started", "checkout.synthetic", true)
		validationCtx, validation := tracer.Start(r.Context(), "checkout.validate")
		validation.SetAttributes(attribute.Bool("checkout.synthetic", true))
		logger.InfoContext(validationCtx, "checkout validation", "checkout.synthetic", true)
		validation.End()
		// The outgoing client span is a sibling of checkout.validate.
		target := *u
		target.Path = "/charge"
		q := url.Values{"delay_ms": {strconv.FormatInt(delay.Milliseconds(), 10)}, "fail": {strconv.FormatBool(fail)}}
		target.RawQuery = q.Encode()
		req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, target.String(), nil)
		if err != nil {
			gatewayError(w, r)
			return
		}
		res, err := client.Do(req)
		if err != nil {
			gatewayError(w, r)
			return
		}
		defer res.Body.Close()
		_, err = io.Copy(io.Discard, io.LimitReader(res.Body, 4096))
		if err != nil || res.StatusCode != 200 {
			logger.ErrorContext(r.Context(), "checkout payment failed", "error_type", "payment_call_failed")
			gatewayError(w, r)
			return
		}
		logger.InfoContext(r.Context(), "checkout completion", "checkout.synthetic", true)
		respond(w, r, 200, "ok")
	}), "GET /checkout", options(tp)...))
	return mux, nil
}

func gatewayError(w http.ResponseWriter, r *http.Request) {
	span := trace.SpanFromContext(r.Context())
	// Export a safe fixed message, never a URL or arbitrary network error.
	span.RecordError(errors.New("payment call failed"))
	span.SetStatus(codes.Error, "payment call failed")
	respond(w, r, http.StatusBadGateway, "payment_failed")
}

// Serve drains active requests before returning. On timeout it closes remaining
// connections; callers flush tracing only after this function returns.
func Serve(ctx context.Context, server *http.Server) error {
	stopped := make(chan struct{})
	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		select {
		case <-ctx.Done():
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := server.Shutdown(shutdownCtx); err != nil {
				_ = server.Close()
			}
		case <-stopped:
		}
	}()
	err := server.ListenAndServe()
	close(stopped)
	<-shutdownDone
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}
