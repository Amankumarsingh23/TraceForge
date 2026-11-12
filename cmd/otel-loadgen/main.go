package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	collector "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"sync"
	"syscall"
	"time"
	"traceforge/internal/ingest"
	"traceforge/internal/loadgen"
	"traceforge/internal/storage"
)

type report struct {
	Load                     loadgen.Report `json:"load"`
	GoVersion                string         `json:"go_version"`
	OS                       string         `json:"os"`
	CPUs                     int            `json:"loadgen_cpus"`
	Connections              int            `json:"connections"`
	Started                  time.Time      `json:"started"`
	Before, After            ingest.Snapshot
	PeakHeap                 uint64  `json:"sampled_peak_heap_bytes"`
	PeakResident             uint64  `json:"sampled_peak_resident_bytes"`
	PeakGoroutines           int     `json:"sampled_peak_goroutines"`
	PeakQueue                int     `json:"sampled_peak_queue"`
	StorageVerified          bool    `json:"storage_verified"`
	StoredRows               uint64  `json:"stored_rows"`
	UniqueSpans              uint64  `json:"unique_stored_spans"`
	StoredRowsPerSecond      float64 `json:"confirmed_rows_per_second"`
	Duplicates               uint64  `json:"physical_duplicates"`
	InsertP50MS              float64 `json:"insert_p50_ms"`
	InsertP95MS              float64 `json:"insert_p95_ms"`
	MeanBatchSize            float64 `json:"mean_batch_size"`
	BatchSamples             int     `json:"batch_samples"`
	MetricSamplesComplete    bool    `json:"metric_samples_complete"`
	DatabaseParts            uint64  `json:"active_database_parts"`
	PartsAvailable           bool    `json:"parts_count_available"`
	AccountingSettled        bool    `json:"accounting_settled"`
	StorageWindowSeconds     float64 `json:"storage_window_seconds"`
	StorageVerificationError string  `json:"storage_verification_error,omitempty"`
	DiagnosticsAvailable     bool    `json:"final_diagnostics_available"`
}

func snapshot(ctx context.Context, endpoint string) (ingest.Snapshot, error) {
	var s ingest.Snapshot
	req, e := http.NewRequestWithContext(ctx, "GET", endpoint+"/debug/ingestion", nil)
	if e != nil {
		return s, e
	}
	client := http.Client{Timeout: 2 * time.Second}
	res, e := client.Do(req)
	if e != nil {
		return s, fmt.Errorf("cannot read ingestion diagnostics")
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return s, fmt.Errorf("diagnostics returned HTTP %d", res.StatusCode)
	}
	e = json.NewDecoder(res.Body).Decode(&s)
	return s, e
}
func run() error {
	endpoint := flag.String("endpoint", "localhost:4319", "direct OTLP/gRPC endpoint")
	httpEndpoint := flag.String("http-endpoint", "http://localhost:8082", "receiver metrics/diagnostics origin")
	requests := flag.Int("requests", 300, "exact request count unless --spans or --duration is used")
	connections := flag.Int("connections", 4, "shared gRPC connections (1..4)")
	duration := flag.Duration("duration", 0, "derive offered span count from rate and duration (up to 30s)")
	verify := flag.Bool("verify-storage", true, "require authenticated ClickHouse row accounting")
	output := flag.String("output", "artifacts/load-report.json", "JSON report output")
	c := loadgen.Config{SpansPerRequest: 50, Concurrency: 32, Seed: 42, Services: 3, RPCTimeout: 6 * time.Second, BaseTime: time.Now().UTC()}
	flag.IntVar(&c.Spans, "spans", 0, "total spans (overrides request count)")
	flag.IntVar(&c.SpansPerRequest, "spans-per-request", c.SpansPerRequest, "1..1000 spans per export")
	flag.IntVar(&c.Concurrency, "concurrency", c.Concurrency, "1..64 concurrent exporters")
	flag.Float64Var(&c.Rate, "rate", 0, "offered spans/sec, zero for unpaced")
	flag.Uint64Var(&c.Seed, "seed", c.Seed, "deterministic payload seed")
	flag.IntVar(&c.Services, "services", c.Services, "synthetic service count")
	flag.StringVar(&c.RunID, "run-id", "", "16 hex chars; generated uniquely by default")
	flag.DurationVar(&c.RPCTimeout, "rpc-timeout", c.RPCTimeout, "per-request timeout")
	base := flag.String("base-time", "", "optional deterministic RFC3339Nano base time")
	flag.Parse()
	if *requests < 1 || *requests > 10000 || *connections < 1 || *connections > 4 || *duration < 0 || *duration > 30*time.Second {
		return fmt.Errorf("load limits out of range")
	}
	if c.Spans == 0 {
		c.Spans = *requests * c.SpansPerRequest
	}
	if *duration > 0 {
		if c.Rate <= 0 {
			return fmt.Errorf("duration requires a positive rate")
		}
		c.Spans = int(c.Rate * duration.Seconds())
	}
	if *base != "" {
		t, e := time.Parse(time.RFC3339Nano, *base)
		if e != nil {
			return fmt.Errorf("invalid base time")
		}
		c.BaseTime = t.UTC()
	}
	if c.RunID == "" {
		b := make([]byte, 8)
		if _, e := rand.Read(b); e != nil {
			return e
		}
		c.RunID = hex.EncodeToString(b)
	}
	if e := c.Validate(); e != nil {
		return e
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	r := report{GoVersion: runtime.Version(), OS: runtime.GOOS, CPUs: runtime.NumCPU(), Connections: *connections, Started: time.Now().UTC()}
	before, e := snapshot(ctx, *httpEndpoint)
	if e != nil {
		return e
	}
	r.Before = before
	if before.PendingRequests != 0 {
		return fmt.Errorf("receiver has pending work; wait for drain before a measured run")
	}
	clients := []loadgen.Client{}
	for i := 0; i < *connections; i++ {
		conn, e := grpc.NewClient(*endpoint, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if e != nil {
			return fmt.Errorf("create OTLP connection failed")
		}
		defer conn.Close()
		clients = append(clients, collector.NewTraceServiceClient(conn))
	}
	var mu sync.Mutex
	sampleDone := make(chan struct{})
	sampleCtx, sampleCancel := context.WithCancel(ctx)
	go func() {
		defer close(sampleDone)
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-sampleCtx.Done():
				return
			case <-ticker.C:
				s, e := snapshot(sampleCtx, *httpEndpoint)
				if e == nil {
					mu.Lock()
					if s.HeapAlloc > r.PeakHeap {
						r.PeakHeap = s.HeapAlloc
					}
					if s.ResidentBytes > r.PeakResident {
						r.PeakResident = s.ResidentBytes
					}
					if s.Goroutines > r.PeakGoroutines {
						r.PeakGoroutines = s.Goroutines
					}
					if s.Queue > r.PeakQueue {
						r.PeakQueue = s.Queue
					}
					mu.Unlock()
				}
			}
		}
	}()
	measureStart := time.Now()
	runCtx, runCancel := context.WithTimeout(ctx, 45*time.Second)
	r.Load, e = loadgen.Run(runCtx, c, clients)
	runCancel()
	sampleCancel()
	<-sampleDone
	if e != nil {
		return e
	}
	// Wait for canceled/unknown requests to settle before independent accounting.
	deadline := time.Now().Add(8 * time.Second)
	for {
		r.After, e = snapshot(context.Background(), *httpEndpoint)
		if e != nil {
			break // Preserve client outcomes even if the receiver stopped.
		}
		r.DiagnosticsAvailable = true
		if r.After.PendingRequests == 0 || time.Now().After(deadline) {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if r.After.HeapAlloc > r.PeakHeap {
		r.PeakHeap = r.After.HeapAlloc
	}
	if r.After.ResidentBytes > r.PeakResident {
		r.PeakResident = r.After.ResidentBytes
	}
	r.AccountingSettled = r.DiagnosticsAvailable && r.After.PendingRequests == 0
	r.StorageWindowSeconds = time.Since(measureStart).Seconds()
	latencies := []int64{}
	sum := 0
	for _, s := range r.After.Samples {
		if s.Sequence > r.Before.Batches {
			latencies = append(latencies, s.InsertNS)
			sum += s.Spans
		}
	}
	r.BatchSamples = len(latencies)
	r.MetricSamplesComplete = r.DiagnosticsAvailable && r.After.Batches >= r.Before.Batches && uint64(r.BatchSamples) == r.After.Batches-r.Before.Batches
	r.InsertP50MS = loadgen.Percentile(latencies, .5)
	r.InsertP95MS = loadgen.Percentile(latencies, .95)
	if r.BatchSamples > 0 {
		r.MeanBatchSize = float64(sum) / float64(r.BatchSamples)
	}
	if *verify {
		check, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		s, e := storage.Open(check)
		if e != nil {
			r.StorageVerificationError = "ClickHouse unavailable for independent accounting"
		} else {
			defer s.Close()
			if e := s.Conn.QueryRow(check, "SELECT count(),uniqExact(tuple(trace_id,span_id)) FROM traceforge.spans WHERE startsWith(trace_id, ?)", c.RunID).Scan(&r.StoredRows, &r.UniqueSpans); e != nil {
				r.StorageVerificationError = "independent storage query failed"
			} else {
				r.PartsAvailable = s.Conn.QueryRow(check, "SELECT count() FROM system.parts WHERE database='traceforge' AND table='spans' AND active").Scan(&r.DatabaseParts) == nil
				r.StorageVerified = true
				r.Duplicates = r.StoredRows - r.UniqueSpans
				r.StoredRowsPerSecond = float64(r.StoredRows) / r.StorageWindowSeconds
			}
		}
	}
	if e := os.MkdirAll(filepath.Dir(*output), 0755); e != nil {
		return e
	}
	b, e := json.MarshalIndent(r, "", "  ")
	if e != nil {
		return e
	}
	if e := os.WriteFile(*output, b, 0644); e != nil {
		return e
	}
	stored := "unverified"
	unique := "unverified"
	if r.StorageVerified {
		stored = fmt.Sprint(r.StoredRows)
		unique = fmt.Sprint(r.UniqueSpans)
	}
	fmt.Printf("run=%s offered=%d ack=%d stored=%s unique=%s elapsed=%.3fs ack/s=%.1f rows/s=%.1f p95=%.2fms mean_batch=%.1f report=%s\n", c.RunID, r.Load.GeneratedSpans, r.Load.AcknowledgedSpans, stored, unique, r.Load.ElapsedSeconds, r.Load.AcknowledgedSpansPerSecond, r.StoredRowsPerSecond, r.Load.P95MS, r.MeanBatchSize, *output)
	if *verify && !r.StorageVerified {
		return fmt.Errorf("report saved; independent storage verification unavailable")
	}
	return nil
}
func main() {
	if e := run(); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
