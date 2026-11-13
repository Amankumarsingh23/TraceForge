// Package logs owns log normalization, independent bounded persistence and query
// semantics. Correlation identifiers never serve as log event identities.
package logs

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

const (
	MaxMessageBytes = 4 << 20
	MaxRecords      = 1000
	MaxRecordBytes  = 64 << 10
)

var ErrBackend = errors.New("log backend unavailable")
var ErrTooLarge = errors.New("log query response exceeds limits")
var ErrCorrupt = errors.New("stored log cannot be interpreted")

type Row struct {
	LogID                  string          `json:"log_id"`
	SourceEventID          string          `json:"source_event_id"`
	TraceID                string          `json:"trace_id"`
	SpanID                 string          `json:"span_id"`
	TraceFlags             uint32          `json:"trace_flags"`
	ServiceName            string          `json:"service_name"`
	SeverityNumber         uint8           `json:"severity_number"`
	SeverityText           string          `json:"severity_text"`
	EventTime              *time.Time      `json:"event_time"`
	ObservedTime           *time.Time      `json:"observed_time"`
	EffectiveTime          time.Time       `json:"effective_time"`
	TimeSource             string          `json:"time_source"`
	BodyText               string          `json:"body_text"`
	BodyJSON               json.RawMessage `json:"body"`
	AttributesJSON         json.RawMessage `json:"attributes"`
	ResourceAttributesJSON json.RawMessage `json:"resource_attributes"`
	ScopeName              string          `json:"scope_name"`
	ScopeVersion           string          `json:"scope_version"`
	EventName              string          `json:"event_name"`
	ResourceJSON           json.RawMessage `json:"resource"`
	ScopeJSON              json.RawMessage `json:"scope"`
	RecordJSON             json.RawMessage `json:"record"`
	ResourceSchemaURL      string          `json:"resource_schema_url"`
	ScopeSchemaURL         string          `json:"scope_schema_url"`
	IngestedAt             time.Time       `json:"ingested_at"`
}
type Query struct {
	From        time.Time `json:"from"`
	To          time.Time `json:"to"`
	Service     string    `json:"service"`
	MinSeverity int       `json:"min_severity"`
	TraceID     string    `json:"trace_id"`
	SpanID      string    `json:"span_id"`
	Text        string    `json:"text"`
	Limit       int       `json:"limit"`
	After       *Position `json:"after,omitempty"`
}
type Position struct {
	Time  time.Time `json:"time"`
	LogID string    `json:"log_id"`
}
type Repository interface {
	SearchLogs(context.Context, Query) ([]Row, error)
}
