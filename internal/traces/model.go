// Package traces interprets bounded observed span records, independent of HTTP
// and ClickHouse. It never infers unobserved spans or delivery guarantees.
package traces

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

var (
	ErrTooLarge = errors.New("trace exceeds detail limits")
	ErrCorrupt  = errors.New("stored span data is invalid")
	ErrBackend  = errors.New("query backend unavailable")
)

const HardMaxRows = 2000

func ValidID(id string, bytes int) bool {
	if len(id) != bytes*2 || id != strings.ToLower(id) || id == strings.Repeat("0", bytes*2) {
		return false
	}
	_, err := hex.DecodeString(id)
	return err == nil
}

type Record struct {
	TraceID            string          `json:"trace_id"`
	SpanID             string          `json:"span_id"`
	ParentSpanID       string          `json:"parent_span_id"`
	ServiceName        string          `json:"service_name"`
	SpanName           string          `json:"span_name"`
	SpanKind           uint8           `json:"span_kind"`
	StartTime          time.Time       `json:"start_time"`
	EndTime            time.Time       `json:"end_time"`
	DurationNS         uint64          `json:"duration_ns"`
	StatusCode         uint8           `json:"status_code"`
	StatusMessage      string          `json:"status_message"`
	ResourceAttributes json.RawMessage `json:"resource_attributes"`
	SpanAttributes     json.RawMessage `json:"span_attributes"`
	ScopeName          string          `json:"scope_name"`
	ScopeVersion       string          `json:"scope_version"`
	Events             json.RawMessage `json:"events"`
	Links              json.RawMessage `json:"links"`
	Resource           json.RawMessage `json:"resource"`
	Scope              json.RawMessage `json:"scope"`
	Span               json.RawMessage `json:"span"`
	ResourceSchemaURL  string          `json:"resource_schema_url"`
	ScopeSchemaURL     string          `json:"scope_schema_url"`
	IngestedAt         time.Time       `json:"ingested_at"`
}

type Node struct {
	Record
	RelativeStartNS    int64   `json:"relative_start_ns"`
	DetachedParentEdge bool    `json:"detached_parent_edge"`
	Children           []*Node `json:"children"`
}
type Conflict struct {
	SpanID   string `json:"span_id"`
	Variants int    `json:"variants"`
}
type TimingWarning struct {
	SpanID       string `json:"span_id"`
	ParentSpanID string `json:"parent_span_id,omitempty"`
	Code         string `json:"code"`
}
type Diagnostics struct {
	PhysicalRows               int             `json:"physical_rows"`
	UniqueSpans                int             `json:"unique_spans"`
	ExactDuplicateRows         int             `json:"exact_duplicate_rows"`
	ConflictingDuplicateGroups int             `json:"conflicting_duplicate_groups"`
	Conflicts                  []Conflict      `json:"conflicts"`
	ObservedRootCount          int             `json:"observed_root_count"`
	OrphanSpanCount            int             `json:"orphan_span_count"`
	MissingParentCount         int             `json:"missing_parent_count"`
	MissingParentIDs           []string        `json:"missing_parent_ids"`
	CycleCount                 int             `json:"cycle_count"`
	Cycles                     [][]string      `json:"cycles"`
	MultipleRoots              bool            `json:"multiple_roots"`
	HasDetectedGaps            bool            `json:"has_detected_gaps"`
	TimingWarnings             []TimingWarning `json:"timing_warnings"`
	Completeness               string          `json:"completeness"`
}
type Envelope struct {
	StartTime  time.Time `json:"start_time"`
	EndTime    time.Time `json:"end_time"`
	DurationNS int64     `json:"duration_ns"`
}
type Detail struct {
	TraceID           string      `json:"trace_id"`
	RootSpans         []*Node     `json:"root_spans"`
	OrphanSpans       []*Node     `json:"orphan_spans"`
	DisconnectedSpans []*Node     `json:"disconnected_spans"`
	ObservedServices  []string    `json:"observed_services"`
	Envelope          *Envelope   `json:"observed_time_envelope"`
	Diagnostics       Diagnostics `json:"diagnostics"`
}

type SearchQuery struct {
	From, To        time.Time
	Service, Status string
	Limit           int
	After           *Position
}
type Position struct {
	LatestStart time.Time `json:"latest_start"`
	TraceID     string    `json:"trace_id"`
}
type Summary struct {
	TraceID     string    `json:"trace_id"`
	FirstStart  time.Time `json:"first_observed_start"`
	LastEnd     time.Time `json:"last_observed_end"`
	LatestStart time.Time `json:"latest_observed_start"`
	Services    []string  `json:"observed_services"`
	UniqueSpans uint64    `json:"unique_span_count"`
	HasError    bool      `json:"has_error_span"`
}
