package metricdata

import (
	"context"
	"encoding/json"
	"errors"
	collector "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	common "go.opentelemetry.io/proto/otlp/common/v1"
	mp "go.opentelemetry.io/proto/otlp/metrics/v1"
	resource "go.opentelemetry.io/proto/otlp/resource/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"math"
	"net/http/httptest"
	"reflect"
	"strconv"
	"sync"
	"testing"
	"time"
)

var epoch = time.Date(2026, 1, 14, 10, 0, 0, 0, time.UTC)

func ns(second int) uint64 { return uint64(epoch.Add(time.Duration(second) * time.Second).UnixNano()) }
func kv(k, v string) *common.KeyValue {
	return &common.KeyValue{Key: k, Value: &common.AnyValue{Value: &common.AnyValue_StringValue{StringValue: v}}}
}
func request(metrics ...*mp.Metric) *collector.ExportMetricsServiceRequest {
	return &collector.ExportMetricsServiceRequest{ResourceMetrics: []*mp.ResourceMetrics{{Resource: &resource.Resource{Attributes: []*common.KeyValue{kv("service.name", "payment"), kv("service.instance.id", "writer-A"), kv("service.namespace", "fixture")}}, ScopeMetrics: []*mp.ScopeMetrics{{Scope: &common.InstrumentationScope{Name: "fixture"}, Metrics: metrics}}}}}
}
func counter(start, end int, value int64) *mp.NumberDataPoint {
	return &mp.NumberDataPoint{StartTimeUnixNano: ns(start), TimeUnixNano: ns(end), Value: &mp.NumberDataPoint_AsInt{AsInt: value}}
}
func sumMetric(temporal mp.AggregationTemporality, monotonic bool, p ...*mp.NumberDataPoint) *mp.Metric {
	return &mp.Metric{Name: "requests", Unit: "{request}", Data: &mp.Metric_Sum{Sum: &mp.Sum{AggregationTemporality: temporal, IsMonotonic: monotonic, DataPoints: p}}}
}
func normalized(t *testing.T, r *collector.ExportMetricsServiceRequest) []Point {
	t.Helper()
	p, n, e := Normalize(r)
	if e != nil || n != 0 {
		t.Fatal("normalization", e, n)
	}
	for i := range p {
		p[i].IngestedAt = epoch
	}
	return p
}
func aggregate(t *testing.T, from, to int, p []Point) *Result {
	t.Helper()
	r, e := Aggregate(context.Background(), Query{From: epoch.Add(time.Duration(from) * time.Second), To: epoch.Add(time.Duration(to) * time.Second)}, p)
	if e != nil {
		t.Fatal(e)
	}
	return r
}
func TestCumulativeCounterAndMissingExport(t *testing.T) {
	p := normalized(t, request(sumMetric(2, true, counter(-10, 0, 100), counter(-10, 10, 180), counter(-10, 20, 300), counter(-10, 30, 350))))
	r := aggregate(t, 0, 30, p)
	s := r.Series[0]
	if *s.Increment != "250" || *s.ObservedRate != 250.0/30 || !s.Complete || len(s.Intervals) != 3 {
		t.Fatalf("wrong cumulative %+v", s)
	}
	for i, v := range []string{"80", "120", "50"} {
		if s.Intervals[i].Value != v {
			t.Fatal(s.Intervals)
		}
	}
	r = aggregate(t, 0, 30, append(p[:2:2], p[3]))
	s = r.Series[0]
	if *s.Increment != "250" || len(s.Intervals) != 2 || s.Intervals[1].Value != "170" {
		t.Fatal("missing export lost combined increment")
	}
}
func TestUnknownBaselineKnownZeroAndReset(t *testing.T) {
	p := normalized(t, request(sumMetric(2, true, counter(-10, 0, 100), counter(-10, 10, 180), counter(15, 20, 25))))
	s := aggregate(t, 0, 20, p).Series[0]
	if *s.Increment != "105" || s.Diagnostics.Resets != 1 || s.ObservedSeconds != 15 || s.Complete {
		t.Fatal("reset/gap", s)
	}
	p = normalized(t, request(sumMetric(2, true, counter(-10, 10, 100), counter(-10, 20, 25))))
	s = aggregate(t, 0, 20, p).Series[0]
	if s.Increment != nil || s.Diagnostics.Baselines != 1 || s.Diagnostics.InvalidDecreases != 1 || s.Complete {
		t.Fatal("invented reset", s)
	}
	p = normalized(t, request(sumMetric(2, true, counter(0, 10, 25))))
	s = aggregate(t, 0, 10, p).Series[0]
	if *s.Increment != "25" || !s.Complete {
		t.Fatal("known zero accumulation start", s)
	}
}
func TestDeltaReplayOverlapGapAndBoundary(t *testing.T) {
	p := normalized(t, request(sumMetric(1, true, counter(0, 10, 80), counter(10, 20, 120), counter(30, 40, 50))))
	p = append(p, p...)
	s := aggregate(t, 0, 40, p).Series[0]
	if *s.Increment != "250" || s.ObservedSeconds != 30 || s.Diagnostics.Replays != 3 || s.Diagnostics.Gaps != 1 || s.Complete {
		t.Fatal("delta gaps/replay", s)
	}
	p = normalized(t, request(sumMetric(1, true, counter(0, 20, 100), counter(10, 30, 100), counter(30, 40, 25))))
	s = aggregate(t, 0, 40, p).Series[0]
	if *s.Increment != "25" || s.Diagnostics.Overlaps != 2 {
		t.Fatal("overlap double counted", s)
	}
	s = aggregate(t, 5, 40, p).Series[0]
	if s.Diagnostics.BoundaryIntervals == 0 {
		t.Fatal("crossing boundary not exposed")
	}
}
func TestSignedSumAndGaugeNotEventTotals(t *testing.T) {
	p := normalized(t, request(sumMetric(1, false, counter(0, 10, 3), counter(10, 20, -2))))
	s := aggregate(t, 0, 20, p).Series[0]
	if *s.Increment != "1" || s.Diagnostics.Resets != 0 || s.Latest != nil {
		t.Fatal("non-monotonic interpreted as reset/state", s)
	}
	g := &mp.Metric{Name: "temperature", Unit: "Cel", Data: &mp.Metric_Gauge{Gauge: &mp.Gauge{DataPoints: []*mp.NumberDataPoint{counter(0, 10, 30), counter(0, 20, 20)}}}}
	p = normalized(t, request(g))
	s = aggregate(t, 0, 20, p).Series[0]
	if *s.Latest != "20" || *s.Mean != 25 || *s.Minimum != 20 || *s.Maximum != 30 || s.Increment != nil || s.ObservedRate != nil {
		t.Fatal("gauge summed", s)
	}
	s = aggregate(t, 21, 30, p).Series[0]
	if s.Latest != nil || len(s.Points) != 0 {
		t.Fatal("stale gauge carried forward")
	}
}
func hist(start, end int, counts ...uint64) *mp.HistogramDataPoint {
	total := uint64(0)
	for _, c := range counts {
		total += c
	}
	return &mp.HistogramDataPoint{StartTimeUnixNano: ns(start), TimeUnixNano: ns(end), ExplicitBounds: []float64{50, 100, 500}, BucketCounts: counts, Count: total}
}
func histogram(temp mp.AggregationTemporality, p ...*mp.HistogramDataPoint) *mp.Metric {
	return &mp.Metric{Name: "latency", Unit: "ms", Data: &mp.Metric_Histogram{Histogram: &mp.Histogram{AggregationTemporality: temp, DataPoints: p}}}
}
func TestHistogramMergeAndPercentileBounds(t *testing.T) {
	a := normalized(t, request(histogram(1, hist(0, 10, 30, 15, 4, 1))))
	req := request(histogram(1, hist(0, 10, 20, 20, 8, 2)))
	req.ResourceMetrics[0].Resource.Attributes[1] = kv("service.instance.id", "writer-B")
	b := normalized(t, req)
	r := aggregate(t, 0, 10, append(a, b...))
	s := r.MergedHistogram
	if s == nil || *s.HistogramCount != "100" || !reflect.DeepEqual(s.HistogramBuckets, []string{"50", "35", "12", "3"}) || *s.P50.Upper != 50 || *s.P95.Lower != 100 || *s.P95.Upper != 500 {
		t.Fatal("bad merged histogram", r)
	}
	p := normalized(t, request(histogram(1, hist(0, 10, 0, 0, 0, 10))))
	s = &aggregate(t, 0, 10, p).Series[0]
	if s.P95.Upper != nil || *s.P95.Lower != 500 {
		t.Fatal("finite overflow quantile invented")
	}
	req.ResourceMetrics[0].ScopeMetrics[0].Metrics[0].GetHistogram().DataPoints[0].ExplicitBounds[0] = 25
	b = normalized(t, req)
	r = aggregate(t, 0, 10, append(a, b...))
	if r.MergedHistogram != nil {
		t.Fatal("merged incompatible boundaries")
	}
}
func TestCumulativeHistogramAndMissingSum(t *testing.T) {
	a, b := hist(-10, 0, 10, 0, 0, 0), hist(-10, 10, 30, 10, 5, 0)
	sa, sb := 100.0, 400.0
	a.Sum = &sa
	b.Sum = &sb
	a.Min = &sa
	b.Min = &sa
	p := normalized(t, request(histogram(2, a, b)))
	s := aggregate(t, 0, 10, p).Series[0]
	if *s.HistogramCount != "35" || *s.HistogramSum != 300 || *s.HistogramMean != 300.0/35 {
		t.Fatal("bad histogram difference", s)
	}
	b.Sum = nil
	p = normalized(t, request(histogram(2, a, b)))
	s = aggregate(t, 0, 10, p).Series[0]
	if s.HistogramSum != nil || s.HistogramMean != nil {
		t.Fatal("missing histogram sum fabricated")
	}
}
func TestCanonicalConflictsOrderingAndTypedIdentity(t *testing.T) {
	p := normalized(t, request(sumMetric(1, true, counter(0, 10, 10))))
	conflict := p[0]
	v := "20"
	conflict.Int = &v
	conflict.IngestedAt = epoch.Add(time.Second)
	rows := []Point{conflict, p[0], conflict, p[0]}
	a := aggregate(t, 0, 10, rows)
	if *a.Series[0].Increment != "10" || a.Series[0].Diagnostics.Conflicts != 1 || a.Series[0].Diagnostics.Replays != 2 || a.Series[0].Complete {
		t.Fatal(a)
	}
	for i, j := 0, len(rows)-1; i < j; i, j = i+1, j-1 {
		rows[i], rows[j] = rows[j], rows[i]
	}
	b := aggregate(t, 0, 10, rows)
	aj, _ := json.Marshal(a)
	bj, _ := json.Marshal(b)
	if string(aj) != string(bj) {
		t.Fatal("order-dependent canonical result")
	}
	req := request(sumMetric(1, true, counter(0, 10, 1)))
	dp := req.ResourceMetrics[0].ScopeMetrics[0].Metrics[0].GetSum().DataPoints[0]
	dp.Attributes = []*common.KeyValue{kv("dimension", "1")}
	aID := normalized(t, req)[0].StreamID
	dp.Attributes = []*common.KeyValue{{Key: "dimension", Value: &common.AnyValue{Value: &common.AnyValue_IntValue{IntValue: 1}}}}
	if normalized(t, req)[0].StreamID == aID {
		t.Fatal("lost type identity")
	}
}
func TestExactIntegerAndNoRecordedValue(t *testing.T) {
	p := normalized(t, request(sumMetric(1, true, counter(0, 10, math.MaxInt64))))
	s := aggregate(t, 0, 10, p).Series[0]
	if *s.Increment != strconv.FormatInt(math.MaxInt64, 10) {
		t.Fatal("integer precision lost")
	}
	dp := counter(10, 20, 0)
	dp.Flags = 1
	p = normalized(t, request(sumMetric(1, true, dp)))
	s = aggregate(t, 0, 20, p).Series[0]
	if s.Increment != nil || s.Diagnostics.NoValues != 1 || s.Complete {
		t.Fatal("missing is not zero")
	}
}

func TestNoRecordedValueDoesNotRestartCumulativeSequence(t *testing.T) {
	missing := counter(0, 20, 0)
	missing.Flags = 1
	p := normalized(t, request(sumMetric(2, true, counter(0, 10, 100), missing, counter(0, 30, 200))))
	s := aggregate(t, 0, 30, p).Series[0]
	if s.Increment == nil || *s.Increment != "200" || s.Diagnostics.NoValues != 1 || s.Complete {
		t.Fatal("missing point restarted sequence or became zero", s)
	}
	g := &mp.Metric{Name: "gauge", Data: &mp.Metric_Gauge{Gauge: &mp.Gauge{DataPoints: []*mp.NumberDataPoint{counter(0, 10, 5), missing}}}}
	s = aggregate(t, 0, 30, normalized(t, request(g))).Series[0]
	if s.Latest != nil || *s.Mean != 5 {
		t.Fatal("stale gauge latest survived explicit no-value", s)
	}
}

func TestSmallFloatingIncrementIsNotRoundedToZero(t *testing.T) {
	p := counter(0, 10, 0)
	p.Value = &mp.NumberDataPoint_AsDouble{AsDouble: 1e-15}
	s := aggregate(t, 0, 10, normalized(t, request(sumMetric(1, true, p)))).Series[0]
	if s.Increment == nil || *s.Increment == "0" || s.ObservedRate == nil || *s.ObservedRate <= 0 {
		t.Fatal("small measurement became zero", s)
	}
}

func TestUnknownSequenceStartRemainsUnreliable(t *testing.T) {
	a, b := counter(0, 0, 100), counter(0, 10, 180)
	a.StartTimeUnixNano = 0
	b.StartTimeUnixNano = 0
	s := aggregate(t, 0, 10, normalized(t, request(sumMetric(2, true, a, b)))).Series[0]
	if s.Increment == nil || *s.Increment != "80" || s.Complete || s.Diagnostics.UnknownStarts != 1 {
		t.Fatal("unknown start represented as a confirmed uninterrupted sequence", s)
	}
}
func TestNormalizeRejectsUnsupportedMalformedAndSensitive(t *testing.T) {
	base := request(sumMetric(1, true, counter(0, 10, 1)))
	cases := []func(*collector.ExportMetricsServiceRequest){
		func(r *collector.ExportMetricsServiceRequest) {
			r.ResourceMetrics[0].Resource.Attributes[0].Value = &common.AnyValue{Value: &common.AnyValue_IntValue{IntValue: 1}}
		},
		func(r *collector.ExportMetricsServiceRequest) {
			r.ResourceMetrics[0].Resource.Attributes = append(r.ResourceMetrics[0].Resource.Attributes, kv("api_key", "must-not-store"))
		},
		func(r *collector.ExportMetricsServiceRequest) {
			r.ResourceMetrics[0].ScopeMetrics[0].Metrics[0].GetSum().DataPoints[0].TimeUnixNano = math.MaxUint64
		},
		func(r *collector.ExportMetricsServiceRequest) {
			r.ResourceMetrics[0].ScopeMetrics[0].Metrics[0].GetSum().DataPoints[0].Value = &mp.NumberDataPoint_AsDouble{AsDouble: math.Inf(1)}
		},
		func(r *collector.ExportMetricsServiceRequest) {
			r.ResourceMetrics[0].ScopeMetrics[0].Metrics[0].GetSum().DataPoints[0].Attributes = []*common.KeyValue{{Key: "nested", Value: &common.AnyValue{Value: &common.AnyValue_ArrayValue{ArrayValue: &common.ArrayValue{Values: []*common.AnyValue{}}}}}}
		},
		func(r *collector.ExportMetricsServiceRequest) {
			r.ResourceMetrics[0].ScopeMetrics[0].Metrics[0].Data = &mp.Metric_ExponentialHistogram{ExponentialHistogram: &mp.ExponentialHistogram{DataPoints: []*mp.ExponentialHistogramDataPoint{{}}}}
		},
	}
	for i, f := range cases {
		r := proto.Clone(base).(*collector.ExportMetricsServiceRequest)
		f(r)
		p, n, e := Normalize(r)
		if e != nil || len(p) != 0 || n != 1 {
			t.Fatal(i, e, n, p)
		}
	}
	bad := hist(0, 10, 1, 1, 1, 1)
	bad.Count = 7
	p, n, e := Normalize(request(histogram(1, bad)))
	if e != nil || n != 1 || len(p) != 0 {
		t.Fatal("bucket sum accepted")
	}
}

type fakeWriter struct {
	entered chan struct{}
	release chan struct{}
	fail    bool
}

func (w *fakeWriter) InsertMetricPoints(ctx context.Context, p []Point) error {
	if w.entered != nil {
		w.entered <- struct{}{}
		select {
		case <-w.release:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if w.fail {
		return errors.New("private driver credentials")
	}
	return nil
}
func TestReceiverACKAfterPersistenceAndFailure(t *testing.T) {
	w := &fakeWriter{entered: make(chan struct{}, 1), release: make(chan struct{})}
	s := NewServer(w)
	done := make(chan error, 1)
	go func() {
		_, e := s.Export(context.Background(), request(sumMetric(1, true, counter(0, 10, 1))))
		done <- e
	}()
	<-w.entered
	select {
	case <-done:
		t.Fatal("false early ACK")
	default:
	}
	close(w.release)
	if e := <-done; e != nil {
		t.Fatal(e)
	}
	s = NewServer(&fakeWriter{fail: true})
	if _, e := s.Export(context.Background(), request(sumMetric(1, true, counter(0, 10, 1)))); status.Code(e) != codes.Unavailable {
		t.Fatal(e)
	}
}
func TestBoundedAdmissionConcurrent(t *testing.T) {
	p := normalized(t, request(sumMetric(1, true, counter(0, 10, 1))))
	a := NewAdmission(4)
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			v := p[0]
			v.StreamKey += strconv.Itoa(i)
			v.StreamID = Hash(v.StreamKey)
			a.Accept([]Point{v})
		}(i)
	}
	wg.Wait()
	if len(a.series) != 4 {
		t.Fatal("admission exceeds cap", len(a.series))
	}
}

type fakeRepository struct{ err error }

func (f fakeRepository) MetricPoints(context.Context, Query) ([]Point, error) { return nil, f.err }
func TestAPIValidationBudgetAndOutage(t *testing.T) {
	for _, tc := range []struct {
		path string
		err  error
		code int
	}{{"/api/v1/metrics?url=x", nil, 400}, {"/api/v1/metrics?from=bad", nil, 400}, {"/api/v1/metrics/query", ErrBudget, 413}, {"/api/v1/metrics/series", ErrBackend, 503}, {"/api/v1/metrics", nil, 200}} {
		w := httptest.NewRecorder()
		NewHandler(fakeRepository{tc.err}).ServeHTTP(w, httptest.NewRequest("GET", tc.path, nil))
		if w.Code != tc.code {
			t.Fatal(w.Code, w.Body.String())
		}
	}
}

func TestIndependentWriterNormalizationAndDefinitionConflicts(t *testing.T) {
	a := normalized(t, request(sumMetric(2, true, counter(-10, 0, 1000), counter(-10, 10, 1010))))
	req := request(sumMetric(2, true, counter(-20, 0, 5), counter(-20, 10, 25)))
	req.ResourceMetrics[0].Resource.Attributes[1] = kv("service.instance.id", "writer-B")
	b := normalized(t, req)
	r := aggregate(t, 0, 10, append(a, b...))
	if r.CombinedSum == nil || r.CombinedSum.Increment != "30" || r.CombinedSum.Sources != 2 || *r.CombinedSum.Rate != 3 {
		t.Fatal("differenced cross-source raw readings", r)
	}
	req = request(sumMetric(2, true, counter(-10, 0, 1000), counter(-10, 10, 1010)))
	req.ResourceMetrics[0].ScopeMetrics[0].Metrics[0].Unit = "byte"
	b = normalized(t, req)
	r = aggregate(t, 0, 10, append(a, b...))
	if r.CombinedSum != nil || !r.Series[0].Diagnostics.DefinitionConflict || r.Series[0].Complete {
		t.Fatal("hidden conflicting definition", r)
	}
}
