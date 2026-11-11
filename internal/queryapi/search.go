package queryapi

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"traceforge/internal/traces"
)

var errInput = errors.New("invalid query parameters or cursor")
var servicePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$`)

type cursor struct {
	Version int             `json:"v"`
	From    time.Time       `json:"from"`
	To      time.Time       `json:"to"`
	Service string          `json:"service"`
	Status  string          `json:"status"`
	Limit   int             `json:"limit"`
	After   traces.Position `json:"after"`
}

func validTime(t time.Time) bool {
	return !t.Before(time.Unix(0, 0)) && !t.After(time.Unix(0, 1<<63-1))
}
func parseUTC(s string) (time.Time, error) {
	t, e := time.Parse(time.RFC3339Nano, s)
	if e != nil || !strings.HasSuffix(s, "Z") || !validTime(t) {
		return time.Time{}, errInput
	}
	return t.UTC(), nil
}

func ParseSearch(values url.Values, now time.Time, maxPage int) (traces.SearchQuery, error) {
	for k, v := range values {
		if len(v) != 1 {
			return traces.SearchQuery{}, errInput
		}
		switch k {
		case "from", "to", "service", "status", "limit", "cursor":
		default:
			return traces.SearchQuery{}, errInput
		}
	}
	q := traces.SearchQuery{To: now.UTC(), Limit: 20}
	if q.Limit > maxPage {
		q.Limit = maxPage
	}
	var saved *cursor
	if values.Has("cursor") {
		s := values.Get("cursor")
		if len(s) > 2048 {
			return q, errInput
		}
		b, e := base64.RawURLEncoding.DecodeString(s)
		if e != nil {
			return q, errInput
		}
		var c cursor
		d := json.NewDecoder(bytes.NewReader(b))
		d.DisallowUnknownFields()
		if d.Decode(&c) != nil || d.Decode(new(any)) != io.EOF || c.Version != 1 {
			return q, errInput
		}
		q.From = c.From
		q.To = c.To
		q.Service = c.Service
		q.Status = c.Status
		q.Limit = c.Limit
		q.After = &c.After
		saved = &c
	}
	var err error
	if values.Has("to") {
		q.To, err = parseUTC(values.Get("to"))
		if err != nil {
			return q, err
		}
	}
	if saved == nil {
		q.From = q.To.Add(-time.Hour)
	}
	if values.Has("from") {
		q.From, err = parseUTC(values.Get("from"))
		if err != nil {
			return q, err
		}
	}
	if values.Has("service") {
		q.Service = values.Get("service")
	}
	if values.Has("status") {
		q.Status = values.Get("status")
	}
	if values.Has("limit") {
		q.Limit, err = strconv.Atoi(values.Get("limit"))
		if err != nil {
			return q, errInput
		}
	}
	if !validTime(q.From) || !validTime(q.To) || !q.From.Before(q.To) || q.To.Sub(q.From) > 24*time.Hour || q.Limit < 1 || q.Limit > maxPage || (q.Service != "" && !servicePattern.MatchString(q.Service)) || (q.Status != "" && q.Status != "error") {
		return q, errInput
	}
	if saved != nil {
		_, fromOffset := q.From.Zone()
		_, toOffset := q.To.Zone()
		_, afterOffset := q.After.LatestStart.Zone()
		if fromOffset != 0 || toOffset != 0 || afterOffset != 0 || !q.From.Equal(saved.From) || !q.To.Equal(saved.To) || q.Service != saved.Service || q.Status != saved.Status || q.Limit != saved.Limit || !traces.ValidID(q.After.TraceID, 16) || q.After.LatestStart.Before(q.From) || !q.After.LatestStart.Before(q.To) {
			return q, errInput
		}
	}
	return q, nil
}
func encodeCursor(q traces.SearchQuery, last traces.Summary) string {
	b, _ := json.Marshal(cursor{Version: 1, From: q.From, To: q.To, Service: q.Service, Status: q.Status, Limit: q.Limit, After: traces.Position{LatestStart: last.LatestStart, TraceID: last.TraceID}})
	return base64.RawURLEncoding.EncodeToString(b)
}
