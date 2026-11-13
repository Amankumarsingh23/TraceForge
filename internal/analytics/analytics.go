// Package analytics computes bounded, trace-derived observations, not OTLP metrics.
package analytics

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math"
	"sort"
	"strconv"
	"time"
	"traceforge/internal/traces"
)

const Version = "trace-red/v1"
const MaxTraces = 128
const MaxRows = 20000
const MaxBytes = 16 << 20
const MaxServices = 64
const MaxEdges = 128
const MaxPoints = 4096

type Query struct {
	From          time.Time `json:"from"`
	To            time.Time `json:"to"`
	BucketSeconds int       `json:"bucket_seconds"`
	Environment   string    `json:"environment"`
	Namespace     string    `json:"namespace"`
	Workload      string    `json:"workload"`
	ServiceID     string    `json:"service_id"`
}

func (q Query) Valid() bool {
	return !q.From.Before(time.Unix(0, 0)) && !q.To.After(time.Unix(0, math.MaxInt64)) && q.From.Before(q.To) && q.To.Sub(q.From) <= time.Hour && (q.BucketSeconds == 60 || q.BucketSeconds == 300 || q.BucketSeconds == 900) && len(q.Environment) <= 128 && len(q.Namespace) <= 128 && (q.Workload == "" || q.Workload == "synthetic" || q.Workload == "fixture" || q.Workload == "live" || q.Workload == "unknown") && (q.ServiceID == "" || traces.ValidID(q.ServiceID, 16))
}

type Repository interface {
	AnalyticsRows(context.Context, Query) ([]traces.Record, error)
}
type Identity struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Namespace   string `json:"namespace"`
	Environment string `json:"environment"`
	Workload    string `json:"workload"`
}
type Stats struct {
	Count         int            `json:"count"`
	Rate          float64        `json:"rate_per_second"`
	Errors        int            `json:"errors"`
	ErrorFraction *float64       `json:"error_fraction"`
	Status        map[string]int `json:"status"`
	HTTPStatus    map[string]int `json:"http_status"`
	Samples       int            `json:"duration_samples"`
	Mean          *float64       `json:"mean_ms"`
	P50           *float64       `json:"p50_ms"`
	P95           *float64       `json:"p95_ms"`
	P99           *float64       `json:"p99_ms"`
}
type Bucket struct {
	From    time.Time `json:"from"`
	To      time.Time `json:"to"`
	Seconds float64   `json:"seconds"`
	Stats   Stats     `json:"stats"`
}
type Service struct {
	Identity Identity `json:"identity"`
	Stats    Stats    `json:"stats"`
	Buckets  []Bucket `json:"buckets"`
	TraceIDs []string `json:"trace_ids"`
}
type Edge struct {
	From     string   `json:"from"`
	To       string   `json:"to"`
	Client   Stats    `json:"client"`
	Server   Stats    `json:"server"`
	TraceIDs []string `json:"trace_ids"`
}
type Diagnostics struct {
	CandidateTraces      int `json:"candidate_traces"`
	PhysicalRows         int `json:"physical_rows"`
	CanonicalSpans       int `json:"canonical_spans"`
	DuplicateRows        int `json:"duplicate_rows"`
	ConflictingGroups    int `json:"conflicting_groups"`
	MissingServiceNames  int `json:"missing_service_names"`
	UnmatchedClients     int `json:"unmatched_clients"`
	MissingServerParents int `json:"missing_server_parents"`
	WrongParentKinds     int `json:"wrong_parent_kinds"`
	AmbiguousClients     int `json:"ambiguous_clients"`
	SameServicePairs     int `json:"same_service_pairs"`
	InvalidDurations     int `json:"invalid_durations"`
	MissingParents       int `json:"missing_parents_all_spans"`
	Cycles               int `json:"cycles"`
	TimingWarnings       int `json:"timing_warnings"`
}
type Result struct {
	Version           string         `json:"version"`
	Query             Query          `json:"query"`
	WindowSeconds     float64        `json:"window_seconds"`
	Units             string         `json:"units"`
	Attribution       string         `json:"attribution"`
	Completeness      string         `json:"completeness"`
	NonSnapshot       bool           `json:"non_snapshot"`
	SelectionComplete bool           `json:"selection_complete"`
	Services          []Service      `json:"services"`
	Edges             []Edge         `json:"edges"`
	Diagnostics       Diagnostics    `json:"diagnostics"`
	Budgets           map[string]int `json:"budgets"`
	DiagnosticsScope  string         `json:"diagnostics_scope"`
}
type attr struct {
	Key   string `json:"key"`
	Value struct {
		String string `json:"stringValue"`
		Int    string `json:"intValue"`
	} `json:"value"`
}

func attributes(raw json.RawMessage) map[string]string {
	var a []attr
	_ = json.Unmarshal(raw, &a)
	m := map[string]string{}
	for _, v := range a {
		if v.Value.Int != "" {
			m[v.Key] = v.Value.Int
		} else {
			m[v.Key] = v.Value.String
		}
	}
	return m
}
func identity(r traces.Record) (Identity, bool) {
	a := attributes(r.ResourceAttributes)
	name := a["service.name"]
	// The ingestion placeholder is not an asserted service identity.
	if name == "" || name == "unknown_service" || name != r.ServiceName {
		return Identity{}, false
	}
	w := a["traceforge.workload.kind"]
	if w != "synthetic" && w != "fixture" && w != "live" {
		w = "unknown"
		mode := attributes(r.SpanAttributes)["traceforge.provider.mode"]
		if mode == "fixture" || mode == "live" {
			w = mode
		}
	}
	i := Identity{Name: name, Namespace: a["service.namespace"], Environment: a["deployment.environment.name"], Workload: w}
	b, _ := json.Marshal([]string{i.Name, i.Namespace, i.Environment, i.Workload})
	h := sha256.Sum256(b)
	i.ID = hex.EncodeToString(h[:16])
	return i, true
}
func matches(i Identity, q Query) bool {
	return (q.Environment == "" || i.Environment == q.Environment) && (q.Namespace == "" || i.Namespace == q.Namespace) && (q.Workload == "" || i.Workload == q.Workload)
}

type accumulator struct {
	stats     Stats
	durations []float64
	evidence  map[string]float64
}

func newAccumulator() *accumulator {
	return &accumulator{stats: Stats{Status: map[string]int{"UNSET": 0, "OK": 0, "ERROR": 0, "OTHER": 0}, HTTPStatus: map[string]int{}}, evidence: map[string]float64{}}
}
func (a *accumulator) add(r traces.Record, d *Diagnostics) {
	a.stats.Count++
	status := "OTHER"
	if r.StatusCode < 3 {
		status = []string{"UNSET", "OK", "ERROR"}[r.StatusCode]
	}
	a.stats.Status[status]++
	if r.StatusCode == 2 {
		a.stats.Errors++
	}
	http := attributes(r.SpanAttributes)["http.response.status_code"]
	n, e := strconv.Atoi(http)
	if e != nil || n < 100 || n > 599 {
		http = "missing"
	}
	a.stats.HTTPStatus[http]++
	duration := float64(r.DurationNS) / 1e6
	if r.EndTime.Before(r.StartTime) || r.DurationNS > math.MaxInt64 {
		d.InvalidDurations++
	} else {
		a.durations = append(a.durations, duration)
	}
	score := duration
	if r.StatusCode == 2 {
		score += 1e16
	}
	if prev, ok := a.evidence[r.TraceID]; !ok || score > prev {
		a.evidence[r.TraceID] = score
	}
}
func (a *accumulator) finish(seconds float64) Stats {
	s := a.stats
	s.Rate = float64(s.Count) / seconds
	if s.Count > 0 {
		x := float64(s.Errors) / float64(s.Count)
		s.ErrorFraction = &x
	}
	s.Samples = len(a.durations)
	if s.Samples > 0 {
		sort.Float64s(a.durations)
		sum := 0.0
		for _, x := range a.durations {
			sum += x
		}
		mean := sum / float64(s.Samples)
		s.Mean = &mean
		p := func(q float64) *float64 { x := a.durations[int(math.Ceil(q*float64(s.Samples)))-1]; return &x }
		s.P50 = p(.50)
		s.P95 = p(.95)
		s.P99 = p(.99)
	}
	return s
}
func (a *accumulator) ids() []string {
	ids := make([]string, 0, len(a.evidence))
	for id := range a.evidence {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		if a.evidence[ids[i]] == a.evidence[ids[j]] {
			return ids[i] < ids[j]
		}
		return a.evidence[ids[i]] > a.evidence[ids[j]]
	})
	if len(ids) > 5 {
		// Preserve a slow non-error example even when errors occupy the top five.
		if a.evidence[ids[4]] >= 1e16 {
			for _, id := range ids[5:] {
				if a.evidence[id] < 1e16 {
					ids[4] = id
					break
				}
			}
		}
		ids = ids[:5]
	}
	return ids
}

// Compute reuses the exact Session 3 canonical selection before windowing.
func Compute(ctx context.Context, q Query, rows []traces.Record) (*Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !q.Valid() {
		return nil, traces.ErrCorrupt
	}
	if len(rows) > MaxRows {
		return nil, traces.ErrTooLarge
	}
	out := &Result{Version: Version, Query: q, WindowSeconds: q.To.Sub(q.From).Seconds(), Units: "durations=ms; rate=observations/window_seconds; errors=explicit_ERROR/count; percentiles=nearest_rank", Attribution: "inbound=SERVER start; edge=CLIENT start; UTC [from,to)", Completeness: "unknown: sampled, missing and late telemetry; no traffic or business-success guarantee", NonSnapshot: true, SelectionComplete: true, Services: []Service{}, Edges: []Edge{}}
	out.Budgets = map[string]int{"candidate_traces": MaxTraces, "physical_rows": MaxRows, "rows_per_trace": traces.HardMaxRows, "serialized_bytes": MaxBytes, "services": MaxServices, "edges": MaxEdges, "bucket_points": MaxPoints, "api_seconds": 4, "storage_seconds": 3, "read_rows_per_query": 1000000, "read_bytes_per_query": 128 << 20, "clickhouse_memory_bytes_per_query": 256 << 20}
	out.DiagnosticsScope = "all candidate traces before identity filters; unmatched/ambiguous CLIENT and parent diagnostics use their owning span start window"
	groups := map[string][]traces.Record{}
	bytes := 0
	for _, r := range rows {
		b, _ := json.Marshal(r)
		bytes += len(b)
		if bytes > MaxBytes {
			return nil, traces.ErrTooLarge
		}
		groups[r.TraceID] = append(groups[r.TraceID], r)
	}
	if len(groups) > MaxTraces {
		return nil, traces.ErrTooLarge
	}
	out.Diagnostics.PhysicalRows = len(rows)
	out.Diagnostics.CandidateTraces = len(groups)
	type serviceAcc struct {
		identity Identity
		total    *accumulator
		buckets  []*accumulator
	}
	type edgeAcc struct {
		from, to       string
		client, server *accumulator
	}
	services := map[string]*serviceAcc{}
	edges := map[string]*edgeAcc{}
	bucketCount := int(math.Ceil(q.To.Sub(q.From).Seconds() / float64(q.BucketSeconds)))
	ensure := func(i Identity) *serviceAcc {
		if services[i.ID] == nil {
			if len(services) >= MaxServices || (len(services)+1)*bucketCount > MaxPoints {
				return nil
			}
			s := &serviceAcc{identity: i, total: newAccumulator()}
			for k := 0; k < bucketCount; k++ {
				s.buckets = append(s.buckets, newAccumulator())
			}
			services[i.ID] = s
		}
		return services[i.ID]
	}
	inside := func(t time.Time) bool { return !t.Before(q.From) && t.Before(q.To) }
	ids := make([]string, 0, len(groups))
	for id := range groups {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		d, e := traces.Reconstruct(id, groups[id], traces.HardMaxRows)
		if e != nil {
			return nil, e
		}
		out.Diagnostics.CanonicalSpans += d.Diagnostics.UniqueSpans
		out.Diagnostics.DuplicateRows += d.Diagnostics.ExactDuplicateRows
		out.Diagnostics.ConflictingGroups += d.Diagnostics.ConflictingDuplicateGroups
		out.Diagnostics.MissingParents += d.Diagnostics.MissingParentCount
		out.Diagnostics.Cycles += d.Diagnostics.CycleCount
		out.Diagnostics.TimingWarnings += len(d.Diagnostics.TimingWarnings)
		nodes := map[string]*traces.Node{}
		stack := append(append(append([]*traces.Node{}, d.RootSpans...), d.OrphanSpans...), d.DisconnectedSpans...)
		for len(stack) > 0 {
			n := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if nodes[n.SpanID] != nil {
				continue
			}
			nodes[n.SpanID] = n
			stack = append(stack, n.Children...)
		}
		children := map[string][]*traces.Node{}
		for _, n := range nodes {
			if n.SpanKind == 2 {
				children[n.ParentSpanID] = append(children[n.ParentSpanID], n)
			}
			i, ok := identity(n.Record)
			if !ok && inside(n.StartTime) {
				out.Diagnostics.MissingServiceNames++
			}
			if n.SpanKind == 2 && inside(n.StartTime) {
				if n.ParentSpanID != "" {
					p := nodes[n.ParentSpanID]
					if p == nil {
						out.Diagnostics.MissingServerParents++
					} else if p.SpanKind != 3 {
						out.Diagnostics.WrongParentKinds++
					}
				}
				if ok && matches(i, q) {
					s := ensure(i)
					if s == nil {
						return nil, traces.ErrTooLarge
					}
					s.total.add(n.Record, &out.Diagnostics)
					k := int(n.StartTime.Sub(q.From).Seconds() / float64(q.BucketSeconds))
					ignored := Diagnostics{}
					s.buckets[k].add(n.Record, &ignored)
				}
			}
		}
		for _, n := range nodes {
			if n.SpanKind != 3 || !inside(n.StartTime) {
				continue
			}
			if i, ok := identity(n.Record); ok && matches(i, q) {
				if ensure(i) == nil {
					return nil, traces.ErrTooLarge
				}
			}
			cs := children[n.SpanID]
			if len(cs) == 0 {
				out.Diagnostics.UnmatchedClients++
				continue
			}
			if len(cs) > 1 {
				out.Diagnostics.AmbiguousClients++
				continue
			}
			server := cs[0]
			from, ok := identity(n.Record)
			to, ok2 := identity(server.Record)
			if !ok || !ok2 {
				continue
			}
			if from.ID == to.ID {
				out.Diagnostics.SameServicePairs++
				continue
			}
			if !matches(from, q) || !matches(to, q) {
				continue
			}
			if ensure(from) == nil || ensure(to) == nil {
				return nil, traces.ErrTooLarge
			}
			key := from.ID + to.ID
			if edges[key] == nil {
				if len(edges) >= MaxEdges {
					return nil, traces.ErrTooLarge
				}
				edges[key] = &edgeAcc{from.ID, to.ID, newAccumulator(), newAccumulator()}
			}
			edges[key].client.add(n.Record, &out.Diagnostics)
			edges[key].server.add(server.Record, &out.Diagnostics)
		}
		if len(services) > MaxServices || len(edges) > MaxEdges || len(services)*bucketCount > MaxPoints {
			return nil, traces.ErrTooLarge
		}
	}
	selected := map[string]bool{}
	for _, a := range edges {
		if q.ServiceID != "" && a.from != q.ServiceID && a.to != q.ServiceID {
			continue
		}
		out.Edges = append(out.Edges, Edge{a.from, a.to, a.client.finish(out.WindowSeconds), a.server.finish(out.WindowSeconds), a.client.ids()})
		selected[a.from] = true
		selected[a.to] = true
	}
	for id, a := range services {
		if q.ServiceID != "" && id != q.ServiceID && !selected[id] {
			continue
		}
		s := Service{Identity: a.identity, Stats: a.total.finish(out.WindowSeconds), Buckets: []Bucket{}, TraceIDs: a.total.ids()}
		for k, b := range a.buckets {
			from := q.From.Add(time.Duration(k*q.BucketSeconds) * time.Second)
			to := from.Add(time.Duration(q.BucketSeconds) * time.Second)
			if to.After(q.To) {
				to = q.To
			}
			seconds := to.Sub(from).Seconds()
			s.Buckets = append(s.Buckets, Bucket{from, to, seconds, b.finish(seconds)})
		}
		out.Services = append(out.Services, s)
	}
	sort.Slice(out.Services, func(i, j int) bool {
		a, b := out.Services[i].Identity, out.Services[j].Identity
		if a.Name == b.Name {
			return a.ID < b.ID
		}
		return a.Name < b.Name
	})
	sort.Slice(out.Edges, func(i, j int) bool { a, b := out.Edges[i], out.Edges[j]; return a.From+a.To < b.From+b.To })
	return out, nil
}
