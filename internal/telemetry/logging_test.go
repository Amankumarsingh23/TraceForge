package telemetry

import (
	"context"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
	collector "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	"google.golang.org/grpc"
	"net"
	"testing"
	"time"
)

type captureLogs struct {
	collector.UnimplementedLogsServiceServer
	calls chan *collector.ExportLogsServiceRequest
}

func (c *captureLogs) Export(_ context.Context, r *collector.ExportLogsServiceRequest) (*collector.ExportLogsServiceResponse, error) {
	c.calls <- r
	return &collector.ExportLogsServiceResponse{}, nil
}
func TestLoggingBridgeExportsRealOTLPContext(t *testing.T) {
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	server := grpc.NewServer()
	capture := &captureLogs{calls: make(chan *collector.ExportLogsServiceRequest, 4)}
	collector.RegisterLogsServiceServer(server, capture)
	go server.Serve(listener)
	defer server.Stop()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	provider, logger, e := NewLogProvider(ctx, "payment", "http://"+listener.Addr().String())
	if e != nil {
		t.Fatal(e)
	}
	defer provider.Shutdown(ctx)
	tp := sdktrace.NewTracerProvider()
	defer tp.Shutdown(ctx)
	active, span := tp.Tracer("test").Start(ctx, "payment.process")
	logger.InfoContext(context.Background(), "service startup")
	logger.ErrorContext(active, "payment failure", "error_type", "synthetic")
	span.End()
	if provider.ForceFlush(ctx) != nil {
		t.Fatal("flush failed")
	}
	req := <-capture.calls
	found, standalone := false, false
	for _, rs := range req.ResourceLogs {
		service := ""
		for _, a := range rs.Resource.Attributes {
			if a.Key == "service.name" {
				service = a.Value.GetStringValue()
			}
		}
		if service != "payment" {
			t.Fatal("resource identity changed")
		}
		for _, ss := range rs.ScopeLogs {
			for _, lr := range ss.LogRecords {
				if lr.Body.GetStringValue() == "payment failure" {
					id := trace.SpanContextFromContext(active)
					if len(lr.TraceId) != 16 || len(lr.SpanId) != 8 || lr.SeverityNumber != 17 {
						t.Fatal("missing real correlation/severity")
					}
					wantTrace := id.TraceID()
					wantSpan := id.SpanID()
					if string(lr.TraceId) != string(wantTrace[:]) || string(lr.SpanId) != string(wantSpan[:]) {
						t.Fatal("wrong active span")
					}
					found = true
				} else if lr.Body.GetStringValue() == "service startup" {
					standalone = len(lr.TraceId) == 0 && len(lr.SpanId) == 0
				}
			}
		}
	}
	if !found || !standalone {
		t.Fatal("correlated/standalone logs missing")
	}
}
func TestLoggingConfigurationBounds(t *testing.T) {
	for _, endpoint := range []string{"", "localhost:4317", "https://localhost:4317", "http://user:secret@localhost:4317", "http://localhost:4317/path"} {
		if p, _, e := NewLogProvider(context.Background(), "payment", endpoint); e == nil {
			p.Shutdown(context.Background())
			t.Fatal("invalid endpoint accepted")
		}
	}
}
