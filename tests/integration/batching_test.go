//go:build integration

package integration

import (
	"context"
	"encoding/json"
	collector "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
	"traceforge/internal/ingest"
)

func TestLiveGracefulBatchDrain(t *testing.T) {
	if os.Getenv("TRACEFORGE_TEST_BENCHMARK") != "1" {
		t.Skip("dedicated benchmark receiver required")
	}
	s := database(t)
	conn, e := grpc.NewClient("localhost:4320", grpc.WithTransportCredentials(insecure.NewCredentials()))
	if e != nil {
		t.Fatal(e)
	}
	defer conn.Close()
	client := collector.NewTraceServiceClient(conn)
	read := func() (ingest.Snapshot, error) {
		var snapshot ingest.Snapshot
		c := http.Client{Timeout: time.Second}
		r, e := c.Get("http://localhost:8083/debug/ingestion")
		if e != nil {
			return snapshot, e
		}
		defer r.Body.Close()
		e = json.NewDecoder(r.Body).Decode(&snapshot)
		return snapshot, e
	}
	initial, e := read()
	if e != nil || initial.Config.BatchWait != time.Second || initial.PendingRequests != 0 {
		t.Fatal("use fresh isolated receiver with --batch-wait=1s", e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	done := make(chan error, 4)
	ids := []string{}
	for i := 0; i < 4; i++ {
		req, tid := request(t)
		ids = append(ids, tid)
		go func(r *collector.ExportTraceServiceRequest) { _, err := client.Export(ctx, r); done <- err }(req)
	}
	deadline := time.Now().Add(800 * time.Millisecond)
	pending := 0
	for time.Now().Before(deadline) {
		snap, err := read()
		if err != nil {
			t.Fatal(err)
		}
		pending = snap.PendingRequests
		if pending == 4 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if pending != 4 {
		t.Fatal("could not observe queued work before shutdown", pending)
	}
	command := func(args ...string) *exec.Cmd {
		c := exec.CommandContext(ctx, "docker", append([]string{"compose", "-f", "compose.yaml", "-f", "compose.benchmark.yaml"}, args...)...)
		c.Dir = filepath.Join("..", "..")
		return c
	}
	defer func() {
		restoreCtx, restoreCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer restoreCancel()
		restore := exec.CommandContext(restoreCtx, "docker", "compose", "-f", "compose.yaml", "-f", "compose.benchmark.yaml", "start", "traceforge-benchmark")
		restore.Dir = filepath.Join("..", "..")
		if out, err := restore.CombinedOutput(); err != nil {
			t.Errorf("restore dedicated receiver: %v %s", err, out)
		}
	}()
	if out, err := command("stop", "-t", "15", "traceforge-benchmark").CombinedOutput(); err != nil {
		t.Fatalf("graceful stop %v %s", err, out)
	}
	for i := 0; i < 4; i++ {
		if err := <-done; err != nil {
			t.Fatal("queued request not safely acknowledged", err)
		}
	}
	for _, id := range ids {
		if n := count(t, s, id); n != 1 {
			t.Fatal("successful drain acknowledgment lacked a stored row", id, n)
		}
	}
	t.Logf("observed 4 pending jobs before SIGTERM; all 4 exports succeeded and each independently stored 1 row; trace_ids=%v", ids)
}

func TestLiveForcedKillPendingBatch(t *testing.T) {
	if os.Getenv("TRACEFORGE_TEST_BENCHMARK") != "1" {
		t.Skip("isolated benchmark receiver required")
	}
	s := database(t)
	conn, e := grpc.NewClient("localhost:4320", grpc.WithTransportCredentials(insecure.NewCredentials()))
	if e != nil {
		t.Fatal(e)
	}
	defer conn.Close()
	client := collector.NewTraceServiceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	done := make(chan error, 4)
	ids := []string{}
	for i := 0; i < 4; i++ {
		req, id := request(t)
		ids = append(ids, id)
		go func(r *collector.ExportTraceServiceRequest) { _, err := client.Export(ctx, r); done <- err }(req)
	}
	deadline := time.Now().Add(800 * time.Millisecond)
	pending := 0
	for time.Now().Before(deadline) {
		c := http.Client{Timeout: time.Second}
		r, err := c.Get("http://localhost:8083/debug/ingestion")
		if err != nil {
			t.Fatal(err)
		}
		var snap ingest.Snapshot
		err = json.NewDecoder(r.Body).Decode(&snap)
		r.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		if snap.Config.BatchWait != time.Second {
			t.Fatal("use isolated 1s wait configuration")
		}
		pending = snap.PendingRequests
		if pending == 4 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if pending != 4 {
		t.Fatal("could not observe pending batch before kill", pending)
	}
	defer func() {
		restoreCtx, stop := context.WithTimeout(context.Background(), 15*time.Second)
		defer stop()
		c := exec.CommandContext(restoreCtx, "docker", "compose", "-f", "compose.yaml", "-f", "compose.benchmark.yaml", "start", "traceforge-benchmark")
		c.Dir = filepath.Join("..", "..")
		if out, err := c.CombinedOutput(); err != nil {
			t.Errorf("restore benchmark receiver %v %s", err, out)
		}
	}()
	kill := exec.CommandContext(ctx, "docker", "compose", "-f", "compose.yaml", "-f", "compose.benchmark.yaml", "kill", "-s", "SIGKILL", "traceforge-benchmark")
	kill.Dir = filepath.Join("..", "..")
	if out, err := kill.CombinedOutput(); err != nil {
		t.Fatalf("isolated kill failed %v %s", err, out)
	}
	for i := 0; i < 4; i++ {
		if err := <-done; err == nil {
			t.Fatal("pending request unexpectedly succeeded before forced kill")
		}
	}
	for _, id := range ids {
		if n := count(t, s, id); n != 0 {
			t.Fatal("experiment did not kill before persistence", id, n)
		}
	}
	t.Logf("SIGKILL with 4 observed pending jobs: no successful acknowledgments and 0 stored rows for each unique fixture; trace_ids=%v", ids)
}
