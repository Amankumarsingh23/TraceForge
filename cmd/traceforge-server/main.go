package main

import (
	"context"
	"flag"
	"fmt"
	collector "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"google.golang.org/grpc"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
	"traceforge/internal/ingest"
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
	healthListener, err := net.Listen("tcp", *healthAddr)
	if err != nil {
		return fmt.Errorf("cannot listen on health address")
	}
	defer healthListener.Close()
	server := ingest.NewServer(store)
	grpcServer := grpc.NewServer(grpc.MaxRecvMsgSize(ingest.MaxMessageBytes), grpc.MaxConcurrentStreams(8), grpc.ConnectionTimeout(5*time.Second))
	collector.RegisterTraceServiceServer(grpcServer, server)
	mux := http.NewServeMux()
	mux.Handle("/api/v1/", queries)
	mux.Handle("/", server.HealthHandler())
	httpServer := &http.Server{Handler: mux, ReadHeaderTimeout: 2 * time.Second, ReadTimeout: 3 * time.Second, WriteTimeout: 6 * time.Second, IdleTimeout: 15 * time.Second}
	errCh := make(chan error, 2)
	go func() { errCh <- grpcServer.Serve(listener) }()
	go func() { errCh <- httpServer.Serve(healthListener) }()
	signals, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	log.Printf("OTLP receiver listening on %s; health on %s; synchronous ClickHouse persistence", *addr, *healthAddr)
	var serveErr error
	select {
	case <-signals.Done():
	case err := <-errCh:
		if err != nil {
			log.Print("listener stopped unexpectedly")
			serveErr = fmt.Errorf("receiver listener stopped unexpectedly")
		}
	}
	done := make(chan struct{})
	go func() { grpcServer.GracefulStop(); close(done) }()
	select {
	case <-done:
	case <-time.After(6 * time.Second):
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
