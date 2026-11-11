//go:build integration

package integration

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	collector "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	common "go.opentelemetry.io/proto/otlp/common/v1"
	resource "go.opentelemetry.io/proto/otlp/resource/v1"
	trace "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
	"traceforge/internal/storage"
)

func id(t *testing.T, n int) []byte {
	t.Helper()
	b := make([]byte, n)
	if _, e := rand.Read(b); e != nil {
		t.Fatal(e)
	}
	return b
}
func attr(k, v string) *common.KeyValue {
	return &common.KeyValue{Key: k, Value: &common.AnyValue{Value: &common.AnyValue_StringValue{StringValue: v}}}
}
func request(t *testing.T) (*collector.ExportTraceServiceRequest, string) {
	t.Helper()
	tid := id(t, 16)
	now := uint64(time.Now().UnixNano())
	s := &trace.Span{TraceId: tid, SpanId: id(t, 8), Name: "integration.synthetic", StartTimeUnixNano: now, EndTimeUnixNano: now + 123456789, Attributes: []*common.KeyValue{attr("password", "synthetic-sensitive-value"), {Key: "integer", Value: &common.AnyValue{Value: &common.AnyValue_IntValue{IntValue: 9223372036854775807}}}}}
	return &collector.ExportTraceServiceRequest{ResourceSpans: []*trace.ResourceSpans{{Resource: &resource.Resource{Attributes: []*common.KeyValue{attr("service.name", "integration")}}, ScopeSpans: []*trace.ScopeSpans{{Scope: &common.InstrumentationScope{Name: "integration", Version: "2"}, Spans: []*trace.Span{s}}}}}}, hex.EncodeToString(tid)
}
func grpcClient(t *testing.T) collector.TraceServiceClient {
	t.Helper()
	c, e := grpc.NewClient("localhost:4319", grpc.WithTransportCredentials(insecure.NewCredentials()))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { c.Close() })
	return collector.NewTraceServiceClient(c)
}
func database(t *testing.T) *storage.Store {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	s, e := storage.Open(ctx)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { s.Close() })
	return s
}
func count(t *testing.T, s *storage.Store, tid string) uint64 {
	t.Helper()
	var n uint64
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if e := s.Conn.QueryRow(ctx, "SELECT count() FROM traceforge.spans WHERE trace_id = ?", tid).Scan(&n); e != nil {
		t.Fatal(e)
	}
	return n
}

func TestOTLPStorageContract(t *testing.T) {
	c := grpcClient(t)
	s := database(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if e := s.Migrate(ctx); e != nil {
		t.Fatal(e)
	}
	if e := s.Migrate(ctx); e != nil {
		t.Fatal("migration is not repeatable", e)
	}
	t.Run("empty", func(t *testing.T) {
		r, e := c.Export(ctx, &collector.ExportTraceServiceRequest{})
		if e != nil || r.PartialSuccess != nil {
			t.Fatal("empty response", e)
		}
	})
	t.Run("multiple_groups_and_scopes", func(t *testing.T) {
		req, tid := request(t)
		base := req.ResourceSpans[0].ScopeSpans[0].Spans[0]
		req.ResourceSpans = nil
		for a := 0; a < 3; a++ {
			rs := &trace.ResourceSpans{Resource: &resource.Resource{Attributes: []*common.KeyValue{attr("service.name", fmt.Sprintf("integration-%d", a))}}}
			for b := 0; b < 2; b++ {
				ss := &trace.ScopeSpans{Scope: &common.InstrumentationScope{Name: fmt.Sprintf("scope-%d", b)}}
				for k := 0; k < 2; k++ {
					ss.Spans = append(ss.Spans, &trace.Span{TraceId: base.TraceId, SpanId: id(t, 8), Name: base.Name, StartTimeUnixNano: base.StartTimeUnixNano, EndTimeUnixNano: base.EndTimeUnixNano, Attributes: base.Attributes})
				}
				rs.ScopeSpans = append(rs.ScopeSpans, ss)
			}
			req.ResourceSpans = append(req.ResourceSpans, rs)
		}
		res, e := c.Export(ctx, req)
		if e != nil || res.PartialSuccess != nil {
			t.Fatal("full response", e)
		}
		if n := count(t, s, tid); n != 12 {
			t.Fatal("stored rows", n)
		}
		var services, scopes uint64
		if e := s.Conn.QueryRow(ctx, "SELECT uniqExact(service_name), uniqExact(scope_name) FROM traceforge.spans WHERE trace_id = ?", tid).Scan(&services, &scopes); e != nil || services != 3 || scopes != 2 {
			t.Fatal("group mapping", services, scopes, e)
		}
		var stored string
		var start, end int64
		if e := s.Conn.QueryRow(ctx, "SELECT span_json, toUnixTimestamp64Nano(start_time), toUnixTimestamp64Nano(end_time) FROM traceforge.spans WHERE trace_id = ? LIMIT 1", tid).Scan(&stored, &start, &end); e != nil {
			t.Fatal(e)
		}
		decoded := &trace.Span{}
		if e := protojson.Unmarshal([]byte(stored), decoded); e != nil {
			t.Fatal(e)
		}
		if strings.Contains(stored, "synthetic-sensitive-value") || decoded.Attributes[1].Value.GetIntValue() != 9223372036854775807 || start != int64(base.StartTimeUnixNano) || end != int64(base.EndTimeUnixNano) {
			t.Fatal("typed/redacted/nanosecond data lost")
		}
		t.Logf("trace=%s rows=12 services=3 scopes=2; nanosecond and typed redacted data verified", tid)
	})
	t.Run("partial_and_retry_duplicates", func(t *testing.T) {
		req, tid := request(t)
		ss := req.ResourceSpans[0].ScopeSpans[0]
		ss.Spans = append(ss.Spans, &trace.Span{}, &trace.Span{})
		res, e := c.Export(ctx, req)
		if e != nil || res.GetPartialSuccess().GetRejectedSpans() != 2 || count(t, s, tid) != 1 {
			t.Fatal("partial response", e)
		}
		ss.Spans = ss.Spans[:1]
		if _, e := c.Export(ctx, req); e != nil {
			t.Fatal(e)
		}
		if n := count(t, s, tid); n != 2 {
			t.Fatal("expected deliberate duplicate on replay", n)
		}
		t.Logf("trace=%s rejected=2; valid rows=1; deliberate replay rows=2 (duplicate demonstrated)", tid)
	})
}

type storedSpan struct {
	ID, Parent, Service, Name string
	Kind                      uint8
	Duration                  uint64
	Status                    uint8
	Events                    string
}

func stored(t *testing.T, s *storage.Store, tid string) []storedSpan {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	rows, e := s.Conn.Query(ctx, "SELECT span_id,parent_span_id,service_name,span_name,span_kind,duration_ns,status_code,events_json FROM traceforge.spans WHERE trace_id = ? ORDER BY start_time", tid)
	if e != nil {
		t.Fatal(e)
	}
	defer rows.Close()
	var all []storedSpan
	for rows.Next() {
		var r storedSpan
		if e := rows.Scan(&r.ID, &r.Parent, &r.Service, &r.Name, &r.Kind, &r.Duration, &r.Status, &r.Events); e != nil {
			t.Fatal(e)
		}
		all = append(all, r)
	}
	if e := rows.Err(); e != nil {
		t.Fatal(e)
	}
	return all
}
func collectorEvidence(t *testing.T, tid string) map[string]string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "docker", "compose", "logs", "--no-color", "--no-log-prefix", "--since=5m", "otel-collector")
	cmd.Dir = filepath.Join("..", "..")
	output, e := cmd.Output()
	if e != nil {
		t.Fatal("Collector evidence", e)
	}
	blocks := regexp.MustCompile(`(?m)^Span #\d+\s*$`).Split(string(output), -1)
	out := map[string]string{}
	value := func(block, key string) string {
		match := regexp.MustCompile(`(?m)^[\t ]*` + key + `[\t ]*:[\t ]*([^\r\n]*)`).FindStringSubmatch(block)
		if len(match) != 2 {
			t.Fatalf("missing Collector field %s", key)
		}
		return strings.TrimSpace(match[1])
	}
	for _, b := range blocks[1:] {
		if !strings.Contains(b, "Trace ID       : "+tid) {
			continue
		}
		out[value(b, "ID")] = value(b, "Parent ID")
	}
	return out
}
func TestDemoTraceStorage(t *testing.T) {
	s := database(t)
	var baseline uint64
	for _, tc := range []struct {
		name, query string
		code        int
		delay       uint64
	}{{"normal", "", 200, 0}, {"delayed", "?delay_ms=500", 200, 500000000}, {"failed", "?fail=true", 502, 0}} {
		t.Run(tc.name, func(t *testing.T) {
			client := &http.Client{Timeout: 10 * time.Second}
			res, e := client.Get("http://localhost:8080/checkout" + tc.query)
			if e != nil {
				t.Fatal(e)
			}
			defer res.Body.Close()
			body, e := io.ReadAll(io.LimitReader(res.Body, 4096))
			if e != nil {
				t.Fatal(e)
			}
			var response map[string]string
			if e := json.Unmarshal(body, &response); e != nil {
				t.Fatal(e)
			}
			tid := response["trace_id"]
			if len(tid) != 32 || res.StatusCode != tc.code {
				t.Fatal("demo response", res.StatusCode, string(body))
			}
			deadline := time.Now().Add(25 * time.Second)
			var all []storedSpan
			for time.Now().Before(deadline) {
				all = stored(t, s, tid)
				if len(all) >= 5 {
					break
				}
				time.Sleep(200 * time.Millisecond)
			}
			unique := map[string]storedSpan{}
			for _, r := range all {
				unique[r.ID] = r
			}
			if len(unique) != 5 {
				t.Fatal("expected 5 distinct spans", len(unique))
			}
			byName := map[string]storedSpan{}
			for _, r := range unique {
				byName[r.Name] = r
			}
			g, p, v, i := byName["GET /checkout"], byName["GET /charge"], byName["checkout.validate"], byName["payment.process"]
			var c storedSpan
			for _, r := range unique {
				if r.Kind == 3 {
					c = r
				}
			}
			if g.ID == "" || p.ID == "" || v.ID == "" || i.ID == "" || c.ID == "" || g.Parent != "" || p.Parent != c.ID || c.Parent != g.ID || v.Parent != g.ID || i.Parent != p.ID || g.Service != "gateway" || p.Service != "payment" {
				t.Fatal("stored hierarchy/service mapping is wrong")
			}
			if i.Duration < tc.delay {
				t.Fatal("delay missing in stored duration")
			}
			if tc.name == "normal" {
				baseline = i.Duration
			}
			if tc.name == "delayed" && i.Duration <= baseline {
				t.Fatal("delayed processing did not increase duration")
			}
			if tc.name == "failed" {
				for _, r := range []storedSpan{g, p, c, i} {
					if r.Status != 2 {
						t.Fatal("missing stored error", r.Name)
					}
				}
				if !strings.Contains(i.Events, "synthetic payment failure") {
					t.Fatal("lost exception")
				}
			}
			evidence := collectorEvidence(t, tid)
			for _, r := range unique {
				parent, ok := evidence[r.ID]
				if !ok || parent != r.Parent {
					t.Fatal("stored identifiers differ from Collector", r.ID)
				}
			}
			t.Logf("trace=%s rows=%d distinct_spans=%d duplicates=%d", tid, len(all), len(unique), len(all)-len(unique))
			for _, r := range all {
				t.Logf("span=%s parent=%s service=%s name=%q duration_ns=%d status=%d", r.ID, r.Parent, r.Service, r.Name, r.Duration, r.Status)
			}
		})
	}
}

func TestDatabaseUnavailable(t *testing.T) {
	if os.Getenv("TRACEFORGE_EXPECT_DB_DOWN") != "1" {
		t.Skip("controlled outage requires TRACEFORGE_EXPECT_DB_DOWN=1")
	}
	c := grpcClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	req, tid := request(t)
	res, e := c.Export(ctx, req)
	if status.Code(e) != codes.Unavailable || res != nil {
		t.Fatalf("expected no ack and Unavailable: %v %v", res, e)
	}
	for _, tc := range []struct {
		path   string
		status int
	}{{"/healthz", 200}, {"/readyz", 503}} {
		client := &http.Client{Timeout: 3 * time.Second}
		r, e := client.Get("http://localhost:8082" + tc.path)
		if e != nil {
			t.Fatal(e)
		}
		r.Body.Close()
		if r.StatusCode != tc.status {
			t.Fatal(tc.path, r.StatusCode)
		}
	}
	t.Logf("trace=%s direct Export returned Unavailable with no success acknowledgment; healthz=200 readyz=503", tid)
}

func TestFreshExportAfterRecovery(t *testing.T) {
	c := grpcClient(t)
	s := database(t)
	req, tid := request(t)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	res, e := c.Export(ctx, req)
	if e != nil || res.PartialSuccess != nil || count(t, s, tid) != 1 {
		t.Fatal("recovery export failed", e)
	}
	if bytes.Equal(req.ResourceSpans[0].ScopeSpans[0].Spans[0].TraceId, make([]byte, 16)) {
		t.Fatal("bad test ID")
	}
	t.Logf("fresh recovery trace=%s rows=1", tid)
}
