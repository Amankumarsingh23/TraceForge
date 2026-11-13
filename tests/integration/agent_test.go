//go:build integration

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	collector "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	common "go.opentelemetry.io/proto/otlp/common/v1"
	resource "go.opentelemetry.io/proto/otlp/resource/v1"
	trace "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type agentTotals struct {
	RunID         string           `json:"run_id"`
	ModelCalls    int              `json:"model_calls"`
	Attempts      int              `json:"provider_attempts"`
	Tools         int              `json:"tool_executions"`
	Input         string           `json:"known_input_tokens"`
	Output        string           `json:"known_output_tokens"`
	Cost          *string          `json:"estimated_nano_usd"`
	KnownCost     string           `json:"known_estimated_nano_usd"`
	UsageCoverage string           `json:"usage_coverage"`
	Operations    []map[string]any `json:"operations"`
}
type agentEnvelope struct {
	Runs         []agentTotals `json:"runs"`
	Completeness string        `json:"completeness"`
}

func TestAgentExecutionStoragePrivacyAndReplay(t *testing.T) {
	db := database(t)
	client := &http.Client{Timeout: 25 * time.Second}
	health, err := client.Get("http://127.0.0.1:8084/healthz")
	if err != nil {
		t.Fatal(err)
	}
	var mode struct {
		Mode string `json:"mode"`
	}
	err = json.NewDecoder(io.LimitReader(health.Body, 1024)).Decode(&mode)
	health.Body.Close()
	if err != nil || health.StatusCode != 200 || mode.Mode != "fixture" {
		t.Fatal("fixture-only integration refuses non-fixture server; no billable calls permitted")
	}
	evidence := map[string]any{}
	for _, tc := range []struct {
		name, scenario, question              string
		steps, calls, attempts, tools, status int
		input, output, cost                   string
	}{
		{"policy", "normal", "What is the refund policy?", 6, 1, 1, 0, 200, "1200", "100", "800000"},
		{"tool", "normal", "Refund for damaged delivery and order status DEMO-1002", 6, 2, 2, 1, 200, "3000", "400", "2300000"},
		{"retry", "retry", "refund policy", 6, 1, 2, 0, 200, "1200", "100", ""},
		{"missing_usage", "missing_usage", "refund policy", 6, 1, 1, 0, 200, "0", "0", ""},
		{"no_results", "no_results", "refund policy", 6, 1, 1, 0, 200, "1200", "100", "800000"},
		{"timeout", "timeout", "refund policy", 6, 1, 2, 0, 504, "0", "0", ""},
		{"permanent", "permanent_error", "refund policy", 6, 1, 1, 0, 502, "0", "0", ""},
		{"bad_arguments", "bad_arguments", "refund policy", 6, 1, 1, 0, 422, "1200", "100", "800000"},
		{"unknown_tool", "unknown_tool", "refund policy", 6, 1, 1, 0, 422, "1200", "100", "800000"},
		{"tool_timeout", "tool_timeout", "order status DEMO-1001", 6, 1, 1, 1, 504, "1200", "100", "800000"},
		{"unknown_order", "unknown_order", "order status DEMO-9999", 6, 1, 1, 1, 422, "1200", "100", "800000"},
		{"max_steps", "max_steps", "order status DEMO-1001", 1, 1, 1, 0, 422, "1200", "100", "800000"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			payload, _ := json.Marshal(map[string]any{"question": tc.question + " password=SESSION07_PRIVACY_MARKER", "scenario": tc.scenario, "max_steps": tc.steps})
			response, e := client.Post("http://127.0.0.1:8084/ask", "application/json", bytes.NewReader(payload))
			if e != nil {
				t.Fatal(e)
			}
			body, e := io.ReadAll(io.LimitReader(response.Body, 65536))
			response.Body.Close()
			if e != nil || response.StatusCode != tc.status {
				t.Fatalf("ask %d %s %v", response.StatusCode, body, e)
			}
			var identity struct {
				RunID   string `json:"run_id"`
				TraceID string `json:"trace_id"`
			}
			if json.Unmarshal(body, &identity) != nil || len(identity.TraceID) != 32 {
				t.Fatal("missing identity")
			}
			var summary agentEnvelope
			deadline := time.Now().Add(20 * time.Second)
			for time.Now().Before(deadline) {
				code := api(t, "/api/v1/traces/"+identity.TraceID+"/agent-summary", &summary)
				if code == 200 && len(summary.Runs) == 1 && summary.Runs[0].Attempts == tc.attempts {
					break
				}
				time.Sleep(200 * time.Millisecond)
			}
			if len(summary.Runs) != 1 {
				t.Fatal("missing persisted agent summary")
			}
			run := summary.Runs[0]
			if run.RunID != identity.RunID || run.ModelCalls != tc.calls || run.Attempts != tc.attempts || run.Tools != tc.tools || run.Input != tc.input || run.Output != tc.output || summary.Completeness != "unknown" {
				t.Fatalf("incorrect actual execution: %+v", run)
			}
			if tc.cost == "" {
				if run.Cost != nil {
					t.Fatal("invented complete estimate")
				}
			} else if run.Cost == nil || *run.Cost != tc.cost {
				t.Fatalf("incorrect estimate: %+v", run)
			}
			// SQL persistence, hydrated detail, projection and correlated logs all exclude raw inputs.
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()
			var secrets uint64
			if e := db.Conn.QueryRow(ctx, `SELECT count() FROM traceforge.spans WHERE trace_id = ? AND position(concat(span_json,resource_json,span_attributes_json),'SESSION07_PRIVACY_MARKER') > 0`, identity.TraceID).Scan(&secrets); e != nil || secrets != 0 {
				t.Fatal("span privacy", e, secrets)
			}
			var logs, secretLogs uint64
			if e := db.Conn.QueryRow(ctx, `SELECT count(),countIf(position(concat(body_json,attributes_json,resource_json,record_json),'SESSION07_PRIVACY_MARKER') > 0) FROM traceforge.logs WHERE trace_id = ?`, identity.TraceID).Scan(&logs, &secretLogs); e != nil || logs == 0 || secretLogs != 0 {
				t.Fatal("log privacy/correlation", e, logs, secretLogs)
			}
			d := detail(t, identity.TraceID)
			encoded, _ := json.Marshal(d)
			if strings.Contains(string(encoded), "SESSION07_PRIVACY_MARKER") {
				t.Fatal("detail contains question")
			}
			if d.Diagnostics.MissingParentCount != 0 || d.Diagnostics.CycleCount != 0 {
				t.Fatal("invalid actual hierarchy", d.Diagnostics)
			}
			if tc.name == "tool" {
				rows, e := db.GetTraceSpans(ctx, identity.TraceID, 100)
				if e != nil {
					t.Fatal(e)
				}
				req := &collector.ExportTraceServiceRequest{}
				for _, r := range rows {
					sp := &trace.Span{}
					rs := &resource.Resource{}
					scope := &common.InstrumentationScope{}
					for _, pair := range []struct {
						raw []byte
						msg proto.Message
					}{{r.Span, sp}, {r.Resource, rs}, {r.Scope, scope}} {
						if e := protojson.Unmarshal(pair.raw, pair.msg); e != nil {
							t.Fatal(e)
						}
					}
					req.ResourceSpans = append(req.ResourceSpans, &trace.ResourceSpans{Resource: rs, SchemaUrl: r.ResourceSchemaURL, ScopeSpans: []*trace.ScopeSpans{{Scope: scope, SchemaUrl: r.ScopeSchemaURL, Spans: []*trace.Span{sp}}}})
				}
				ack, e := grpcClient(t).Export(ctx, req)
				if e != nil || ack.PartialSuccess != nil {
					t.Fatal("replay", e)
				}
				var after agentEnvelope
				api(t, "/api/v1/traces/"+identity.TraceID+"/agent-summary", &after)
				b1, _ := json.Marshal(run)
				b2, _ := json.Marshal(after.Runs[0])
				if !bytes.Equal(b1, b2) {
					t.Fatal("replay changed logical accounting")
				}
				replay := detail(t, identity.TraceID)
				if replay.Diagnostics.PhysicalRows != 2*d.Diagnostics.PhysicalRows || replay.Diagnostics.ExactDuplicateRows != d.Diagnostics.UniqueSpans {
					t.Fatal("replay diagnostics", replay.Diagnostics)
				}
			}
			evidence[tc.name] = map[string]any{"trace_id": identity.TraceID, "run_id": identity.RunID, "model_calls": run.ModelCalls, "attempts": run.Attempts, "tools": run.Tools, "input": run.Input, "output": run.Output, "estimated_nano_usd": run.Cost, "logs": logs, "unique_spans": d.Diagnostics.UniqueSpans}
			t.Logf("%s trace=%s spans=%d logs=%d calls=%d attempts=%d cost=%v", tc.name, identity.TraceID, d.Diagnostics.UniqueSpans, logs, run.ModelCalls, run.Attempts, tc.cost)
		})
	}
	if !t.Failed() {
		dir := filepath.Join("..", "..", "artifacts", "session07")
		if e := os.MkdirAll(dir, 0755); e != nil {
			t.Fatal(e)
		}
		b, _ := json.MarshalIndent(evidence, "", "  ")
		if e := os.WriteFile(filepath.Join(dir, "evidence.json"), b, 0600); e != nil {
			t.Fatal(e)
		}
	}
}
