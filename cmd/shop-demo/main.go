package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
	"traceforge/internal/demo"
	"traceforge/internal/telemetry"
)

func run() error {
	service := flag.String("service", "gateway", "gateway or payment")
	port := flag.Int("port", 0, "loopback HTTP port (default 8080/8081)")
	paymentURL := flag.String("payment-url", "http://localhost:8081", "payment origin")
	metricInterval := flag.Duration("metric-interval", 2*time.Second, "local metric export interval (200ms..1m)")
	flag.Parse()
	if *service != "gateway" && *service != "payment" {
		return fmt.Errorf("invalid service")
	}
	if *port == 0 {
		*port = 8080
		if *service == "payment" {
			*port = 8081
		}
	}
	if *port < 1 || *port > 65535 {
		return fmt.Errorf("invalid port")
	}
	protocol := os.Getenv("OTEL_EXPORTER_OTLP_PROTOCOL")
	if protocol != "" && protocol != "grpc" {
		return fmt.Errorf("only OTLP grpc is supported")
	}
	endpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	if endpoint == "" {
		endpoint = "http://localhost:4317"
	}
	tp, err := telemetry.NewProvider(context.Background(), *service, endpoint)
	if err != nil {
		return err
	}
	mp, err := telemetry.NewMeterProvider(context.Background(), *service, endpoint, *metricInterval)
	if err != nil {
		_ = tp.Shutdown(context.Background())
		return err
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if e := mp.ForceFlush(ctx); e != nil {
			log.Print("metric force flush failed; delivery unconfirmed")
		}
		if e := mp.Shutdown(ctx); e != nil {
			log.Print("metric shutdown failed; delivery unconfirmed")
		}
	}()
	lp, logger, err := telemetry.NewLogProvider(context.Background(), *service, endpoint)
	if err != nil {
		_ = tp.Shutdown(context.Background())
		return err
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if lp.Shutdown(ctx) != nil {
			log.Print("logging shutdown failed; buffered log delivery is not confirmed")
		}
	}()
	logger.InfoContext(context.Background(), "service startup", "service.role", *service)
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := tp.Shutdown(ctx); err != nil {
			log.Printf("tracing shutdown failed: %v", err)
		}
	}()
	var handler http.Handler = demo.Payment(tp, logger)
	if *service == "gateway" {
		handler, err = demo.Gateway(tp, *paymentURL, nil, logger)
		if err != nil {
			return err
		}
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	route := "/charge"
	if *service == "gateway" {
		route = "/checkout"
	}
	handler, err = telemetry.InstrumentInbound(mp, route, handler)
	if err != nil {
		return err
	}
	defer stop()
	server := &http.Server{Addr: fmt.Sprintf("127.0.0.1:%d", *port), Handler: handler, ReadHeaderTimeout: 2 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 6 * time.Second, IdleTimeout: 30 * time.Second}
	log.Printf("%s listening on %s", *service, server.Addr)
	return demo.Serve(ctx, server)
}

func main() {
	if err := run(); err != nil {
		log.Print(err)
		os.Exit(1)
	}
}
