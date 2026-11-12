package loadgen

import (
	"context"
	"encoding/hex"
	collector "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"reflect"
	"sync"
	"testing"
	"time"
	"traceforge/internal/ingest"
)

type fakeClient struct {
	mu    sync.Mutex
	calls int
	fail  bool
}

func (f *fakeClient) Export(ctx context.Context, r *collector.ExportTraceServiceRequest, _ ...grpc.CallOption) (*collector.ExportTraceServiceResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.fail {
		return nil, status.Error(codes.Unavailable, "synthetic overload")
	}
	return &collector.ExportTraceServiceResponse{}, nil
}
func config() Config {
	return Config{Spans: 105, SpansPerRequest: 20, Concurrency: 4, Seed: 42, Services: 3, RunID: "1234567890abcdef", BaseTime: time.Now().UTC(), RPCTimeout: time.Second}
}
func TestDeterministicValidSyntheticPayload(t *testing.T) {
	c := config()
	a := Build(c, 1, 20)
	b := Build(c, 1, 20)
	if !reflect.DeepEqual(a, b) {
		t.Fatal("payload not deterministic")
	}
	rows, rejected, e := ingest.Normalize(context.Background(), a)
	if e != nil || rejected != 0 || len(rows) != 20 {
		t.Fatal("invalid synthetic payload", e, rejected)
	}
	seen := map[string]bool{}
	for _, r := range rows {
		if r.TraceID[:16] != c.RunID || seen[r.SpanID] {
			t.Fatal("IDs not unique/scoped")
		}
		seen[r.SpanID] = true
	}
	other := Build(c, 2, 20)
	if hex.EncodeToString(other.ResourceSpans[0].ScopeSpans[0].Spans[0].TraceId) == rows[0].TraceID {
		t.Fatal("repeated trace ID")
	}
}
func TestLoadCountsAndFailureAccounting(t *testing.T) {
	c := config()
	f := &fakeClient{}
	r, e := Run(context.Background(), c, []Client{f})
	if e != nil || r.AttemptedRequests != 6 || r.GeneratedSpans != 105 || r.AcknowledgedSpans != 105 || r.SuccessfulRequests != 6 {
		t.Fatal("bad successful accounting", r, e)
	}
	f = &fakeClient{fail: true}
	r, e = Run(context.Background(), c, []Client{f})
	if e != nil || r.RetryableFailures != 6 || r.UnknownOutcomes != 6 || r.AcknowledgedSpans != 0 || r.GeneratedSpans != 105 {
		t.Fatal("failed attempts counted as success", r, e)
	}
}
func TestLoadLimitsAndQuantiles(t *testing.T) {
	c := config()
	c.RunID = "0000000000000000"
	if c.Validate() == nil {
		t.Fatal("zero isolation prefix accepted")
	}
	c = config()
	c.SpansPerRequest = 1001
	if c.Validate() == nil {
		t.Fatal("oversized request accepted")
	}
	if Percentile([]int64{3000000, 1000000, 2000000}, .5) != 2 || Percentile(nil, .95) != 0 {
		t.Fatal("bad quantile")
	}
}
