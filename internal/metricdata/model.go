// Package metricdata defines the supported OTLP metric contract, independently
// from trace-derived analytics and receiver self-monitoring.
package metricdata

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	collector "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	common "go.opentelemetry.io/proto/otlp/common/v1"
	mp "go.opentelemetry.io/proto/otlp/metrics/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"traceforge/internal/ingest"
)

const Version = "otlp-metrics/v1"
const MaxPoints = 2048
const MaxBytes = 4 << 20
const MaxSeries = 4096
const MaxQuerySeries = 32
const MaxQueryRows = 12000

var ErrBudget = errors.New("metric budget exceeded")
var ErrBackend = errors.New("metric storage unavailable")
var ErrInvalid = errors.New("invalid metric data")

type Point struct {
	StreamID       string          `json:"stream_id"`
	StreamKey      string          `json:"stream_key"`
	DefinitionKey  string          `json:"definition_key"`
	BaseID         string          `json:"base_id"`
	Service        string          `json:"service"`
	Namespace      string          `json:"namespace"`
	Environment    string          `json:"environment"`
	Workload       string          `json:"workload"`
	Instance       string          `json:"instance"`
	Name           string          `json:"name"`
	Unit           string          `json:"unit"`
	Description    string          `json:"description"`
	Type           string          `json:"type"`
	Temporality    int32           `json:"temporality"`
	Monotonic      bool            `json:"monotonic"`
	Resource       json.RawMessage `json:"resource"`
	Scope          json.RawMessage `json:"scope"`
	Attributes     json.RawMessage `json:"attributes"`
	ResourceSchema string          `json:"resource_schema"`
	ScopeSchema    string          `json:"scope_schema"`
	Start          uint64          `json:"start_ns,string"`
	End            uint64          `json:"end_ns,string"`
	Int            *string         `json:"int_value"`
	Double         *float64        `json:"double_value"`
	Count          string          `json:"count"`
	Sum            *float64        `json:"sum"`
	Bounds         []float64       `json:"bounds"`
	Buckets        []string        `json:"buckets"`
	Min            *float64        `json:"min"`
	Max            *float64        `json:"max"`
	Flags          uint32          `json:"flags"`
	Overflow       bool            `json:"overflow"`
	IngestedAt     time.Time       `json:"ingested_at"`
}

func Hash(s string) string  { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }
func encoded(v any) string  { b, _ := json.Marshal(v); return string(b) }
func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }

var sensitive = regexp.MustCompile(`(?i)(password|passwd|authorization|cookie|api.?key|token|secret|prompt|request.?id|user.?id|trace.?id|span.?id|email|phone|private.?key|credential)`)

func text(s string, n int) bool {
	return len(s) <= n && utf8.ValidString(s) && ingest.SanitizeText(s) == s && !strings.Contains(s, "-----BEGIN")
}

// Reject sensitive identity attributes rather than redacting them into collisions.
// V1 accepts bounded scalar values only; nested AnyValues are explicit rejections.
func attrs(a []*common.KeyValue) (json.RawMessage, map[string]string, error) {
	if len(a) > 32 {
		return nil, nil, ErrInvalid
	}
	a = append([]*common.KeyValue{}, a...)
	sort.Slice(a, func(i, j int) bool {
		if a[i] == nil {
			return a[j] != nil
		}
		if a[j] == nil {
			return false
		}
		return a[i].Key < a[j].Key
	})
	parts := []json.RawMessage{}
	values := map[string]string{}
	total := 0
	for _, kv := range a {
		if kv == nil || kv.Value == nil || !text(kv.Key, 128) || sensitive.MatchString(kv.Key) {
			return nil, nil, ErrInvalid
		}
		if _, ok := values[kv.Key]; ok {
			return nil, nil, ErrInvalid
		}
		switch v := kv.Value.Value.(type) {
		case *common.AnyValue_StringValue:
			if !text(v.StringValue, 256) || sensitive.MatchString(v.StringValue) {
				return nil, nil, ErrInvalid
			}
			values[kv.Key] = v.StringValue
		case *common.AnyValue_IntValue:
			values[kv.Key] = strconv.FormatInt(v.IntValue, 10)
		case *common.AnyValue_DoubleValue:
			if !finite(v.DoubleValue) {
				return nil, nil, ErrInvalid
			}
			values[kv.Key] = strconv.FormatFloat(v.DoubleValue, 'g', -1, 64)
		case *common.AnyValue_BoolValue:
			values[kv.Key] = strconv.FormatBool(v.BoolValue)
		default:
			return nil, nil, ErrInvalid
		}
		b, e := protojson.Marshal(kv)
		if e != nil {
			return nil, nil, e
		}
		total += len(b)
		if total > 8192 {
			return nil, nil, ErrInvalid
		}
		parts = append(parts, b)
	}
	return json.RawMessage(encoded(parts)), values, nil
}
func supportedCount(m *mp.Metric) int {
	if m == nil {
		return 0
	}
	switch d := m.Data.(type) {
	case *mp.Metric_Gauge:
		return len(d.Gauge.GetDataPoints())
	case *mp.Metric_Sum:
		return len(d.Sum.GetDataPoints())
	case *mp.Metric_Histogram:
		return len(d.Histogram.GetDataPoints())
	case *mp.Metric_ExponentialHistogram:
		return len(d.ExponentialHistogram.GetDataPoints())
	case *mp.Metric_Summary:
		return len(d.Summary.GetDataPoints())
	}
	return 0
}

// Normalize does not call recursive protobuf sizing before checking attribute
// shape. gRPC already caps wire bytes; nested values are rejected untraversed.
func Normalize(req *collector.ExportMetricsServiceRequest) ([]Point, int, error) {
	if req == nil {
		return nil, 0, ErrInvalid
	}
	count := 0
	groups := 0
	for _, r := range req.ResourceMetrics {
		if r == nil {
			continue
		}
		groups++
		for _, s := range r.ScopeMetrics {
			if s == nil {
				continue
			}
			groups++
			for _, m := range s.Metrics {
				count += supportedCount(m)
				groups++
				if count > MaxPoints || groups > 4096 {
					return nil, 0, ErrBudget
				}
			}
		}
	}
	if groups > 4096 {
		return nil, 0, ErrBudget
	}
	result := []Point{}
	rejected := 0
	size := 0
	for _, r := range req.ResourceMetrics {
		if r == nil {
			continue
		}
		var ra []*common.KeyValue
		if r.Resource != nil {
			ra = r.Resource.Attributes
		}
		resource, rv, re := attrs(ra)
		// Semantic identity fields are strings. Keep other scalar attributes
		// typed, but never coerce an integer service/writer into a string identity.
		for _, kv := range ra {
			if kv == nil || kv.Value == nil {
				continue
			}
			switch kv.Key {
			case "service.name", "service.instance.id", "service.namespace", "deployment.environment.name", "traceforge.workload.kind":
				if _, ok := kv.Value.Value.(*common.AnyValue_StringValue); !ok {
					re = ErrInvalid
				}
			}
		}
		if r.Resource != nil && (r.Resource.DroppedAttributesCount > 0 || len(r.Resource.EntityRefs) > 0) {
			re = ErrInvalid
		}
		for _, s := range r.ScopeMetrics {
			if s == nil {
				continue
			}
			scope := s.Scope
			if scope == nil {
				scope = &common.InstrumentationScope{}
			}
			sa, _, se := attrs(scope.Attributes)
			scopeCopy := &common.InstrumentationScope{Name: scope.Name, Version: scope.Version, DroppedAttributesCount: scope.DroppedAttributesCount}
			scopeJSON, _ := protojson.Marshal(scopeCopy)
			scopeKey := encoded([]any{json.RawMessage(scopeJSON), sa})
			for _, m := range s.Metrics {
				if m == nil {
					continue
				}
				n := supportedCount(m)
				if re != nil || se != nil || scope.DroppedAttributesCount > 0 || rv["service.name"] == "" || rv["service.instance.id"] == "" || !text(m.Name, 128) || m.Name == "" || !text(m.Unit, 64) || !text(m.Description, 512) || !text(scope.Name, 128) || !text(scope.Version, 128) || !text(r.SchemaUrl, 256) || !text(s.SchemaUrl, 256) || len(m.Metadata) > 0 || sensitive.MatchString(m.Name+" "+m.Unit+" "+m.Description+" "+scope.Name+" "+scope.Version+" "+r.SchemaUrl+" "+s.SchemaUrl) {
					rejected += n
					continue
				}
				base := Point{Service: rv["service.name"], Namespace: rv["service.namespace"], Environment: rv["deployment.environment.name"], Workload: rv["traceforge.workload.kind"], Instance: rv["service.instance.id"], Name: m.Name, Unit: m.Unit, Description: m.Description, Resource: resource, Scope: json.RawMessage(scopeKey), ResourceSchema: r.SchemaUrl, ScopeSchema: s.SchemaUrl, Bounds: []float64{}, Buckets: []string{}}
				var numbers []*mp.NumberDataPoint
				var histograms []*mp.HistogramDataPoint
				switch d := m.Data.(type) {
				case *mp.Metric_Gauge:
					base.Type = "gauge"
					numbers = d.Gauge.GetDataPoints()
				case *mp.Metric_Sum:
					base.Type = "sum"
					base.Temporality = int32(d.Sum.GetAggregationTemporality())
					base.Monotonic = d.Sum.GetIsMonotonic()
					numbers = d.Sum.GetDataPoints()
				case *mp.Metric_Histogram:
					base.Type = "histogram"
					base.Temporality = int32(d.Histogram.GetAggregationTemporality())
					histograms = d.Histogram.GetDataPoints()
				default:
					rejected += n
					continue
				}
				accept := func(p Point, a []*common.KeyValue, exemplars int) {
					var av map[string]string
					var e error
					p.Attributes, av, e = attrs(a)
					if e != nil || exemplars > 0 || p.End == 0 || p.End > math.MaxInt64 || p.Start > p.End || (p.Type != "gauge" && (p.Temporality < 1 || p.Temporality > 2 || p.Temporality == 1 && (p.Start == 0 || p.Start == p.End))) || p.Flags&^uint32(1) != 0 {
						rejected++
						return
					}
					p.Overflow = av["otel.metric.overflow"] == "true"
					for _, kv := range a {
						if kv.Key == "otel.metric.overflow" {
							v, ok := kv.Value.Value.(*common.AnyValue_BoolValue)
							if !ok || !v.BoolValue {
								rejected++
								return
							}
						}
					}
					p.BaseID = Hash(encoded([]any{p.Resource, p.Scope, p.ResourceSchema, p.ScopeSchema, p.Name}))
					p.DefinitionKey = encoded([]any{p.Type, p.Unit, p.Temporality, p.Monotonic})
					p.StreamKey = encoded([]any{p.Resource, p.Scope, p.ResourceSchema, p.ScopeSchema, p.Name, p.DefinitionKey, p.Attributes})
					p.StreamID = Hash(p.StreamKey)
					b := encoded(p)
					size += len(b)
					if size > MaxBytes {
						return
					}
					result = append(result, p)
				}
				for _, d := range numbers {
					if d == nil {
						rejected++
						continue
					}
					p := base
					p.Start = d.StartTimeUnixNano
					p.End = d.TimeUnixNano
					p.Flags = d.Flags
					switch v := d.Value.(type) {
					case *mp.NumberDataPoint_AsInt:
						x := strconv.FormatInt(v.AsInt, 10)
						p.Int = &x
						if p.Monotonic && v.AsInt < 0 {
							rejected++
							continue
						}
					case *mp.NumberDataPoint_AsDouble:
						if !finite(v.AsDouble) || p.Monotonic && v.AsDouble < 0 {
							rejected++
							continue
						}
						x := v.AsDouble
						p.Double = &x
					default:
						if p.Flags&1 == 0 {
							rejected++
							continue
						}
					}
					accept(p, d.Attributes, len(d.Exemplars))
				}
				for _, d := range histograms {
					if d == nil {
						rejected++
						continue
					}
					p := base
					p.Start = d.StartTimeUnixNano
					p.End = d.TimeUnixNano
					p.Flags = d.Flags
					p.Count = strconv.FormatUint(d.Count, 10)
					p.Sum = d.Sum
					p.Min = d.Min
					p.Max = d.Max
					p.Bounds = append([]float64{}, d.ExplicitBounds...)
					p.Buckets = []string{}
					bad := len(p.Bounds) > 64 || len(d.BucketCounts) != len(p.Bounds)+1 && !(p.Flags&1 != 0 && len(d.BucketCounts) == 0 && len(p.Bounds) == 0)
					total := uint64(0)
					for i, b := range p.Bounds {
						if !finite(b) || i > 0 && b <= p.Bounds[i-1] {
							bad = true
						}
					}
					for _, v := range d.BucketCounts {
						if math.MaxUint64-total < v {
							bad = true
						}
						total += v
						p.Buckets = append(p.Buckets, strconv.FormatUint(v, 10))
					}
					if total != d.Count {
						bad = true
					}
					for _, v := range []*float64{p.Sum, p.Min, p.Max} {
						if v != nil && !finite(*v) {
							bad = true
						}
					}
					if p.Min != nil && p.Max != nil && *p.Min > *p.Max {
						bad = true
					}
					if d.Count == 0 && (p.Sum != nil && *p.Sum != 0 || p.Min != nil || p.Max != nil) {
						bad = true
					}
					if bad {
						rejected++
						continue
					}
					accept(p, d.Attributes, len(d.Exemplars))
				}
				if size > MaxBytes {
					return nil, 0, ErrBudget
				}
			}
		}
	}
	return result, rejected, nil
}
