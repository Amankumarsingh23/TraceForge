// Package reliability evaluates observed reliability, never an uptime/SLA guarantee.
package reliability

import (
	"encoding/json"
	"fmt"
	"math/big"
	"regexp"
	"time"
	"traceforge/internal/metricdata"
)

const Version = "reliability/v1"

type Rule struct {
	ID           string `json:"id"`
	Version      int    `json:"version"`
	LongSeconds  int    `json:"long_seconds"`
	ShortSeconds int    `json:"short_seconds"`
	Threshold    string `json:"threshold"`
	Severity     string `json:"severity"`
	Confirm      int    `json:"confirm"`
	Recover      int    `json:"recover"`
}
type Definition struct {
	ID               string   `json:"id"`
	Version          int      `json:"version"`
	Description      string   `json:"description"`
	Kind             string   `json:"kind"`
	SourceVersion    string   `json:"source_version"`
	Service          string   `json:"service"`
	Namespace        string   `json:"namespace"`
	Environment      string   `json:"environment"`
	Mode             string   `json:"mode"`
	Provider         string   `json:"provider"`
	Route            string   `json:"route"`
	Writers          []string `json:"expected_writers"`
	Target           string   `json:"target"`
	WindowSeconds    int      `json:"window_seconds"`
	ThresholdSeconds string   `json:"threshold_seconds,omitempty"`
	MinCount         int      `json:"min_count"`
	MinCoverage      string   `json:"min_coverage"`
	Rules            []Rule   `json:"rules"`
}

func (d Definition) Key() string { return fmt.Sprintf("%s-v%d", d.ID, d.Version) }
func Hash(v any) string          { b, _ := json.Marshal(v); return metricdata.Hash(string(b)) }

var identifier = regexp.MustCompile(`^[a-z][a-z0-9_-]{1,63}$`)
var decimal = regexp.MustCompile(`^(?:0|[1-9][0-9]*)(?:\.[0-9]{1,12})?$`)

func rat(s string) (*big.Rat, bool) {
	if !decimal.MatchString(s) {
		return nil, false
	}
	return new(big.Rat).SetString(s)
}
func (d Definition) Validate() error {
	target, ok := rat(d.Target)
	if !ok || target.Sign() <= 0 || target.Cmp(big.NewRat(1, 1)) >= 0 {
		return fmt.Errorf("target must be strictly between zero and one")
	}
	coverage, ok := rat(d.MinCoverage)
	if !ok || coverage.Sign() <= 0 || coverage.Cmp(big.NewRat(1, 1)) > 0 {
		return fmt.Errorf("invalid minimum coverage")
	}
	if !identifier.MatchString(d.ID) || d.Version < 1 || d.Version > 10000 || len(d.Description) > 512 || d.Service == "" || len(d.Service) > 128 || len(d.Namespace) > 128 || d.Environment == "" || len(d.Environment) > 128 || (d.Mode != "synthetic" && d.Mode != "fixture" && d.Mode != "live") || d.WindowSeconds < 1 || d.WindowSeconds > 30*86400 || d.MinCount < 1 || d.MinCount > 1000000 || len(d.Writers) > 8 || len(d.Rules) > 4 {
		return fmt.Errorf("invalid bounded SLO definition")
	}
	switch d.Kind {
	case "availability", "latency":
		if d.SourceVersion != metricdata.Version || d.Route != "/checkout" && d.Route != "/charge" {
			return fmt.Errorf("unsupported metric population/version")
		}
	case "provider_attempt", "model_operation":
		if d.SourceVersion != "agentops/v1" || d.Provider == "" || len(d.Provider) > 128 {
			return fmt.Errorf("unsupported provider population/version")
		}
	default:
		return fmt.Errorf("unsupported SLO kind")
	}
	if d.Kind == "latency" {
		t, ok := rat(d.ThresholdSeconds)
		if !ok || t.Sign() <= 0 {
			return fmt.Errorf("latency threshold must be positive seconds")
		}
	}
	seen := map[string]bool{}
	for _, w := range d.Writers {
		if w == "" || len(w) > 256 || seen[w] {
			return fmt.Errorf("invalid writer population")
		}
		seen[w] = true
	}
	seen = map[string]bool{}
	for _, r := range d.Rules {
		t, ok := rat(r.Threshold)
		if !identifier.MatchString(r.ID) || r.Version < 1 || seen[r.ID] || !ok || t.Sign() <= 0 || r.ShortSeconds < 1 || r.LongSeconds < r.ShortSeconds || r.LongSeconds > d.WindowSeconds || r.Confirm < 1 || r.Confirm > 10 || r.Recover < 1 || r.Recover > 10 || r.Severity != "critical" && r.Severity != "warning" {
			return fmt.Errorf("invalid burn rule")
		}
		seen[r.ID] = true
	}
	return nil
}

type Evidence struct {
	Status      string         `json:"status"`
	Reason      string         `json:"reason"`
	From        time.Time      `json:"from"`
	To          time.Time      `json:"to"`
	Total       string         `json:"total"`
	Good        string         `json:"good"`
	Bad         string         `json:"bad"`
	Coverage    float64        `json:"coverage"`
	Diagnostics map[string]any `json:"diagnostics"`
	TraceIDs    []string       `json:"trace_ids"`
	Details     any            `json:"details,omitempty"`
}
type Budget struct {
	SLI             string `json:"sli"`
	BadFraction     string `json:"bad_fraction"`
	AllowedFraction string `json:"allowed_bad_fraction"`
	AllowedEvents   string `json:"allowed_bad_events"`
	Consumed        string `json:"budget_consumed"`
	Remaining       string `json:"budget_remaining"`
	Burn            string `json:"burn_rate"`
	Meets           bool   `json:"meets_target"`
}

func exact(v *big.Rat) string {
	if v.IsInt() {
		return v.Num().String()
	}
	return v.RatString()
}
func Calculate(target, total, good, bad string) (*Budget, error) {
	t, ok := rat(target)
	if !ok || t.Sign() <= 0 || t.Cmp(big.NewRat(1, 1)) >= 0 {
		return nil, fmt.Errorf("invalid target")
	}
	parse := func(s string) *big.Int {
		v, ok := new(big.Int).SetString(s, 10)
		if !ok || v.Sign() < 0 {
			return nil
		}
		return v
	}
	n, g, b := parse(total), parse(good), parse(bad)
	if n == nil || g == nil || b == nil || n.Sign() == 0 || new(big.Int).Add(g, b).Cmp(n) != 0 {
		return nil, fmt.Errorf("invalid or empty event population")
	}
	nr := new(big.Rat).SetInt(n)
	sli := new(big.Rat).SetFrac(g, n)
	bf := new(big.Rat).SetFrac(b, n)
	allowed := new(big.Rat).Sub(big.NewRat(1, 1), t)
	events := new(big.Rat).Mul(nr, allowed)
	consumed := new(big.Rat).Quo(new(big.Rat).SetInt(b), events)
	remaining := new(big.Rat).Sub(events, new(big.Rat).SetInt(b))
	burn := new(big.Rat).Quo(bf, allowed)
	return &Budget{exact(sli), exact(bf), exact(allowed), exact(events), exact(consumed), exact(remaining), exact(burn), sli.Cmp(t) >= 0}, nil
}

type Window struct {
	Evidence Evidence `json:"evidence"`
	Budget   *Budget  `json:"budget"`
}
type RuleResult struct {
	Rule     Rule   `json:"rule"`
	Long     Window `json:"long"`
	Short    Window `json:"short"`
	Decision string `json:"decision"`
}
type Evaluation struct {
	Status     string       `json:"status"`
	ID         string       `json:"id"`
	SLOKey     string       `json:"slo_key"`
	Definition Definition   `json:"definition"`
	Cutoff     time.Time    `json:"cutoff"`
	Revision   int          `json:"revision"`
	Overall    Window       `json:"overall"`
	Rules      []RuleResult `json:"rules"`
	CreatedAt  time.Time    `json:"created_at"`
}
type Head struct {
	Fingerprint    string    `json:"fingerprint"`
	SLOKey         string    `json:"slo_key"`
	RuleID         string    `json:"rule_id"`
	State          string    `json:"state"`
	IncidentID     string    `json:"incident_id"`
	LastCutoff     time.Time `json:"last_cutoff"`
	Breaches       int       `json:"breaches"`
	Recoveries     int       `json:"recoveries"`
	LastDataStatus string    `json:"last_data_status"`
}
type Incident struct {
	ID             string     `json:"id"`
	Fingerprint    string     `json:"fingerprint"`
	SLOKey         string     `json:"slo_key"`
	RuleID         string     `json:"rule_id"`
	Severity       string     `json:"severity"`
	State          string     `json:"state"`
	FirstFired     time.Time  `json:"first_fired"`
	LastEvaluation string     `json:"last_evaluation"`
	ResolvedAt     *time.Time `json:"resolved_at"`
}
type Event struct {
	ID           string    `json:"id"`
	IncidentID   string    `json:"incident_id"`
	Kind         string    `json:"kind"`
	Cutoff       time.Time `json:"cutoff"`
	EvaluationID string    `json:"evaluation_id"`
}

// Transition is pure. UNKNOWN preserves active/pending state and breaks any
// confirmation/recovery streak; stale cutoffs/revisions cannot advance state.
func Transition(h Head, e Evaluation, r RuleResult) (Head, *Event) {
	if !e.Cutoff.After(h.LastCutoff) {
		return h, nil
	}
	h.LastCutoff = e.Cutoff
	h.LastDataStatus = r.Decision
	if r.Decision == "UNKNOWN" {
		h.Breaches = 0
		h.Recoveries = 0
		return h, nil
	}
	if r.Decision == "BREACH" {
		h.Recoveries = 0
		h.Breaches++
		if h.State == "FIRING" {
			return h, nil
		}
		if h.Breaches < r.Rule.Confirm {
			h.State = "PENDING"
			return h, nil
		}
		h.State = "FIRING"
		h.IncidentID = Hash([]any{h.Fingerprint, e.Cutoff})
		ev := &Event{ID: Hash([]string{h.IncidentID, "FIRING"}), IncidentID: h.IncidentID, Kind: "FIRING", Cutoff: e.Cutoff, EvaluationID: e.ID}
		return h, ev
	}
	h.Breaches = 0
	if h.State == "FIRING" {
		h.Recoveries++
		if h.Recoveries < r.Rule.Recover {
			return h, nil
		}
		ev := &Event{ID: Hash([]string{h.IncidentID, "RESOLVED"}), IncidentID: h.IncidentID, Kind: "RESOLVED", Cutoff: e.Cutoff, EvaluationID: e.ID}
		h.State = "INACTIVE"
		h.IncidentID = ""
		h.Recoveries = 0
		return h, ev
	}
	h.State = "INACTIVE"
	h.Recoveries = 0
	return h, nil
}
