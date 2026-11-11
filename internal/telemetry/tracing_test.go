package telemetry

import (
	"context"
	"go.opentelemetry.io/otel/propagation"
	"testing"
)

func TestInvalidConfiguration(t *testing.T) {
	for _, endpoint := range []string{"", "localhost:4317", "http://localhost", "https://localhost:4317", "http://user:secret@localhost:4317", "http://localhost:4317/v1/traces"} {
		if tp, err := NewProvider(context.Background(), "gateway", endpoint); err == nil {
			tp.Shutdown(context.Background())
			t.Fatalf("accepted %q", endpoint)
		}
	}
	if _, err := NewProvider(context.Background(), "unknown", "http://localhost:4317"); err == nil {
		t.Fatal("accepted invalid service")
	}
}

func TestProviderShutdown(t *testing.T) {
	tp, err := NewProvider(context.Background(), "gateway", "http://localhost:4317")
	if err != nil {
		t.Fatal(err)
	}
	// No spans exported: shutdown needs no live Collector.
	if err := tp.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := tp.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestBaggagePropagation(t *testing.T) {
	ctx := Propagator().Extract(context.Background(), propagation.MapCarrier{"baggage": "synthetic=demo"})
	carrier := propagation.MapCarrier{}
	Propagator().Inject(ctx, carrier)
	if carrier.Get("baggage") != "synthetic=demo" {
		t.Fatal("baggage lost")
	}
}
