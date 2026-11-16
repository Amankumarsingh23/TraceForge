package security

import (
	"context"
	"encoding/json"
	"errors"
	collectorlog "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	collectormetric "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	collectortrace "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	common "go.opentelemetry.io/proto/otlp/common/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"time"
)

type Backend struct {
	HTTP string
	Conn *grpc.ClientConn
}
type bucket struct {
	tokens       float64
	at           time.Time
	ingest, read int
}
type Gateway struct {
	Auth     Authenticator
	Backends map[string]Backend
	mu       sync.Mutex
	state    map[string]*bucket
	global   int
}

func NewGateway(auth Authenticator, backends map[string]Backend) *Gateway {
	return &Gateway{Auth: auth, Backends: backends, state: map[string]*bucket{}}
}
func (g *Gateway) admit(id string, ingest bool) (func(), bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	b := g.state[id]
	if b == nil {
		b = &bucket{tokens: 12, at: time.Now()}
		g.state[id] = b
	}
	now := time.Now()
	b.tokens += now.Sub(b.at).Seconds() * 8
	if b.tokens > 12 {
		b.tokens = 12
	}
	b.at = now
	n := b.read
	if ingest {
		n = b.ingest
	}
	if b.tokens < 1 || n >= 2 || g.global >= 6 {
		return nil, false
	}
	b.tokens--
	g.global++
	if ingest {
		b.ingest++
	} else {
		b.read++
	}
	return func() {
		g.mu.Lock()
		defer g.mu.Unlock()
		g.global--
		if ingest {
			b.ingest--
		} else {
			b.read--
		}
	}, true
}
func bearer(values []string) (string, error) {
	if len(values) != 1 || !strings.HasPrefix(values[0], "Bearer ") {
		return "", ErrInvalid
	}
	return strings.TrimPrefix(values[0], "Bearer "), nil
}
func (g *Gateway) authenticate(ctx context.Context, values []string) (Identity, error) {
	key, e := bearer(values)
	if e != nil {
		return Identity{}, e
	}
	return g.Auth.Authenticate(ctx, key)
}
func failHTTP(w http.ResponseWriter, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"code": "security_boundary", "message": http.StatusText(code)}})
}
func (g *Gateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/healthz" {
		w.WriteHeader(200)
		return
	}
	i, e := g.authenticate(r.Context(), r.Header.Values("Authorization"))
	if e != nil {
		code := 401
		if errors.Is(e, ErrUnavailable) {
			code = 503
		}
		failHTTP(w, code)
		return
	}
	backend, ok := g.Backends[i.Tenant]
	if !ok {
		failHTTP(w, 403)
		return
	}
	if h := r.Header.Get("X-Tenant-ID"); h != "" && h != i.Tenant {
		failHTTP(w, 403)
		return
	}
	if r.Method == "GET" && r.URL.Path == "/api/v1/security/identity" {
		release, ok := g.admit(i.Tenant, false)
		if !ok {
			failHTTP(w, 429)
			return
		}
		defer release()
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(i)
		return
	}
	scope := "read"
	target := ""
	if strings.HasPrefix(r.URL.Path, "/api/v1/manage/") {
		scope = "manage"
		switch r.URL.Path {
		case "/api/v1/manage/slos":
			target = "/internal/security/slos"
		case "/api/v1/manage/evaluate":
			target = "/internal/security/evaluate"
		case "/api/v1/manage/diagnostics":
			target = "/debug/ingestion"
		default:
			failHTTP(w, 404)
			return
		}
		if (target == "/debug/ingestion" && r.Method != "GET") || (target != "/debug/ingestion" && r.Method != "POST") {
			failHTTP(w, 405)
			return
		}
	} else if r.Method != "GET" || !strings.HasPrefix(r.URL.Path, "/api/v1/") {
		failHTTP(w, 404)
		return
	}
	if !i.Allows(scope) {
		failHTTP(w, 403)
		return
	}
	release, ok := g.admit(i.Tenant, false)
	if !ok {
		failHTTP(w, 429)
		return
	}
	defer release()
	if r.URL.Path == "/api/v1/security/session" {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(i)
		return
	}
	origin, e := url.Parse(backend.HTTP)
	if e != nil {
		failHTTP(w, 503)
		return
	}
	proxy := httputil.NewSingleHostReverseProxy(origin)
	proxy.Transport = &http.Transport{ResponseHeaderTimeout: 4 * time.Second, MaxConnsPerHost: 4, DisableKeepAlives: true}
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, e error) { failHTTP(w, 503) }
	proxy.ModifyResponse = func(resp *http.Response) error {
		resp.Header.Set("Cache-Control", "no-store")
		resp.Header.Del("Set-Cookie")
		return nil
	}
	r = r.Clone(r.Context())
	r.Header = r.Header.Clone()
	r.Header.Del("Authorization")
	r.Header.Del("Cookie")
	r.Header.Del("X-Tenant-ID")
	r.Header.Del("Forwarded")
	r.Header.Del("X-Forwarded-For")
	r.Header.Del("X-Forwarded-Host")
	if target != "" {
		r.URL.Path = target
		r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	}
	proxy.ServeHTTP(w, r)
}

func tenantSpoof(msg proto.Message, owner string) bool {
	bad := false
	nodes, depth := 0, 0
	var walk func(protoreflect.Message)
	walk = func(m protoreflect.Message) {
		nodes++
		depth++
		defer func() { depth-- }()
		if nodes > 65536 || depth > 32 {
			bad = true
			return
		}
		if bad {
			return
		}
		if kv, ok := m.Interface().(*common.KeyValue); ok {
			key := strings.ToLower(strings.ReplaceAll(kv.Key, "_", "."))
			if key == "tenant" || key == "tenant.id" || key == "traceforge.tenant.id" {
				v, ok := kv.Value.GetValue().(*common.AnyValue_StringValue)
				if !ok || v.StringValue != owner {
					bad = true
					return
				}
			}
		}
		m.Range(func(fd protoreflect.FieldDescriptor, v protoreflect.Value) bool {
			if fd.Kind() != protoreflect.MessageKind {
				return !bad
			}
			if fd.IsList() {
				l := v.List()
				for n := 0; n < l.Len(); n++ {
					walk(l.Get(n).Message())
				}
			} else if !fd.IsMap() {
				walk(v.Message())
			}
			return !bad
		})
	}
	walk(msg.ProtoReflect())
	return bad
}
func (g *Gateway) ingestion(ctx context.Context, msg proto.Message) (Backend, func(), error) {
	md, _ := metadata.FromIncomingContext(ctx)
	i, e := g.authenticate(ctx, md.Get("authorization"))
	if e != nil {
		c := codes.Unauthenticated
		if errors.Is(e, ErrUnavailable) {
			c = codes.Unavailable
		}
		return Backend{}, nil, status.Error(c, "authentication unavailable or invalid")
	}
	if !i.Allows("ingest") {
		return Backend{}, nil, status.Error(codes.PermissionDenied, "ingest permission required")
	}
	for _, v := range md.Get("x-tenant-id") {
		if v != i.Tenant {
			return Backend{}, nil, status.Error(codes.PermissionDenied, "foreign tenant claim")
		}
	}
	if tenantSpoof(msg, i.Tenant) {
		return Backend{}, nil, status.Error(codes.PermissionDenied, "foreign tenant claim")
	}
	if proto.Size(msg) > 1<<20 {
		return Backend{}, nil, status.Error(codes.ResourceExhausted, "tenant request byte budget")
	}
	backend, ok := g.Backends[i.Tenant]
	if !ok || backend.Conn == nil {
		return Backend{}, nil, status.Error(codes.Unavailable, "tenant backend unavailable")
	}
	release, ok := g.admit(i.Tenant, true)
	if !ok {
		return Backend{}, nil, status.Error(codes.ResourceExhausted, "tenant/global admission budget")
	}
	return backend, release, nil
}

type TraceGateway struct {
	collectortrace.UnimplementedTraceServiceServer
	G *Gateway
}

func (s TraceGateway) Export(ctx context.Context, r *collectortrace.ExportTraceServiceRequest) (*collectortrace.ExportTraceServiceResponse, error) {
	b, done, e := s.G.ingestion(ctx, r)
	if e != nil {
		return nil, e
	}
	defer done()
	ctx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	return collectortrace.NewTraceServiceClient(b.Conn).Export(ctx, r)
}

type LogGateway struct {
	collectorlog.UnimplementedLogsServiceServer
	G *Gateway
}

func (s LogGateway) Export(ctx context.Context, r *collectorlog.ExportLogsServiceRequest) (*collectorlog.ExportLogsServiceResponse, error) {
	b, done, e := s.G.ingestion(ctx, r)
	if e != nil {
		return nil, e
	}
	defer done()
	ctx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	return collectorlog.NewLogsServiceClient(b.Conn).Export(ctx, r)
}

type MetricGateway struct {
	collectormetric.UnimplementedMetricsServiceServer
	G *Gateway
}

func (s MetricGateway) Export(ctx context.Context, r *collectormetric.ExportMetricsServiceRequest) (*collectormetric.ExportMetricsServiceResponse, error) {
	b, done, e := s.G.ingestion(ctx, r)
	if e != nil {
		return nil, e
	}
	defer done()
	ctx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	return collectormetric.NewMetricsServiceClient(b.Conn).Export(ctx, r)
}
