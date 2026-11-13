package logs

import (
	"context"
	"errors"
	collector "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	trace "go.opentelemetry.io/proto/otlp/logs/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

type writeCall struct {
	rows  []Row
	reply chan error
}
type controlledWriter struct{ calls chan writeCall }

func (w *controlledWriter) Ping(context.Context) error { return nil }
func (w *controlledWriter) InsertLogs(ctx context.Context, rows []Row) error {
	c := writeCall{append([]Row{}, rows...), make(chan error, 1)}
	select {
	case w.calls <- c:
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case err := <-c.reply:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

type manualTimer struct{ ch chan time.Time }

func (t *manualTimer) C() <-chan time.Time { return t.ch }
func (t *manualTimer) Stop()               {}

type manualClock struct {
	mu      sync.Mutex
	now     time.Time
	created chan *manualTimer
}

func (c *manualClock) Now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.now }
func (c *manualClock) Timer(time.Duration) batchTimer {
	t := &manualTimer{ch: make(chan time.Time, 1)}
	c.created <- t
	return t
}
func (c *manualClock) fire(t *manualTimer, d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	now := c.now
	c.mu.Unlock()
	t.ch <- now
}

func engine(t *testing.T, c Config, clock batchClock) (*Batcher, *controlledWriter) {
	t.Helper()
	w := &controlledWriter{calls: make(chan writeCall, 16)}
	e := newBatcher(w, c, NewMetrics(c), clock)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()
		_ = e.Shutdown(ctx)
		wait, stop := context.WithTimeout(context.Background(), time.Second)
		defer stop()
		if err := e.Wait(wait); err != nil {
			t.Error("workers leaked", err)
		}
	})
	return e, w
}
func job(t *testing.T, e *Batcher, n int, ctx context.Context) *ticket {
	t.Helper()
	r := fixture()
	sp := first(r)
	r.ResourceLogs[0].ScopeLogs[0].LogRecords = make([]*trace.LogRecord, n)
	for i := 0; i < n; i++ {
		r.ResourceLogs[0].ScopeLogs[0].LogRecords[i] = sp
	}
	count, weight, err := Inspect(r)
	if err != nil {
		t.Fatal(err)
	}
	j, err := e.Reserve(ctx, count, weight)
	if err != nil {
		t.Fatal(err)
	}
	rows, _, err := Normalize(ctx, r, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Submit(j, rows); err != nil {
		t.Fatal(err)
	}
	return j
}
func nextWrite(t *testing.T, w *controlledWriter) writeCall {
	t.Helper()
	select {
	case c := <-w.calls:
		return c
	case <-time.After(time.Second):
		t.Fatal("writer never started")
		return writeCall{}
	}
}
func result(t *testing.T, j *ticket) error {
	t.Helper()
	select {
	case e := <-j.result:
		return e
	case <-time.After(time.Second):
		t.Fatal("job not completed")
		return nil
	}
}

func TestCoalescedWriteAcknowledgments(t *testing.T) {
	c := DefaultConfig()
	c.BatchTarget = 3
	c.BatchWait = time.Second
	clock := &manualClock{now: time.Now(), created: make(chan *manualTimer, 4)}
	e, w := engine(t, c, clock)
	a := job(t, e, 1, context.Background())
	<-clock.created
	b := job(t, e, 2, context.Background())
	call := nextWrite(t, w)
	if len(call.rows) != 3 {
		t.Fatal("requests not coalesced")
	}
	select {
	case <-a.result:
		t.Fatal("early acknowledgment")
	default:
	}
	call.reply <- nil
	if result(t, a) != nil || result(t, b) != nil {
		t.Fatal("shared success failed")
	}
	s := e.metrics.Snapshot()
	if s.Batches != 1 || s.PersistedRecords != 3 || s.PendingRequests != 0 || s.ReservedBytes != 0 {
		t.Fatalf("leaked accounting %+v", s)
	}
}
func TestTimerAndOversizedRequestBatch(t *testing.T) {
	c := DefaultConfig()
	c.BatchTarget = 3
	c.BatchWait = time.Second
	clock := &manualClock{now: time.Now(), created: make(chan *manualTimer, 4)}
	e, w := engine(t, c, clock)
	a := job(t, e, 1, context.Background())
	timer := <-clock.created
	clock.fire(timer, time.Second)
	call := nextWrite(t, w)
	if len(call.rows) != 1 {
		t.Fatal("timer did not flush low-volume work")
	}
	call.reply <- nil
	if result(t, a) != nil {
		t.Fatal("timer result")
	}
	b := job(t, e, 5, context.Background())
	call = nextWrite(t, w)
	if len(call.rows) != 5 {
		t.Fatal("large request was split/truncated")
	}
	call.reply <- nil
	if result(t, b) != nil {
		t.Fatal("large job failed")
	}
}

func TestExpiredOldestJobStillCoalescesReadyBacklog(t *testing.T) {
	c := DefaultConfig()
	c.BatchTarget = 3
	c.BatchWait = time.Second
	clock := &manualClock{now: time.Now(), created: make(chan *manualTimer, 4)}
	e, w := engine(t, c, clock)
	a := job(t, e, 3, context.Background())
	call := nextWrite(t, w)
	b := job(t, e, 1, context.Background())
	d := job(t, e, 1, context.Background())
	f := job(t, e, 1, context.Background())
	clock.mu.Lock()
	clock.now = clock.now.Add(2 * time.Second)
	clock.mu.Unlock()
	call.reply <- nil
	call = nextWrite(t, w)
	if len(call.rows) != 3 {
		t.Fatal("expired queue wait degraded ready backlog into tiny inserts")
	}
	call.reply <- nil
	for _, j := range []*ticket{a, b, d, f} {
		if result(t, j) != nil {
			t.Fatal("backlog failed")
		}
	}
}
func TestQueueSpanByteAndInFlightBounds(t *testing.T) {
	for _, kind := range []string{"queue", "records", "bytes"} {
		t.Run(kind, func(t *testing.T) {
			c := DefaultConfig()
			c.BatchTarget = 1
			c.BatchWait = 0
			c.AdmissionWait = 5 * time.Millisecond
			if kind == "queue" {
				c.QueueRequests = 1
			}
			if kind == "records" {
				c.BufferedRecords = 2
			}
			e, w := engine(t, c, realClock{})
			a := job(t, e, 1, context.Background())
			call := nextWrite(t, w)
			b := job(t, e, 1, context.Background())
			if kind == "bytes" {
				e.mu.Lock()
				e.config.BufferedBytes = e.bytes
				e.mu.Unlock()
			}
			_, _, err := Inspect(fixture())
			if err != nil {
				t.Fatal(err)
			}
			_, weight, _ := Inspect(fixture())
			start := time.Now()
			if _, err := e.Reserve(context.Background(), 1, weight); !errors.Is(err, ErrOverloaded) {
				t.Fatal("unbounded admission", err)
			}
			if time.Since(start) > time.Second {
				t.Fatal("admission wait unbounded")
			}
			s := e.metrics.Snapshot()
			if s.PendingRequests != 2 || s.ReservedRecords != 2 || s.Queue != 1 || s.AdmissionRejections == 0 {
				t.Fatal("bad capacity accounting", s)
			}
			call.reply <- nil
			next := nextWrite(t, w)
			next.reply <- nil
			if result(t, a) != nil || result(t, b) != nil {
				t.Fatal("admitted work dropped")
			}
		})
	}
	c := DefaultConfig()
	c.InFlight = 1
	c.AdmissionWait = 5 * time.Millisecond
	e, _ := engine(t, c, realClock{})
	release, err := e.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.Begin(context.Background()); !errors.Is(err, ErrOverloaded) {
		t.Fatal("unbounded handlers")
	}
	release()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := e.Begin(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal("canceled caller admitted")
	}
	if _, err := e.Reserve(context.Background(), c.BufferedRecords+1, 1); !errors.Is(err, ErrBudget) {
		t.Fatal("unfit request budget not rejected")
	}
}
func TestQueuedCancellationAndSharedCancellation(t *testing.T) {
	c := DefaultConfig()
	c.BatchTarget = 2
	c.BatchWait = time.Second
	clock := &manualClock{now: time.Now(), created: make(chan *manualTimer, 8)}
	e, w := engine(t, c, clock)
	ctx, cancel := context.WithCancel(context.Background())
	a := job(t, e, 1, ctx)
	<-clock.created
	cancel()
	b := job(t, e, 1, context.Background())
	call := nextWrite(t, w)
	if len(call.rows) != 1 {
		t.Fatal("queued cancellation persisted before write")
	}
	call.reply <- nil
	if !errors.Is(result(t, a), context.Canceled) || result(t, b) != nil {
		t.Fatal("cancellation not isolated")
	}
	ctx, cancel = context.WithCancel(context.Background())
	a = job(t, e, 1, ctx)
	<-clock.created
	b = job(t, e, 1, context.Background())
	call = nextWrite(t, w)
	cancel()
	call.reply <- nil
	if result(t, a) != nil || result(t, b) != nil {
		t.Fatal("one caller canceled shared persistence")
	}
	if e.metrics.Snapshot().PersistedRecords != 3 {
		t.Fatal("confirmed late persistence not counted")
	}
}
func TestBatchFailureAmbiguityAndShutdown(t *testing.T) {
	c := DefaultConfig()
	c.BatchTarget = 2
	c.BatchWait = time.Second
	clock := &manualClock{now: time.Now(), created: make(chan *manualTimer, 4)}
	e, w := engine(t, c, clock)
	a := job(t, e, 1, context.Background())
	<-clock.created
	b := job(t, e, 1, context.Background())
	call := nextWrite(t, w)
	// The writer may have stored these rows before an acknowledgment was lost.
	physicallyStored := len(call.rows)
	call.reply <- errors.New("ambiguous commit result")
	if result(t, a) == nil || result(t, b) == nil || physicallyStored != 2 || e.metrics.Snapshot().PersistenceFailures != 1 {
		t.Fatal("ambiguous failure acknowledged as success")
	}
	a = job(t, e, 1, context.Background())
	<-clock.created
	drained := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		drained <- e.Shutdown(ctx)
	}()
	call = nextWrite(t, w)
	if _, err := e.Reserve(context.Background(), 1, 1); !errors.Is(err, ErrStopped) {
		t.Fatal("shutdown accepted new work")
	}
	call.reply <- nil
	if result(t, a) != nil || <-drained != nil {
		t.Fatal("final shutdown flush failed")
	}
}
func TestShutdownTimeoutFailsQueuedAndActive(t *testing.T) {
	c := DefaultConfig()
	c.BatchTarget = 1
	c.BatchWait = 0
	e, w := engine(t, c, realClock{})
	a := job(t, e, 1, context.Background())
	_ = nextWrite(t, w)
	b := job(t, e, 1, context.Background())
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if err := e.Shutdown(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("unbounded drain", err)
	}
	if result(t, a) == nil || result(t, b) == nil {
		t.Fatal("unresolved job got success")
	}
	wait, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	if err := e.Wait(wait); err != nil {
		t.Fatal("worker did not terminate")
	}
	s := e.metrics.Snapshot()
	if s.PendingRequests != 0 || s.ReservedBytes != 0 || s.Queue != 0 {
		t.Fatal("shutdown leaked reservations", s)
	}
}
func TestBufferedServerPartialSuccessAndMetrics(t *testing.T) {
	f := &fakeStore{}
	c := DefaultConfig()
	c.BatchTarget = 1
	c.BatchWait = 0
	s, err := NewConfiguredServer(f, c)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = s.Shutdown(ctx)
	}()
	r := fixture()
	r.ResourceLogs[0].ScopeLogs[0].LogRecords = append(r.ResourceLogs[0].ScopeLogs[0].LogRecords, &trace.LogRecord{TraceId: []byte{1}})
	res, err := s.Export(context.Background(), r)
	if err != nil || res.GetPartialSuccess().GetRejectedLogRecords() != 1 || s.Snapshot().PersistedRecords != 1 {
		t.Fatal("partial success contract changed", err)
	}
	w := httptest.NewRecorder()
	s.HealthHandler().ServeHTTP(w, httptest.NewRequest("GET", "/metrics/logs", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), "traceforge_logs_records_persisted_total 1") || strings.Contains(w.Body.String(), first(r).Body.GetStringValue()+"=") {
		t.Fatal("metrics missing or unbounded labels")
	}
	if _, err := s.Export(context.Background(), nil); status.Code(err) != codes.InvalidArgument {
		t.Fatal("malformed envelope accepted")
	}
	_, cost, _ := Inspect(fixture())
	rows, _, _ := Normalize(context.Background(), fixture(), time.Now().UTC())
	if cost < retainedBytes(rows) {
		t.Fatal("reservation underestimates retained payload")
	}
}

func TestSharedBatchKeepsEachRequestsRejectionCount(t *testing.T) {
	c := DefaultConfig()
	c.BatchTarget = 2
	c.BatchWait = time.Second
	clock := &manualClock{now: time.Now(), created: make(chan *manualTimer, 4)}
	e, w := engine(t, c, clock)
	s := &Server{store: w, config: c, metrics: e.metrics, batcher: e}
	type outcome struct {
		response *collector.ExportLogsServiceResponse
		err      error
	}
	aDone := make(chan outcome, 1)
	bDone := make(chan outcome, 1)
	a := fixture()
	a.ResourceLogs[0].ScopeLogs[0].LogRecords = append(a.ResourceLogs[0].ScopeLogs[0].LogRecords, &trace.LogRecord{TraceId: []byte{1}}, &trace.LogRecord{TraceId: []byte{1}})
	b := fixture()
	b.ResourceLogs[0].ScopeLogs[0].LogRecords = append(b.ResourceLogs[0].ScopeLogs[0].LogRecords, &trace.LogRecord{TraceId: []byte{1}})
	go func() { r, e := s.Export(context.Background(), a); aDone <- outcome{r, e} }()
	<-clock.created
	go func() { r, e := s.Export(context.Background(), b); bDone <- outcome{r, e} }()
	call := nextWrite(t, w)
	if len(call.rows) != 2 {
		t.Fatal("valid subsets not combined")
	}
	select {
	case <-aDone:
		t.Fatal("partial success before persistence")
	default:
	}
	call.reply <- nil
	x, y := <-aDone, <-bDone
	if x.err != nil || y.err != nil || x.response.GetPartialSuccess().GetRejectedLogRecords() != 2 || y.response.GetPartialSuccess().GetRejectedLogRecords() != 1 {
		t.Fatal("request validation counts mixed")
	}
	if st := s.Snapshot(); st.PersistedRecords != 2 || st.InvalidRecords != 3 || st.PendingRequests != 0 {
		t.Fatal("bad shared accounting", st)
	}
}
