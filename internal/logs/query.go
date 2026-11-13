package logs

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"traceforge/internal/traces"
)

var ErrInput = errors.New("invalid log filters, UTC window or cursor")
var servicePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$`)

type cursor struct {
	Version int   `json:"v"`
	Query   Query `json:"q"`
}

func utc(value string) (time.Time, error) {
	t, e := time.Parse(time.RFC3339Nano, value)
	if e != nil || !strings.HasSuffix(value, "Z") || t.Before(time.Unix(0, 0)) || t.After(time.Unix(0, mathMaxInt64)) {
		return time.Time{}, ErrInput
	}
	return t.UTC(), nil
}

const mathMaxInt64 = 1<<63 - 1

func sameFilters(a, b Query) bool {
	return a.From.Equal(b.From) && a.To.Equal(b.To) && a.Service == b.Service && a.MinSeverity == b.MinSeverity && a.TraceID == b.TraceID && a.SpanID == b.SpanID && a.Text == b.Text && a.Limit == b.Limit
}
func ParseQuery(values url.Values, traceID string, now time.Time) (Query, error) {
	q := Query{To: now.UTC(), Limit: 50}
	q.From = q.To.Add(-time.Hour)
	if traceID != "" {
		q.From = q.To.Add(-24 * time.Hour)
	}
	for k, v := range values {
		if len(v) != 1 {
			return q, ErrInput
		}
		switch k {
		case "from", "to", "service", "min_severity", "trace_id", "span_id", "text", "limit", "cursor":
		default:
			return q, ErrInput
		}
	}
	var saved *Query
	if values.Has("cursor") {
		s := values.Get("cursor")
		if len(s) > 2048 || s == "" {
			return q, ErrInput
		}
		raw, e := base64.RawURLEncoding.DecodeString(s)
		if e != nil {
			return q, ErrInput
		}
		var c cursor
		d := json.NewDecoder(bytes.NewReader(raw))
		d.DisallowUnknownFields()
		if d.Decode(&c) != nil || d.Decode(new(any)) != io.EOF || c.Version != 1 || c.Query.After == nil {
			return q, ErrInput
		}
		q = c.Query
		saved = &c.Query
	}
	if traceID != "" && !traces.ValidID(traceID, 16) {
		return q, ErrInput
	}
	var e error
	if values.Has("to") {
		q.To, e = utc(values.Get("to"))
		if e != nil {
			return q, e
		}
		if saved == nil {
			q.From = q.To.Add(-time.Hour)
			if traceID != "" {
				q.From = q.To.Add(-24 * time.Hour)
			}
		}
	}
	if values.Has("from") {
		q.From, e = utc(values.Get("from"))
		if e != nil {
			return q, e
		}
	}
	for key, target := range map[string]*string{"service": &q.Service, "trace_id": &q.TraceID, "span_id": &q.SpanID, "text": &q.Text} {
		if values.Has(key) {
			*target = values.Get(key)
		}
	}
	if values.Has("min_severity") {
		q.MinSeverity, e = strconv.Atoi(values.Get("min_severity"))
		if e != nil {
			return q, ErrInput
		}
	}
	if values.Has("limit") {
		q.Limit, e = strconv.Atoi(values.Get("limit"))
		if e != nil {
			return q, ErrInput
		}
	}
	if traceID != "" {
		if q.TraceID != "" && q.TraceID != traceID {
			return q, ErrInput
		}
		q.TraceID = traceID
	}
	if !q.From.Before(q.To) || q.To.Sub(q.From) > 24*time.Hour || q.From.Before(time.Unix(0, 0)) || q.To.After(time.Unix(0, mathMaxInt64)) || q.MinSeverity < 0 || q.MinSeverity > 24 || q.Limit < 1 || q.Limit > 100 || q.Service != "" && !servicePattern.MatchString(q.Service) || !text(q.Text, 256) || q.TraceID != "" && !traces.ValidID(q.TraceID, 16) || q.SpanID != "" && (!traces.ValidID(q.SpanID, 8) || q.TraceID == "") {
		return q, ErrInput
	}
	if saved != nil {
		if !sameFilters(q, *saved) || !traces.ValidID(q.After.LogID, 16) || q.After.Time.Before(q.From) || !q.After.Time.Before(q.To) {
			return q, ErrInput
		}
		for _, t := range []time.Time{q.From, q.To, q.After.Time} {
			_, offset := t.Zone()
			if offset != 0 {
				return q, ErrInput
			}
		}
	}
	return q, nil
}
func EncodeCursor(q Query, last Row) string {
	q.After = &Position{Time: last.EffectiveTime, LogID: last.LogID}
	b, _ := json.Marshal(cursor{1, q})
	return base64.RawURLEncoding.EncodeToString(b)
}

type queryHandler struct {
	repo  Repository
	slots chan struct{}
}

func NewQueryHandler(repo Repository) http.Handler {
	h := &queryHandler{repo: repo, slots: make(chan struct{}, 2)}
	m := http.NewServeMux()
	m.HandleFunc("/api/v1/logs", h.search)
	m.HandleFunc("/api/v1/traces/{trace_id}/logs", h.search)
	return m
}
func writeError(w http.ResponseWriter, code int, name, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"code": name, "message": message}})
}
func (h *queryHandler) search(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		w.Header().Set("Allow", "GET")
		writeError(w, 405, "method_not_allowed", "use GET")
		return
	}
	values, e := url.ParseQuery(r.URL.RawQuery)
	if e != nil {
		writeError(w, 400, "invalid_log_query", ErrInput.Error())
		return
	}
	q, e := ParseQuery(values, r.PathValue("trace_id"), time.Now())
	if e != nil {
		writeError(w, 400, "invalid_log_query", ErrInput.Error())
		return
	}
	select {
	case h.slots <- struct{}{}:
		defer func() { <-h.slots }()
	default:
		writeError(w, 503, "query_busy", "log query concurrency limit reached")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	rows, e := h.repo.SearchLogs(ctx, q)
	if e != nil {
		switch {
		case errors.Is(e, ErrTooLarge):
			writeError(w, 413, "logs_too_large", "log response exceeds supported size")
		case errors.Is(e, context.DeadlineExceeded):
			writeError(w, 504, "query_timeout", "log query deadline exceeded")
		case errors.Is(e, context.Canceled):
			writeError(w, 408, "query_canceled", "log query canceled")
		case errors.Is(e, ErrCorrupt):
			writeError(w, 500, "stored_data_invalid", "stored log data could not be interpreted")
		default:
			writeError(w, 503, "backend_unavailable", "log backend unavailable or resource limit exceeded")
		}
		return
	}
	next := ""
	if len(rows) > q.Limit {
		rows = rows[:q.Limit]
		next = EncodeCursor(q, rows[len(rows)-1])
	}
	if rows == nil {
		rows = []Row{}
	}
	result := struct {
		Logs        []Row     `json:"logs"`
		Next        string    `json:"next_cursor"`
		From        time.Time `json:"from"`
		To          time.Time `json:"to"`
		Consistency string    `json:"consistency"`
	}{rows, next, q.From, q.To, "non_snapshot_raw_rows"}
	raw, e := json.Marshal(result)
	if e != nil {
		writeError(w, 500, "stored_data_invalid", "stored log data invalid")
		return
	}
	if len(raw) > 8<<20 {
		writeError(w, 413, "logs_too_large", "log response exceeds supported size")
		return
	}
	if ctx.Err() != nil {
		writeError(w, 504, "query_timeout", "log query deadline exceeded")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(raw)
}
