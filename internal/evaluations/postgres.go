package evaluations

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"time"
	"traceforge/internal/tenant"
)

type Store struct{ pool *pgxpool.Pool }

func Open(ctx context.Context) (*Store, error) {
	owner, e := tenant.Current()
	if e != nil {
		return nil, e
	}
	// Explicit identity avoids OS-user discovery in the scratch/non-root image.
	// PostgreSQL is restricted to the local Compose network/loopback in this session.
	cfg, err := pgxpool.ParseConfig("user=traceforge dbname=traceforge host=127.0.0.1 sslmode=disable")
	if err != nil {
		return nil, errors.New("invalid PostgreSQL configuration")
	}
	cfg.ConnConfig.Host = os.Getenv("POSTGRES_HOST")
	if cfg.ConnConfig.Host == "" {
		cfg.ConnConfig.Host = "127.0.0.1"
	}
	cfg.ConnConfig.Port = 5432
	cfg.ConnConfig.Database = "traceforge"
	cfg.ConnConfig.User = "traceforge"
	cfg.ConnConfig.Password = os.Getenv("POSTGRES_PASSWORD")
	cfg.ConnConfig.ConnectTimeout = 2 * time.Second
	cfg.MaxConns = 4
	cfg.MinConns = 0
	cfg.ConnConfig.RuntimeParams["statement_timeout"] = "1800"
	cfg.ConnConfig.RuntimeParams["default_transaction_read_only"] = "on"
	cfg.ConnConfig.RuntimeParams["search_path"] = tenant.Schema(owner)
	if cfg.ConnConfig.Password == "" {
		return nil, errors.New("PostgreSQL password not configured")
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, errors.New("PostgreSQL configuration unavailable")
	}
	return &Store{pool}, nil
}
func (s *Store) Close()              { s.pool.Close() }
func decode(raw []byte) (any, error) { var v any; e := json.Unmarshal(raw, &v); return v, e }
func (s *Store) Read(ctx context.Context, kind, run, caseID string, limit int, after string) (any, error) {
	if kind == "comparison" || kind == "decision" {
		sql := `SELECT to_jsonb(c) FROM eval_comparisons c WHERE comparison_id=$1`
		if kind == "decision" {
			sql = `SELECT to_jsonb(d) FROM gate_decisions d WHERE decision_id=$1`
		}
		var raw []byte
		err := s.pool.QueryRow(ctx, sql, run).Scan(&raw)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		if err != nil {
			return nil, err
		}
		return decode(raw)
	}
	if kind == "comparisons" || kind == "experiments" || kind == "comparison_cases" {
		return s.readComparisons(ctx, kind, run, limit, after)
	}
	if kind == "run" || kind == "case" {
		sql := `SELECT to_jsonb(r) FROM eval_runs r WHERE run_id=$1`
		args := []any{run}
		if kind == "case" {
			sql = `SELECT cr.result || jsonb_build_object('case',c.content,'source_facts',d.content->'facts') FROM eval_case_results cr JOIN eval_cases c USING(dataset_id,dataset_version,case_id) JOIN eval_datasets d ON d.dataset_id=cr.dataset_id AND d.version=cr.dataset_version WHERE cr.run_id=$1 AND cr.case_id=$2`
			args = append(args, caseID)
		}
		var raw []byte
		err := s.pool.QueryRow(ctx, sql, args...).Scan(&raw)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		if err != nil {
			return nil, err
		}
		return decode(raw)
	}
	// Run IDs are random; list pagination binds a stable (started_at,run_id) key.
	sql := `SELECT to_jsonb(r) FROM eval_runs r WHERE ($1='' OR (started_at,run_id)<(SELECT started_at,run_id FROM eval_runs WHERE run_id=$1)) ORDER BY started_at DESC,run_id DESC LIMIT $2`
	args := []any{after, limit + 1}
	key := "runs"
	if kind == "cases" {
		var exists bool
		if err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM eval_runs WHERE run_id=$1)`, run).Scan(&exists); err != nil {
			return nil, err
		}
		if !exists {
			return nil, ErrNotFound
		}
		sql = `SELECT cr.result || jsonb_build_object('case',c.content) FROM eval_case_results cr JOIN eval_cases c USING(dataset_id,dataset_version,case_id) WHERE cr.run_id=$1 AND cr.case_id>$2 ORDER BY cr.case_id LIMIT $3`
		args = []any{run, after, limit + 1}
		key = "cases"
	}
	rows, err := s.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []any{}
	next := ""
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		v, err := decode(raw)
		if err != nil {
			return nil, err
		}
		if len(items) == limit {
			last := items[len(items)-1].(map[string]any)
			if kind == "runs" {
				next = last["run_id"].(string)
			} else {
				next = last["case_id"].(string)
			}
			break
		}
		items = append(items, v)
	}
	if rows.Err() != nil {
		return nil, rows.Err()
	}
	return map[string]any{key: items, "next_cursor": next, "consistency": "non_snapshot", "trace_linkage": "independent_eventual_visibility"}, nil
}

func (s *Store) readComparisons(ctx context.Context, kind, run string, limit int, after string) (any, error) {
	sql := `SELECT jsonb_build_object('comparison_id',comparison_id,'baseline_run_id',baseline_run_id,'candidate_run_id',candidate_run_id,'created_at',created_at,'decisions',(SELECT coalesce(jsonb_agg(jsonb_build_object('decision_id',decision_id,'outcome',outcome,'policy_checksum',policy_checksum)),'[]') FROM gate_decisions d WHERE d.comparison_id=c.comparison_id)) FROM eval_comparisons c WHERE comparison_id>$1 ORDER BY comparison_id LIMIT $2`
	args := []any{after, limit + 1}
	key := "comparisons"
	idKey := "comparison_id"
	if kind == "experiments" {
		sql = `SELECT to_jsonb(c) FROM experiment_configs c WHERE checksum>$1 ORDER BY checksum LIMIT $2`
		key = "experiments"
		idKey = "checksum"
	}
	if kind == "comparison_cases" {
		var exists bool
		if err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM eval_comparisons WHERE comparison_id=$1)`, run).Scan(&exists); err != nil {
			return nil, err
		}
		if !exists {
			return nil, ErrNotFound
		}
		sql = `SELECT v FROM eval_comparisons c CROSS JOIN LATERAL jsonb_array_elements(c.result->'cases') v WHERE comparison_id=$1 AND v->>'case_id'>$2 ORDER BY v->>'case_id' LIMIT $3`
		args = []any{run, after, limit + 1}
		key = "cases"
		idKey = "case_id"
	}
	rows, err := s.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []any{}
	next := ""
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		value, err := decode(raw)
		if err != nil {
			return nil, err
		}
		if len(items) == limit {
			next = items[len(items)-1].(map[string]any)[idKey].(string)
			break
		}
		items = append(items, value)
	}
	if rows.Err() != nil {
		return nil, rows.Err()
	}
	return map[string]any{key: items, "next_cursor": next, "consistency": "non_snapshot"}, nil
}
