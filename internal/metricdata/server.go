package metricdata

import (
	"context"
	collector "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"sync"
	"sync/atomic"
	"time"
)

type Writer interface {
	InsertMetricPoints(context.Context, []Point) error
}

// Admission is lifetime-bounded and concurrency-safe. Reservations survive failed
// writes (ambiguous inserts may exist). Restart resets this local guard, not storage.
type Admission struct {
	mu        sync.Mutex
	series    map[string]string
	resources map[string]int
	writers   map[string]bool
	limit     int
}

func NewAdmission(limit int) *Admission {
	return &Admission{series: map[string]string{}, resources: map[string]int{}, writers: map[string]bool{}, limit: limit}
}
func (a *Admission) Accept(points []Point) ([]Point, int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := []Point{}
	rejected := 0
	for _, p := range points {
		if old, ok := a.series[p.StreamID]; ok {
			if old != p.StreamKey {
				rejected++
				continue
			}
			out = append(out, p)
			continue
		}
		resource := Hash(string(p.Resource))
		writer := p.Instance
		if len(p.StreamKey) > 32768 || len(a.series) >= a.limit || a.resources[resource] >= 256 || a.resources[resource] == 0 && len(a.resources) >= 128 || !a.writers[writer] && len(a.writers) >= 128 {
			rejected++
			continue
		}
		a.series[p.StreamID] = p.StreamKey
		a.resources[resource]++
		a.writers[writer] = true
		out = append(out, p)
	}
	return out, rejected
}

type Server struct {
	collector.UnimplementedMetricsServiceServer
	writer                                Writer
	admission                             *Admission
	slots                                 chan struct{}
	active                                sync.WaitGroup
	mu                                    sync.Mutex
	closed                                bool
	received, rejected, confirmed, failed atomic.Uint64
}

func NewServer(w Writer) *Server {
	return &Server{writer: w, admission: NewAdmission(MaxSeries), slots: make(chan struct{}, 2)}
}
func (s *Server) Export(ctx context.Context, req *collector.ExportMetricsServiceRequest) (*collector.ExportMetricsServiceResponse, error) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil, status.Error(codes.Unavailable, "metric receiver draining")
	}
	s.active.Add(1)
	s.mu.Unlock()
	defer s.active.Done()
	admissionCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()
	select {
	case s.slots <- struct{}{}:
		defer func() { <-s.slots }()
	case <-admissionCtx.Done():
		return nil, status.Error(codes.Unavailable, "metric persistence slots full; retry may replay points")
	}
	points, rejected, e := Normalize(req)
	if e != nil {
		return nil, status.Error(codes.InvalidArgument, "metric export exceeds shape, point or byte budget")
	}
	s.received.Add(uint64(len(points) + rejected))
	points, limited := s.admission.Accept(points)
	rejected += limited
	s.rejected.Add(uint64(rejected))
	if len(points) > 0 {
		writeCtx, writeCancel := context.WithTimeout(ctx, 3*time.Second)
		e = s.writer.InsertMetricPoints(writeCtx, points)
		writeCancel()
		if e != nil {
			s.failed.Add(1)
			if ctx.Err() != nil {
				return nil, status.FromContextError(ctx.Err()).Err()
			}
			return nil, status.Error(codes.Unavailable, "metric persistence not confirmed; retries may duplicate physical points")
		}
		s.confirmed.Add(uint64(len(points)))
	}
	response := &collector.ExportMetricsServiceResponse{}
	if rejected > 0 {
		response.PartialSuccess = &collector.ExportMetricsPartialSuccess{RejectedDataPoints: int64(rejected), ErrorMessage: "unsupported, invalid, sensitive, exemplar-bearing or cardinality-rejected data points"}
	}
	return response, nil
}
func (s *Server) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	done := make(chan struct{})
	go func() { s.active.Wait(); close(done) }()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (s *Server) Snapshot() map[string]uint64 {
	return map[string]uint64{"received_points": s.received.Load(), "rejected_points": s.rejected.Load(), "confirmed_physical_points": s.confirmed.Load(), "failed_writes": s.failed.Load()}
}
