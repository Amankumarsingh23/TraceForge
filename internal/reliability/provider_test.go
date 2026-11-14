package reliability

import (
	"context"
	"testing"
	"time"
	"traceforge/internal/queryapi"
)

func TestProviderAttemptsLogicalRecoveryAndUsage(t *testing.T) {
	d := definition()
	d.Kind = "provider_attempt"
	d.Mode = "fixture"
	d.Provider = "fixture"
	d.SourceVersion = "agentops/v1"
	ops := []queryapi.AgentObservation{}
	add := func(id, parent, role, outcome, errorType string) {
		status := uint8(0)
		if outcome != "success" {
			status = 2
		}
		ops = append(ops, queryapi.AgentObservation{Service: d.Service, Environment: d.Environment, Namespace: d.Namespace, TraceID: "trace", SpanID: id, ParentID: parent, RunID: "run", Role: role, Mode: "fixture", Provider: "fixture", Outcome: outcome, Error: errorType, Start: epoch, Status: status, UsageComplete: outcome == "success", DurationNS: 1000000})
	}
	add("m1", "", "model", "success", "")
	add("a1", "m1", "attempt", "success", "")
	add("m2", "", "model", "success", "")
	add("a2", "m2", "attempt", "provider_timeout", "provider_timeout")
	add("a3", "m2", "attempt", "success", "")
	add("m3", "", "model", "provider_permanent_error", "provider_permanent_error")
	add("a4", "m3", "attempt", "provider_permanent_error", "provider_permanent_error")
	e := ProviderEvidence(d, epoch.Add(-time.Second), epoch.Add(time.Second), append(ops, ops...), 0, 0)
	p := e.Details.(ProviderReport)
	if p.AttemptMeanMS == nil || *p.AttemptMeanMS != 1 || p.LogicalMeanMS == nil || *p.LogicalMeanMS != 1 || p.LogicalP95MS == nil || *p.LogicalP95MS != 1 {
		t.Fatal("attempt and logical duration populations missing", p)
	}
	if e.Total != "4" || e.Good != "2" || p.Logical != 3 || p.SuccessfulLogical != 2 || p.RetryRecovered != 1 || p.Timeouts != 1 || p.ProviderErrors != 1 || p.UsageComplete != 2 {
		t.Fatal(e, p)
	}
	d.Kind = "model_operation"
	e = ProviderEvidence(d, epoch.Add(-time.Second), epoch.Add(time.Second), ops, 0, 0)
	if e.Total != "3" || e.Good != "2" {
		t.Fatal(e)
	}
	e = ProviderEvidence(d, epoch.Add(-time.Second), epoch.Add(time.Second), ops, 1, 0)
	if e.Status != "INSUFFICIENT_EVIDENCE" {
		t.Fatal("conflicts trusted", e)
	}
}

func TestProviderRateLimitAndLiveIsolation(t *testing.T) {
	d := definition()
	d.Kind = "provider_attempt"
	d.Mode = "fixture"
	d.Provider = "fixture"
	model := queryapi.AgentObservation{TraceID: "trace", SpanID: "model", RunID: "run", Role: "model", Mode: d.Mode, Provider: d.Provider, Service: d.Service, Environment: d.Environment, Namespace: d.Namespace, Outcome: "provider_rate_limited", Error: "provider_rate_limited", Status: 2, Start: epoch}
	attempt := model
	attempt.SpanID = "attempt"
	attempt.Role = "attempt"
	attempt.ParentID = "model"
	e := ProviderEvidence(d, epoch.Add(-time.Second), epoch.Add(time.Second), []queryapi.AgentObservation{model, attempt}, 0, 0)
	p := e.Details.(ProviderReport)
	if p.RateLimited != 1 || p.ProviderErrors != 0 || e.Bad != "1" {
		t.Fatal(e)
	}
	d.Mode = "live"
	e = ProviderEvidence(d, epoch.Add(-time.Second), epoch.Add(time.Second), []queryapi.AgentObservation{model, attempt}, 0, 0)
	if e.Status != "NO_OBSERVATIONS" || e.Total != "0" {
		t.Fatal("fixture events became live evidence", e)
	}
}

type fakeClock struct {
	now   time.Time
	waits int
}

func (c *fakeClock) Now() time.Time                                 { return c.now }
func (c *fakeClock) Wait(ctx context.Context, d time.Duration) bool { c.waits++; return false }

type fakeEvaluator struct {
	cutoffs    []time.Time
	definition Definition
}

func (f *fakeEvaluator) Definition(context.Context, string) (Definition, error) {
	return f.definition, nil
}
func (f *fakeEvaluator) Evaluate(ctx context.Context, s Source, d Definition, t time.Time, r int) (Evaluation, error) {
	f.cutoffs = append(f.cutoffs, t)
	return Evaluation{}, nil
}
func TestSchedulerAlignedClockAndBoundedShutdown(t *testing.T) {
	clock := &fakeClock{now: epoch.Add(19 * time.Second)}
	evaluator := &fakeEvaluator{definition: definition()}
	Run(context.Background(), clock, evaluator, UnavailableSource{}, []string{"test"}, 5*time.Second, 3*time.Second)
	if len(evaluator.cutoffs) != 1 || !evaluator.cutoffs[0].Equal(epoch.Add(15*time.Second)) || clock.waits != 1 {
		t.Fatal(evaluator, clock)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	Run(ctx, clock, evaluator, UnavailableSource{}, []string{"test"}, 5*time.Second, 0)
	if len(evaluator.cutoffs) != 1 {
		t.Fatal("scheduler evaluated after cancel")
	}
}
