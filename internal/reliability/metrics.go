package reliability

import (
	"context"
	"encoding/json"
	"math/big"
	"time"
	"traceforge/internal/metricdata"
)

type Metrics interface {
	MetricPoints(context.Context, metricdata.Query) ([]metricdata.Point, error)
}
type MetricSource struct{ Repo Metrics }

func unknown(from, to time.Time, status, reason string) Evidence {
	return Evidence{From: from, To: to, Status: status, Reason: reason, Diagnostics: map[string]any{}, TraceIDs: []string{}}
}
func attrs(raw json.RawMessage) map[string]string {
	var a []struct {
		Key   string `json:"key"`
		Value struct {
			String string `json:"stringValue"`
		} `json:"value"`
	}
	_ = json.Unmarshal(raw, &a)
	m := map[string]string{}
	for _, v := range a {
		m[v.Key] = v.Value.String
	}
	return m
}
func unreliable(s metricdata.Series) bool {
	d := s.Diagnostics
	return s.Status != "observed" || d.Conflicts > 0 || d.DefinitionConflict || d.Overlaps > 0 || d.InvalidDecreases > 0 || d.NoValues > 0 || d.UnknownStarts > 0 || d.Overflow
}
func (s MetricSource) Measure(ctx context.Context, d Definition, from, to time.Time) Evidence {
	fail := func(status, reason string) Evidence { return unknown(from, to, status, reason) }
	if to.Sub(from) > time.Hour {
		return fail("INCOMPATIBLE", "raw metric window exceeds Session11 one-hour limit; no unbounded rolling scan")
	}
	if len(d.Writers) == 0 {
		return fail("INSUFFICIENT_EVIDENCE", "expected writer population is undeclared")
	}
	q := metricdata.Query{From: from, To: to, Service: d.Service, Namespace: d.Namespace}
	rows, e := s.Repo.MetricPoints(ctx, q)
	if e != nil {
		return fail("BACKEND_UNAVAILABLE", "metric read unavailable, canceled or over budget")
	}
	r, e := metricdata.Aggregate(ctx, q, rows)
	if e != nil {
		return fail("INCOMPATIBLE", "metric canonicalization/aggregation failed or exceeded budget")
	}
	type input struct{ req, other *metricdata.Series }
	byWriter := map[string]*input{}
	expected := map[string]bool{}
	for _, w := range d.Writers {
		expected[w] = true
	}
	diagnostics := map[string]any{"source": metricdata.Version, "population": "declared writers; completed GET on fixed route; observed exported eligible intervals only", "expected_writers": d.Writers, "quota_rejection_attribution": "no durable per-scope ingestion ledger; expected stream and overflow checks do not prove complete traffic"}
	secondName := "traceforge.requests.errors"
	if d.Kind == "latency" {
		secondName = "traceforge.request.duration"
	}
	for i := range r.Series {
		series := &r.Series[i]
		p := series.Metadata
		if p.Environment != d.Environment || p.Workload != d.Mode || p.Name != "traceforge.requests.completed" && p.Name != secondName {
			continue
		}
		if unreliable(*series) {
			return fail("INSUFFICIENT_EVIDENCE", "conflicting, missing-value, unknown-sequence, overlapping or SDK-overflow metric evidence")
		}
		a := attrs(p.Attributes)
		if a["http.route"] != d.Route || a["http.request.method"] != "GET" {
			continue
		}
		if !expected[p.Instance] {
			return fail("INSUFFICIENT_EVIDENCE", "observed eligible writer is outside the frozen expected population")
		}
		if byWriter[p.Instance] == nil {
			byWriter[p.Instance] = &input{}
		}
		item := byWriter[p.Instance]
		var scope []json.RawMessage
		var identity struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		}
		if json.Unmarshal(p.Scope, &scope) != nil || len(scope) != 2 || json.Unmarshal(scope[0], &identity) != nil || identity.Name != "traceforge.demo" || identity.Version != "0.1.0" {
			return fail("INCOMPATIBLE", "unsupported instrumentation scope")
		}
		if p.Name == "traceforge.requests.completed" {
			if item.req != nil || p.Type != "sum" || !p.Monotonic || p.Unit != "{request}" {
				return fail("INCOMPATIBLE", "ambiguous/incompatible request stream")
			}
			item.req = series
		} else {
			if item.other != nil {
				return fail("INCOMPATIBLE", "ambiguous counterpart stream")
			}
			if d.Kind == "availability" && (p.Type != "sum" || !p.Monotonic || p.Unit != "{request}") || d.Kind == "latency" && (p.Type != "histogram" || p.Unit != "s") {
				return fail("INCOMPATIBLE", "counter/histogram type or unit mismatch")
			}
			item.other = series
		}
	}
	total, bad := new(big.Int), new(big.Int)
	minimumCoverage := big.NewRat(1, 1)
	allDetails := map[string]any{}
	for _, writer := range d.Writers {
		item := byWriter[writer]
		if item == nil || item.req == nil || item.other == nil {
			return fail("INSUFFICIENT_EVIDENCE", "required writer request/counterpart stream is missing")
		}
		a, b := item.req, item.other
		if string(a.Metadata.Attributes) != string(b.Metadata.Attributes) {
			return fail("INCOMPATIBLE", "request/counterpart dimensions differ")
		}
		if len(a.Intervals) != len(b.Intervals) {
			return fail("INSUFFICIENT_EVIDENCE", "request/counterpart eligible interval sets differ")
		}
		bm := map[[2]uint64]metricdata.Interval{}
		for _, v := range b.Intervals {
			bm[[2]uint64{v.Start, v.End}] = v
		}
		covered := int64(0)
		nt, nb := new(big.Int), new(big.Int)
		for _, iv := range a.Intervals {
			other, ok := bm[[2]uint64{iv.Start, iv.End}]
			if !ok {
				return fail("INSUFFICIENT_EVIDENCE", "numerator/denominator intervals are not identical")
			}
			n, ok := new(big.Int).SetString(iv.Value, 10)
			if !ok || n.Sign() < 0 {
				return fail("INCOMPATIBLE", "request increments must be nonnegative exact event counts")
			}
			var errors *big.Int
			if d.Kind == "availability" {
				errors, ok = new(big.Int).SetString(other.Value, 10)
				if !ok || errors.Sign() < 0 || errors.Cmp(n) > 0 {
					return fail("INCOMPATIBLE", "bad events exceed eligible matched request events")
				}
			} else {
				count, ok := new(big.Int).SetString(other.Count, 10)
				if !ok || count.Cmp(n) != 0 {
					return fail("INCOMPATIBLE", "histogram count does not match completed-request population")
				}
				threshold, _ := rat(d.ThresholdSeconds)
				index := -1
				for k, bound := range b.Metadata.Bounds {
					br := new(big.Rat).SetFloat64(bound)
					tf, _ := threshold.Float64()
					if bound == tf || br.Cmp(threshold) == 0 {
						index = k
						break
					}
				}
				if index < 0 {
					return fail("INCOMPATIBLE", "latency threshold is not an actual explicit bucket boundary; exact classification unavailable")
				}
				good := new(big.Int)
				for k := 0; k <= index; k++ {
					if k >= len(other.Buckets) {
						return fail("INCOMPATIBLE", "histogram boundary/count mismatch")
					}
					v, ok := new(big.Int).SetString(other.Buckets[k], 10)
					if !ok || v.Sign() < 0 {
						return fail("INCOMPATIBLE", "invalid histogram interval bucket")
					}
					good.Add(good, v)
				}
				errors = new(big.Int).Sub(n, good)
				if errors.Sign() < 0 {
					return fail("INCOMPATIBLE", "histogram good events exceed request count")
				}
			}
			nt.Add(nt, n)
			nb.Add(nb, errors)
			covered += int64(iv.End - iv.Start)
		}
		coverage := new(big.Rat).SetFrac(big.NewInt(covered), big.NewInt(to.Sub(from).Nanoseconds()))
		if coverage.Cmp(minimumCoverage) < 0 {
			minimumCoverage = coverage
		}
		min, _ := rat(d.MinCoverage)
		if coverage.Cmp(min) < 0 {
			return fail("INSUFFICIENT_EVIDENCE", "declared writer interval coverage below policy minimum")
		}
		total.Add(total, nt)
		bad.Add(bad, nb)
		allDetails[writer] = map[string]any{"total": nt.String(), "bad": nb.String(), "covered_nanoseconds": covered, "request": a.Diagnostics, "counterpart": b.Diagnostics, "intervals": a.Intervals}
	}
	ev := unknown(from, to, "MEASURED", "")
	ev.Total = total.String()
	ev.Bad = bad.String()
	ev.Good = new(big.Int).Sub(total, bad).String()
	ev.Coverage, _ = minimumCoverage.Float64()
	ev.Diagnostics = diagnostics
	ev.Details = allDetails
	if total.Sign() == 0 {
		ev.Status = "NO_OBSERVATIONS"
		ev.Reason = "no eligible completed operations in supported intervals"
	} else if total.Cmp(big.NewInt(int64(d.MinCount))) < 0 {
		ev.Status = "INSUFFICIENT_EVIDENCE"
		ev.Reason = "too few eligible observations"
	}
	return ev
}
