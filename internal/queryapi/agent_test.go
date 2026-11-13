package queryapi

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
	"traceforge/internal/traces"
)

func agentRecord(span int, role string, extra map[string]any) traces.Record {
	r := validRecord()
	r.SpanID = fmt.Sprintf("%016x", span)
	r.StartTime = r.StartTime.Add(time.Duration(span) * time.Millisecond)
	r.EndTime = r.StartTime.Add(time.Millisecond)
	a := map[string]any{"traceforge.run.id": strings.Repeat("a", 32), "traceforge.operation.kind": role, "traceforge.provider.mode": "fixture"}
	for k, v := range extra {
		a[k] = v
	}
	attrs := []any{}
	for k, v := range a {
		value := map[string]any{}
		switch x := v.(type) {
		case int:
			value["intValue"] = fmt.Sprint(x)
		case bool:
			value["boolValue"] = x
		default:
			value["stringValue"] = x
		}
		attrs = append(attrs, map[string]any{"key": k, "value": value})
	}
	r.SpanAttributes, _ = json.Marshal(attrs)
	return r
}
func priced(input, output int) map[string]any {
	return map[string]any{
		"traceforge.usage.accounting": "attempt", "gen_ai.usage.input_tokens": input, "gen_ai.usage.output_tokens": output, "gen_ai.usage.cache_read.input_tokens": 0,
		"traceforge.pricing.currency": "USD", "traceforge.pricing.version": "fixture-v1", "traceforge.pricing.source": "https://example.invalid/fixture", "traceforge.pricing.input_per_million": "0.50", "traceforge.pricing.output_per_million": "2.00", "traceforge.pricing.cached_input_per_million": "0.10", "traceforge.pricing.hypothetical": true,
	}
}
func TestAgentCanonicalAttemptAccounting(t *testing.T) {
	a, b := agentRecord(1, "attempt", priced(1200, 100)), agentRecord(2, "attempt", priced(1800, 300))
	rows := []traces.Record{a, b, a, b, agentRecord(3, "agent", priced(3000, 400)), agentRecord(4, "model", priced(3000, 400))}
	d, e := traces.Reconstruct(traceID, rows, 100)
	if e != nil {
		t.Fatal(e)
	}
	s := summarizeAgent(d)
	run := s.Runs[0]
	if run.ProviderAttempts != 2 || run.ModelCalls != 1 || run.KnownInput != "3000" || run.KnownOutput != "400" || run.EstimatedCost == nil || *run.EstimatedCost != "2300000" || s.Completeness != "unknown" || s.Diagnostics.ExactDuplicateRows != 2 {
		t.Fatalf("bad summary: %+v %+v", run, s.Diagnostics)
	}
	// A timed out attempt remains unknown even if the successful retry has usage.
	rows = append(rows, agentRecord(5, "attempt", map[string]any{"traceforge.usage.accounting": "attempt", "error.type": "provider_timeout"}))
	d, e = traces.Reconstruct(traceID, rows, 100)
	if e != nil {
		t.Fatal(e)
	}
	run = summarizeAgent(d).Runs[0]
	if run.ProviderAttempts != 3 || run.EstimatedCost != nil || run.KnownCost != "2300000" || run.UsageCoverage != "incomplete" {
		t.Fatalf("invented totals: %+v", run)
	}
}
func TestAgentSubsetPricingAndMissingValues(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(map[string]any)
		cost   string
	}{
		{"cached", func(a map[string]any) {
			a["gen_ai.usage.cache_read.input_tokens"] = 40
			a["gen_ai.usage.reasoning.output_tokens"] = 10
		}, "74000"},
		{"missing_cached", func(a map[string]any) { delete(a, "gen_ai.usage.cache_read.input_tokens") }, ""},
		{"invalid_subset", func(a map[string]any) { a["gen_ai.usage.cache_read.input_tokens"] = 101 }, ""},
		{"missing_pricing", func(a map[string]any) { delete(a, "traceforge.pricing.version") }, ""},
		{"string_token_secret", func(a map[string]any) { a["gen_ai.usage.input_tokens"] = "SECRET" }, ""},
		{"round_half_up", func(a map[string]any) {
			a["gen_ai.usage.input_tokens"] = 1
			a["gen_ai.usage.output_tokens"] = 0
			a["traceforge.pricing.input_per_million"] = "0.0005"
		}, "1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := priced(100, 20)
			tc.change(a)
			d, e := traces.Reconstruct(traceID, []traces.Record{agentRecord(1, "attempt", a)}, 100)
			if e != nil {
				t.Fatal(e)
			}
			r := summarizeAgent(d).Runs[0]
			if tc.cost == "" {
				if r.EstimatedCost != nil {
					t.Fatal("expected unknown")
				}
			} else if r.EstimatedCost == nil || *r.EstimatedCost != tc.cost {
				t.Fatalf("cost %+v", r)
			}
		})
	}
}
func TestAgentEndpointBoundsAndNoAITrace(t *testing.T) {
	for _, tc := range []struct {
		path string
		rows []traces.Record
		code int
	}{
		{traceID, []traces.Record{validRecord()}, 200}, {traceID, nil, 404}, {"abc", nil, 400}, {traceID + "?limit=1", nil, 400},
	} {
		path := "/api/v1/traces/" + tc.path + "/agent-summary"
		if strings.Contains(tc.path, "?") {
			path = "/api/v1/traces/" + traceID + "/agent-summary?limit=1"
		}
		w := call(handlerFor(t, &fakeRepo{rows: tc.rows}, time.Second), path)
		if w.Code != tc.code {
			t.Fatalf("%s %d %s", path, w.Code, w.Body)
		}
	}
}
