package telemetry

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"go.opentelemetry.io/contrib/bridges/otelslog"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/resource"
	"log/slog"
	"net/url"
	"os"
	"time"
)

var processInstance = uuid.NewString()

func serviceResource(service string) *resource.Resource {
	return resource.NewSchemaless(attribute.String("service.name", service), attribute.String("service.instance.id", processInstance), attribute.String("service.version", "0.1.0"), attribute.String("deployment.environment.name", "local"), attribute.String("service.namespace", os.Getenv("TRACEFORGE_SERVICE_NAMESPACE")), attribute.String("traceforge.workload.kind", "synthetic"))
}
func NewLogProvider(ctx context.Context, service, endpoint string) (*sdklog.LoggerProvider, *slog.Logger, error) {
	u, e := url.Parse(endpoint)
	if e != nil || u == nil || u.Scheme != "http" || u.Hostname() == "" || u.Port() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" && u.Path != "/" || service != "gateway" && service != "payment" {
		return nil, nil, errors.New("invalid local log exporter configuration")
	}
	exporter, e := otlploggrpc.New(ctx, otlploggrpc.WithEndpoint(u.Host), otlploggrpc.WithInsecure(), otlploggrpc.WithTimeout(3*time.Second))
	if e != nil {
		return nil, nil, errors.New("create log exporter failed")
	}
	processor := sdklog.NewBatchProcessor(exporter, sdklog.WithMaxQueueSize(512), sdklog.WithExportMaxBatchSize(128), sdklog.WithExportInterval(time.Second), sdklog.WithExportTimeout(3*time.Second))
	provider := sdklog.NewLoggerProvider(sdklog.WithResource(serviceResource(service)), sdklog.WithProcessor(processor))
	return provider, otelslog.NewLogger("traceforge/demo", otelslog.WithLoggerProvider(provider)), nil
}
