// Package telemetry configures the standard OpenTelemetry SDK for the demo.
package telemetry

import (
	"context"
	"fmt"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"net/url"
	"os"
)

func Propagator() propagation.TextMapPropagator {
	return propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{})
}

// NewProvider validates configuration, but exporter construction does not prove
// Collector connectivity. Export failures are reported by the SDK error handler.
func NewProvider(ctx context.Context, service, endpoint string) (*sdktrace.TracerProvider, error) {
	sampler, err := SamplingProfile(os.Getenv("OTEL_TRACES_SAMPLER"), os.Getenv("OTEL_TRACES_SAMPLER_ARG"))
	if err != nil {
		return nil, err
	}
	u, err := url.Parse(endpoint)
	if err != nil || u == nil || u.Scheme != "http" || u.Hostname() == "" || u.Port() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return nil, fmt.Errorf("OTLP endpoint must be an http://host:port URL for this local gRPC demo")
	}
	if service != "gateway" && service != "payment" {
		return nil, fmt.Errorf("service must be gateway or payment")
	}
	exporter, err := otlptracegrpc.New(ctx, otlptracegrpc.WithEndpoint(u.Host), otlptracegrpc.WithInsecure())
	if err != nil {
		return nil, fmt.Errorf("create OTLP exporter: %w", err)
	}
	res := serviceResource(service)
	return sdktrace.NewTracerProvider(sdktrace.WithResource(res), sdktrace.WithSampler(sampler), sdktrace.WithBatcher(exporter)), nil
}
