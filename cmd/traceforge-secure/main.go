// Local authenticated gateway. Backends must be private fixed tenant bindings.
package main

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"flag"
	"fmt"
	collectorlog "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	collectormetric "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	collectortrace "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"golang.org/x/net/netutil"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"math/big"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
	"traceforge/internal/reliability"
	"traceforge/internal/security"
	"traceforge/internal/storage"
)

func run() error {
	mode := flag.String("mode", "serve", "serve/bootstrap/revoke")
	file := flag.String("file", "artifacts/session14/credentials.json", "private bootstrap output")
	owner := flag.String("tenant", "alpha", "trusted local administration owner")
	id := flag.String("credential-id", "", "revoke only this owner's credential")
	httpAddr := flag.String("http", "127.0.0.1:8444", "TLS HTTP")
	grpcAddr := flag.String("grpc", "127.0.0.1:4334", "TLS OTLP")
	certDir := flag.String("cert-dir", "artifacts/session14/certs", "certificate directory")
	flag.Parse()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	registry, e := security.Open(ctx)
	if e != nil {
		return e
	}
	defer registry.Close()
	if *mode == "bootstrap" {
		if _, err := os.Stat(*file); err == nil {
			return fmt.Errorf("bootstrap output already exists; preserve credentials and explicitly rotate before generating replacements")
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("bootstrap output cannot be checked")
		}
		if e = registry.Migrate(ctx); e != nil {
			return fmt.Errorf("security migration unavailable")
		}
		keys := map[string]map[string]string{}
		ids := map[string]map[string]string{}
		for _, tenant := range []string{"legacy", "alpha", "beta"} {
			keys[tenant] = map[string]string{}
			ids[tenant] = map[string]string{}
			for _, scope := range []string{"read", "ingest", "manage"} {
				key, cid, err := registry.Issue(ctx, tenant, scope)
				if err != nil {
					return err
				}
				keys[tenant][scope] = key
				ids[tenant][scope] = cid
			}
		}
		for _, t := range []string{"alpha", "beta"} {
			_ = os.Setenv("TRACEFORGE_TRUSTED_TENANT", "legacy")
			_ = os.Setenv("CLICKHOUSE_DATABASE", "")
			ch, err := storage.Open(ctx)
			if err != nil {
				return err
			}
			err = ch.Conn.Exec(ctx, "CREATE DATABASE IF NOT EXISTS tf_tenant_"+t)
			_ = ch.Close()
			if err != nil {
				return fmt.Errorf("tenant database bootstrap failed")
			}
			_ = os.Setenv("TRACEFORGE_TRUSTED_TENANT", t)
			ch, err = storage.Open(ctx)
			if err != nil {
				return err
			}
			err = ch.Migrate(ctx)
			_ = ch.Close()
			if err != nil {
				return err
			}
			pg, err := reliability.Open(ctx)
			if err != nil {
				return err
			}
			err = pg.Migrate(ctx)
			pg.Close()
			if err != nil {
				return err
			}
		}
		if e = os.MkdirAll(filepath.Dir(*file), 0700); e != nil {
			return e
		}
		secret := make([]byte, 32)
		if _, e = rand.Read(secret); e != nil {
			return e
		}
		data, _ := json.MarshalIndent(map[string]any{"keys": keys, "ids": ids, "session_secret": fmt.Sprintf("%x", secret)}, "", "  ")
		if e = os.WriteFile(*file, data, 0600); e != nil {
			return e
		}
		if e = certificate(*certDir); e != nil {
			return e
		}
		fmt.Println("Bootstrapped private credentials/certificates; keys were not printed")
		return nil
	}
	if *mode == "revoke" {
		return registry.Revoke(ctx, *id, *owner)
	}
	if *mode != "serve" {
		return fmt.Errorf("unsupported mode")
	}
	backends := map[string]security.Backend{}
	for _, t := range []string{"legacy", "alpha", "beta"} {
		h := os.Getenv("TRACEFORGE_BACKEND_" + map[string]string{"alpha": "ALPHA", "beta": "BETA", "legacy": "LEGACY"}[t])
		if h == "" {
			if t == "legacy" {
				continue
			}
			return fmt.Errorf("private tenant backend not configured")
		}
		conn, err := grpc.NewClient(h+":4319", grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			return err
		}
		defer conn.Close()
		backends[t] = security.Backend{HTTP: "http://" + h + ":8082", Conn: conn}
	}
	gateway := security.NewGateway(registry, backends)
	tlsCred, e := credentials.NewServerTLSFromFile(filepath.Join(*certDir, "server.crt"), filepath.Join(*certDir, "server.key"))
	if e != nil {
		return fmt.Errorf("TLS files unavailable")
	}
	server := grpc.NewServer(grpc.Creds(tlsCred), grpc.MaxRecvMsgSize(1<<20), grpc.MaxConcurrentStreams(8))
	collectortrace.RegisterTraceServiceServer(server, security.TraceGateway{G: gateway})
	collectorlog.RegisterLogsServiceServer(server, security.LogGateway{G: gateway})
	collectormetric.RegisterMetricsServiceServer(server, security.MetricGateway{G: gateway})
	listener, e := net.Listen("tcp", *grpcAddr)
	if e != nil {
		return e
	}
	defer listener.Close()
	httpListener, e := net.Listen("tcp", *httpAddr)
	if e != nil {
		return e
	}
	defer httpListener.Close()
	listener = netutil.LimitListener(listener, 16)
	httpListener = netutil.LimitListener(httpListener, 24)
	httpServer := &http.Server{Handler: gateway, ReadHeaderTimeout: 2 * time.Second, ReadTimeout: 4 * time.Second, WriteTimeout: 7 * time.Second, IdleTimeout: 10 * time.Second}
	errs := make(chan error, 2)
	go func() { errs <- server.Serve(listener) }()
	go func() {
		errs <- httpServer.ServeTLS(httpListener, filepath.Join(*certDir, "server.crt"), filepath.Join(*certDir, "server.key"))
	}()
	sig, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	select {
	case <-sig.Done():
	case <-errs:
		return fmt.Errorf("secure listener stopped")
	}
	shutdown, c := context.WithTimeout(context.Background(), 5*time.Second)
	defer c()
	_ = httpServer.Shutdown(shutdown)
	server.Stop()
	return nil
}
func certificate(dir string) error {
	if e := os.MkdirAll(dir, 0700); e != nil {
		return e
	}
	key, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		return e
	}
	serial, e := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if e != nil {
		return e
	}
	cert := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: "TraceForge local secure demo"}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(7 * 24 * time.Hour), DNSNames: []string{"localhost", "secure-gateway", "collector-alpha", "collector-beta"}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment | x509.KeyUsageCertSign, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, IsCA: true, BasicConstraintsValid: true}
	der, e := x509.CreateCertificate(rand.Reader, cert, cert, &key.PublicKey, key)
	if e != nil {
		return e
	}
	if e = os.WriteFile(filepath.Join(dir, "server.crt"), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0600); e != nil {
		return e
	}
	return os.WriteFile(filepath.Join(dir, "server.key"), pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}), 0600)
}
func main() {
	if e := run(); e != nil {
		fmt.Fprintln(os.Stderr, "secure operation failed:", e)
		os.Exit(1)
	}
}
