package storage

import (
	"context"
	"fmt"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"regexp"
	"strings"
)

var benchmarkDatabase = regexp.MustCompile(`^tf_bench_[a-z0-9_]{1,48}$`)

func validateDatabase(name string) error {
	if name != "traceforge" && name != "tf_tenant_alpha" && name != "tf_tenant_beta" && !benchmarkDatabase.MatchString(name) {
		return fmt.Errorf("database must be traceforge or an isolated tf_bench_ identifier")
	}
	return nil
}
func (s *Store) sql(q string) string {
	if s.Database == "" || s.Database == "traceforge" {
		return q
	}
	return strings.ReplaceAll(q, "traceforge.", s.Database+".")
}
func (s *Store) query(ctx context.Context, q string, args ...any) (driver.Rows, error) {
	return s.Conn.Query(ctx, s.sql(q), args...)
}
func (s *Store) exec(ctx context.Context, q string, args ...any) error {
	return s.Conn.Exec(ctx, s.sql(q), args...)
}
func (s *Store) prepareBatch(ctx context.Context, q string) (driver.Batch, error) {
	return s.Conn.PrepareBatch(ctx, s.sql(q))
}
