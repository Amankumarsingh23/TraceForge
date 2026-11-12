package ingest

import (
	"context"
	"errors"
	"sync"
	"time"
)

var ErrOverloaded = errors.New("ingestion capacity unavailable")
var ErrStopped = errors.New("ingestion stopping; persistence outcome may be unknown")
var ErrBudget = errors.New("export exceeds configured payload or span budget")

type jobState uint8

const (
	normalizing jobState = iota
	queued
	batching
	writing
	finished
)

type ticket struct {
	ctx      context.Context
	rows     []Row
	result   chan error
	spans    int
	bytes    int64
	queuedAt time.Time
	state    jobState
	reported bool
}
type batchTimer interface {
	C() <-chan time.Time
	Stop()
}
type batchClock interface {
	Now() time.Time
	Timer(time.Duration) batchTimer
}
type realClock struct{}
type realTimer struct{ *time.Timer }

func (realClock) Now() time.Time                   { return time.Now() }
func (realClock) Timer(d time.Duration) batchTimer { return realTimer{time.NewTimer(d)} }
func (t realTimer) C() <-chan time.Time            { return t.Timer.C }
func (t realTimer) Stop()                          { t.Timer.Stop() }

type Batcher struct {
	store                          Store
	config                         Config
	metrics                        *Metrics
	clock                          batchClock
	mu                             sync.Mutex
	jobs                           chan *ticket
	tickets                        map[*ticket]bool
	reservedQueue, inFlight, spans int
	bytes                          int64
	closing                        bool
	changed                        chan struct{}
	stop                           chan struct{}
	ctx                            context.Context
	cancel                         context.CancelFunc
	workers                        sync.WaitGroup
	done                           chan struct{}
}

func newBatcher(store Store, c Config, m *Metrics, clock batchClock) *Batcher {
	ctx, cancel := context.WithCancel(context.Background())
	e := &Batcher{store: store, config: c, metrics: m, clock: clock, jobs: make(chan *ticket, c.QueueRequests), tickets: map[*ticket]bool{}, changed: make(chan struct{}), stop: make(chan struct{}), ctx: ctx, cancel: cancel, done: make(chan struct{})}
	for i := 0; i < c.Workers; i++ {
		e.workers.Add(1)
		go e.run()
	}
	go func() {
		e.workers.Wait()
		for {
			select {
			case <-e.jobs:
			default:
				m.change(func(s *Snapshot) { s.State = "stopped" })
				close(e.done)
				return
			}
		}
	}()
	return e
}
func (e *Batcher) notify() { close(e.changed); e.changed = make(chan struct{}) }
func (e *Batcher) stateMetrics() {
	e.metrics.change(func(s *Snapshot) {
		s.InFlight = e.inFlight
		s.PendingRequests = len(e.tickets)
		s.ReservedSpans = e.spans
		s.ReservedBytes = e.bytes
		if e.bytes > s.PeakReservedBytes {
			s.PeakReservedBytes = e.bytes
		}
		if e.closing {
			s.State = "draining"
		} else {
			s.State = "running"
		}
	})
}

// Begin bounds handlers before normalization. No new goroutine is created.
func (e *Batcher) Begin(ctx context.Context) (func(), error) {
	timer := time.NewTimer(e.config.AdmissionWait)
	defer timer.Stop()
	for {
		e.mu.Lock()
		if ctx.Err() != nil {
			e.mu.Unlock()
			e.metrics.reject("context")
			return nil, ctx.Err()
		}
		if e.closing {
			e.mu.Unlock()
			e.metrics.reject("shutdown")
			return nil, ErrStopped
		}
		if e.inFlight < e.config.InFlight {
			e.inFlight++
			e.stateMetrics()
			e.mu.Unlock()
			return func() { e.mu.Lock(); e.inFlight--; e.stateMetrics(); e.notify(); e.mu.Unlock() }, nil
		}
		change := e.changed
		e.mu.Unlock()
		select {
		case <-ctx.Done():
			e.metrics.reject("context")
			return nil, ctx.Err()
		case <-timer.C:
			e.metrics.reject("inflight")
			return nil, ErrOverloaded
		case <-change:
		}
	}
}
func (e *Batcher) Reserve(ctx context.Context, spans int, bytes int64) (*ticket, error) {
	return e.reserve(ctx, ctx, spans, bytes)
}
func (e *Batcher) reserve(ctx, workCtx context.Context, spans int, bytes int64) (*ticket, error) {
	if spans > e.config.BufferedSpans || bytes > e.config.BufferedBytes {
		e.metrics.reject("request_budget")
		return nil, ErrBudget
	}
	timer := time.NewTimer(e.config.AdmissionWait)
	defer timer.Stop()
	for {
		e.mu.Lock()
		if ctx.Err() != nil {
			e.mu.Unlock()
			e.metrics.reject("context")
			return nil, ctx.Err()
		}
		if e.closing {
			e.mu.Unlock()
			e.metrics.reject("shutdown")
			return nil, ErrStopped
		}
		if e.reservedQueue < e.config.QueueRequests && len(e.tickets) < e.config.PendingRequests && e.spans+spans <= e.config.BufferedSpans && e.bytes+bytes <= e.config.BufferedBytes {
			t := &ticket{ctx: workCtx, result: make(chan error, 1), spans: spans, bytes: bytes, state: normalizing}
			e.tickets[t] = true
			e.reservedQueue++
			e.spans += spans
			e.bytes += bytes
			e.stateMetrics()
			e.mu.Unlock()
			return t, nil
		}
		change := e.changed
		e.mu.Unlock()
		select {
		case <-ctx.Done():
			e.metrics.reject("context")
			return nil, ctx.Err()
		case <-timer.C:
			e.metrics.reject("capacity")
			return nil, ErrOverloaded
		case <-change:
		}
	}
}
func (e *Batcher) report(t *ticket, err error) {
	if !t.reported {
		t.result <- err
		t.reported = true
	}
}
func (e *Batcher) finish(t *ticket, err error) {
	if t.state == finished {
		return
	}
	if t.state == normalizing || t.state == queued {
		e.reservedQueue--
	}
	if t.state == queued {
		e.metrics.change(func(s *Snapshot) { s.Queue-- })
	}
	t.state = finished
	delete(e.tickets, t)
	e.spans -= t.spans
	e.bytes -= t.bytes
	e.stateMetrics()
	e.notify()
	e.report(t, err)
}
func (e *Batcher) Abort(t *ticket, err error) { e.mu.Lock(); defer e.mu.Unlock(); e.finish(t, err) }
func (e *Batcher) Submit(t *ticket, rows []Row) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if t.state == finished {
		return ErrStopped
	}
	if t.ctx.Err() != nil {
		e.finish(t, t.ctx.Err())
		return t.ctx.Err()
	}
	if e.ctx.Err() != nil {
		e.finish(t, ErrStopped)
		return ErrStopped
	}
	if retainedBytes(rows) > t.bytes {
		e.finish(t, ErrBudget)
		return ErrBudget
	}
	t.rows = rows
	t.state = queued
	t.queuedAt = e.clock.Now()
	e.metrics.change(func(s *Snapshot) {
		s.Queue++
		if s.Queue > s.PeakQueue {
			s.PeakQueue = s.Queue
		}
	})
	// Reserve charged this queue position before normalization; this cannot block.
	e.jobs <- t
	e.notify()
	return nil
}
func (e *Batcher) dequeue(t *ticket) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	if t.state == finished {
		return false
	}
	e.reservedQueue--
	e.metrics.change(func(s *Snapshot) { s.Queue-- })
	t.state = batching
	e.notify()
	return true
}
func (e *Batcher) next() (*ticket, bool) {
	for {
		e.mu.Lock()
		closing := e.closing
		empty := len(e.tickets) == 0
		change := e.changed
		e.mu.Unlock()
		if closing && empty {
			return nil, false
		}
		if closing {
			select {
			case t := <-e.jobs:
				if e.dequeue(t) {
					return t, true
				}
			case <-change:
			case <-e.ctx.Done():
				return nil, false
			}
		} else {
			select {
			case t := <-e.jobs:
				if e.dequeue(t) {
					return t, true
				}
			case <-e.stop:
			case <-e.ctx.Done():
				return nil, false
			}
		}
	}
}
func (e *Batcher) collect(first *ticket) ([]*ticket, *ticket) {
	batch := []*ticket{first}
	size := len(first.rows)
	if size >= e.config.BatchTarget || e.config.BatchWait == 0 {
		return batch, nil
	}
	wait := first.queuedAt.Add(e.config.BatchWait).Sub(e.clock.Now())
	if wait <= 0 {
		return e.ready(batch, size)
	}
	timer := e.clock.Timer(wait)
	defer timer.Stop()
	for {
		if !e.clock.Now().Before(first.queuedAt.Add(e.config.BatchWait)) {
			return e.ready(batch, size)
		}
		select {
		case t := <-e.jobs:
			if !e.dequeue(t) {
				continue
			}
			if size+len(t.rows) > e.config.BatchTarget {
				return batch, t
			}
			batch = append(batch, t)
			size += len(t.rows)
			if size >= e.config.BatchTarget {
				return batch, nil
			}
		case <-timer.C():
			return e.ready(batch, size)
		case <-e.stop:
			return e.ready(batch, size)
		case <-e.ctx.Done():
			return batch, nil
		}
	}
}

// An expired timer prohibits MORE waiting, not combining work already ready.
// Draining only ready jobs avoids one-insert-per-request collapse after backlog.
func (e *Batcher) ready(batch []*ticket, size int) ([]*ticket, *ticket) {
	for size < e.config.BatchTarget {
		select {
		case t := <-e.jobs:
			if !e.dequeue(t) {
				continue
			}
			if size+len(t.rows) > e.config.BatchTarget {
				return batch, t
			}
			batch = append(batch, t)
			size += len(t.rows)
		default:
			return batch, nil
		}
	}
	return batch, nil
}
func (e *Batcher) flush(batch []*ticket) {
	e.mu.Lock()
	active := make([]*ticket, 0, len(batch))
	rows := []Row{}
	var oldest time.Time
	for _, t := range batch {
		if t.state == finished {
			continue
		}
		if t.ctx.Err() != nil {
			e.finish(t, t.ctx.Err())
			continue
		}
		if e.ctx.Err() != nil {
			e.finish(t, ErrStopped)
			continue
		}
		t.state = writing
		active = append(active, t)
		rows = append(rows, t.rows...)
		if oldest.IsZero() || t.queuedAt.Before(oldest) {
			oldest = t.queuedAt
		}
		e.metrics.queueWait.Observe(e.clock.Now().Sub(t.queuedAt).Seconds())
	}
	e.mu.Unlock()
	if len(active) == 0 {
		return
	}
	start := time.Now()
	wait := e.clock.Now().Sub(oldest)
	ctx, cancel := context.WithTimeout(e.ctx, e.config.WriteTimeout)
	err := e.store.Insert(ctx, rows)
	cancel()
	e.metrics.batch(len(rows), len(active), time.Since(start), wait, err)
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.ctx.Err() != nil {
		err = ErrStopped
	}
	for _, t := range active {
		e.finish(t, err)
	}
}
func (e *Batcher) run() {
	defer e.workers.Done()
	var carry *ticket
	for {
		first := carry
		carry = nil
		if first == nil {
			var ok bool
			first, ok = e.next()
			if !ok {
				return
			}
		}
		batch, next := e.collect(first)
		carry = next
		e.flush(batch)
	}
}
func (e *Batcher) StopAdmissions() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if !e.closing {
		e.closing = true
		close(e.stop)
		e.stateMetrics()
		e.notify()
	}
}
func (e *Batcher) Shutdown(ctx context.Context) error {
	e.StopAdmissions()
	select {
	case <-e.done:
		e.cancel()
		e.metrics.change(func(s *Snapshot) { s.State = "stopped" })
		return nil
	case <-ctx.Done():
		e.cancel()
		e.mu.Lock()
		for t := range e.tickets {
			if t.state == writing {
				e.report(t, ErrStopped)
			} else {
				e.finish(t, ErrStopped)
			}
		}
		e.mu.Unlock()
		return ctx.Err()
	}
}
func (e *Batcher) Wait(ctx context.Context) error {
	select {
	case <-e.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
