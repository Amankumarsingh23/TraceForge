package metricdata

import (
	mp "go.opentelemetry.io/proto/otlp/metrics/v1"
	"testing"
)

func TestSession14SensitiveMetricMetadataRejectsInsteadOfMerging(t *testing.T) {
	for _, field := range []string{"description", "name", "scope", "email-dimension"} {
		metric := sumMetric(mp.AggregationTemporality_AGGREGATION_TEMPORALITY_DELTA, true, counter(0, 1, 1))
		req := request(metric)
		switch field {
		case "description":
			metric.Description = "token=S14_MARKER"
		case "name":
			metric.Name = "credential.metric"
		case "scope":
			req.ResourceMetrics[0].ScopeMetrics[0].Scope.Name = "api_key=S14_MARKER"
		case "email-dimension":
			metric.GetSum().DataPoints[0].Attributes = append(metric.GetSum().DataPoints[0].Attributes, kv("user.email", "synthetic@example.test"))
		}
		rows, rejected, e := Normalize(req)
		if e != nil || len(rows) != 0 || rejected != 1 {
			t.Fatal(field, len(rows), rejected, e)
		}
	}
}
