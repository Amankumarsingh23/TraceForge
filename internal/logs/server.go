package logs

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	collector "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	_ "google.golang.org/grpc/encoding/gzip" // Collector OTLP exporter defaults to gzip.
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"
	"net/http"
	"time"
)

type Store interface {
	InsertLogs(context.Context, []Row) error
	Ping(context.Context) error
}

type Server struct {
	collector.UnimplementedLogsServiceServer
	store   Store
	batcher *Batcher
	metrics *Metrics
	config  Config
}

func NewServer(store Store) *Server {
	s, err := NewConfiguredServer(store, DefaultConfig())
	if err != nil {
		panic(err)
	}
	return s
}
func NewConfiguredServer(store Store, c Config) (*Server, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	m := NewMetrics(c)
	return &Server{store: store, config: c, metrics: m, batcher: newBatcher(store, c, m, realClock{})}, nil
}
func (s *Server) Shutdown(ctx context.Context) error { return s.batcher.Shutdown(ctx) }
func (s *Server) Wait(ctx context.Context) error     { return s.batcher.Wait(ctx) }
func (s *Server) Snapshot() Snapshot                 { return s.metrics.Snapshot() }
func (s *Server) ingestError(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return status.FromContextError(ctx.Err()).Err()
	}
	if errors.Is(err, ErrBudget) {
		return status.Error(codes.InvalidArgument, "export cannot fit configured log record/payload budget")
	}
	st := status.New(codes.Unavailable, "persistence not confirmed or admission unavailable; retries may duplicate rows")
	with, e := st.WithDetails(&errdetails.RetryInfo{RetryDelay: durationpb.New(s.config.AdmissionWait)})
	if e == nil {
		st = with
	}
	return st.Err()
}

func (s *Server) Export(ctx context.Context, req *collector.ExportLogsServiceRequest) (*collector.ExportLogsServiceResponse, error) {
	start := time.Now()
	s.metrics.requests.Inc()
	s.metrics.change(func(st *Snapshot) { st.Requests++ })
	defer func() { s.metrics.exportLatency.Observe(time.Since(start).Seconds()) }()
	ctx, cancel := context.WithTimeout(ctx, s.config.ExportTimeout)
	defer cancel()
	admitCtx, admitCancel := context.WithTimeout(ctx, s.config.AdmissionWait)
	defer admitCancel()
	release, err := s.batcher.Begin(admitCtx)
	if err != nil {
		return nil, s.ingestError(ctx, err)
	}
	defer release()
	count, cost, err := Inspect(req)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid export envelope, message size or span count")
	}
	s.metrics.received.Add(float64(count))
	s.metrics.change(func(st *Snapshot) { st.ReceivedRecords += uint64(count) })
	var job *ticket
	if count > 0 {
		job, err = s.batcher.reserve(admitCtx, ctx, count, cost)
		if err != nil {
			return nil, s.ingestError(ctx, err)
		}
	}
	admitCancel()
	rows, rejected, err := Normalize(ctx, req, time.Now().UTC())
	if err != nil {
		if job != nil {
			s.batcher.Abort(job, err)
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, status.FromContextError(err).Err()
		}
		return nil, status.Error(codes.InvalidArgument, "invalid export envelope, message size or span count")
	}
	s.metrics.invalid.Add(float64(rejected))
	s.metrics.change(func(st *Snapshot) { st.InvalidRecords += uint64(rejected) })
	if len(rows) > 0 {
		if err := s.batcher.Submit(job, rows); err != nil {
			return nil, s.ingestError(ctx, err)
		}
		select {
		case err := <-job.result:
			if err != nil {
				return nil, s.ingestError(ctx, err)
			}
		case <-ctx.Done():
			return nil, status.FromContextError(ctx.Err()).Err()
		}
	} else if job != nil {
		s.batcher.Abort(job, nil)
	}
	if err := ctx.Err(); err != nil {
		return nil, status.FromContextError(err).Err()
	}
	res := &collector.ExportLogsServiceResponse{}
	if rejected > 0 {
		res.PartialSuccess = &collector.ExportLogsPartialSuccess{RejectedLogRecords: rejected, ErrorMessage: "rejected records failed identifier, timestamp, structure or size validation; do not retry partial success"}
	}
	return res, nil
}

func (s *Server) HealthHandler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /metrics/logs", promhttp.HandlerFor(s.metrics.Registry, promhttp.HandlerOpts{}))
	mux.HandleFunc("GET /debug/log-ingestion", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(s.Snapshot())
	})
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
