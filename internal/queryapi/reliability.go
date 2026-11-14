package queryapi

import (
	"time"
	"traceforge/internal/traces"
)

// AgentObservation reuses the Session7 typed attribute/usage interpretation.
// It is emitted only from an already canonical trace forest.
type AgentObservation struct {
	TraceID, SpanID, ParentID, RunID, Role, Mode, Provider, Outcome, Error, Service, Environment, Namespace string
	Start                                                                                                   time.Time
	DurationNS                                                                                              uint64
	Status                                                                                                  uint8
	UsageComplete                                                                                           bool
}

func CanonicalAgentObservations(d *traces.Detail) []AgentObservation {
	summary := summarizeAgent(d)
	usage := map[string]bool{}
	for _, run := range summary.Runs {
		for _, op := range run.Operations {
			if op["role"] == "attempt" {
				_, a := op["input_tokens"]
				_, b := op["output_tokens"]
				id, _ := op["span_id"].(string)
				usage[id] = a && b
			}
		}
	}
	out := []AgentObservation{}
	stack := append(append(append([]*traces.Node{}, d.RootSpans...), d.OrphanSpans...), d.DisconnectedSpans...)
	seen := map[string]bool{}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if seen[n.SpanID] {
			continue
		}
		seen[n.SpanID] = true
		stack = append(stack, n.Children...)
		a := agentAttrs(n.SpanAttributes)
		role := textAttr(a, "traceforge.operation.kind")
		run := textAttr(a, "traceforge.run.id")
		if role != "attempt" && role != "model" || !traces.ValidID(run, 16) {
			continue
		}
		r := agentAttrs(n.ResourceAttributes)
		out = append(out, AgentObservation{n.TraceID, n.SpanID, n.ParentSpanID, run, role, textAttr(a, "traceforge.provider.mode"), textAttr(a, "gen_ai.provider.name"), textAttr(a, "traceforge.outcome"), textAttr(a, "error.type"), n.ServiceName, textAttr(r, "deployment.environment.name"), textAttr(r, "service.namespace"), n.StartTime, n.DurationNS, n.StatusCode, usage[n.SpanID]})
	}
	return out
}
