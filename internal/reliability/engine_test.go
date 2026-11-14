package reliability

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"
)

var epoch = time.Date(2026, 11, 14, 10, 0, 0, 0, time.UTC)

func TestOversizedEvidenceCannotFire(t *testing.T) {
	e := Build(context.Background(), sourceFunc(func(_ context.Context, _ Definition, a, b time.Time) Evidence {
		v := eventEvidence(a, b, "10", "0", "10")
		v.Reason = strings.Repeat("x", 4<<20)
		return v
	}), definition(), epoch, 1)
	if e.Status != "UNKNOWN" || e.Overall.Budget != nil || e.Rules[0].Decision != "UNKNOWN" || e.Rules[0].Long.Budget != nil {
		t.Fatal("oversized evidence produced a decision")
	}
}

func definition() Definition {
	return Definition{ID: "test-availability", Version: 1, Kind: "availability", SourceVersion: "otlp-metrics/v1", Service: "payment", Namespace: "test", Environment: "local", Mode: "synthetic", Route: "/charge", Writers: []string{"writer"}, Target: "0.9", WindowSeconds: 20, MinCount: 1, MinCoverage: "0.7", Rules: []Rule{{ID: "fast", Version: 1, LongSeconds: 20, ShortSeconds: 10, Threshold: "2", Severity: "critical", Confirm: 1, Recover: 1}}}
}
func TestExactBudgetArithmetic(t *testing.T) {
	for _, c := range []struct {
		target, total, good, bad, allowed, consumed, remaining, burn string
		meets                                                        bool
	}{{"0.999", "200000", "199860", "140", "200", "7/10", "60", "7/10", true}, {"0.95", "1000", "940", "60", "50", "6/5", "-10", "6/5", false}, {"0.99999", "10", "9", "1", "1/10000", "10000", "-9999/10000", "10000", false}} {
		b, e := Calculate(c.target, c.total, c.good, c.bad)
		if e != nil || b.AllowedEvents != c.allowed || b.Consumed != c.consumed || b.Remaining != c.remaining || b.Burn != c.burn || b.Meets != c.meets {
			t.Fatal(b, e)
		}
	}
}
func TestEmptyAndInvalidPopulations(t *testing.T) {
	for _, c := range [][4]string{{"0.99", "0", "0", "0"}, {"1", "10", "10", "0"}, {"0.9", "10", "9", "2"}, {"0.9", "10", "-1", "11"}, {"0.9", "missing", "0", "0"}} {
		if _, e := Calculate(c[0], c[1], c[2], c[3]); e == nil {
			t.Fatal("accepted invalid denominator", c)
		}
	}
}

type sourceFunc func(context.Context, Definition, time.Time, time.Time) Evidence

func (f sourceFunc) Measure(ctx context.Context, d Definition, a, b time.Time) Evidence {
	return f(ctx, d, a, b)
}
func eventEvidence(a, b time.Time, n, g, bad string) Evidence {
	e := unknown(a, b, "MEASURED", "")
	e.Total = n
	e.Good = g
	e.Bad = bad
	e.Coverage = 1
	return e
}
func TestAlignedMultiWindowRulesAndUnknown(t *testing.T) {
	d := definition()
	source := sourceFunc(func(ctx context.Context, d Definition, a, b time.Time) Evidence {
		if b != epoch {
			t.Fatal("unaligned cutoff")
		}
		if b.Sub(a) == 10*time.Second {
			return eventEvidence(a, b, "100", "90", "10")
		}
		return eventEvidence(a, b, "100", "80", "20")
	})
	e := Build(context.Background(), source, d, epoch, 1)
	if e.Rules[0].Decision != "HEALTHY" {
		t.Fatal("AND rule fired with short burn below threshold", e)
	}
	source = sourceFunc(func(ctx context.Context, d Definition, a, b time.Time) Evidence {
		if b.Sub(a) == 10*time.Second {
			return unknown(a, b, "BACKEND_UNAVAILABLE", "missing")
		}
		return eventEvidence(a, b, "100", "80", "20")
	})
	e = Build(context.Background(), source, d, epoch, 1)
	if e.Rules[0].Decision != "UNKNOWN" {
		t.Fatal("missing short window became healthy")
	}
}
func TestThresholdBoundaryAndConfirmation(t *testing.T) {
	d := definition()
	d.Rules[0].Confirm = 2
	d.Rules[0].Recover = 2
	source := sourceFunc(func(ctx context.Context, d Definition, a, b time.Time) Evidence {
		return eventEvidence(a, b, "10", "8", "2")
	})
	e := Build(context.Background(), source, d, epoch, 1)
	if e.Rules[0].Decision != "BREACH" {
		t.Fatal("threshold equality excluded")
	}
	h := Head{Fingerprint: "fingerprint", State: "INACTIVE"}
	h, event := Transition(h, e, e.Rules[0])
	if h.State != "PENDING" || event != nil {
		t.Fatal(h, event)
	}
	e.Cutoff = epoch.Add(time.Second)
	h, event = Transition(h, e, e.Rules[0])
	if h.State != "FIRING" || event == nil || event.Kind != "FIRING" {
		t.Fatal(h, event)
	}
	incident := h.IncidentID
	e.Cutoff = epoch.Add(2 * time.Second)
	r := e.Rules[0]
	r.Decision = "UNKNOWN"
	h, event = Transition(h, e, r)
	if h.State != "FIRING" || h.IncidentID != incident || event != nil {
		t.Fatal("unknown resolved active alert")
	}
	r.Decision = "HEALTHY"
	e.Cutoff = epoch.Add(3 * time.Second)
	h, event = Transition(h, e, r)
	if event != nil || h.State != "FIRING" {
		t.Fatal("premature recovery")
	}
	e.Cutoff = epoch.Add(4 * time.Second)
	h, event = Transition(h, e, r)
	if event == nil || event.Kind != "RESOLVED" || h.State != "INACTIVE" {
		t.Fatal(h, event)
	}
}
func TestStaleEvaluationAndNewIncident(t *testing.T) {
	d := definition()
	e := Evaluation{ID: "one", Cutoff: epoch}
	r := RuleResult{Rule: d.Rules[0], Decision: "BREACH"}
	h := Head{Fingerprint: "f", State: "INACTIVE"}
	h, _ = Transition(h, e, r)
	old := h
	e.Cutoff = epoch.Add(-time.Second)
	r.Decision = "HEALTHY"
	h, event := Transition(h, e, r)
	if !reflect.DeepEqual(old, h) || event != nil {
		t.Fatal("stale update changed active incident")
	}
	e.Cutoff = epoch.Add(time.Second)
	h, _ = Transition(h, e, r)
	e.Cutoff = epoch.Add(2 * time.Second)
	r.Decision = "BREACH"
	h, event = Transition(h, e, r)
	if event == nil || h.IncidentID == old.IncidentID {
		t.Fatal("new breach reused resolved incident")
	}
}
func TestDefinitionValidation(t *testing.T) {
	d := definition()
	if e := d.Validate(); e != nil {
		t.Fatal(e)
	}
	d.Target = "1"
	if d.Validate() == nil {
		t.Fatal("target1 accepted")
	}
	d = definition()
	d.Rules[0].ShortSeconds = 21
	if d.Validate() == nil {
		t.Fatal("inverted windows")
	}
	d = definition()
	d.Writers = []string{"one", "one"}
	if d.Validate() == nil {
		t.Fatal("duplicate writer")
	}
}
