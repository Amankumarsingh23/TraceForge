package queryapi

import (
	"encoding/json"
	"math/big"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"time"
	"traceforge/internal/traces"
)

// This is a projection of the canonical detail forest, never a second ingestion
// path. Only provider-attempt spans form the accounting boundary.
type agentRun struct {
	AgentName             string           `json:"agent_name"`
	ObservedDurationNS    string           `json:"observed_duration_ns"`
	RunID                 string           `json:"run_id"`
	Mode                  string           `json:"mode"`
	Outcome               string           `json:"outcome"`
	Versions              map[string]any   `json:"versions"`
	ModelCalls            int              `json:"model_calls"`
	ProviderAttempts      int              `json:"provider_attempts"`
	ToolExecutions        int              `json:"tool_executions"`
	KnownInput            string           `json:"known_input_tokens"`
	KnownOutput           string           `json:"known_output_tokens"`
	KnownCached           string           `json:"known_cached_input_tokens"`
	KnownReasoning        string           `json:"known_reasoning_output_tokens"`
	UsageCoverage         string           `json:"usage_coverage"`
	CompleteUsageAttempts int              `json:"complete_usage_attempts"`
	CostCoverage          string           `json:"cost_coverage"`
	KnownCost             string           `json:"known_estimated_nano_usd"`
	EstimatedCost         *string          `json:"estimated_nano_usd"`
	PricedAttempts        int              `json:"priced_attempts"`
	Operations            []map[string]any `json:"operations"`
}
type agentSummary struct {
	TraceID      string             `json:"trace_id"`
	Completeness string             `json:"completeness"`
	Accounting   string             `json:"accounting"`
	Diagnostics  traces.Diagnostics `json:"diagnostics"`
	Runs         []*agentRun        `json:"runs"`
}

func (h *handler) agentSummary(w http.ResponseWriter, r *http.Request) { h.readDetail(w, r, true) }

type attribute struct {
	Key   string                     `json:"key"`
	Value map[string]json.RawMessage `json:"value"`
}

func agentAttrs(raw json.RawMessage) map[string]any {
	var values []attribute
	out := map[string]any{}
	if json.Unmarshal(raw, &values) != nil {
		return out
	}
	for _, a := range values {
		if len(a.Value) != 1 {
			continue
		}
		for kind, raw := range a.Value {
			switch kind {
			case "stringValue":
				var s string
				if json.Unmarshal(raw, &s) == nil && len(s) <= 256 {
					out[a.Key] = s
				}
			case "intValue":
				var s string
				if json.Unmarshal(raw, &s) == nil {
					if n, e := strconv.ParseInt(s, 10, 64); e == nil {
						out[a.Key] = n
					}
				}
			case "boolValue":
				var b bool
				if json.Unmarshal(raw, &b) == nil {
					out[a.Key] = b
				}
			case "arrayValue":
				var array struct {
					Values []map[string]json.RawMessage `json:"values"`
				}
				if json.Unmarshal(raw, &array) != nil || len(array.Values) > 5 {
					continue
				}
				list := []any{}
				for _, item := range array.Values {
					for t, v := range item {
						switch t {
						case "stringValue":
							var s string
							if json.Unmarshal(v, &s) == nil && len(s) <= 128 {
								list = append(list, s)
							}
						case "intValue":
							var s string
							if json.Unmarshal(v, &s) == nil {
								if n, e := strconv.ParseInt(s, 10, 64); e == nil {
									list = append(list, n)
								}
							}
						case "doubleValue":
							var f float64
							if json.Unmarshal(v, &f) == nil {
								list = append(list, f)
							}
						}
					}
				}
				out[a.Key] = list
			}
		}
	}
	return out
}
func textAttr(a map[string]any, k string) string { s, _ := a[k].(string); return s }
func counter(a map[string]any, k string) *int64 {
	n, ok := a[k].(int64)
	if !ok || n < 0 || n > 10_000_000 {
		return nil
	}
	return &n
}

var ratePattern = regexp.MustCompile(`^(0|[1-9][0-9]{0,4})(\.[0-9]{1,9})?$`)

func rate(a map[string]any, k string) *big.Rat {
	s := textAttr(a, "traceforge.pricing."+k)
	if !ratePattern.MatchString(s) {
		return nil
	}
	r, ok := new(big.Rat).SetString(s)
	if !ok || r.Cmp(big.NewRat(10000, 1)) > 0 {
		return nil
	}
	return r
}
func attemptCost(a map[string]any, input, output, cached *int64) *big.Int {
	if input == nil || output == nil || textAttr(a, "traceforge.pricing.currency") != "USD" || textAttr(a, "traceforge.pricing.version") == "" || textAttr(a, "traceforge.pricing.source") == "" {
		return nil
	}
	in, out := rate(a, "input_per_million"), rate(a, "output_per_million")
	if in == nil || out == nil {
		return nil
	}
	c := int64(0)
	cache := new(big.Rat)
	if _, ok := a["traceforge.pricing.cached_input_per_million"]; ok {
		cache = rate(a, "cached_input_per_million")
		if cache == nil || cached == nil || *cached > *input {
			return nil
		}
		c = *cached
	}
	total := new(big.Rat).Mul(big.NewRat(*input-c, 1), in)
	total.Add(total, new(big.Rat).Mul(big.NewRat(c, 1), cache))
	total.Add(total, new(big.Rat).Mul(big.NewRat(*output, 1), out))
	total.Mul(total, big.NewRat(1000, 1)) // USD/million -> integer nano-USD.
	total.Add(total, big.NewRat(1, 2))    // positive decimal ROUND_HALF_UP per attempt.
	return new(big.Int).Quo(total.Num(), total.Denom())
}

func summarizeAgent(d *traces.Detail) agentSummary {
	result := agentSummary{TraceID: d.TraceID, Completeness: "unknown", Accounting: "canonical_provider_attempts_only; estimated_not_billed", Diagnostics: d.Diagnostics, Runs: []*agentRun{}}
	nodes := append(append(append([]*traces.Node{}, d.RootSpans...), d.OrphanSpans...), d.DisconnectedSpans...)
	all := []*traces.Node{}
	seen := map[string]bool{}
	for len(nodes) > 0 {
		n := nodes[len(nodes)-1]
		nodes = nodes[:len(nodes)-1]
		if seen[n.SpanID] {
			continue
		}
		seen[n.SpanID] = true
		all = append(all, n)
		nodes = append(nodes, n.Children...)
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].StartTime.Equal(all[j].StartTime) {
			return all[i].SpanID < all[j].SpanID
		}
		return all[i].StartTime.Before(all[j].StartTime)
	})
	runs := map[string]*agentRun{}
	totals := map[string][4]int64{}
	costs := map[string]*big.Int{}
	bounds := map[string][2]time.Time{}
	for _, n := range all {
		a := agentAttrs(n.SpanAttributes)
		id := textAttr(a, "traceforge.run.id")
		role := textAttr(a, "traceforge.operation.kind")
		if !traces.ValidID(id, 16) {
			continue
		}
		switch role {
		case "agent", "model", "attempt", "retrieval", "tool", "finalize", "http":
		default:
			continue
		}
		run := runs[id]
		if run == nil {
			run = &agentRun{RunID: id, Mode: textAttr(a, "traceforge.provider.mode"), Outcome: "unknown", Versions: map[string]any{}, Operations: []map[string]any{}}
			runs[id] = run
			result.Runs = append(result.Runs, run)
			costs[id] = new(big.Int)
			bounds[id] = [2]time.Time{n.StartTime, n.EndTime}
		}
		window := bounds[id]
		if n.StartTime.Before(window[0]) {
			window[0] = n.StartTime
		}
		if n.EndTime.After(window[1]) {
			window[1] = n.EndTime
		}
		bounds[id] = window
		if name := textAttr(a, "gen_ai.agent.name"); name != "" {
			run.AgentName = name
		}
		if run.Mode != textAttr(a, "traceforge.provider.mode") {
			run.Mode = "mixed_or_unknown"
		}
		op := map[string]any{"span_id": n.SpanID, "role": role, "duration_ns": strconv.FormatUint(n.DurationNS, 10), "status_code": n.StatusCode}
		for _, key := range []string{"traceforge.outcome", "error.type", "gen_ai.provider.name", "gen_ai.request.model", "gen_ai.response.model", "gen_ai.response.id", "gen_ai.response.finish_reasons", "traceforge.model.call_index", "traceforge.provider.attempt", "traceforge.usage.source", "traceforge.usage.status", "traceforge.retrieval.top_k", "traceforge.retrieval.document_ids", "traceforge.retrieval.ranks", "traceforge.retrieval.scores", "traceforge.context.document_ids", "gen_ai.tool.name", "traceforge.tool.executed", "traceforge.pricing.currency", "traceforge.pricing.input_per_million", "traceforge.pricing.output_per_million", "traceforge.pricing.cached_input_per_million", "traceforge.pricing.version", "traceforge.pricing.source", "traceforge.pricing.hypothetical"} {
			if value, ok := a[key]; ok {
				op[key] = value
			}
		}
		for _, key := range []string{"traceforge.prompt.version", "traceforge.corpus.version", "traceforge.retriever.version", "traceforge.agent.version", "traceforge.gen_ai.convention"} {
			if value, ok := a[key]; ok {
				run.Versions[key] = value
			}
		}
		switch role {
		case "agent":
			run.Outcome = textAttr(a, "traceforge.outcome")
			if run.Outcome == "" {
				run.Outcome = "unknown"
			}
		case "model":
			run.ModelCalls++
		case "tool":
			if a["traceforge.tool.executed"] == true {
				run.ToolExecutions++
			}
		case "attempt":
			run.ProviderAttempts++
			if textAttr(a, "traceforge.usage.accounting") != "attempt" {
				break
			}
			input, output := counter(a, "gen_ai.usage.input_tokens"), counter(a, "gen_ai.usage.output_tokens")
			cached, reasoning := counter(a, "gen_ai.usage.cache_read.input_tokens"), counter(a, "gen_ai.usage.reasoning.output_tokens")
			// Invalid subset claims invalidate the whole attempt's accounting.
			if cached != nil && (input == nil || *cached > *input) || reasoning != nil && (output == nil || *reasoning > *output) {
				input = nil
				output = nil
				cached = nil
				reasoning = nil
				op["traceforge.usage.status"] = "invalid"
			}
			t := totals[id]
			for i, c := range []*int64{input, output, cached, reasoning} {
				if c != nil {
					t[i] += *c
					op[[]string{"input_tokens", "output_tokens", "cached_input_tokens", "reasoning_output_tokens"}[i]] = strconv.FormatInt(*c, 10)
				}
			}
			totals[id] = t
			if input != nil && output != nil {
				run.CompleteUsageAttempts++
			}
			if cost := attemptCost(a, input, output, cached); cost != nil {
				costs[id].Add(costs[id], cost)
				run.PricedAttempts++
				op["estimated_nano_usd"] = cost.String()
			}
		}
		run.Operations = append(run.Operations, op)
	}
	for _, run := range result.Runs {
		window := bounds[run.RunID]
		run.ObservedDurationNS = strconv.FormatInt(window[1].Sub(window[0]).Nanoseconds(), 10)
		if run.AgentName == "" {
			run.AgentName = "unknown"
		}
		t := totals[run.RunID]
		run.KnownInput = strconv.FormatInt(t[0], 10)
		run.KnownOutput = strconv.FormatInt(t[1], 10)
		run.KnownCached = strconv.FormatInt(t[2], 10)
		run.KnownReasoning = strconv.FormatInt(t[3], 10)
		run.UsageCoverage = "incomplete"
		run.CostCoverage = "incomplete"
		if run.ProviderAttempts == 0 {
			run.UsageCoverage = "unknown"
			run.CostCoverage = "unknown"
		} else {
			if run.CompleteUsageAttempts == run.ProviderAttempts {
				run.UsageCoverage = "complete_for_observed_attempts"
			}
			if run.PricedAttempts == run.ProviderAttempts {
				run.CostCoverage = "complete_for_observed_attempts"
				v := costs[run.RunID].String()
				run.EstimatedCost = &v
			}
		}
		run.KnownCost = costs[run.RunID].String()
	}
	return result
}
