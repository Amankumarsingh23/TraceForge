package ingest

import (
	"errors"
	collector "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"google.golang.org/protobuf/proto"
	"time"
)

type Config struct {
	QueueRequests   int           `json:"queue_requests"`
	InFlight        int           `json:"in_flight"`
	PendingRequests int           `json:"pending_requests"`
	BufferedSpans   int           `json:"buffered_spans"`
	BufferedBytes   int64         `json:"buffered_bytes"`
	BatchTarget     int           `json:"batch_target"`
	Workers         int           `json:"workers"`
	BatchWait       time.Duration `json:"batch_wait_ns"`
	AdmissionWait   time.Duration `json:"admission_wait_ns"`
	WriteTimeout    time.Duration `json:"write_timeout_ns"`
	ExportTimeout   time.Duration `json:"export_timeout_ns"`
}

func DefaultConfig() Config {
	return Config{QueueRequests: 64, InFlight: 32, PendingRequests: 128, BufferedSpans: 16384, BufferedBytes: 128 << 20, BatchTarget: 1000, Workers: 1, BatchWait: 100 * time.Millisecond, AdmissionWait: 50 * time.Millisecond, WriteTimeout: 3 * time.Second, ExportTimeout: 5 * time.Second}
}
func (c Config) Validate() error {
	if c.QueueRequests < 1 || c.QueueRequests > 1024 || c.InFlight < 1 || c.InFlight > 256 || c.PendingRequests < c.QueueRequests || c.PendingRequests > 1024 || c.BufferedSpans < 1 || c.BufferedSpans > 100000 || c.BufferedBytes < 1 || c.BufferedBytes > 512<<20 || c.BatchTarget < 1 || c.BatchTarget > 10000 || c.Workers < 1 || c.Workers > 4 || c.BatchWait < 0 || c.BatchWait > time.Second || c.AdmissionWait <= 0 || c.AdmissionWait > time.Second || c.WriteTimeout <= 0 || c.WriteTimeout > 5*time.Second || c.ExportTimeout <= 0 || c.ExportTimeout > 10*time.Second {
		return errors.New("ingestion limits out of range")
	}
	return nil
}

// Inspect runs before normalized rows are allocated. Resource/scope metadata is
// charged PER SPAN because normalization repeats it in each stored row. This is
// a conservative payload reservation (64x protobuf bytes plus row overhead),
// not a claim to measure Go RSS or gRPC transport/GC overhead exactly.
func Inspect(req *collector.ExportTraceServiceRequest) (int, int64, error) {
	if req == nil || proto.Size(req) > MaxMessageBytes || len(req.ResourceSpans) > MaxSpans {
		return 0, 0, errors.New("invalid export envelope")
	}
	count, scopes := 0, 0
	var cost int64
	for _, rs := range req.ResourceSpans {
		if rs == nil {
			return 0, 0, errors.New("nil resource group")
		}
		scopes += len(rs.ScopeSpans)
		if scopes > MaxSpans {
			return 0, 0, errors.New("scope group limit")
		}
		for _, ss := range rs.ScopeSpans {
			if ss == nil {
				return 0, 0, errors.New("nil scope group")
			}
			count += len(ss.Spans)
			if count > MaxSpans {
				return 0, 0, errors.New("span count limit")
			}
			for _, sp := range ss.Spans {
				cost += 64*int64(proto.Size(rs.Resource)+proto.Size(ss.Scope)+proto.Size(sp)+len(rs.SchemaUrl)+len(ss.SchemaUrl)+512) + 512
			}
		}
	}
	return count, cost, nil
}
func retainedBytes(rows []Row) int64 {
	n := int64(512 + cap(rows)*512)
	for _, r := range rows {
		for _, s := range []string{r.TraceID, r.SpanID, r.ParentSpanID, r.ServiceName, r.SpanName, r.StatusMessage, r.ResourceAttributesJSON, r.SpanAttributesJSON, r.ScopeName, r.ScopeVersion, r.EventsJSON, r.LinksJSON, r.ResourceJSON, r.ScopeJSON, r.SpanJSON, r.ResourceSchemaURL, r.ScopeSchemaURL} {
			n += int64(len(s))
		}
	}
	return n
}
