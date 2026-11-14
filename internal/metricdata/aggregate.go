package metricdata

import (
	"context"
	"math"
	"math/big"
	"sort"
	"strconv"
	"time"
)

type Query struct {
	From      time.Time `json:"from"`
	To        time.Time `json:"to"`
	Name      string    `json:"name"`
	Service   string    `json:"service"`
	Namespace string    `json:"namespace"`
	StreamID  string    `json:"stream_id"`
}

func (q Query) Valid() bool {
	return !q.From.Before(time.Unix(0, 0)) && q.To.After(q.From) && q.To.Sub(q.From) <= time.Hour && !q.To.After(time.Unix(0, math.MaxInt64)) && len(q.Name) <= 128 && len(q.Service) <= 128 && len(q.Namespace) <= 128 && (q.StreamID == "" || len(q.StreamID) == 64)
}

type Diagnostics struct {
	Physical           int  `json:"physical_points"`
	Canonical          int  `json:"canonical_points"`
	Replays            int  `json:"replays"`
	Conflicts          int  `json:"conflicts"`
	Resets             int  `json:"resets"`
	Gaps               int  `json:"gaps"`
	Overlaps           int  `json:"overlaps"`
	Baselines          int  `json:"baselines"`
	UnknownStarts      int  `json:"unknown_sequence_start_points"`
	InvalidDecreases   int  `json:"invalid_decreases"`
	BoundaryIntervals  int  `json:"boundary_intervals"`
	NoValues           int  `json:"no_recorded_values"`
	Overflow           bool `json:"overflow"`
	DefinitionConflict bool `json:"definition_conflict"`
}
type Interval struct {
	Start   uint64   `json:"start_ns,string"`
	End     uint64   `json:"end_ns,string"`
	Value   string   `json:"increment"`
	Rate    *float64 `json:"rate_per_second"`
	Count   string   `json:"count"`
	Sum     *float64 `json:"sum"`
	Buckets []string `json:"buckets"`
}
type Quantile struct {
	Lower   *float64 `json:"lower"`
	Upper   *float64 `json:"upper"`
	Meaning string   `json:"meaning"`
}
type Series struct {
	Metadata         Point       `json:"metadata"`
	Points           []Point     `json:"points"`
	Intervals        []Interval  `json:"intervals"`
	Diagnostics      Diagnostics `json:"diagnostics"`
	Increment        *string     `json:"increment"`
	ObservedSeconds  float64     `json:"observed_seconds"`
	ObservedRate     *float64    `json:"observed_rate_per_second"`
	CoverageFraction float64     `json:"coverage_fraction"`
	Complete         bool        `json:"complete"`
	Latest           *string     `json:"latest"`
	Minimum          *float64    `json:"minimum_observation"`
	Maximum          *float64    `json:"maximum_observation"`
	Mean             *float64    `json:"mean_observation"`
	HistogramCount   *string     `json:"histogram_count"`
	HistogramSum     *float64    `json:"histogram_sum"`
	HistogramMean    *float64    `json:"histogram_mean"`
	HistogramBuckets []string    `json:"histogram_buckets"`
	P50              *Quantile   `json:"p50_bucket"`
	P95              *Quantile   `json:"p95_bucket"`
	P99              *Quantile   `json:"p99_bucket"`
	Status           string      `json:"status"`
}
type Result struct {
	Version         string       `json:"version"`
	Query           Query        `json:"query"`
	Series          []Series     `json:"series"`
	MergedHistogram *Series      `json:"merged_histogram"`
	MergeStatus     string       `json:"merge_status"`
	Caveat          string       `json:"caveat"`
	CombinedSum     *CombinedSum `json:"combined_sum"`
	SumMergeStatus  string       `json:"sum_merge_status"`
}
type CombinedSum struct {
	Increment string   `json:"increment"`
	Rate      *float64 `json:"rate_per_second"`
	Sources   int      `json:"source_streams"`
	Coverage  string   `json:"coverage"`
}

func number(p Point) *big.Rat {
	if p.Int != nil {
		r, _ := new(big.Rat).SetString(*p.Int)
		return r
	}
	if p.Double != nil {
		return new(big.Rat).SetFloat64(*p.Double)
	}
	return nil
}
func rational(r *big.Rat) string {
	if r.IsInt() {
		return r.Num().String()
	}
	// Keep small floating-point measurements visible; fixed decimal places can
	// silently turn legitimate sub-micro measurements into zero.
	return new(big.Float).SetPrec(128).SetRat(r).Text('g', 17)
}
func uints(values []string) ([]*big.Int, bool) {
	out := []*big.Int{}
	for _, v := range values {
		x, ok := new(big.Int).SetString(v, 10)
		if !ok || x.Sign() < 0 {
			return nil, false
		}
		out = append(out, x)
	}
	return out, true
}
func stringsOf(values []*big.Int) []string {
	out := []string{}
	for _, v := range values {
		out = append(out, v.String())
	}
	return out
}
func quantile(bounds []float64, counts []*big.Int, total *big.Int, p int) *Quantile {
	if total.Sign() == 0 {
		return nil
	}
	rank := new(big.Int).Mul(total, big.NewInt(int64(p)))
	rank.Add(rank, big.NewInt(99))
	rank.Div(rank, big.NewInt(100))
	sum := new(big.Int)
	for i, c := range counts {
		sum.Add(sum, c)
		if sum.Cmp(rank) >= 0 {
			q := &Quantile{Meaning: "nearest-rank bucket range, not an exact percentile; null endpoint is unbounded"}
			if i > 0 {
				x := bounds[i-1]
				q.Lower = &x
			}
			if i < len(bounds) {
				x := bounds[i]
				q.Upper = &x
			}
			return q
		}
	}
	return nil
}
func histogramSummary(s *Series, bounds []float64, counts []*big.Int, sum *float64) {
	total := new(big.Int)
	for _, c := range counts {
		total.Add(total, c)
	}
	v := total.String()
	s.HistogramCount = &v
	s.HistogramBuckets = stringsOf(counts)
	s.HistogramSum = sum
	if sum != nil && total.Sign() > 0 {
		n, _ := new(big.Float).SetInt(total).Float64()
		m := *sum / n
		s.HistogramMean = &m
	}
	s.P50 = quantile(bounds, counts, total, 50)
	s.P95 = quantile(bounds, counts, total, 95)
	s.P99 = quantile(bounds, counts, total, 99)
}
func canonical(rows []Point) ([]Point, Diagnostics, error) {
	d := Diagnostics{Physical: len(rows)}
	groups := map[string]Point{}
	contents := map[string]string{}
	variants := map[string]map[string]bool{}
	for _, p := range rows {
		if Hash(p.StreamKey) != p.StreamID {
			return nil, d, ErrInvalid
		}
		key := strconv.FormatUint(p.End, 10)
		if p.Temporality == 1 {
			key += "/" + strconv.FormatUint(p.Start, 10)
		}
		v := p
		v.IngestedAt = time.Time{}
		content := encoded(v)
		if variants[key] == nil {
			variants[key] = map[string]bool{}
		}
		if variants[key][content] {
			d.Replays++
		} else if len(variants[key]) == 1 {
			d.Conflicts++
		}
		variants[key][content] = true
		old, ok := groups[key]
		if ok {
			if p.IngestedAt.After(old.IngestedAt) || p.IngestedAt.Equal(old.IngestedAt) && content >= contents[key] {
				continue
			}
		}
		groups[key] = p
		contents[key] = content
	}
	result := []Point{}
	for _, p := range groups {
		result = append(result, p)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].End == result[j].End {
			return result[i].Start < result[j].Start
		}
		return result[i].End < result[j].End
	})
	d.Canonical = len(result)
	return result, d, nil
}

// Aggregate normalizes every source stream separately. Intervals must be fully
// inside (from,to] for exact contribution; boundary crossings are diagnosed.
func Aggregate(ctx context.Context, q Query, rows []Point) (*Result, error) {
	if !q.Valid() {
		return nil, ErrInvalid
	}
	if len(rows) > MaxQueryRows {
		return nil, ErrBudget
	}
	r := &Result{Version: Version, Query: q, Series: []Series{}, MergeStatus: "no compatible histogram observations", Caveat: "Observed OTLP points; no complete-traffic guarantee. Points use (from,to]; increments use fully covered intervals [start,end]. Predecessors are bounded to one hour before from. Rates divide by observed covered seconds, not an assumed full window. Two reads are not a snapshot; gauge latest is only latest within the selected window."}
	grouped := map[string][]Point{}
	defs := map[string]map[string]bool{}
	keys := map[string]string{}
	bytes := 0
	for _, p := range rows {
		if e := ctx.Err(); e != nil {
			return nil, e
		}
		bytes += len(encoded(p))
		if bytes > 16<<20 {
			return nil, ErrBudget
		}
		if key, ok := keys[p.StreamID]; ok && key != p.StreamKey {
			return nil, ErrInvalid
		}
		keys[p.StreamID] = p.StreamKey
		grouped[p.StreamID] = append(grouped[p.StreamID], p)
		if defs[p.BaseID] == nil {
			defs[p.BaseID] = map[string]bool{}
		}
		defs[p.BaseID][p.DefinitionKey] = true
	}
	if len(grouped) > MaxQuerySeries {
		return nil, ErrBudget
	}
	ids := []string{}
	for id := range grouped {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	from, to := uint64(q.From.UnixNano()), uint64(q.To.UnixNano())
	for _, id := range ids {
		points, d, e := canonical(grouped[id])
		if e != nil {
			return nil, e
		}
		if len(points) == 0 {
			continue
		}
		meta := points[0]
		s := Series{Metadata: meta, Points: []Point{}, Intervals: []Interval{}, Diagnostics: d, HistogramBuckets: []string{}, Status: "observed"}
		s.Diagnostics.DefinitionConflict = len(defs[meta.BaseID]) > 1
		total := new(big.Rat)
		hasTotal := false
		counts := []*big.Int{}
		sum := 0.0
		sumKnown := true
		bounds := []float64{}
		histInitialized := false
		gaugeMean := 0.0
		gaugeN := 0
		var prev *Point
		lastEnd := uint64(0)
		// Delta overlap detection excludes every member of an overlap component.
		overlapping := map[int]bool{}
		if meta.Temporality == 1 {
			order := make([]int, len(points))
			for i := range order {
				order[i] = i
			}
			sort.Slice(order, func(i, j int) bool { return points[order[i]].Start < points[order[j]].Start })
			component := []int{}
			end := uint64(0)
			flush := func() {
				if len(component) > 1 {
					for _, i := range component {
						overlapping[i] = true
					}
				}
			}
			for _, i := range order {
				if len(component) > 0 && points[i].Start >= end {
					flush()
					component = nil
					end = 0
				}
				component = append(component, i)
				if points[i].End > end {
					end = points[i].End
				}
			}
			flush()
		}
		for i := range points {
			p := points[i]
			if e := ctx.Err(); e != nil {
				return nil, e
			}
			if p.End > to {
				continue
			}
			in := p.End > from
			if in && meta.Temporality == 2 && p.Start == 0 {
				s.Diagnostics.UnknownStarts++
			}
			s.Diagnostics.Overflow = s.Diagnostics.Overflow || p.Overflow
			if in {
				s.Points = append(s.Points, p)
			}
			if p.Flags&1 != 0 {
				if in {
					s.Diagnostics.NoValues++
					if meta.Type == "gauge" {
						s.Latest = nil
					}
				}
				// A missing export is not a new zero accumulation sequence. Retain
				// the preceding valid cumulative baseline for a compatible recovery.
				continue
			}
			if meta.Type == "gauge" {
				if in {
					v := number(p)
					if v == nil {
						return nil, ErrInvalid
					}
					str := rational(v)
					s.Latest = &str
					x, _ := v.Float64()
					gaugeN++
					gaugeMean = gaugeMean*(float64(gaugeN-1)/float64(gaugeN)) + x/float64(gaugeN)
					if s.Minimum == nil || x < *s.Minimum {
						v := x
						s.Minimum = &v
					}
					if s.Maximum == nil || x > *s.Maximum {
						v := x
						s.Maximum = &v
					}
				}
				continue
			}
			start := p.Start
			if in && meta.Type == "sum" && !meta.Monotonic && meta.Temporality == 2 {
				if value := number(p); value != nil {
					v := rational(value)
					s.Latest = &v
				}
			}
			var before *Point
			usable := true
			if meta.Temporality == 2 {
				if prev == nil {
					if p.Start == 0 || p.Start < from || p.Start == p.End {
						if in {
							s.Diagnostics.Baselines++
						}
						prev = &points[i]
						continue
					}
				} else if p.Start == prev.Start {
					start = prev.End
					before = prev
				} else if p.Start >= prev.End && p.Start < p.End {
					if in {
						s.Diagnostics.Resets++
					}
				} else {
					if in {
						s.Diagnostics.Overlaps++
					}
					usable = false
				}
			} else if overlapping[i] {
				if in {
					s.Diagnostics.Overlaps++
				}
				usable = false
			}
			if in && start < from {
				s.Diagnostics.BoundaryIntervals++
				usable = false
			}
			if start >= p.End {
				usable = false
			}
			if !in {
				prev = &points[i]
				continue
			}
			if (lastEnd > 0 && start > lastEnd) || (lastEnd == 0 && start > from) {
				s.Diagnostics.Gaps++
			}
			interval := Interval{Start: start, End: p.End, Buckets: []string{}}
			if meta.Type == "sum" {
				v := number(p)
				if v == nil {
					return nil, ErrInvalid
				}
				if before != nil {
					b := number(*before)
					if b == nil {
						return nil, ErrInvalid
					}
					v.Sub(v, b)
				}
				if meta.Monotonic && v.Sign() < 0 {
					s.Diagnostics.InvalidDecreases++
					usable = false
				}
				interval.Value = rational(v)
				seconds := float64(p.End-start) / 1e9
				x, _ := v.Float64()
				rate := x / seconds
				if finite(rate) {
					interval.Rate = &rate
				}
				if usable {
					total.Add(total, v)
					hasTotal = true
				}
			}
			if meta.Type == "histogram" {
				cs, ok := uints(p.Buckets)
				if !ok || len(cs) != len(p.Bounds)+1 {
					return nil, ErrInvalid
				}
				partialSum := p.Sum
				if before != nil {
					if encoded(before.Bounds) != encoded(p.Bounds) {
						s.Status = "incompatible histogram boundaries"
						usable = false
					} else {
						bs, ok := uints(before.Buckets)
						if !ok || len(bs) != len(cs) {
							return nil, ErrInvalid
						}
						for k := range cs {
							cs[k].Sub(cs[k], bs[k])
							if cs[k].Sign() < 0 {
								s.Diagnostics.InvalidDecreases++
								usable = false
							}
						}
						if p.Sum != nil && before.Sum != nil {
							v := *p.Sum - *before.Sum
							partialSum = &v
						} else {
							partialSum = nil
						}
					}
				}
				interval.Buckets = stringsOf(cs)
				n := new(big.Int)
				for _, c := range cs {
					n.Add(n, c)
				}
				interval.Count = n.String()
				interval.Sum = partialSum
				if usable {
					if !histInitialized {
						bounds = append([]float64{}, p.Bounds...)
						for range cs {
							counts = append(counts, new(big.Int))
						}
						histInitialized = true
					}
					if encoded(bounds) != encoded(p.Bounds) {
						s.Status = "incompatible histogram boundaries"
						usable = false
					} else {
						for k, c := range cs {
							counts[k].Add(counts[k], c)
						}
						if partialSum == nil {
							sumKnown = false
						} else {
							sum += *partialSum
						}
					}
				}
			}
			if usable {
				s.Intervals = append(s.Intervals, interval)
				s.ObservedSeconds += float64(p.End-start) / 1e9
				lastEnd = p.End
			}
			prev = &points[i]
		}
		if gaugeN > 0 {
			if finite(gaugeMean) {
				s.Mean = &gaugeMean
			}
		}
		if hasTotal {
			v := rational(total)
			s.Increment = &v
			if s.ObservedSeconds > 0 {
				x, _ := total.Float64()
				v := x / s.ObservedSeconds
				if finite(v) {
					s.ObservedRate = &v
				}
			}
		}
		if histInitialized {
			var sp *float64
			if sumKnown && finite(sum) {
				sp = &sum
			}
			histogramSummary(&s, bounds, counts, sp)
		}
		s.CoverageFraction = s.ObservedSeconds / q.To.Sub(q.From).Seconds()
		s.Complete = meta.Type != "gauge" && s.CoverageFraction >= 1-1e-9 && d.Conflicts == 0 && !s.Diagnostics.DefinitionConflict && !s.Diagnostics.Overflow && s.Diagnostics.InvalidDecreases == 0 && s.Diagnostics.Overlaps == 0 && s.Diagnostics.NoValues == 0
		if d.Conflicts > 0 || s.Diagnostics.DefinitionConflict {
			s.Status = "unreliable: conflicting observations/definitions"
			s.Complete = false
		}
		if s.Diagnostics.InvalidDecreases > 0 {
			s.Status = "unreliable: decrease without a confirmed reset"
			s.Complete = false
		}
		if s.Diagnostics.Overflow {
			s.Status = "SDK overflow: dimension coverage unreliable"
			s.Complete = false
		}
		if s.Diagnostics.UnknownStarts > 0 {
			s.Status = "unreliable: unknown accumulation start; adjacent differences are conditional"
			s.Complete = false
		}
		r.Series = append(r.Series, s)
	}
	// Only combine matching attributed Sum populations, after per-writer conversion.
	var sumMeta *Point
	sumTotal := new(big.Rat)
	sumSources := 0
	allCovered := true
	sumOK := true
	sumWriters := map[string]bool{}
	r.SumMergeStatus = "no compatible Sum observations"
	for _, s := range r.Series {
		if s.Metadata.Type != "sum" {
			continue
		}
		m := s.Metadata
		if sumMeta == nil {
			v := m
			sumMeta = &v
		} else {
			a := *sumMeta
			if m.Name != a.Name || m.Service != a.Service || m.Namespace != a.Namespace || m.Environment != a.Environment || m.Workload != a.Workload || m.Unit != a.Unit || m.Monotonic != a.Monotonic || string(m.Scope) != string(a.Scope) || string(m.Attributes) != string(a.Attributes) {
				sumOK = false
			}
		}
		if s.Increment == nil || s.Status != "observed" || sumWriters[m.Instance] {
			sumOK = false
			continue
		}
		v, ok := new(big.Rat).SetString(*s.Increment)
		if !ok {
			return nil, ErrInvalid
		}
		sumTotal.Add(sumTotal, v)
		sumSources++
		sumWriters[m.Instance] = true
		allCovered = allCovered && s.Complete
	}
	if sumOK && sumSources > 0 {
		v := &CombinedSum{Increment: rational(sumTotal), Sources: sumSources, Coverage: "increments from independently normalized writers; source coverage remains per series; no full-window rate when incomplete"}
		if allCovered {
			x, _ := sumTotal.Float64()
			rate := x / q.To.Sub(q.From).Seconds()
			if finite(rate) {
				v.Rate = &rate
			}
		}
		r.CombinedSum = v
		r.SumMergeStatus = "compatible observed increments combined after source normalization"
	} else if sumMeta != nil {
		r.SumMergeStatus = "incompatible, missing, overflow or unreliable Sum populations; combine withheld"
	}
	// Merge only compatible observed incremental histograms, across source writers.
	var merged *Series
	var mcounts []*big.Int
	msum := 0.0
	known := true
	seenWriters := map[string]bool{}
	for i := range r.Series {
		s := &r.Series[i]
		if s.Metadata.Type != "histogram" || s.HistogramCount == nil {
			continue
		}
		m := s.Metadata
		if merged == nil {
			v := *s
			v.Points = []Point{}
			v.Intervals = []Interval{}
			v.Metadata = m
			merged = &v
			for range s.HistogramBuckets {
				mcounts = append(mcounts, new(big.Int))
			}
		} else {
			a := merged.Metadata
			if m.Name != a.Name || m.Unit != a.Unit || m.Service != a.Service || m.Namespace != a.Namespace || m.Environment != a.Environment || string(m.Scope) != string(a.Scope) || m.Workload != a.Workload || encoded(m.Bounds) != encoded(a.Bounds) || string(m.Attributes) != string(a.Attributes) {
				r.MergeStatus = "incompatible histogram identities, dimensions, units or boundaries; query one compatible population"
				return r, nil
			}
		}
		if s.Status != "observed" || s.Diagnostics.Conflicts > 0 || s.Diagnostics.DefinitionConflict || s.Diagnostics.InvalidDecreases > 0 || s.Diagnostics.Overlaps > 0 || seenWriters[m.Instance] {
			r.MergeStatus = "unreliable or overlapping writer definitions; merge withheld"
			return r, nil
		}
		seenWriters[m.Instance] = true
		cs, ok := uints(s.HistogramBuckets)
		if !ok {
			return nil, ErrInvalid
		}
		for k, c := range cs {
			mcounts[k].Add(mcounts[k], c)
		}
		if s.HistogramSum == nil {
			known = false
		} else {
			msum += *s.HistogramSum
		}
	}
	if merged != nil {
		var sp *float64
		if known && finite(msum) {
			sp = &msum
		}
		histogramSummary(merged, merged.Metadata.Bounds, mcounts, sp)
		merged.Status = "merged observed incremental distributions; source coverage remains per series"
		r.MergedHistogram = merged
		r.MergeStatus = merged.Status
	}
	return r, nil
}
