package main

import (
	"context"
	"flag"
	"fmt"
	collectorlogs "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	collector "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"golang.org/x/net/netutil"
	"google.golang.org/grpc"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
	"traceforge/internal/ingest"
	"traceforge/internal/logs"
	"traceforge/internal/queryapi"
	"traceforge/internal/storage"
)

func run() error {
	// Container invocation explicitly overrides loopback defaults.
	addr := flag.String("grpc-address", "127.0.0.1:4319", "OTLP/gRPC listen address")
	healthAddr := flag.String("health-address", "127.0.0.1:8082", "HTTP health address")
	migrate := flag.Bool("migrate-only", false, "apply repeatable migration and exit")
	probe := flag.Bool("healthcheck", false, "probe local readyz and exit (container health check)")
	maxRows := flag.Int("max-trace-rows", 1000, "maximum physical rows in a trace detail (1..2000)")
	maxPage := flag.Int("max-page-size", 100, "maximum search page size (1..200)")
	queryTimeout := flag.Duration("query-timeout", 2*time.Second, "HTTP query timeout (up to 4s)")
	ingestConfig := ingest.DefaultConfig()
	logConfig := logs.DefaultConfig()
	flag.IntVar(&logConfig.QueueRequests, "log-queue-requests", logConfig.QueueRequests, "independent log queued/normalizing slots")
	flag.IntVar(&logConfig.InFlight, "log-max-inflight", logConfig.InFlight, "active log Export handlers")
	flag.IntVar(&logConfig.BufferedRecords, "log-buffer-records", logConfig.BufferedRecords, "reserved log records")
	flag.Int64Var(&logConfig.BufferedBytes, "log-buffer-bytes", logConfig.BufferedBytes, "conservative reserved log payload bytes")
	flag.IntVar(&logConfig.BatchTarget, "log-batch-target", logConfig.BatchTarget, "target records per log write")
	flag.DurationVar(&logConfig.BatchWait, "log-batch-wait", logConfig.BatchWait, "log assembly wait")
	mode := flag.String("ingestion-mode", "batched", "batched or direct (one insert per request control)")
	flag.IntVar(&ingestConfig.BatchTarget, "batch-target", ingestConfig.BatchTarget, "target spans per insert; whole requests stay intact")
	flag.DurationVar(&ingestConfig.BatchWait, "batch-wait", ingestConfig.BatchWait, "oldest queued job's maximum batching wait")
	flag.IntVar(&ingestConfig.QueueRequests, "queue-requests", ingestConfig.QueueRequests, "queued/normalizing request slots")
	flag.IntVar(&ingestConfig.InFlight, "max-inflight", ingestConfig.InFlight, "active Export handlers")
	flag.IntVar(&ingestConfig.PendingRequests, "pending-requests", ingestConfig.PendingRequests, "all owned normalizing/queued/batched/writing jobs")
	flag.IntVar(&ingestConfig.BufferedSpans, "buffer-spans", ingestConfig.BufferedSpans, "reserved spans across all owned jobs")
	flag.Int64Var(&ingestConfig.BufferedBytes, "buffer-bytes", ingestConfig.BufferedBytes, "conservative payload reservation budget in bytes")
	flag.IntVar(&ingestConfig.Workers, "workers", ingestConfig.Workers, "bounded persistence workers (1..4)")
	flag.DurationVar(&ingestConfig.AdmissionWait, "admission-wait", ingestConfig.AdmissionWait, "maximum combined admission wait")
	flag.DurationVar(&ingestConfig.WriteTimeout, "write-timeout", ingestConfig.WriteTimeout, "shared batch insertion deadline")
	drainTimeout := flag.Duration("drain-timeout", 10*time.Second, "graceful ingestion drain deadline")
	flag.Parse()
	if *probe {
		client := http.Client{Timeout: 2 * time.Second}
		res, err := client.Get("http://127.0.0.1:8082/readyz")
		if err != nil {
			return fmt.Errorf("readiness probe failed")
		}
		defer res.Body.Close()
		if res.StatusCode != 200 {
			return fmt.Errorf("database not ready")
		}
		return nil
	}
	if *mode == "direct" {
		ingestConfig.BatchTarget = 1
		ingestConfig.BatchWait = 0
	} else if *mode != "batched" {
		return fmt.Errorf("ingestion-mode must be direct or batched")
	}
	if err := ingestConfig.Validate(); err != nil {
		return err
	}
	if err := logConfig.Validate(); err != nil {
		return err
	}
	if *drainTimeout <= 0 || *drainTimeout > 15*time.Second {
		return fmt.Errorf("drain-timeout out of range")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	store, err := storage.Open(ctx)
	if err != nil {
		cancel()
		return err
	}
	defer store.Close()
	err = store.Migrate(ctx)
	cancel()
	if err != nil {
		return err
	}
	if *migrate {
		return nil
	}
	queryConfig := queryapi.DefaultConfig()
	queryConfig.MaxRows = *maxRows
	queryConfig.MaxPageSize = *maxPage
	queryConfig.Timeout = *queryTimeout
	queries, err := queryapi.NewHandler(store, queryConfig)
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp", *addr)
	if err != nil {
		return fmt.Errorf("cannot listen on OTLP address")
	}
	defer listener.Close()
	listener = netutil.LimitListener(listener, 8)
	healthListener, err := net.Listen("tcp", *healthAddr)
	if err != nil {
		return fmt.Errorf("cannot listen on health address")
	}
	defer healthListener.Close()
	server, err := ingest.NewConfiguredServer(store, ingestConfig)
	if err != nil {
		return err
	}
	logServer, err := logs.NewConfiguredServer(store, logConfig)
	if err != nil {
		return err
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = logServer.Shutdown(ctx)
	}()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
	}()
	grpcServer := grpc.NewServer(grpc.MaxRecvMsgSize(ingest.MaxMessageBytes), grpc.MaxConcurrentStreams(8), grpc.ConnectionTimeout(5*time.Second))
	collector.RegisterTraceServiceServer(grpcServer, server)
	collectorlogs.RegisterLogsServiceServer(grpcServer, logServer)
	mux := http.NewServeMux()
	logQueries := logs.NewQueryHandler(store)
	mux.Handle("/api/v1/logs", logQueries)
	mux.Handle("/api/v1/traces/{trace_id}/logs", logQueries)
	mux.Handle("/debug/log-ingestion", logServer.HealthHandler())
	mux.Handle("/metrics/logs", logServer.HealthHandler())
	mux.Handle("/api/v1/", queries)
	mux.Handle("/", server.HealthHandler())
	httpServer := &http.Server{Handler: mux, ReadHeaderTimeout: 2 * time.Second, ReadTimeout: 3 * time.Second, WriteTimeout: 6 * time.Second, IdleTimeout: 15 * time.Second}
	errCh := make(chan error, 2)
	go func() { errCh <- grpcServer.Serve(listener) }()
	go func() { errCh <- httpServer.Serve(healthListener) }()
	signals, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	log.Printf("OTLP receiver %s; health/query/metrics %s; mode=%s target=%d workers=%d; acknowledgments wait for ClickHouse", *addr, *healthAddr, *mode, ingestConfig.BatchTarget, ingestConfig.Workers)
	var serveErr error
	select {
	case <-signals.Done():
	case err := <-errCh:
		if err != nil {
			log.Print("listener stopped unexpectedly")
			serveErr = fmt.Errorf("receiver listener stopped unexpectedly")
		}
	}
	drainCtx, drainCancel := context.WithTimeout(context.Background(), *drainTimeout)
	drained := make(chan error, 2)
	go func() { drained <- server.Shutdown(drainCtx) }()
	go func() { drained <- logServer.Shutdown(drainCtx) }()
	for range 2 {
		if err := <-drained; err != nil {
			log.Print("signal ingestion drain timed out; unresolved outcomes are failed/unknown")
		}
	}
	drainCancel()
	// Closing the pool also interrupts a forced drain's remaining database work.
	_ = store.Close()
	waitCtx, waitCancel := context.WithTimeout(context.Background(), 2*time.Second)
	if err := server.Wait(waitCtx); err != nil {
		log.Print("workers did not exit before forced shutdown deadline")
	}
	if err := logServer.Wait(waitCtx); err != nil {
		log.Print("log workers did not exit before forced shutdown deadline")
	}
	waitCancel()
	done := make(chan struct{})
	go func() { grpcServer.GracefulStop(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		grpcServer.Stop()
		<-done
	}
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer shutdownCancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		_ = httpServer.Close()
	}
	return serveErr
}
func main() {
	if err := run(); err != nil {
		log.Print(err)
		os.Exit(1)
	}
}
