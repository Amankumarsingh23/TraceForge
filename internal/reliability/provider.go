package reliability

import (
	"context"
	"math/big"
	"sort"
	"time"
	"traceforge/internal/analytics"
	"traceforge/internal/queryapi"
	"traceforge/internal/traces"
)

type TraceRows interface {
	AnalyticsRows(context.Context, analytics.Query) ([]traces.Record, error)
}
type ProviderSource struct{ Repo TraceRows }
type ProviderReport struct {
	Attempts           int      `json:"attempts"`
	SuccessfulAttempts int      `json:"successful_attempts"`
	Logical            int      `json:"logical_operations"`
	SuccessfulLogical  int      `json:"successful_logical_operations"`
	RetryRecovered     int      `json:"retry_recovered"`
	Timeouts           int      `json:"timeouts"`
	RateLimited        int      `json:"rate_limited"`
	ProviderErrors     int      `json:"provider_errors"`
	Canceled           int      `json:"canceled_or_deadline"`
	Unknown            int      `json:"unknown_outcomes"`
	UsageComplete      int      `json:"usage_complete_attempts"`
	UsageCoverage      string   `json:"usage_coverage"`
	AttemptMeanMS      *float64 `json:"attempt_mean_ms"`
	AttemptP95MS       *float64 `json:"attempt_p95_ms"`
	LogicalMeanMS      *float64 `json:"logical_mean_ms"`
	LogicalP95MS       *float64 `json:"logical_p95_ms"`
	Mode               string   `json:"mode"`
	Provider           string   `json:"provider"`
}

func (s ProviderSource) Measure(ctx context.Context, d Definition, from, to time.Time) Evidence {
	if to.Sub(from) > time.Hour {
		return unknown(from, to, "INCOMPATIBLE", "provider window exceeds bounded trace query")
	}
	rows, e := s.Repo.AnalyticsRows(ctx, analytics.Query{From: from, To: to, BucketSeconds: 60})
	if e != nil {
		return unknown(from, to, "BACKEND_UNAVAILABLE", "provider trace source unavailable or budget exceeded")
	}
	groups := map[string][]traces.Record{}
	for _, r := range rows {
		groups[r.TraceID] = append(groups[r.TraceID], r)
	}
	observed := []queryapi.AgentObservation{}
	conflicts, gaps := 0, 0
	for id, rows := range groups {
		detail, e := traces.Reconstruct(id, rows, traces.HardMaxRows)
		if e != nil {
			return unknown(from, to, "INCOMPATIBLE", "provider canonical reconstruction failed")
		}
		ops := queryapi.CanonicalAgentObservations(detail)
		matching := false
		for _, o := range ops {
			if o.Service == d.Service && o.Environment == d.Environment && o.Namespace == d.Namespace && o.Provider == d.Provider && o.Mode == d.Mode {
				observed = append(observed, o)
				matching = true
			}
		}
		if matching {
			conflicts += detail.Diagnostics.ConflictingDuplicateGroups
			gaps += detail.Diagnostics.MissingParentCount + detail.Diagnostics.CycleCount + len(detail.Diagnostics.TimingWarnings)
		}
	}
	return ProviderEvidence(d, from, to, observed, conflicts, gaps)
}
func ProviderEvidence(d Definition, from, to time.Time, ops []queryapi.AgentObservation, conflicts, gaps int) Evidence {
	p := ProviderReport{Mode: d.Mode, Provider: d.Provider}
	durations := []float64{}
	logicalDurations := []float64{}
	children := map[string][]queryapi.AgentObservation{}
	ids := map[string]bool{}
	seen := map[string]bool{}
	unique := []queryapi.AgentObservation{}
	for _, o := range ops {
		if o.Mode != d.Mode || o.Provider != d.Provider || o.Service != d.Service || o.Environment != d.Environment || o.Namespace != d.Namespace {
			continue
		}
		key := o.TraceID + o.SpanID
		if seen[key] {
			continue
		}
		seen[key] = true
		unique = append(unique, o)
		if o.Role == "attempt" {
			children[o.TraceID+o.ParentID] = append(children[o.TraceID+o.ParentID], o)
		}
	}
	inside := func(t time.Time) bool { return !t.Before(from) && t.Before(to) }
	models := map[string]queryapi.AgentObservation{}
	for _, o := range unique {
		if o.Role == "model" {
			models[o.TraceID+o.SpanID] = o
		}
	}
	success := func(o queryapi.AgentObservation) bool { return o.Outcome == "success" && o.Status != 2 }
	known := func(o queryapi.AgentObservation) bool { return success(o) || o.Status == 2 && o.Outcome != "" }
	for _, o := range unique {
		if !inside(o.Start) {
			continue
		}
		ids[o.TraceID] = true
		if !known(o) {
			p.Unknown++
		}
		if o.Role == "attempt" {
			parent, ok := models[o.TraceID+o.ParentID]
			if !ok || parent.RunID != o.RunID {
				p.Unknown++
			}
			p.Attempts++
			if success(o) {
				p.SuccessfulAttempts++
			}
			if o.UsageComplete {
				p.UsageComplete++
			}
			switch o.Error {
			case "provider_timeout":
				p.Timeouts++
			case "provider_rate_limited":
				p.RateLimited++
			case "request_deadline_or_canceled":
				p.Canceled++
			default:
				if !success(o) && known(o) {
					p.ProviderErrors++
				}
			}
			durations = append(durations, float64(o.DurationNS)/1e6)
		} else if o.Role == "model" {
			logicalDurations = append(logicalDurations, float64(o.DurationNS)/1e6)
			p.Logical++
			if success(o) {
				p.SuccessfulLogical++
			}
			attempts := children[o.TraceID+o.SpanID]
			hadBad, hadGood := false, false
			for _, a := range attempts {
				if a.RunID != o.RunID || a.Mode != o.Mode || a.Provider != o.Provider {
					p.Unknown++
					continue
				}
				hadGood = hadGood || success(a)
				hadBad = hadBad || !success(a)
			}
			if len(attempts) == 0 || success(o) && !hadGood {
				p.Unknown++
			}
			if success(o) && hadBad && hadGood {
				p.RetryRecovered++
			}
		}
	}
	if len(durations) > 0 {
		sort.Float64s(durations)
		sum := 0.0
		for _, v := range durations {
			sum += v
		}
		mean := sum / float64(len(durations))
		p.AttemptMeanMS = &mean
		v := durations[(95*len(durations)+99)/100-1]
		p.AttemptP95MS = &v
	}
	if len(logicalDurations) > 0 {
		sort.Float64s(logicalDurations)
		sum := 0.0
		for _, v := range logicalDurations {
			sum += v
		}
		mean := sum / float64(len(logicalDurations))
		p.LogicalMeanMS = &mean
		v := logicalDurations[(95*len(logicalDurations)+99)/100-1]
		p.LogicalP95MS = &v
	}
	p.UsageCoverage = "unknown"
	if p.Attempts > 0 {
		p.UsageCoverage = "incomplete"
		if p.UsageComplete == p.Attempts {
			p.UsageCoverage = "complete_for_observed_attempts"
		}
	}
	ev := unknown(from, to, "MEASURED", "")
	ev.Details = p
	ev.Coverage = 1
	ev.Diagnostics = map[string]any{"source": "agentops/v1", "population": "observed canonical model/attempt starts; tools and parent summaries excluded", "conflicting_groups": conflicts, "trace_gaps": gaps, "completeness": "unknown; timeout does not prove provider never received request; operational success is not answer quality"}
	for id := range ids {
		ev.TraceIDs = append(ev.TraceIDs, id)
	}
	sort.Strings(ev.TraceIDs)
	if len(ev.TraceIDs) > 8 {
		ev.TraceIDs = ev.TraceIDs[:8]
	}
	n, g := p.Attempts, p.SuccessfulAttempts
	if d.Kind == "model_operation" {
		n, g = p.Logical, p.SuccessfulLogical
	}
	ev.Total = big.NewInt(int64(n)).String()
	ev.Good = big.NewInt(int64(g)).String()
	ev.Bad = big.NewInt(int64(n - g)).String()
	if n == 0 {
		ev.Coverage = 0
		ev.Status = "NO_OBSERVATIONS"
		ev.Reason = "no eligible observed provider population"
	} else if n < d.MinCount || p.Unknown > 0 || conflicts > 0 || gaps > 0 {
		ev.Status = "INSUFFICIENT_EVIDENCE"
		ev.Reason = "provider evidence has unknown outcomes, gaps, conflicts or insufficient observations"
	}
	return ev
}

type Sources struct {
	Metric   MetricSource
	Provider ProviderSource
}

func (s Sources) Measure(ctx context.Context, d Definition, from, to time.Time) Evidence {
	if d.Kind == "availability" || d.Kind == "latency" {
		return s.Metric.Measure(ctx, d, from, to)
	}
	return s.Provider.Measure(ctx, d, from, to)
}
