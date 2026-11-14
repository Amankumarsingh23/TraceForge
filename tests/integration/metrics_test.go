//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	apiMetric "go.opentelemetry.io/otel/metric"
	sdk "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/exemplar"
	sdkresource "go.opentelemetry.io/otel/sdk/resource"
	collector "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	common "go.opentelemetry.io/proto/otlp/common/v1"
	mp "go.opentelemetry.io/proto/otlp/metrics/v1"
	resource "go.opentelemetry.io/proto/otlp/resource/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"math/big"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"
	"traceforge/internal/metricdata"
)

func metricClient(t *testing.T) collector.MetricsServiceClient {
	t.Helper()
	c, e := grpc.NewClient("localhost:4319", grpc.WithTransportCredentials(insecure.NewCredentials()))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { c.Close() })
	return collector.NewMetricsServiceClient(c)
}
func metricQuery(t *testing.T, q url.Values) *metricdata.Result {
	t.Helper()
	var r metricdata.Result
	if code := api(t, "/api/v1/metrics/query?"+q.Encode(), &r); code != 200 {
		t.Fatalf("metric query HTTP %d: %s", code, q.Encode())
	}
	return &r
}
func fixturePoint(start, end time.Time, value int64) *mp.NumberDataPoint {
	return &mp.NumberDataPoint{StartTimeUnixNano: uint64(start.UnixNano()), TimeUnixNano: uint64(end.UnixNano()), Value: &mp.NumberDataPoint_AsInt{AsInt: value}}
}
func metricFixture(t *testing.T) (*collector.ExportMetricsServiceRequest, url.Values, string) {
	base := time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC)
	suffix := fmt.Sprintf("%x", id(t, 6))
	service := "s11-metrics-" + suffix
	ns := "fixture-" + suffix
	req := &collector.ExportMetricsServiceRequest{}
	for writer := 0; writer < 2; writer++ {
		initial := int64(1000)
		values := []int64{1000, 1080, 1200, 1250}
		counts := []uint64{30, 15, 4, 1}
		if writer == 1 {
			initial = 100
			values = []int64{initial, 110, 130, 140}
			counts = []uint64{20, 20, 8, 2}
		}
		sum := &mp.Sum{AggregationTemporality: mp.AggregationTemporality_AGGREGATION_TEMPORALITY_CUMULATIVE, IsMonotonic: true}
		for i, v := range values {
			sum.DataPoints = append(sum.DataPoints, fixturePoint(base.Add(-time.Second), base.Add(time.Duration(i*10)*time.Second), v))
		}
		h := &mp.HistogramDataPoint{StartTimeUnixNano: uint64(base.UnixNano()), TimeUnixNano: uint64(base.Add(10 * time.Second).UnixNano()), Count: 50, ExplicitBounds: []float64{.05, .1, .5}, BucketCounts: counts}
		metrics := []*mp.Metric{{Name: "fixture.counter", Unit: "{request}", Data: &mp.Metric_Sum{Sum: sum}}, {Name: "fixture.histogram", Unit: "s", Data: &mp.Metric_Histogram{Histogram: &mp.Histogram{AggregationTemporality: 1, DataPoints: []*mp.HistogramDataPoint{h}}}}}
		req.ResourceMetrics = append(req.ResourceMetrics, &mp.ResourceMetrics{Resource: &resource.Resource{Attributes: []*common.KeyValue{attr("service.name", service), attr("service.instance.id", fmt.Sprintf("%s-writer-%d", suffix, writer)), attr("service.namespace", ns), attr("traceforge.workload.kind", "fixture")}}, ScopeMetrics: []*mp.ScopeMetrics{{Scope: &common.InstrumentationScope{Name: "session11.fixture", Version: "1"}, Metrics: metrics}}})
	}
	q := url.Values{"from": {base.Format(time.RFC3339Nano)}, "to": {base.Add(30 * time.Second).Format(time.RFC3339Nano)}, "namespace": {ns}, "service": {service}}
	return req, q, service
}
func TestMetricStorageTemporalReplayAndCardinality(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancel()
	req, q, service := metricFixture(t)
	c := metricClient(t)
	ack, e := c.Export(ctx, req)
	if e != nil || ack.PartialSuccess != nil {
		t.Fatal("metric export", e, ack)
	}
	q.Set("name", "fixture.counter")
	before := metricQuery(t, q)
	if before.CombinedSum == nil || before.CombinedSum.Increment != "290" || len(before.Series) != 2 {
		t.Fatal("source-normalized counts", before)
	}
	q.Set("name", "fixture.histogram")
	hist := metricQuery(t, q)
	if hist.MergedHistogram == nil || *hist.MergedHistogram.HistogramCount != "100" || *hist.MergedHistogram.P95.Lower != .1 || *hist.MergedHistogram.P95.Upper != .5 {
		t.Fatal("histogram merge", hist)
	}
	if _, e = c.Export(ctx, req); e != nil {
		t.Fatal(e)
	}
	after := metricQuery(t, q)
	for _, s := range after.Series {
		if s.Diagnostics.Replays != 1 || s.Diagnostics.Canonical != 1 || *s.HistogramCount != "50" {
			t.Fatal("histogram replay inflated", s)
		}
	}
	q.Set("name", "fixture.counter")
	after = metricQuery(t, q)
	if after.CombinedSum.Increment != "290" || after.Series[0].Diagnostics.Replays != 4 {
		t.Fatal("counter replay inflated", after)
	}
	// Unsupported points are counted individually while a valid sibling persists.
	unsupported := &mp.Metric{Name: "unsupported", Data: &mp.Metric_ExponentialHistogram{ExponentialHistogram: &mp.ExponentialHistogram{DataPoints: []*mp.ExponentialHistogramDataPoint{{}, {}}}}}
	req.ResourceMetrics = req.ResourceMetrics[:1]
	scope := req.ResourceMetrics[0].ScopeMetrics[0]
	scope.Metrics = []*mp.Metric{scope.Metrics[0], unsupported}
	ack, e = c.Export(ctx, req)
	if e != nil || ack.GetPartialSuccess().GetRejectedDataPoints() != 2 {
		t.Fatal("partial-success point accounting", e, ack)
	}
	// Dedicated resource makes the declared per-resource budget exactly measurable.
	cardinality := &mp.Sum{AggregationTemporality: 1, IsMonotonic: true}
	base := time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC)
	for i := 0; i < 300; i++ {
		p := fixturePoint(base, base.Add(time.Second), 1)
		p.Attributes = []*common.KeyValue{attr("fixture.dimension", fmt.Sprint(i))}
		cardinality.DataPoints = append(cardinality.DataPoints, p)
	}
	req.ResourceMetrics[0].Resource.Attributes = append(req.ResourceMetrics[0].Resource.Attributes, attr("fixture.resource", "cardinality"))
	scope.Metrics = []*mp.Metric{{Name: "fixture.cardinality", Data: &mp.Metric_Sum{Sum: cardinality}}}
	ack, e = c.Export(ctx, req)
	if e != nil || ack.GetPartialSuccess().GetRejectedDataPoints() != 44 {
		t.Fatal("bounded cardinality admission", e, ack)
	}
	store := database(t)
	var rows uint64
	if e = store.Conn.QueryRow(ctx, `SELECT count() FROM traceforge.metric_points WHERE service_name=? AND metric_name=?`, service, "fixture.cardinality").Scan(&rows); e != nil || rows != 256 {
		t.Fatal("actual cardinality storage", rows, e)
	}
	q.Set("name", "fixture.cardinality")
	if code := api(t, "/api/v1/metrics/query?"+q.Encode(), nil); code != 413 {
		t.Fatal("series overflow silently truncated", code)
	}
	// Fresh valid export after previous checks demonstrates no admission-as-ACK.
	evidence := map[string]any{"service": service, "query": q.Encode(), "counter_increment": "290", "histogram_count": "100", "cardinality_accepted": 256, "cardinality_rejected": 44}
	path := filepath.Join("..", "..", "artifacts", "session11", "fixtures.json")
	b, _ := json.MarshalIndent(evidence, "", "  ")
	if e = os.WriteFile(path, b, 0600); e != nil {
		t.Fatal(e)
	}
	t.Logf("actual stored: two writers counter290; histogram100 p95(.1,.5]s; replay stable; unsupported2; cardinality256 accepted/44 rejected; service=%s", service)
}
func TestSDKMetricOverflowPreservesObservedTotal(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	from := time.Now().UTC().Add(-time.Second)
	suffix := fmt.Sprintf("%x", id(t, 6))
	service := "s11-sdk-" + suffix
	exporter, e := otlpmetricgrpc.New(ctx, otlpmetricgrpc.WithEndpoint("localhost:4317"), otlpmetricgrpc.WithInsecure())
	if e != nil {
		t.Fatal(e)
	}
	provider := sdk.NewMeterProvider(sdk.WithResource(sdkresource.NewSchemaless(attribute.String("service.name", service), attribute.String("service.instance.id", suffix), attribute.String("traceforge.workload.kind", "fixture"))), sdk.WithReader(sdk.NewPeriodicReader(exporter, sdk.WithInterval(time.Minute))), sdk.WithCardinalityLimit(4), sdk.WithExemplarFilter(exemplar.AlwaysOffFilter))
	defer provider.Shutdown(context.Background())
	counter, e := provider.Meter("sdk.cardinality.fixture").Int64Counter("fixture.sdk_overflow")
	if e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 64; i++ {
		counter.Add(ctx, 1, apiMetric.WithAttributes(attribute.String("outcome", fmt.Sprint(i))))
	}
	// Immediate collect can have start==end on coarse Windows clocks. Wait for
	// a measurable accumulation interval rather than treating it as known zero.
	time.Sleep(25 * time.Millisecond)
	if e = provider.ForceFlush(ctx); e != nil {
		t.Fatal(e)
	}
	q := url.Values{"from": {from.Format(time.RFC3339Nano)}, "to": {time.Now().UTC().Format(time.RFC3339Nano)}, "service": {service}, "name": {"fixture.sdk_overflow"}}
	var r *metricdata.Result
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); {
		r = metricQuery(t, q)
		if len(r.Series) == 4 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if len(r.Series) != 4 {
		t.Fatal("SDK cardinality rows", r)
	}
	total := new(big.Int)
	overflow := false
	for _, s := range r.Series {
		if s.Increment == nil {
			t.Fatal("missing known-start increment", s)
		}
		v, ok := new(big.Int).SetString(*s.Increment, 10)
		if !ok {
			t.Fatal(s.Increment)
		}
		total.Add(total, v)
		overflow = overflow || s.Diagnostics.Overflow
	}
	if total.String() != "64" || !overflow || r.CombinedSum != nil {
		t.Fatal("overflow lost overall total or hid dimensions", r)
	}
	b, _ := json.MarshalIndent(map[string]any{"service": service, "query": q.Encode(), "series": r.Series, "total": "64"}, "", "  ")
	if e = os.WriteFile(filepath.Join("..", "..", "artifacts", "session11", "sdk-overflow.json"), b, 0600); e != nil {
		t.Fatal(e)
	}
	t.Log("real SDK cardinality limit4 exported4 streams including overflow; summed observed increments64, dimension coverage explicitly unreliable")
}
func TestMetricDatabaseUnavailable(t *testing.T) {
	if os.Getenv("TRACEFORGE_EXPECT_DB_DOWN") != "1" {
		t.Skip("explicit controlled local ClickHouse outage")
	}
	req, _, _ := metricFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, e := metricClient(t).Export(ctx, req); status.Code(e) != codes.Unavailable {
		t.Fatal("metric export falsely acknowledged without database", e)
	}
}
