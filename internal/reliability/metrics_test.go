package reliability

import (
	"context"
	collector "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	common "go.opentelemetry.io/proto/otlp/common/v1"
	mp "go.opentelemetry.io/proto/otlp/metrics/v1"
	resource "go.opentelemetry.io/proto/otlp/resource/v1"
	"testing"
	"time"
	"traceforge/internal/metricdata"
)

func attr(k, v string) *common.KeyValue {
	return &common.KeyValue{Key: k, Value: &common.AnyValue{Value: &common.AnyValue_StringValue{StringValue: v}}}
}

type metricRepo struct {
	points []metricdata.Point
	fail   bool
}

func (r metricRepo) MetricPoints(context.Context, metricdata.Query) ([]metricdata.Point, error) {
	if r.fail {
		return nil, metricdata.ErrBackend
	}
	return r.points, nil
}
func metricFixture(t *testing.T) []metricdata.Point { return metricFixtureWriter(t, "writer", 10, 1) }
func metricFixtureWriter(t *testing.T, writer string, step, badStep int) []metricdata.Point {
	t.Helper()
	metrics := []*mp.Metric{}
	for _, name := range []string{"traceforge.requests.completed", "traceforge.requests.errors"} {
		sum := &mp.Sum{AggregationTemporality: 2, IsMonotonic: true}
		for i := 0; i < 3; i++ {
			value := int64(100 + i*step)
			if name == "traceforge.requests.errors" {
				value = int64(i * badStep)
			}
			sum.DataPoints = append(sum.DataPoints, &mp.NumberDataPoint{StartTimeUnixNano: uint64(epoch.Add(-time.Second).UnixNano()), TimeUnixNano: uint64(epoch.Add(time.Duration(i*10) * time.Second).UnixNano()), Value: &mp.NumberDataPoint_AsInt{AsInt: value}, Attributes: []*common.KeyValue{attr("http.route", "/charge"), attr("http.request.method", "GET")}})
		}
		metrics = append(metrics, &mp.Metric{Name: name, Unit: "{request}", Data: &mp.Metric_Sum{Sum: sum}})
	}
	hist := &mp.Histogram{AggregationTemporality: 2}
	for i := 0; i < 3; i++ {
		hist.DataPoints = append(hist.DataPoints, &mp.HistogramDataPoint{StartTimeUnixNano: uint64(epoch.Add(-time.Second).UnixNano()), TimeUnixNano: uint64(epoch.Add(time.Duration(i*10) * time.Second).UnixNano()), Count: uint64(i * 10), ExplicitBounds: []float64{.1, .25, .5}, BucketCounts: []uint64{uint64(i * 8), uint64(i), uint64(i), 0}, Attributes: []*common.KeyValue{attr("http.route", "/charge"), attr("http.request.method", "GET")}})
	}
	metrics = append(metrics, &mp.Metric{Name: "traceforge.request.duration", Unit: "s", Data: &mp.Metric_Histogram{Histogram: hist}})
	req := &collector.ExportMetricsServiceRequest{ResourceMetrics: []*mp.ResourceMetrics{{Resource: &resource.Resource{Attributes: []*common.KeyValue{attr("service.name", "payment"), attr("service.namespace", "test"), attr("service.instance.id", writer), attr("deployment.environment.name", "local"), attr("traceforge.workload.kind", "synthetic")}}, ScopeMetrics: []*mp.ScopeMetrics{{Scope: &common.InstrumentationScope{Name: "traceforge.demo", Version: "0.1.0"}, Metrics: metrics}}}}}
	points, n, e := metricdata.Normalize(req)
	if e != nil || n != 0 {
		t.Fatal(e, n)
	}
	return points
}
func TestMatchedAvailabilityAndWeightedCounts(t *testing.T) {
	p := metricFixture(t)
	d := definition()
	ev := (MetricSource{metricRepo{points: p}}).Measure(context.Background(), d, epoch, epoch.Add(20*time.Second))
	if ev.Status != "MEASURED" || ev.Total != "20" || ev.Bad != "2" || ev.Good != "18" || ev.Coverage != 1 {
		t.Fatal(ev)
	}
	p = append(p, p...)
	ev = (MetricSource{metricRepo{points: p}}).Measure(context.Background(), d, epoch, epoch.Add(20*time.Second))
	if ev.Total != "20" {
		t.Fatal("replay inflated SLI", ev)
	}
}

func TestWeightedWriterPopulationNotAveragePercentages(t *testing.T) {
	d := definition()
	d.Writers = []string{"writer", "small"}
	rows := append(metricFixture(t), metricFixtureWriter(t, "small", 1, 0)...)
	e := (MetricSource{metricRepo{points: rows}}).Measure(context.Background(), d, epoch, epoch.Add(20*time.Second))
	if e.Total != "22" || e.Bad != "2" || e.Good != "20" {
		t.Fatal("writer error percentages were averaged", e)
	}
	b, err := Calculate(d.Target, e.Total, e.Good, e.Bad)
	if err != nil || b.BadFraction != "1/11" {
		t.Fatal(b, err)
	}
}
func TestHistogramObjectiveBoundary(t *testing.T) {
	d := definition()
	d.Kind = "latency"
	d.ThresholdSeconds = "0.25"
	s := MetricSource{metricRepo{points: metricFixture(t)}}
	e := s.Measure(context.Background(), d, epoch, epoch.Add(20*time.Second))
	if e.Status != "MEASURED" || e.Good != "18" || e.Bad != "2" {
		t.Fatal(e)
	}
	d.ThresholdSeconds = "0.3"
	e = s.Measure(context.Background(), d, epoch, epoch.Add(20*time.Second))
	if e.Status != "INCOMPATIBLE" {
		t.Fatal("estimated an exact within-bucket good count", e)
	}
}
func TestMetricMissingWriterOutageAndIntervalMismatch(t *testing.T) {
	d := definition()
	p := metricFixture(t)
	d.Writers = append(d.Writers, "missing")
	e := (MetricSource{metricRepo{points: p}}).Measure(context.Background(), d, epoch, epoch.Add(20*time.Second))
	if e.Status != "INSUFFICIENT_EVIDENCE" {
		t.Fatal(e)
	}
	d = definition()
	e = (MetricSource{metricRepo{fail: true}}).Measure(context.Background(), d, epoch, epoch.Add(20*time.Second))
	if e.Status != "BACKEND_UNAVAILABLE" {
		t.Fatal(e)
	}
	for i := range p {
		if p[i].Name == "traceforge.requests.errors" {
			p[i].End++
		}
	}
	e = (MetricSource{metricRepo{points: p}}).Measure(context.Background(), d, epoch, epoch.Add(20*time.Second))
	if e.Status == "MEASURED" {
		t.Fatal("mismatched denominator interval accepted")
	}
}
func TestUndeclaredAndOversizedWindowsRemainUnknown(t *testing.T) {
	d := definition()
	d.Writers = nil
	s := MetricSource{metricRepo{}}
	if e := s.Measure(context.Background(), d, epoch, epoch.Add(time.Minute)); e.Status != "INSUFFICIENT_EVIDENCE" {
		t.Fatal(e)
	}
	d = definition()
	if e := s.Measure(context.Background(), d, epoch, epoch.Add(30*24*time.Hour)); e.Status != "INCOMPATIBLE" {
		t.Fatal("unbounded30day query attempted", e)
	}
}
