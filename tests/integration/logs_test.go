//go:build integration

package integration

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	collectorlogs "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	common "go.opentelemetry.io/proto/otlp/common/v1"
	logpb "go.opentelemetry.io/proto/otlp/logs/v1"
	resource "go.opentelemetry.io/proto/otlp/resource/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
	logsapi "traceforge/internal/logs"
)

type logPage struct {
	Logs []logsapi.Row `json:"logs"`
	Next string        `json:"next_cursor"`
}

func logClient(t *testing.T) collectorlogs.LogsServiceClient {
	t.Helper()
	c, e := grpc.NewClient("localhost:4319", grpc.WithTransportCredentials(insecure.NewCredentials()))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { c.Close() })
	return collectorlogs.NewLogsServiceClient(c)
}
func logRequest(t *testing.T) (*collectorlogs.ExportLogsServiceRequest, string) {
	t.Helper()
	tid := id(t, 16)
	return &collectorlogs.ExportLogsServiceRequest{ResourceLogs: []*logpb.ResourceLogs{{Resource: &resource.Resource{Attributes: []*common.KeyValue{attr("service.name", "s06-"+hex.EncodeToString(id(t, 8)))}}, ScopeLogs: []*logpb.ScopeLogs{{Scope: &common.InstrumentationScope{Name: "traceforge/session06-test"}, LogRecords: []*logpb.LogRecord{{TraceId: tid, SpanId: id(t, 8), TimeUnixNano: uint64(time.Now().UnixNano()), SeverityNumber: logpb.SeverityNumber_SEVERITY_NUMBER_INFO, Body: &common.AnyValue{Value: &common.AnyValue_StringValue{StringValue: "session06 repeated event"}}}}}}}}}, hex.EncodeToString(tid)
}
func fetchLogs(t *testing.T, path string) logPage {
	t.Helper()
	var page logPage
	if code := api(t, path, &page); code != 200 {
		t.Fatalf("log API returned %d", code)
	}
	return page
}
func saveLogEvidence(t *testing.T, name string, value any) {
	t.Helper()
	dir := filepath.Join("..", "..", "artifacts", "session06")
	if os.MkdirAll(dir, 0755) != nil {
		t.Fatal("create evidence directory failed")
	}
	raw, e := json.MarshalIndent(value, "", "  ")
	if e != nil {
		t.Fatal(e)
	}
	if os.WriteFile(filepath.Join(dir, name), raw, 0600) != nil {
		t.Fatal("save evidence failed")
	}
}
func TestLogStorageQueryPrivacyAndReplay(t *testing.T) {
	c := logClient(t)
	db := database(t)
	req, tid := logRequest(t)
	ss := req.ResourceLogs[0].ScopeLogs[0]
	base := ss.LogRecords[0]
	base.TimeUnixNano = uint64(time.Now().UTC().Truncate(time.Second).Add(123456789 * time.Nanosecond).UnixNano())
	base.ObservedTimeUnixNano = base.TimeUnixNano + 100
	base.Attributes = []*common.KeyValue{attr("log.record.uid", "synthetic-source-event")}
	service := req.ResourceLogs[0].Resource.Attributes[0].Value.GetStringValue()
	spanID := hex.EncodeToString(base.SpanId)
	ss.LogRecords = []*logpb.LogRecord{base, proto.Clone(base).(*logpb.LogRecord), proto.Clone(base).(*logpb.LogRecord)}
	traceOnly := proto.Clone(base).(*logpb.LogRecord)
	traceOnly.SpanId = nil
	traceOnly.Body = &common.AnyValue{Value: &common.AnyValue_StringValue{StringValue: "session06 trace-only"}}
	ss.LogRecords = append(ss.LogRecords, traceOnly)
	secret := proto.Clone(base).(*logpb.LogRecord)
	secret.Attributes = append(secret.Attributes, attr("authorization", "s06-private-auth"))
	secret.Body = &common.AnyValue{Value: &common.AnyValue_KvlistValue{KvlistValue: &common.KeyValueList{Values: []*common.KeyValue{attr("password", "s06-private-password"), {Key: "large", Value: &common.AnyValue{Value: &common.AnyValue_IntValue{IntValue: 9223372036854775807}}}, {Key: "nested", Value: &common.AnyValue{Value: &common.AnyValue_ArrayValue{ArrayValue: &common.ArrayValue{Values: []*common.AnyValue{{Value: &common.AnyValue_StringValue{StringValue: "inline Bearer s06-private-token"}}}}}}}}}}}
	ss.LogRecords = append(ss.LogRecords, secret, &logpb.LogRecord{TraceId: []byte{1}})
	standalone := proto.Clone(base).(*logpb.LogRecord)
	standalone.TraceId = nil
	standalone.SpanId = nil
	standalone.TimeUnixNano = 0
	standalone.Body = &common.AnyValue{Value: &common.AnyValue_StringValue{StringValue: "session06 standalone"}}
	fallback := proto.Clone(standalone).(*logpb.LogRecord)
	fallback.ObservedTimeUnixNano = 0
	fallback.Body = &common.AnyValue{Value: &common.AnyValue_StringValue{StringValue: "session06 ingestion fallback"}}
	req.ResourceLogs = append(req.ResourceLogs, &logpb.ResourceLogs{Resource: proto.Clone(req.ResourceLogs[0].Resource).(*resource.Resource), ScopeLogs: []*logpb.ScopeLogs{{LogRecords: []*logpb.LogRecord{standalone, fallback}}}})
	req.ResourceLogs[0].Resource.Attributes = append(req.ResourceLogs[0].Resource.Attributes, attr("api_key", "s06-private-key"))
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	for run := 1; run <= 2; run++ {
		ack, e := c.Export(ctx, req)
		if e != nil || ack.GetPartialSuccess().GetRejectedLogRecords() != 1 {
			t.Fatal("bad partial persistence response", e, ack)
		}
		var count, unique uint64
		if e = db.Conn.QueryRow(ctx, "SELECT count(),uniqExact(log_id) FROM traceforge.logs WHERE service_name = ?", service).Scan(&count, &unique); e != nil || count != uint64(run*7) || unique != count {
			t.Fatal("physical log rows collapsed or missing", count, unique, e)
		}
	}
	var raw string
	var ns int64
	if e := db.Conn.QueryRow(ctx, "SELECT concat(body_text,body_json,attributes_json,resource_json,record_json),toUnixTimestamp64Nano(event_time) FROM traceforge.logs WHERE trace_id = ? AND position(body_json, '9223372036854775807') > 0 LIMIT 1", tid).Scan(&raw, &ns); e != nil {
		t.Fatal(e)
	}
	if ns != int64(base.TimeUnixNano) || !strings.Contains(raw, "9223372036854775807") {
		t.Fatal("lost nanoseconds/int64")
	}
	for _, s := range []string{"s06-private-auth", "s06-private-password", "s06-private-token", "s06-private-key"} {
		if strings.Contains(raw, s) {
			t.Fatal("secret persisted")
		}
	}
	tracePage := fetchLogs(t, "/api/v1/traces/"+tid+"/logs")
	spanPage := fetchLogs(t, "/api/v1/traces/"+tid+"/logs?span_id="+spanID)
	if len(tracePage.Logs) != 10 || len(spanPage.Logs) != 8 {
		t.Fatal("correlation scope wrong", len(tracePage.Logs), len(spanPage.Logs))
	}
	serialized, _ := json.Marshal(tracePage)
	if strings.Contains(string(serialized), "s06-private-") {
		t.Fatal("secret exposed via API")
	}
	if api(t, "/api/v1/traces/"+tid, nil) != 404 {
		t.Fatal("fixture should have no observed trace yet")
	}
	// The same span ID under another trace must never enter the selected span page.
	other, otherID := logRequest(t)
	other.ResourceLogs[0].ScopeLogs[0].LogRecords[0].SpanId = base.SpanId
	if _, e := c.Export(ctx, other); e != nil {
		t.Fatal(e)
	}
	if len(fetchLogs(t, "/api/v1/traces/"+tid+"/logs?span_id="+spanID).Logs) != 8 {
		t.Fatal("matched span ID without trace")
	}
	all := fetchLogs(t, "/api/v1/logs?service="+service)
	if len(all.Logs) != 14 {
		t.Fatal("standalone rows lost")
	}
	fallbackCount, standaloneCount := 0, 0
	for _, row := range all.Logs {
		if row.TraceID == "" {
			standaloneCount++
		}
		if row.TimeSource == "ingestion" {
			fallbackCount++
			if row.EventTime != nil || row.ObservedTime != nil {
				t.Fatal("fallback masquerades as source time")
			}
		}
	}
	if standaloneCount != 4 || fallbackCount != 2 {
		t.Fatal("optional context/time lost")
	}
	q := url.Values{"service": {service}, "limit": {"2"}}
	seen := map[string]bool{}
	pages := 0
	var last *logsapi.Row
	for {
		page := fetchLogs(t, "/api/v1/logs?"+q.Encode())
		pages++
		for _, r := range page.Logs {
			if seen[r.LogID] {
				t.Fatal("duplicate cursor row")
			}
			seen[r.LogID] = true
			if last != nil && (r.EffectiveTime.After(last.EffectiveTime) || r.EffectiveTime.Equal(last.EffectiveTime) && r.LogID >= last.LogID) {
				t.Fatal("incorrect deterministic ordering")
			}
			copy := r
			last = &copy
		}
		if page.Next == "" {
			break
		}
		if pages > 8 {
			t.Fatal("pagination did not terminate")
		}
		q = url.Values{"cursor": {page.Next}}
	}
	if len(seen) != 14 || pages != 7 {
		t.Fatal("pagination dropped records")
	}
	literal := fetchLogs(t, "/api/v1/logs?service="+service+"&text="+url.QueryEscape("%_?"))
	if len(literal.Logs) != 0 {
		t.Fatal("literal text interpreted as query pattern")
	}
	traceReq, _ := request(t)
	traceReq.ResourceSpans[0].ScopeSpans[0].Spans[0].TraceId = base.TraceId
	if _, e := grpcClient(t).Export(ctx, traceReq); e != nil {
		t.Fatal(e)
	}
	if len(fetchLogs(t, "/api/v1/traces/"+tid+"/logs").Logs) != 10 {
		t.Fatal("late trace changed independent logs")
	}
	saveLogEvidence(t, "fixtures.json", map[string]any{"trace_id": tid, "span_id": spanID, "other_trace_id": otherID, "service": service, "physical_rows": 14, "trace_logs": 10, "span_logs": 8, "standalone_rows": 4, "fallback_rows": 2, "pages": pages, "from": time.Unix(0, int64(base.TimeUnixNano)).Add(-time.Minute).UTC().Format(time.RFC3339Nano), "to": time.Unix(0, int64(base.TimeUnixNano)).Add(time.Minute).UTC().Format(time.RFC3339Nano)})
	t.Logf("service=%s trace=%s span=%s rows=14 unique_log_ids=14 trace_logs=10 span_logs=8 standalone=4 fallback=2 pages=7; replay retained, secrets absent, exact ns=%d", service, tid, spanID, ns)
}
func TestRealDemoLogCorrelation(t *testing.T) {
	evidence := map[string]any{}
	client := &http.Client{Timeout: 6 * time.Second}
	for _, scenario := range []struct {
		name, path string
		status     int
	}{{"normal", "", 200}, {"delayed", "?delay_ms=500", 200}, {"failed", "?fail=true", 502}} {
		t.Run(scenario.name, func(t *testing.T) {
			response, e := client.Get("http://localhost:8080/checkout" + scenario.path)
			if e != nil {
				t.Fatal(e)
			}
			var body struct {
				TraceID string `json:"trace_id"`
			}
			e = json.NewDecoder(response.Body).Decode(&body)
			response.Body.Close()
			if e != nil || response.StatusCode != scenario.status {
				t.Fatal("bad demo HTTP response")
			}
			deadline := time.Now().Add(20 * time.Second)
			var page logPage
			var paymentID string
			for time.Now().Before(deadline) {
				page = fetchLogs(t, "/api/v1/traces/"+body.TraceID+"/logs")
				var d struct {
					RootSpans   []json.RawMessage `json:"root_spans"`
					Diagnostics struct {
						Unique int `json:"unique_spans"`
					} `json:"diagnostics"`
				}
				if api(t, "/api/v1/traces/"+body.TraceID, &d) == 200 && d.Diagnostics.Unique == 5 && len(page.Logs) >= 5 {
					trace := detail(t, body.TraceID)
					for _, node := range flatten(trace) {
						if node.SpanName == "payment.process" {
							paymentID = node.SpanID
						}
					}
					if paymentID != "" {
						break
					}
				}
				time.Sleep(150 * time.Millisecond)
			}
			if paymentID == "" {
				t.Fatal("trace/log data did not arrive")
			}
			observed := flatten(detail(t, body.TraceID))
			validationFound := false
			for _, row := range page.Logs {
				node := observed[row.SpanID]
				if row.TraceID != body.TraceID || node == nil || node.ServiceName != row.ServiceName {
					t.Fatal("log points to the wrong observed trace/span/service")
				}
				if row.BodyText == "checkout validation" {
					validationFound = node.SpanName == "checkout.validate"
				}
			}
			if !validationFound {
				t.Fatal("validation log lost its active validation span")
			}
			spanPage := fetchLogs(t, "/api/v1/traces/"+body.TraceID+"/logs?span_id="+paymentID)
			foundStart, foundEnd, foundDelay, foundError := false, false, false, false
			for _, row := range spanPage.Logs {
				if row.TraceID != body.TraceID || row.SpanID != paymentID || row.ServiceName != "payment" {
					t.Fatal("wrong active span correlation")
				}
				switch row.BodyText {
				case "payment started":
					foundStart = true
				case "payment completion":
					foundEnd = true
				case "controlled payment delay":
					foundDelay = row.SeverityNumber == 13
				case "payment failure":
					foundError = row.SeverityNumber == 17
				}
			}
			if !foundStart || scenario.name == "failed" && !foundError || scenario.name != "failed" && !foundEnd || scenario.name == "delayed" && !foundDelay {
				t.Fatal("real demo messages missing", spanPage)
			}
			if len(page.Logs) <= len(spanPage.Logs) {
				t.Fatal("trace and span scopes not distinguished")
			}
			evidence[scenario.name] = map[string]any{"trace_id": body.TraceID, "payment_span_id": paymentID, "trace_logs": len(page.Logs), "span_logs": len(spanPage.Logs), "http_status": response.StatusCode}
			t.Logf("%s trace=%s payment.process=%s trace_logs=%d payment_logs=%d HTTP=%d", scenario.name, body.TraceID, paymentID, len(page.Logs), len(spanPage.Logs), response.StatusCode)
		})
	}
	startup := fetchLogs(t, "/api/v1/logs?service=payment&text=service+startup")
	if len(startup.Logs) == 0 || startup.Logs[0].TraceID != "" || startup.Logs[0].SpanID != "" {
		t.Fatal("real standalone startup log missing")
	}
	saveLogEvidence(t, "demo.json", evidence)
}
func TestMixedSignalLogAndTraceBudgets(t *testing.T) {
	logsClient, tracesClient := logClient(t), grpcClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var wg sync.WaitGroup
	outcomes := make(chan error, 16)
	for range 8 {
		logReq, _ := logRequest(t)
		traceReq, _ := request(t)
		wg.Add(2)
		go func() {
			defer wg.Done()
			ack, e := logsClient.Export(ctx, logReq)
			if e == nil && ack.PartialSuccess != nil {
				e = fmt.Errorf("unexpected partial logs")
			}
			outcomes <- e
		}()
		go func() {
			defer wg.Done()
			ack, e := tracesClient.Export(ctx, traceReq)
			if e == nil && ack.PartialSuccess != nil {
				e = fmt.Errorf("unexpected partial traces")
			}
			outcomes <- e
		}()
	}
	wg.Wait()
	close(outcomes)
	for e := range outcomes {
		if e != nil {
			t.Fatal(e)
		}
	}
	for _, path := range []string{"/debug/ingestion", "/debug/log-ingestion"} {
		var state struct {
			Queue    int `json:"queue"`
			Pending  int `json:"pending_requests"`
			Reserved int `json:"reserved_bytes"`
		}
		if api(t, path, &state) != 200 || state.Queue != 0 || state.Pending != 0 || state.Reserved != 0 {
			t.Fatal("mixed-signal reservation leak", path, state)
		}
	}
	t.Log("8 concurrent log + 8 trace requests confirmed; both independent queues and payload reservations settled to zero")
}
func TestLogDatabaseUnavailable(t *testing.T) {
	if os.Getenv("TRACEFORGE_EXPECT_DB_DOWN") != "1" {
		t.Skip("controlled outage only")
	}
	req, tid := logRequest(t)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	ack, e := logClient(t).Export(ctx, req)
	if status.Code(e) != codes.Unavailable || ack != nil {
		t.Fatal("false log persistence success", e)
	}
	if api(t, "/api/v1/logs", nil) != 503 || api(t, "/api/v1/traces/"+tid+"/logs", nil) != 503 {
		t.Fatal("outage returned log success")
	}
	t.Logf("log trace=%s Export Unavailable, no ack; log/search correlation APIs 503", tid)
}
