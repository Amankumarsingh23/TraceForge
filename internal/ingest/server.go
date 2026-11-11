package ingest

import (
	"context"
	"errors"
	collector "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"google.golang.org/grpc/codes"
	_ "google.golang.org/grpc/encoding/gzip" // Collector OTLP exporter defaults to gzip.
	"google.golang.org/grpc/status"
	"net/http"
	"time"
)

type Store interface {
	Insert(context.Context, []Row) error
	Ping(context.Context) error
}

type Server struct {
	collector.UnimplementedTraceServiceServer
	store Store
	slots chan struct{}
}

func NewServer(store Store) *Server { return &Server{store: store, slots: make(chan struct{}, 4)} }

func (s *Server) Export(ctx context.Context, req *collector.ExportTraceServiceRequest) (*collector.ExportTraceServiceResponse, error) {
	if err := ctx.Err(); err != nil {
		return nil, status.FromContextError(err).Err()
	}
	select {
	case s.slots <- struct{}{}:
		defer func() { <-s.slots }()
	default:
		return nil, status.Error(codes.Unavailable, "ingestion concurrency limit; retry later")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	rows, rejected, err := Normalize(ctx, req)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, status.FromContextError(err).Err()
		}
		return nil, status.Error(codes.InvalidArgument, "invalid export envelope, message size or span count")
	}
	if len(rows) > 0 {
		if err := s.store.Insert(ctx, rows); err != nil {
			if ctx.Err() != nil {
				return nil, status.FromContextError(ctx.Err()).Err()
			}
			return nil, status.Error(codes.Unavailable, "ClickHouse insertion not confirmed; retry may duplicate rows")
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, status.FromContextError(err).Err()
	}
	res := &collector.ExportTraceServiceResponse{}
	if rejected > 0 {
		res.PartialSuccess = &collector.ExportTracePartialSuccess{RejectedSpans: rejected, ErrorMessage: "rejected spans failed identifier, timestamp, structure or size validation; do not retry partial success"}
	}
	return res, nil
}

func (s *Server) HealthHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200); _, _ = w.Write([]byte("alive\n")) })
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), time.Second)
		defer cancel()
		if err := s.store.Ping(ctx); err != nil {
			http.Error(w, "ClickHouse unavailable", 503)
			return
		}
		w.WriteHeader(200)
		_, _ = w.Write([]byte("ready\n"))
	})
	return mux
}
