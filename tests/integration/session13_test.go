//go:build integration

package integration

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	collectormetric "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	collectortrace "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	common "go.opentelemetry.io/proto/otlp/common/v1"
	metric "go.opentelemetry.io/proto/otlp/metrics/v1"
	resource "go.opentelemetry.io/proto/otlp/resource/v1"
	otrace "go.opentelemetry.io/proto/otlp/trace/v1"
	"os"
	"strings"
	"testing"
	"time"
	"traceforge/internal/analytics"
	"traceforge/internal/ingest"
	"traceforge/internal/metricdata"
	"traceforge/internal/reliability"
	"traceforge/internal/storage"
	"traceforge/internal/traces"
)

func TestSession13CrossPartitionHydrationAndPredecessor(t *testing.T) {
	if os.Getenv("TRACEFORGE_STORAGE_BENCH_DB") == "" {
		t.Skip("explicit isolated experiment")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	conn := database(t)
	db := "tf_bench_boundary_" + fmt.Sprint(time.Now().UnixNano())
	if e := conn.Conn.Exec(ctx, "CREATE DATABASE "+db); e != nil {
		t.Fatal(e)
	}
	s := storage.Store{Conn: conn.Conn, Database: db}
	if e := s.Migrate(ctx); e != nil {
		t.Fatal(e)
	}
	boundary := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	tid := id(t, 16)
	parent := id(t, 8)
	req := &collectortrace.ExportTraceServiceRequest{}
	for i, name := range []string{"boundary-caller", "boundary-server"} {
		span := &otrace.Span{TraceId: tid, SpanId: parent, Name: "boundary", Kind: otrace.Span_SPAN_KIND_CLIENT, StartTimeUnixNano: uint64(boundary.Add(-time.Millisecond).UnixNano()), EndTimeUnixNano: uint64(boundary.Add(time.Second).UnixNano())}
		if i == 1 {
			span.SpanId = id(t, 8)
			span.ParentSpanId = parent
			span.Kind = otrace.Span_SPAN_KIND_SERVER
			span.StartTimeUnixNano = uint64(boundary.Add(time.Millisecond).UnixNano())
		}
		req.ResourceSpans = append(req.ResourceSpans, &otrace.ResourceSpans{Resource: &resource.Resource{Attributes: []*common.KeyValue{attr("service.name", name), attr("traceforge.workload.kind", "fixture")}}, ScopeSpans: []*otrace.ScopeSpans{{Spans: []*otrace.Span{span}}}})
	}
	normalized, rejected, e := ingest.Normalize(ctx, req)
	if e != nil || rejected != 0 {
		t.Fatal(e, rejected)
	}
	if e = s.Insert(ctx, normalized); e != nil {
		t.Fatal(e)
	}
	rows, e := s.GetTraceSpans(ctx, hex.EncodeToString(tid), 100)
	if e != nil || len(rows) != 2 {
		t.Fatal("cross-partition trace lost", e, len(rows))
	}
	q := analytics.Query{From: boundary.Add(-time.Second), To: boundary, BucketSeconds: 60, Workload: "fixture"}
	rows, e = s.AnalyticsRows(ctx, q)
	if e != nil || len(rows) != 2 {
		t.Fatal("cross-window hydration lost", e, len(rows))
	}
	result, e := analytics.Compute(ctx, q, rows)
	if e != nil || len(result.Edges) != 1 {
		t.Fatal("cross-partition edge lost", e, result)
	}
	counter := &metric.Metric{Name: "boundary.counter", Data: &metric.Metric_Sum{Sum: &metric.Sum{IsMonotonic: true, AggregationTemporality: 2, DataPoints: []*metric.NumberDataPoint{fixturePoint(boundary.Add(-time.Minute), boundary.Add(-time.Millisecond), 10), fixturePoint(boundary.Add(-time.Minute), boundary.Add(time.Second), 15)}}}}
	mp := &collectormetric.ExportMetricsServiceRequest{ResourceMetrics: []*metric.ResourceMetrics{{Resource: &resource.Resource{Attributes: []*common.KeyValue{attr("service.name", "boundary-counter"), attr("service.instance.id", "one")}}, ScopeMetrics: []*metric.ScopeMetrics{{Metrics: []*metric.Metric{counter}}}}}}
	points, reject, e := metricdata.Normalize(mp)
	if e != nil || reject != 0 {
		t.Fatal(e, reject)
	}
	if e = s.InsertMetricPoints(ctx, points); e != nil {
		t.Fatal(e)
	}
	mq := metricdata.Query{From: boundary.Add(-time.Millisecond), To: boundary.Add(time.Second), Name: "boundary.counter", Service: "boundary-counter"}
	read, e := s.MetricPoints(ctx, mq)
	if e != nil || len(read) != 2 {
		t.Fatal("predecessor in previous partition missing", e, len(read))
	}
	agg, e := metricdata.Aggregate(ctx, mq, read)
	if e != nil || agg.CombinedSum == nil || agg.CombinedSum.Increment != "5" {
		t.Fatal("incorrect cross-month increment", e, agg)
	}
	t.Logf("%s: two monthly partitions hydrated, cross-window edge1, cumulative previous-partition predecessor gives5", db)
}

// The optimized storage path is compared using the production Go canonical
// engines, not a SQL approximation of conflict tie-breaking.
func TestSession13LayoutsCanonicalMetricsSLOAndProvider(t *testing.T) {
	db := os.Getenv("TRACEFORGE_STORAGE_BENCH_DB")
	if db == "" {
		t.Skip("explicit isolated Session13 benchmark database required")
	}
	if !strings.HasPrefix(db, "tf_bench_") || strings.ContainsAny(db, " ;'\"-.") {
		t.Fatal("invalid isolated database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	connection := database(t)
	var source storage.Store = storage.Store{Conn: connection.Conn, Database: db}
	candidateDB := "tf_bench_verify_" + fmt.Sprint(time.Now().UnixNano())
	if e := connection.Conn.Exec(ctx, "CREATE DATABASE "+candidateDB); e != nil {
		t.Fatal(e)
	}
	candidate := storage.Store{Conn: connection.Conn, Database: candidateDB}
	if e := candidate.Migrate(ctx); e != nil {
		t.Fatal(e)
	}
	if e := candidate.Migrate(ctx); e != nil {
		t.Fatal("idempotent migration", e)
	}
	// Keep the candidate service layout in a new database, without replacing originals.
	if e := connection.Conn.Exec(ctx, "RENAME TABLE "+candidateDB+".spans TO "+candidateDB+".empty_reference"); e != nil {
		t.Fatal(e)
	}
	if e := connection.Conn.Exec(ctx, "CREATE TABLE "+candidateDB+".spans AS "+db+".service_first"); e != nil {
		t.Fatal(e)
	}
	for _, q := range []string{
		"INSERT INTO " + candidateDB + ".spans SELECT * FROM " + db + ".service_first",
		"INSERT INTO " + candidateDB + ".metric_points SELECT * FROM " + db + ".metric_points",
		"INSERT INTO " + candidateDB + ".logs SELECT * FROM " + db + ".logs",
	} {
		if e := connection.Conn.Exec(ctx, q); e != nil {
			t.Fatal(e)
		}
	}
	var ids []string
	rows, e := connection.Conn.Query(ctx, "SELECT trace_id FROM "+db+".spans GROUP BY trace_id HAVING count()>5 AND countIf(scope_name='traceforge/s13')>0 ORDER BY trace_id LIMIT 8")
	if e != nil {
		t.Fatal(e)
	}
	for rows.Next() {
		var id string
		if e = rows.Scan(&id); e != nil {
			t.Fatal(e)
		}
		ids = append(ids, id)
	}
	rows.Close()
	if len(ids) == 0 {
		t.Fatal("replay/conflict fixtures absent")
	}
	for _, id := range ids {
		a, e := source.GetTraceSpans(ctx, id, 1000)
		if e != nil {
			t.Fatal(e)
		}
		b, e := candidate.GetTraceSpans(ctx, id, 1000)
		if e != nil {
			t.Fatal(e)
		}
		ca, e := traces.Reconstruct(id, a, 1000)
		if e != nil {
			t.Fatal(e)
		}
		cb, e := traces.Reconstruct(id, b, 1000)
		if e != nil {
			t.Fatal(e)
		}
		sameJSON(t, ca, cb)
	}
	q := analytics.Query{From: time.Date(2026, 9, 30, 23, 59, 59, 0, time.UTC), To: time.Date(2026, 9, 30, 23, 59, 59, 100000000, time.UTC), BucketSeconds: 60}
	ar, e := source.AnalyticsRows(ctx, q)
	if e != nil {
		t.Fatal(e)
	}
	br, e := candidate.AnalyticsRows(ctx, q)
	if e != nil {
		t.Fatal(e)
	}
	ac, e := analytics.Compute(ctx, q, ar)
	if e != nil {
		t.Fatal(e)
	}
	bc, e := analytics.Compute(ctx, q, br)
	if e != nil {
		t.Fatal(e)
	}
	sameJSON(t, ac, bc)
	if len(ac.Services) != 2 || len(ac.Edges) != 1 || ac.Edges[0].Client.Count != 20 {
		t.Fatal("benchmark must exercise nonempty canonical RED and paired edges", ac)
	}
	// Required predecessor lookup and exact budgets use the real Session12 writer.
	raw, e := os.ReadFile("../../artifacts/session12/definitions.json")
	if e != nil {
		t.Fatal(e)
	}
	var definitions []reliability.Definition
	if e = json.Unmarshal(raw, &definitions); e != nil {
		t.Fatal(e)
	}
	cutoff := time.Date(2026, 11, 14, 6, 28, 39, 0, time.UTC)
	checked := false
	for _, d := range definitions {
		if d.Kind != "availability" {
			continue
		}
		checked = true
		mq := metricdata.Query{From: cutoff.Add(-20 * time.Second), To: cutoff, Service: d.Service, Namespace: d.Namespace}
		ap, e := source.MetricPoints(ctx, mq)
		if e != nil {
			t.Fatal(e)
		}
		bp, e := candidate.MetricPoints(ctx, mq)
		if e != nil {
			t.Fatal(e)
		}
		aa, e := metricdata.Aggregate(ctx, mq, ap)
		if e != nil {
			t.Fatal(e)
		}
		bb, e := metricdata.Aggregate(ctx, mq, bp)
		if e != nil {
			t.Fatal(e)
		}
		sameJSON(t, aa, bb)
		ae := reliability.Build(ctx, reliability.MetricSource{Repo: &source}, d, cutoff, 1)
		be := reliability.Build(ctx, reliability.MetricSource{Repo: &candidate}, d, cutoff, 1)
		sameJSON(t, ae.Overall, be.Overall)
		sameJSON(t, ae.Rules, be.Rules)
		if ae.Overall.Evidence.Total != "23" || ae.Overall.Evidence.Bad != "19" || ae.Rules[0].Decision != "BREACH" {
			t.Fatal("reference semantics changed", ae)
		}
	}
	if !checked {
		t.Fatal("availability definition absent")
	}
	// Reuse actual canonical provider run spans, including transport replay.
	providerIDs := []string{"a052d602231c2156995300176889c235", "aa98fb79c488dd620e600a58cacce607", "0edc8d9ed8ed4d19e31d5d188a612d0d"}
	for _, store := range []*storage.Store{&source, &candidate} {
		if e = connection.Conn.Exec(ctx, "INSERT INTO "+store.Database+".spans SELECT * FROM traceforge.spans WHERE trace_id IN (?)", providerIDs); e != nil {
			t.Fatal(e)
		}
	}
	for _, d := range definitions {
		if d.Kind != "provider_attempt" || d.Mode != "fixture" {
			continue
		}
		to := time.Date(2026, 11, 14, 6, 30, 10, 0, time.UTC)
		a := reliability.ProviderSource{Repo: &source}.Measure(ctx, d, to.Add(-30*time.Second), to)
		b := reliability.ProviderSource{Repo: &candidate}.Measure(ctx, d, to.Add(-30*time.Second), to)
		sameJSON(t, a, b)
		if a.Total != "4" || a.Good != "2" {
			t.Fatal(a)
		}
	}
	t.Logf("reference=%s candidate=%s: replay/conflict canonical traces, RED/edges, metric predecessor and23/19 BREACH, provider4/2 unchanged", db, candidateDB)
}
func sameJSON(t *testing.T, a, b any) {
	t.Helper()
	aa, _ := json.Marshal(a)
	bb, _ := json.Marshal(b)
	if string(aa) != string(bb) {
		t.Fatalf("canonical result differs: %.1000s vs %.1000s", aa, bb)
	}
}
