package logs

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

type BatchSample struct {
	Sequence uint64 `json:"sequence"`
	Records  int    `json:"records"`
	Requests int    `json:"requests"`
	InsertNS int64  `json:"insert_ns"`
	WaitNS   int64  `json:"wait_ns"`
	Success  bool   `json:"success"`
}
type Snapshot struct {
	Config              Config        `json:"config"`
	Requests            uint64        `json:"requests"`
	ReceivedRecords     uint64        `json:"received_records"`
	PersistedRecords    uint64        `json:"persisted_records"`
	InvalidRecords      uint64        `json:"invalid_records"`
	PersistenceFailures uint64        `json:"persistence_failures"`
	AdmissionRejections uint64        `json:"admission_rejections"`
	Batches             uint64        `json:"batches"`
	Queue               int           `json:"queue"`
	PeakQueue           int           `json:"peak_queue"`
	InFlight            int           `json:"in_flight"`
	PendingRequests     int           `json:"pending_requests"`
	ReservedRecords     int           `json:"reserved_records"`
	ReservedBytes       int64         `json:"reserved_bytes"`
	PeakReservedBytes   int64         `json:"peak_reserved_bytes"`
	HeapAlloc           uint64        `json:"heap_alloc_bytes"`
	Goroutines          int           `json:"goroutines"`
	ResidentBytes       uint64        `json:"resident_bytes"`
	State               string        `json:"state"`
	Samples             []BatchSample `json:"recent_batches"`
}
type Metrics struct {
	Registry                                                         *prometheus.Registry
	requests, received, persisted, invalid, failures                 prometheus.Counter
	rejections                                                       *prometheus.CounterVec
	batchSize, insertLatency, flushLatency, queueWait, exportLatency prometheus.Histogram
	mu                                                               sync.Mutex
	stats                                                            Snapshot
	ring                                                             [2048]BatchSample
	next, count                                                      int
}

func NewMetrics(c Config) *Metrics {
	m := &Metrics{Registry: prometheus.NewRegistry(), stats: Snapshot{Config: c, State: "running"}}
	counter := func(name, help string) prometheus.Counter {
		x := prometheus.NewCounter(prometheus.CounterOpts{Name: "traceforge_logs_" + name, Help: help})
		m.Registry.MustRegister(x)
		return x
	}
	m.requests = counter("otlp_requests_total", "Export calls received, including failed calls.")
	m.received = counter("records_received_total", "Span messages received within valid envelopes.")
	m.persisted = counter("records_persisted_total", "Physical span rows in writes reported successful; canceled callers may be included.")
	m.invalid = counter("invalid_records_total", "Records rejected by validation.")
	m.failures = counter("persistence_failures_total", "Failed batch insert attempts, including ambiguous outcomes.")
	m.rejections = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "traceforge_logs_admission_rejections_total", Help: "Admission refusals by fixed reason."}, []string{"reason"})
	m.Registry.MustRegister(m.rejections)
	for _, reason := range []string{"shutdown", "request_budget", "inflight", "capacity", "context"} {
		m.rejections.WithLabelValues(reason).Add(0)
	}
	hist := func(name, help string, buckets []float64) prometheus.Histogram {
		x := prometheus.NewHistogram(prometheus.HistogramOpts{Name: "traceforge_logs_" + name, Help: help, Buckets: buckets})
		m.Registry.MustRegister(x)
		return x
	}
	m.batchSize = hist("batch_records", "Physical records per attempted insert.", []float64{1, 10, 20, 50, 100, 200, 500, 1000, 2000, 5000, 10000})
	b := []float64{.001, .005, .01, .02, .05, .1, .25, .5, 1, 2, 5, 10}
	m.insertLatency = hist("insert_seconds", "Time spent in ClickHouse Insert.", b)
	m.flushLatency = hist("batch_flush_seconds", "Oldest queued job to completed insert.", b)
	m.queueWait = hist("queue_wait_seconds", "Per-job time before a write attempt or cancellation skip.", b)
	m.exportLatency = hist("export_seconds", "Export handling duration, all outcomes.", b)
	for name, read := range map[string]func(Snapshot) float64{
		"queue_requests": func(s Snapshot) float64 { return float64(s.Queue) }, "queue_capacity_requests": func(s Snapshot) float64 { return float64(s.Config.QueueRequests) }, "in_flight_requests": func(s Snapshot) float64 { return float64(s.InFlight) }, "pending_requests": func(s Snapshot) float64 { return float64(s.PendingRequests) }, "buffered_reserved_records": func(s Snapshot) float64 { return float64(s.ReservedRecords) }, "buffered_reserved_bytes": func(s Snapshot) float64 { return float64(s.ReservedBytes) }, "buffered_capacity_bytes": func(s Snapshot) float64 { return float64(s.Config.BufferedBytes) }, "buffered_capacity_records": func(s Snapshot) float64 { return float64(s.Config.BufferedRecords) },
	} {
		fn := read
		m.Registry.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: "traceforge_logs_" + name, Help: "Bounded ingestion state."}, func() float64 { m.mu.Lock(); defer m.mu.Unlock(); return fn(m.stats) }))
	}
	m.Registry.MustRegister(collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	return m
}
func (m *Metrics) change(fn func(*Snapshot)) { m.mu.Lock(); defer m.mu.Unlock(); fn(&m.stats) }
func (m *Metrics) reject(reason string) {
	m.rejections.WithLabelValues(reason).Inc()
	m.change(func(s *Snapshot) { s.AdmissionRejections++ })
}
func (m *Metrics) batch(records, requests int, insert, wait time.Duration, err error) {
	m.batchSize.Observe(float64(records))
	m.insertLatency.Observe(insert.Seconds())
	m.flushLatency.Observe((wait + insert).Seconds())
	if err == nil {
		m.persisted.Add(float64(records))
	} else {
		m.failures.Inc()
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stats.Batches++
	if err == nil {
		m.stats.PersistedRecords += uint64(records)
	} else {
		m.stats.PersistenceFailures++
	}
	m.ring[m.next] = BatchSample{m.stats.Batches, records, requests, insert.Nanoseconds(), wait.Nanoseconds(), err == nil}
	m.next = (m.next + 1) % len(m.ring)
	if m.count < len(m.ring) {
		m.count++
	}
}
func (m *Metrics) Snapshot() Snapshot {
	m.mu.Lock()
	s := m.stats
	s.Samples = make([]BatchSample, 0, m.count)
	for i := 0; i < m.count; i++ {
		s.Samples = append(s.Samples, m.ring[(m.next-m.count+i+len(m.ring))%len(m.ring)])
	}
	m.mu.Unlock()
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	s.HeapAlloc = mem.HeapAlloc
	s.Goroutines = runtime.NumGoroutine()
	if raw, err := os.ReadFile("/proc/self/statm"); err == nil {
		fields := strings.Fields(string(raw))
		if len(fields) > 1 {
			pages, _ := strconv.ParseUint(fields[1], 10, 64)
			s.ResidentBytes = pages * uint64(os.Getpagesize())
		}
	}
	return s
}
