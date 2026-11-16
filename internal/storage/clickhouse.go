package storage

import (
	"context"
	_ "embed"
	"errors"
	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"os"
	"time"
	"traceforge/internal/ingest"
)

//go:embed migrations/0001_spans.sql
var Migration string

//go:embed migrations/0002_logs.sql
var LogMigration string

//go:embed migrations/0003_metric_points.sql
var MetricMigration string

//go:embed migrations/0004_log_trace_index.sql
var LogTraceIndexMigration string

type Store struct {
	Conn     driver.Conn
	Database string
}

// Open uses existing local credentials, never a DSN containing a password.
func Open(ctx context.Context) (*Store, error) {
	database := os.Getenv("CLICKHOUSE_DATABASE")
	if database == "" {
		database = "traceforge"
	}
	if err := validateDatabase(database); err != nil {
		return nil, err
	}
	addr := os.Getenv("CLICKHOUSE_ADDR")
	if addr == "" {
		addr = "localhost:8123"
	}
	user := os.Getenv("CLICKHOUSE_USER")
	if user == "" {
		user = "traceforge"
	}
	password := os.Getenv("CLICKHOUSE_PASSWORD")
	if password == "" {
		return nil, errors.New("CLICKHOUSE_PASSWORD is required")
	}
	protocol := clickhouse.HTTP
	switch os.Getenv("CLICKHOUSE_PROTOCOL") {
	case "", "http":
	case "native":
		protocol = clickhouse.Native
	default:
		return nil, errors.New("CLICKHOUSE_PROTOCOL must be http or native")
	}
	conn, err := clickhouse.Open(&clickhouse.Options{Addr: []string{addr}, Protocol: protocol, Auth: clickhouse.Auth{Database: database, Username: user, Password: password}, DialTimeout: time.Second, ReadTimeout: 3 * time.Second, MaxOpenConns: 6, MaxIdleConns: 2,
		Settings: clickhouse.Settings{"async_insert": 0, "insert_deduplicate": 0, "max_execution_time": 5}})
	if err != nil {
		return nil, errors.New("ClickHouse connection configuration failed")
	}
	s := &Store{Conn: conn, Database: database}
	if err := s.Ping(ctx); err != nil {
		_ = conn.Close()
		return nil, errors.New("ClickHouse startup readiness failed; check address, credentials and container health")
	}
	return s, nil
}

func (s *Store) Migrate(ctx context.Context) error {
	if err := s.exec(ctx, MetricMigration); err != nil {
		return errors.New("ClickHouse migration 0003 failed; check database permissions and schema")
	}
	if err := s.exec(ctx, Migration); err != nil {
		return errors.New("ClickHouse migration 0001 failed; check database permissions and schema")
	}
	if err := s.exec(ctx, LogMigration); err != nil {
		return errors.New("ClickHouse migration 0002 failed; check database permissions and schema")
	}
	if err := s.exec(ctx, LogTraceIndexMigration); err != nil {
		return errors.New("ClickHouse migration 0004 failed; check index support and permissions")
	}
	return nil
}
func (s *Store) Close() error                   { return s.Conn.Close() }
func (s *Store) Ping(ctx context.Context) error { return s.Conn.Ping(ctx) }

func (s *Store) Insert(ctx context.Context, rows []ingest.Row) error {
	if len(rows) == 0 {
		return nil
	}
	batch, err := s.prepareBatch(ctx, `INSERT INTO traceforge.spans (
trace_id,span_id,parent_span_id,service_name,span_name,span_kind,start_time,end_time,duration_ns,status_code,status_message,
resource_attributes_json,span_attributes_json,scope_name,scope_version,events_json,links_json,resource_json,scope_json,span_json,resource_schema_url,scope_schema_url)`)
	if err != nil {
		return errors.New("prepare span batch failed")
	}
	defer batch.Close()
	for _, r := range rows {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := batch.Append(r.TraceID, r.SpanID, r.ParentSpanID, r.ServiceName, r.SpanName, r.SpanKind, r.StartTime, r.EndTime, r.DurationNS, r.StatusCode, r.StatusMessage, r.ResourceAttributesJSON, r.SpanAttributesJSON, r.ScopeName, r.ScopeVersion, r.EventsJSON, r.LinksJSON, r.ResourceJSON, r.ScopeJSON, r.SpanJSON, r.ResourceSchemaURL, r.ScopeSchemaURL); err != nil {
			return errors.New("append normalized span failed")
		}
	}
	if err := batch.Send(); err != nil {
		return errors.New("send span batch failed; outcome may be ambiguous")
	}
	return nil
}
