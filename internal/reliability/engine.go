package reliability

import (
	"context"
	"encoding/json"
	"math/big"
	"time"
)

type Source interface {
	Measure(context.Context, Definition, time.Time, time.Time) Evidence
}
type UnavailableSource struct{}

func (UnavailableSource) Measure(ctx context.Context, d Definition, from, to time.Time) Evidence {
	return unknown(from, to, "BACKEND_UNAVAILABLE", "telemetry backend unavailable; no healthy/recovered state inferred")
}

func Build(ctx context.Context, source Source, d Definition, cutoff time.Time, revision int) Evaluation {
	e := Evaluation{ID: Hash([]any{d.Key(), Hash(d), cutoff.UTC(), revision}), SLOKey: d.Key(), Definition: d, Cutoff: cutoff.UTC(), Revision: revision, Rules: []RuleResult{}, CreatedAt: time.Now().UTC()}
	cache := map[int]Window{}
	measure := func(seconds int) Window {
		if w, ok := cache[seconds]; ok {
			return w
		}
		from := cutoff.Add(-time.Duration(seconds) * time.Second)
		ev := source.Measure(ctx, d, from, cutoff)
		if !ev.From.Equal(from) || !ev.To.Equal(cutoff) {
			ev = unknown(from, cutoff, "INCOMPATIBLE", "source returned a different measurement interval")
		}
		w := Window{Evidence: ev}
		if ev.Status == "MEASURED" {
			b, err := Calculate(d.Target, ev.Total, ev.Good, ev.Bad)
			if err != nil {
				w.Evidence.Status = "INCOMPATIBLE"
				w.Evidence.Reason = "numerator/denominator population invalid"
			} else {
				w.Budget = b
			}
		}
		cache[seconds] = w
		return w
	}
	e.Overall = measure(d.WindowSeconds)
	e.Status = "UNKNOWN"
	if e.Overall.Evidence.Status == "MEASURED" {
		e.Status = "MEASURED"
	}
	for _, rule := range d.Rules {
		r := RuleResult{Rule: rule, Long: measure(rule.LongSeconds), Short: measure(rule.ShortSeconds), Decision: "UNKNOWN"}
		if e.Overall.Evidence.Status == "MEASURED" && r.Long.Budget != nil && r.Short.Budget != nil {
			threshold, _ := rat(rule.Threshold)
			long, _ := new(big.Rat).SetString(r.Long.Budget.Burn)
			short, _ := new(big.Rat).SetString(r.Short.Budget.Burn)
			r.Decision = "HEALTHY"
			if long.Cmp(threshold) >= 0 && short.Cmp(threshold) >= 0 {
				r.Decision = "BREACH"
			}
		}
		e.Rules = append(e.Rules, r)
	}
	if raw, err := json.Marshal(e); err != nil || len(raw) > 4<<20 {
		e.Status = "UNKNOWN"
		e.Overall = Window{Evidence: unknown(e.Overall.Evidence.From, cutoff, "INCOMPATIBLE", "evaluation evidence exceeds4MiB budget")}
		for i := range e.Rules {
			r := &e.Rules[i]
			r.Decision = "UNKNOWN"
			r.Long = Window{Evidence: unknown(r.Long.Evidence.From, cutoff, "INCOMPATIBLE", "evaluation byte budget exceeded")}
			r.Short = Window{Evidence: unknown(r.Short.Evidence.From, cutoff, "INCOMPATIBLE", "evaluation byte budget exceeded")}
		}
	}
	return e
}
