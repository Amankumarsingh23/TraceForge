package ingest

import (
	"context"
	"errors"
	collector "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	trace "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"net"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeStore struct {
	mu      sync.Mutex
	rows    []Row
	err     error
	entered chan struct{}
	release chan struct{}
}

func (f *fakeStore) Insert(ctx context.Context, rows []Row) error {
	if f.entered != nil {
		f.entered <- struct{}{}
	}
	if f.release != nil {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-f.release:
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	f.rows = append(f.rows, rows...)
	return nil
}
func (f *fakeStore) Ping(context.Context) error { return f.err }
func client(t *testing.T, s *Server) collector.TraceServiceClient {
	t.Helper()
	l := bufconn.Listen(1 << 20)
	g := grpc.NewServer(grpc.MaxRecvMsgSize(MaxMessageBytes))
	collector.RegisterTraceServiceServer(g, s)
	go g.Serve(l)
	c, e := grpc.NewClient("passthrough:///bufnet", grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return l.Dial() }), grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithDefaultCallOptions(grpc.UseCompressor("gzip")))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { c.Close(); g.Stop(); l.Close() })
	return collector.NewTraceServiceClient(c)
}
func TestExportResponses(t *testing.T) {
	f := &fakeStore{}
	c := client(t, NewServer(f))
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	res, e := c.Export(ctx, &collector.ExportTraceServiceRequest{})
	if e != nil || res.PartialSuccess != nil {
		t.Fatal("empty export failed")
	}
	res, e = c.Export(ctx, fixture())
	if e != nil || res.PartialSuccess != nil || len(f.rows) != 1 {
		t.Fatal("full success failed")
	}
	r := fixture()
	r.ResourceSpans[0].ScopeSpans[0].Spans = append(r.ResourceSpans[0].ScopeSpans[0].Spans, &trace.Span{}, &trace.Span{})
	res, e = c.Export(ctx, r)
	if e != nil || res.GetPartialSuccess().GetRejectedSpans() != 2 || len(f.rows) != 2 {
		t.Fatal("partial success incorrect")
	}
	r.ResourceSpans[0].ScopeSpans[0].Spans = []*trace.Span{{}, {}}
	res, e = c.Export(ctx, r)
	if e != nil || res.GetPartialSuccess().GetRejectedSpans() != 2 || len(f.rows) != 2 {
		t.Fatal("all-invalid partial success incorrect")
	}
}
func TestUnavailableAndLimits(t *testing.T) {
	c := client(t, NewServer(&fakeStore{err: errors.New("password=do-not-leak")}))
	_, e := c.Export(context.Background(), fixture())
	if status.Code(e) != codes.Unavailable || strings.Contains(e.Error(), "do-not-leak") {
		t.Fatalf("unsafe error %v", e)
	}
	r := fixture()
	r.ResourceSpans[0].ScopeSpans[0].Spans = make([]*trace.Span, MaxSpans+1)
	if _, e := c.Export(context.Background(), r); status.Code(e) != codes.InvalidArgument {
		t.Fatalf("count status %v", e)
	}
	r = fixture()
	first(r).Name = strings.Repeat("x", MaxMessageBytes+1)
	if _, e := c.Export(context.Background(), r); status.Code(e) != codes.ResourceExhausted {
		t.Fatalf("message status %v", e)
	}
}
func TestAcknowledgmentBoundaryAndCancellation(t *testing.T) {
	f := &fakeStore{entered: make(chan struct{}, 1), release: make(chan struct{})}
	s := NewServer(f)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, e := s.Export(ctx, fixture()); done <- e }()
	<-f.entered
	select {
	case <-done:
		t.Fatal("acknowledged before write")
	case <-time.After(15 * time.Millisecond):
	}
	cancel()
	if e := <-done; status.Code(e) != codes.Canceled {
		t.Fatalf("cancellation %v", e)
	}
	f = &fakeStore{entered: make(chan struct{}, 1), release: make(chan struct{})}
	s = NewServer(f)
	go func() { _, e := s.Export(context.Background(), fixture()); done <- e }()
	<-f.entered
	select {
	case <-done:
		t.Fatal("early acknowledgment")
	default:
	}
	close(f.release)
	if e := <-done; e != nil {
		t.Fatal(e)
	}
}
func TestExportDeadlineAndConcurrency(t *testing.T) {
	f := &fakeStore{entered: make(chan struct{}, 4), release: make(chan struct{})}
	s := NewServer(f)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, e := s.Export(ctx, fixture()); status.Code(e) != codes.DeadlineExceeded {
		t.Fatal("deadline not preserved")
	}
	<-f.entered
	done := make(chan error, 4)
	for i := 0; i < 4; i++ {
		go func() { _, e := s.Export(context.Background(), fixture()); done <- e }()
	}
	for i := 0; i < 4; i++ {
		<-f.entered
	}
	if _, e := s.Export(context.Background(), fixture()); status.Code(e) != codes.Unavailable {
		t.Fatal("concurrency not bounded")
	}
	close(f.release)
	for i := 0; i < 4; i++ {
		if e := <-done; e != nil {
			t.Fatal(e)
		}
	}
}
func TestHealthAndReadiness(t *testing.T) {
	s := NewServer(&fakeStore{err: errors.New("database unavailable")})
	for _, tc := range []struct {
		path string
		code int
	}{{"/healthz", 200}, {"/readyz", 503}} {
		w := httptest.NewRecorder()
		s.HealthHandler().ServeHTTP(w, httptest.NewRequest("GET", tc.path, nil))
		if w.Code != tc.code {
			t.Fatal(tc.path, w.Code)
		}
	}
	s = NewServer(&fakeStore{})
	w := httptest.NewRecorder()
	s.HealthHandler().ServeHTTP(w, httptest.NewRequest("GET", "/readyz", nil))
	if w.Code != 200 {
		t.Fatal("ready database rejected")
	}
}
