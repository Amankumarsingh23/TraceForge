// Package loadgen emits bounded synthetic OTLP workloads without retries.
package loadgen

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	collector "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	common "go.opentelemetry.io/proto/otlp/common/v1"
	resource "go.opentelemetry.io/proto/otlp/resource/v1"
	trace "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"math"
	"sort"
	"sync"
	"time"
)

type Config struct {
	Spans           int           `json:"total_spans"`
	SpansPerRequest int           `json:"spans_per_request"`
	Concurrency     int           `json:"concurrency"`
	Rate            float64       `json:"configured_spans_per_second"`
	Seed            uint64        `json:"seed"`
	Services        int           `json:"services"`
	RunID           string        `json:"run_id"`
	BaseTime        time.Time     `json:"base_time"`
	RPCTimeout      time.Duration `json:"rpc_timeout_ns"`
}

func (c Config) Validate() error {
	b, e := hex.DecodeString(c.RunID)
	if e != nil || len(b) != 8 || c.RunID == "0000000000000000" || c.RunID != hex.EncodeToString(b) || c.Spans < 1 || c.Spans > 200000 || c.SpansPerRequest < 1 || c.SpansPerRequest > 1000 || c.Concurrency < 1 || c.Concurrency > 64 || c.Services < 1 || c.Services > 16 || c.Rate < 0 || c.Rate > 1000000 || math.IsNaN(c.Rate) || math.IsInf(c.Rate, 0) || c.RPCTimeout <= 0 || c.RPCTimeout > 10*time.Second || c.BaseTime.IsZero() {
		return errors.New("load configuration exceeds bounded synthetic limits")
	}
	requests := (c.Spans + c.SpansPerRequest - 1) / c.SpansPerRequest
	if requests > 10000 || c.BaseTime.Before(time.Unix(0, 1)) || c.BaseTime.Add(time.Duration(requests)*time.Millisecond+time.Second).After(time.Unix(0, 1<<63-1)) || (c.Rate > 0 && float64(c.Spans)/c.Rate > 60) {
		return errors.New("request count, schedule or timestamp range exceeds load limits")
	}
	return nil
}

type Client interface {
	Export(context.Context, *collector.ExportTraceServiceRequest, ...grpc.CallOption) (*collector.ExportTraceServiceResponse, error)
}
type Report struct {
	Config                     Config  `json:"config"`
	AttemptedRequests          int     `json:"attempted_requests"`
	GeneratedSpans             int     `json:"generated_spans"`
	SuccessfulRequests         int     `json:"successful_requests"`
	AcknowledgedSpans          int     `json:"acknowledged_spans"`
	PartialRequests            int     `json:"partial_requests"`
	RejectedSpans              int     `json:"rejected_spans"`
	RetryableFailures          int     `json:"retryable_failures"`
	PermanentFailures          int     `json:"permanent_failures"`
	UnknownOutcomes            int     `json:"unknown_outcomes"`
	ElapsedSeconds             float64 `json:"elapsed_seconds"`
	OfferedSpansPerSecond      float64 `json:"actual_offered_spans_per_second"`
	AcknowledgedSpansPerSecond float64 `json:"acknowledged_spans_per_second"`
	P50MS                      float64 `json:"p50_ack_ms"`
	P95MS                      float64 `json:"p95_ack_ms"`
	P99MS                      float64 `json:"p99_ack_ms"`
	AllOutcomeP95MS            float64 `json:"p95_all_outcomes_ms"`
}

func Percentile(values []int64, p float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sorted := append([]int64{}, values...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	n := int(math.Ceil(p*float64(len(sorted)))) - 1
	if n < 0 {
		n = 0
	}
	if n >= len(sorted) {
		n = len(sorted) - 1
	}
	return float64(sorted[n]) / 1e6
}

func Build(c Config, index, n int) *collector.ExportTraceServiceRequest {
	prefix, _ := hex.DecodeString(c.RunID)
	tid := make([]byte, 16)
	copy(tid, prefix)
	binary.BigEndian.PutUint64(tid[8:], uint64(index+1)^c.Seed)
	// Trace IDs are nonzero even for an explicitly all-zero prefix and suffix.
	if binary.BigEndian.Uint64(tid[:8]) == 0 && binary.BigEndian.Uint64(tid[8:]) == 0 {
		tid[0] = 1
	}
	req := &collector.ExportTraceServiceRequest{}
	groups := make([]*trace.ResourceSpans, c.Services)
	for i := range groups {
		groups[i] = &trace.ResourceSpans{Resource: &resource.Resource{Attributes: []*common.KeyValue{{Key: "service.name", Value: &common.AnyValue{Value: &common.AnyValue_StringValue{StringValue: fmt.Sprintf("load-service-%d", i)}}}}}, ScopeSpans: []*trace.ScopeSpans{{Scope: &common.InstrumentationScope{Name: "traceforge/loadgen", Version: "0.1.0"}}}}
	}
	base := c.BaseTime.Add(time.Duration(index) * time.Millisecond).UnixNano()
	root := make([]byte, 8)
	binary.BigEndian.PutUint64(root, 1)
	for i := 0; i < n; i++ {
		id := make([]byte, 8)
		binary.BigEndian.PutUint64(id, uint64(i+1))
		sp := &trace.Span{TraceId: tid, SpanId: id, Name: "synthetic.operation", Kind: trace.Span_SPAN_KIND_INTERNAL, StartTimeUnixNano: uint64(base + int64(i)*10000), EndTimeUnixNano: uint64(base + int64(i)*10000 + 500000), Attributes: []*common.KeyValue{{Key: "load.synthetic", Value: &common.AnyValue{Value: &common.AnyValue_BoolValue{BoolValue: true}}}, {Key: "load.seed", Value: &common.AnyValue{Value: &common.AnyValue_IntValue{IntValue: int64(c.Seed)}}}}}
		if i == 0 {
			sp.Kind = trace.Span_SPAN_KIND_SERVER
			sp.EndTimeUnixNano = uint64(base + int64(n)*10000 + 1000000)
		} else {
			sp.ParentSpanId = root
		}
		g := groups[(i+int(c.Seed%uint64(c.Services)))%c.Services]
		g.ScopeSpans[0].Spans = append(g.ScopeSpans[0].Spans, sp)
	}
	for _, g := range groups {
		if len(g.ScopeSpans[0].Spans) > 0 {
			req.ResourceSpans = append(req.ResourceSpans, g)
		}
	}
	return req
}

func Run(ctx context.Context, c Config, clients []Client) (Report, error) {
	r := Report{Config: c}
	if e := c.Validate(); e != nil {
		return r, e
	}
	if len(clients) == 0 {
		return r, errors.New("no OTLP client")
	}
	type work struct{ index, n int }
	jobs := make(chan work, c.Concurrency)
	var mu sync.Mutex
	var wg sync.WaitGroup
	successLatency, allLatency := []int64{}, []int64{}
	start := time.Now()
	for worker := 0; worker < c.Concurrency; worker++ {
		wg.Add(1)
		go func(client Client) {
			defer wg.Done()
			for j := range jobs {
				if ctx.Err() != nil {
					return
				}
				req := Build(c, j.index, j.n)
				rpc, cancel := context.WithTimeout(ctx, c.RPCTimeout)
				begin := time.Now()
				res, err := client.Export(rpc, req)
				elapsed := time.Since(begin).Nanoseconds()
				cancel()
				mu.Lock()
				r.AttemptedRequests++
				r.GeneratedSpans += j.n
				allLatency = append(allLatency, elapsed)
				if err == nil {
					accepted := j.n - int(res.GetPartialSuccess().GetRejectedSpans())
					r.AcknowledgedSpans += accepted
					if res.PartialSuccess == nil {
						r.SuccessfulRequests++
					} else {
						r.PartialRequests++
						r.RejectedSpans += j.n - accepted
					}
					successLatency = append(successLatency, elapsed)
				} else {
					code := status.Code(err)
					retry := code == codes.Unavailable
					if code == codes.ResourceExhausted {
						for _, d := range status.Convert(err).Details() {
							if _, ok := d.(*errdetails.RetryInfo); ok {
								retry = true
							}
						}
					}
					if retry {
						r.RetryableFailures++
						r.UnknownOutcomes++
					} else if code == codes.Canceled || code == codes.DeadlineExceeded || code == codes.Unknown || code == codes.Internal {
						r.UnknownOutcomes++
					} else {
						r.PermanentFailures++
					}
				}
				mu.Unlock()
			}
		}(clients[worker%len(clients)])
	}
	generated := 0
	index := 0
produce:
	for generated < c.Spans {
		if c.Rate > 0 {
			when := start.Add(time.Duration(float64(generated) / c.Rate * float64(time.Second)))
			timer := time.NewTimer(time.Until(when))
			select {
			case <-timer.C:
			case <-ctx.Done():
				timer.Stop()
				break produce
			}
		}
		n := c.SpansPerRequest
		if n > c.Spans-generated {
			n = c.Spans - generated
		}
		select {
		case jobs <- work{index, n}:
			generated += n
			index++
		case <-ctx.Done():
			break produce
		}
	}
	close(jobs)
	wg.Wait()
	r.ElapsedSeconds = time.Since(start).Seconds()
	r.OfferedSpansPerSecond = float64(r.GeneratedSpans) / r.ElapsedSeconds
	r.AcknowledgedSpansPerSecond = float64(r.AcknowledgedSpans) / r.ElapsedSeconds
	r.P50MS = Percentile(successLatency, .5)
	r.P95MS = Percentile(successLatency, .95)
	r.P99MS = Percentile(successLatency, .99)
	r.AllOutcomeP95MS = Percentile(allLatency, .95)
	return r, nil
}
