// Package ingest implements the OTLP trace persistence boundary.
package ingest

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	collector "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	common "go.opentelemetry.io/proto/otlp/common/v1"
	resource "go.opentelemetry.io/proto/otlp/resource/v1"
	trace "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"math"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	MaxMessageBytes = 4 << 20
	MaxSpans        = 1000
	MaxSpanBytes    = 64 << 10
)

// JSON columns use protobuf JSON, retaining AnyValue's explicit type tags and
// int64 strings. Complete sanitized messages preserve flags and dropped counts.
type Row struct {
	TraceID, SpanID, ParentSpanID, ServiceName, SpanName                 string
	SpanKind                                                             uint8
	StartTime, EndTime                                                   time.Time
	DurationNS                                                           uint64
	StatusCode                                                           uint8
	StatusMessage                                                        string
	ResourceAttributesJSON, SpanAttributesJSON                           string
	ScopeName, ScopeVersion, EventsJSON, LinksJSON                       string
	ResourceJSON, ScopeJSON, SpanJSON, ResourceSchemaURL, ScopeSchemaURL string
}

func validID(id []byte, size int) bool {
	return len(id) == size && !bytes.Equal(id, make([]byte, size))
}
func validTime(n uint64) bool     { return n > 0 && n <= math.MaxInt64 }
func text(s string, max int) bool { return len(s) <= max && utf8.ValidString(s) }

func sensitive(key string) bool {
	k := strings.ToLower(key)
	k = strings.NewReplacer(".", "", "_", "", "-", "", "[", "", "]", "", " ", "").Replace(k)
	for _, exact := range []string{"prompt", "completion", "genaiprompt", "genaicompletion", "genaiinputmessages", "genaioutputmessages", "toolarguments"} {
		if k == exact {
			return true
		}
	}
	for _, word := range []string{"authorization", "password", "passwd", "secret", "token", "apikey", "credential", "cookie", "privatekey", "email", "phone", "ssn", "creditcard"} {
		if strings.Contains(k, word) {
			return true
		}
	}
	return false
}

func safeString(s string) string {
	l := strings.ToLower(s)
	if strings.HasPrefix(l, "bearer ") || strings.HasPrefix(l, "basic ") || strings.Contains(l, "private key-----") || strings.Contains(l, "password=") || strings.Contains(l, "api_key=") || strings.Contains(l, "token=") {
		return "[REDACTED]"
	}
	if u, e := url.Parse(s); e == nil && u.Host != "" && (u.Scheme == "http" || u.Scheme == "https") {
		changed := u.User != nil
		u.User = nil
		q := u.Query()
		for k := range q {
			if sensitive(k) {
				q.Set(k, "[REDACTED]")
				changed = true
			}
		}
		if changed {
			u.RawQuery = q.Encode()
			return u.String()
		}
	}
	return s
}

// attrs validates recursively, with a total nested item budget per attribute set.
// It only operates on cloned messages so the incoming request is never mutated.
func attrs(kvs []*common.KeyValue) error { budget := 256; return sanitizeAttrs(kvs, 0, &budget) }

// GenAI usage counters are public numerical telemetry, not credential tokens.
// Only exact current semantic keys with bounded integer values bypass the
// conservative token-key rule; strings and unrelated keys remain redacted.
func publicGenAIUsage(key string, value *common.AnyValue) bool {
	switch key {
	case "gen_ai.usage.input_tokens", "gen_ai.usage.output_tokens", "gen_ai.usage.cache_read.input_tokens", "gen_ai.usage.reasoning.output_tokens":
	default:
		return false
	}
	if value == nil {
		return false
	}
	v, ok := value.Value.(*common.AnyValue_IntValue)
	return ok && v.IntValue >= 0 && v.IntValue <= 10000000
}

// SanitizeAttributes and SanitizeValue apply the existing bounded typed-value
// policy to caller-owned clones. Log ingestion adds its free-form text policy.
func SanitizeAttributes(kvs []*common.KeyValue) error { return attrs(kvs) }
func SanitizeValue(v *common.AnyValue) error          { budget := 256; return sanitizeValue(v, 0, &budget) }
func SanitizeText(s string) string                    { return safeString(s) }
func sanitizeAttrs(kvs []*common.KeyValue, depth int, budget *int) error {
	if depth > 8 {
		return errors.New("attribute nesting limit")
	}
	for _, kv := range kvs {
		*budget--
		if *budget < 0 || kv == nil || !text(kv.Key, 256) || kv.Key == "" {
			return errors.New("attribute limit or malformed key")
		}
		if err := sanitizeValue(kv.Value, depth+1, budget); err != nil {
			return err
		}
		if sensitive(kv.Key) && !publicGenAIUsage(kv.Key, kv.Value) {
			kv.Value = &common.AnyValue{Value: &common.AnyValue_StringValue{StringValue: "[REDACTED]"}}
		}
	}
	return nil
}
func sanitizeValue(v *common.AnyValue, depth int, budget *int) error {
	if v == nil || v.Value == nil || depth > 8 {
		return errors.New("malformed attribute value")
	}
	switch x := v.Value.(type) {
	case *common.AnyValue_StringValue:
		if !text(x.StringValue, 4096) {
			return errors.New("attribute string limit")
		}
		x.StringValue = safeString(x.StringValue)
	case *common.AnyValue_BytesValue:
		if len(x.BytesValue) > 4096 {
			return errors.New("attribute bytes limit")
		}
	case *common.AnyValue_ArrayValue:
		if x.ArrayValue == nil {
			return errors.New("nil array")
		}
		for _, item := range x.ArrayValue.Values {
			*budget--
			if *budget < 0 {
				return errors.New("array limit")
			}
			if err := sanitizeValue(item, depth+1, budget); err != nil {
				return err
			}
		}
	case *common.AnyValue_KvlistValue:
		if x.KvlistValue == nil {
			return errors.New("nil kvlist")
		}
		return sanitizeAttrs(x.KvlistValue.Values, depth+1, budget)
	}
	return nil
}

func encode(m proto.Message) (string, error) { b, e := protojson.Marshal(m); return string(b), e }
func encodeList[T proto.Message](items []T) (string, error) {
	values := make([]json.RawMessage, 0, len(items))
	for _, item := range items {
		b, e := protojson.Marshal(item)
		if e != nil {
			return "", e
		}
		values = append(values, b)
	}
	b, e := json.Marshal(values)
	return string(b), e
}

// Normalize rejects malformed individual spans but treats excessive request
// size/count and malformed group envelopes as request-level errors.
func Normalize(ctx context.Context, req *collector.ExportTraceServiceRequest) ([]Row, int64, error) {
	if req == nil || proto.Size(req) > MaxMessageBytes || len(req.ResourceSpans) > MaxSpans {
		return nil, 0, errors.New("invalid export envelope or size")
	}
	total, scopes := 0, 0
	for _, rs := range req.ResourceSpans {
		if rs == nil {
			return nil, 0, errors.New("nil resource group")
		}
		scopes += len(rs.ScopeSpans)
		if scopes > MaxSpans {
			return nil, 0, errors.New("scope group limit")
		}
		for _, ss := range rs.ScopeSpans {
			if ss == nil {
				return nil, 0, errors.New("nil scope group")
			}
			total += len(ss.Spans)
			if total > MaxSpans {
				return nil, 0, errors.New("span count limit")
			}
		}
	}
	rows := make([]Row, 0, total)
	var rejected int64
	for _, rs := range req.ResourceSpans {
		for _, ss := range rs.ScopeSpans {
			for _, s := range ss.Spans {
				if err := ctx.Err(); err != nil {
					return nil, 0, err
				}
				r, err := normalizeSpan(rs, ss, s)
				if err != nil {
					rejected++
					continue
				}
				rows = append(rows, r)
			}
		}
	}
	return rows, rejected, nil
}

func normalizeSpan(rs *trace.ResourceSpans, ss *trace.ScopeSpans, original *trace.Span) (Row, error) {
	var r Row
	if original == nil || !validID(original.TraceId, 16) || !validID(original.SpanId, 8) || (len(original.ParentSpanId) > 0 && !validID(original.ParentSpanId, 8)) {
		return r, errors.New("invalid identifier")
	}
	if !validTime(original.StartTimeUnixNano) || !validTime(original.EndTimeUnixNano) || original.EndTimeUnixNano < original.StartTimeUnixNano {
		return r, errors.New("invalid timestamp")
	}
	if original.Kind < 0 || original.Kind > 5 || original.GetStatus().GetCode() < 0 || original.GetStatus().GetCode() > 2 || !text(original.Name, 1024) || original.Name == "" || !text(original.TraceState, 512) || !text(original.GetStatus().GetMessage(), 4096) || len(original.Events) > 128 || len(original.Links) > 128 || proto.Size(original) > MaxSpanBytes || proto.Size(rs.Resource) > 16384 || proto.Size(ss.Scope) > 16384 || !text(rs.SchemaUrl, 4096) || !text(ss.SchemaUrl, 4096) {
		return r, errors.New("span field limit")
	}
	s := proto.Clone(original).(*trace.Span)
	res := &resource.Resource{}
	if rs.Resource != nil {
		res = proto.Clone(rs.Resource).(*resource.Resource)
	}
	scope := &common.InstrumentationScope{}
	if ss.Scope != nil {
		scope = proto.Clone(ss.Scope).(*common.InstrumentationScope)
	}
	if !text(scope.Name, 1024) || !text(scope.Version, 1024) {
		return r, errors.New("scope text limit")
	}
	s.Name = safeString(s.Name)
	s.TraceState = safeString(s.TraceState)
	scope.Name = safeString(scope.Name)
	scope.Version = safeString(scope.Version)
	if s.Status != nil {
		s.Status.Message = safeString(s.Status.Message)
	}
	for _, set := range [][]*common.KeyValue{res.Attributes, scope.Attributes, s.Attributes} {
		if err := attrs(set); err != nil {
			return r, err
		}
	}
	for _, ev := range s.Events {
		if ev == nil || !validTime(ev.TimeUnixNano) || !text(ev.Name, 1024) {
			return r, errors.New("invalid event")
		}
		if err := attrs(ev.Attributes); err != nil {
			return r, err
		}
		ev.Name = safeString(ev.Name)
	}
	for _, link := range s.Links {
		if link == nil || !validID(link.TraceId, 16) || !validID(link.SpanId, 8) || !text(link.TraceState, 512) {
			return r, errors.New("invalid link")
		}
		if err := attrs(link.Attributes); err != nil {
			return r, err
		}
		link.TraceState = safeString(link.TraceState)
	}
	r.ServiceName = "unknown_service"
	for _, kv := range res.Attributes {
		if kv.Key == "service.name" && kv.Value.GetStringValue() != "" {
			r.ServiceName = kv.Value.GetStringValue()
		}
	}
	r.TraceID = hex.EncodeToString(s.TraceId)
	r.SpanID = hex.EncodeToString(s.SpanId)
	r.ParentSpanID = hex.EncodeToString(s.ParentSpanId)
	r.SpanName = s.Name
	r.SpanKind = uint8(s.Kind)
	r.StartTime = time.Unix(0, int64(s.StartTimeUnixNano)).UTC()
	r.EndTime = time.Unix(0, int64(s.EndTimeUnixNano)).UTC()
	r.DurationNS = s.EndTimeUnixNano - s.StartTimeUnixNano
	r.StatusCode = uint8(s.GetStatus().GetCode())
	r.StatusMessage = s.GetStatus().GetMessage()
	r.ScopeName = scope.Name
	r.ScopeVersion = scope.Version
	r.ResourceSchemaURL = safeString(rs.SchemaUrl)
	r.ScopeSchemaURL = safeString(ss.SchemaUrl)
	var err error
	for _, pair := range []struct {
		target *string
		msg    proto.Message
	}{{&r.ResourceJSON, res}, {&r.ScopeJSON, scope}, {&r.SpanJSON, s}} {
		*pair.target, err = encode(pair.msg)
		if err != nil {
			return Row{}, errors.New("invalid protobuf text")
		}
	}
	r.ResourceAttributesJSON, err = encodeList(res.Attributes)
	if err != nil {
		return Row{}, err
	}
	r.SpanAttributesJSON, err = encodeList(s.Attributes)
	if err != nil {
		return Row{}, err
	}
	r.EventsJSON, err = encodeList(s.Events)
	if err != nil {
		return Row{}, err
	}
	r.LinksJSON, err = encodeList(s.Links)
	if err != nil {
		return Row{}, err
	}
	return r, nil
}
