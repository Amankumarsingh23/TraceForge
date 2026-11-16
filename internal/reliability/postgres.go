package reliability

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"time"
	"traceforge/internal/tenant"
)

//go:embed migration.sql
var migration string
var ErrNotFound = errors.New("reliability record not found")
var ErrTooLarge = errors.New("reliability response budget exceeded")

type Store struct{ pool *pgxpool.Pool }

func Open(ctx context.Context) (*Store, error) {
	owner, err := tenant.Current()
	if err != nil {
		return nil, err
	}
	cfg, e := pgxpool.ParseConfig("user=traceforge dbname=traceforge host=127.0.0.1 sslmode=disable")
	if e != nil {
		return nil, e
	}
	if host := os.Getenv("POSTGRES_HOST"); host != "" {
		cfg.ConnConfig.Host = host
	}
	cfg.ConnConfig.Password = os.Getenv("POSTGRES_PASSWORD")
	if cfg.ConnConfig.Password == "" {
		return nil, errors.New("PostgreSQL password not configured")
	}
	cfg.ConnConfig.ConnectTimeout = 2 * time.Second
	cfg.MaxConns = 4
	cfg.ConnConfig.RuntimeParams["statement_timeout"] = "2500"
	cfg.ConnConfig.RuntimeParams["lock_timeout"] = "2000"
	cfg.ConnConfig.RuntimeParams["search_path"] = tenant.Schema(owner)
	pool, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		return nil, e
	}
	return &Store{pool}, nil
}
func (s *Store) Close()                            { s.pool.Close() }
func (s *Store) Migrate(ctx context.Context) error { _, e := s.pool.Exec(ctx, migration); return e }
func (s *Store) Register(ctx context.Context, d Definition) error {
	if e := d.Validate(); e != nil {
		return e
	}
	raw, _ := json.Marshal(d)
	_, e := s.pool.Exec(ctx, `INSERT INTO slo_versions(key,slo_id,version,checksum,definition) VALUES($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING`, d.Key(), d.ID, d.Version, Hash(d), raw)
	if e != nil {
		return e
	}
	var checksum string
	if e = s.pool.QueryRow(ctx, `SELECT checksum FROM slo_versions WHERE key=$1`, d.Key()).Scan(&checksum); e != nil {
		return e
	}
	if checksum != Hash(d) {
		return errors.New("SLO version already frozen with different content")
	}
	return nil
}
func (s *Store) Definition(ctx context.Context, key string) (Definition, error) {
	var d Definition
	var raw []byte
	e := s.pool.QueryRow(ctx, `SELECT definition FROM slo_versions WHERE key=$1`, key).Scan(&raw)
	if errors.Is(e, pgx.ErrNoRows) {
		return d, ErrNotFound
	}
	if e != nil {
		return d, e
	}
	e = json.Unmarshal(raw, &d)
	return d, e
}
func (s *Store) Evaluation(ctx context.Context, id string) (Evaluation, error) {
	var e Evaluation
	var raw []byte
	err := s.pool.QueryRow(ctx, `SELECT result FROM slo_evaluations WHERE id=$1`, id).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return e, ErrNotFound
	}
	if err != nil {
		return e, err
	}
	err = json.Unmarshal(raw, &e)
	return e, err
}
func (s *Store) Evaluate(ctx context.Context, source Source, d Definition, cutoff time.Time, revision int) (Evaluation, error) {
	if e := d.Validate(); e != nil {
		return Evaluation{}, e
	}
	if revision < 1 || revision > 100 || cutoff.Location() != time.UTC || cutoff.Nanosecond() != 0 {
		return Evaluation{}, errors.New("evaluation requires whole-second UTC cutoff and bounded revision")
	}
	id := Hash([]any{d.Key(), Hash(d), cutoff.UTC(), revision})
	if old, e := s.Evaluation(ctx, id); e == nil {
		return old, nil
	} else if !errors.Is(e, ErrNotFound) {
		return Evaluation{}, e
	}
	e := Build(ctx, source, d, cutoff, revision)
	return s.Persist(ctx, e)
}
func (s *Store) Persist(ctx context.Context, e Evaluation) (Evaluation, error) {
	if e.SLOKey != e.Definition.Key() || e.ID != Hash([]any{e.Definition.Key(), Hash(e.Definition), e.Cutoff.UTC(), e.Revision}) {
		return e, errors.New("evaluation identity does not match frozen scope/cutoff/revision")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return e, err
	}
	defer tx.Rollback(context.Background())
	var checksum string
	// Serialize all state transitions for one frozen SLO, including the initial
	// head creation. No race-prone process-local mutex or missing-row lock.
	if err = tx.QueryRow(ctx, `SELECT checksum FROM slo_versions WHERE key=$1 FOR UPDATE`, e.SLOKey).Scan(&checksum); err != nil {
		return e, err
	}
	if checksum != Hash(e.Definition) {
		return e, errors.New("evaluation definition mismatch")
	}
	var old []byte
	err = tx.QueryRow(ctx, `SELECT result FROM slo_evaluations WHERE id=$1`, e.ID).Scan(&old)
	if err == nil {
		var existing Evaluation
		err = json.Unmarshal(old, &existing)
		return existing, err
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return e, err
	}
	for _, r := range e.Rules {
		fingerprint := Hash([]any{e.SLOKey, checksum, r.Rule.ID, r.Rule.Version})
		h := Head{Fingerprint: fingerprint, SLOKey: e.SLOKey, RuleID: r.Rule.ID, State: "INACTIVE"}
		var raw []byte
		err = tx.QueryRow(ctx, `SELECT head FROM slo_alert_heads WHERE fingerprint=$1 FOR UPDATE`, fingerprint).Scan(&raw)
		if err == nil {
			if err = json.Unmarshal(raw, &h); err != nil {
				return e, err
			}
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return e, err
		}
		before := h
		h, event := Transition(h, e, r)
		head, _ := json.Marshal(h)
		if _, err = tx.Exec(ctx, `INSERT INTO slo_alert_heads(fingerprint,slo_key,head) VALUES($1,$2,$3) ON CONFLICT(fingerprint) DO UPDATE SET head=excluded.head`, fingerprint, e.SLOKey, head); err != nil {
			return e, err
		}
		if event != nil {
			if event.Kind == "FIRING" {
				incident := Incident{event.IncidentID, fingerprint, e.SLOKey, r.Rule.ID, r.Rule.Severity, "FIRING", e.Cutoff, e.ID, nil}
				raw, _ = json.Marshal(incident)
				if _, err = tx.Exec(ctx, `INSERT INTO slo_incidents(id,fingerprint,slo_key,state,result) VALUES($1,$2,$3,'FIRING',$4)`, incident.ID, fingerprint, e.SLOKey, raw); err != nil {
					return e, err
				}
			} else {
				var incident Incident
				if err = tx.QueryRow(ctx, `SELECT result FROM slo_incidents WHERE id=$1 FOR UPDATE`, event.IncidentID).Scan(&raw); err != nil {
					return e, err
				}
				if err = json.Unmarshal(raw, &incident); err != nil {
					return e, err
				}
				incident.State = "RESOLVED"
				incident.ResolvedAt = &e.Cutoff
				incident.LastEvaluation = e.ID
				raw, _ = json.Marshal(incident)
				if _, err = tx.Exec(ctx, `UPDATE slo_incidents SET state='RESOLVED',result=$2 WHERE id=$1`, incident.ID, raw); err != nil {
					return e, err
				}
			}
			raw, _ = json.Marshal(event)
			if _, err = tx.Exec(ctx, `INSERT INTO slo_alert_events(id,incident_id,cutoff,result) VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING`, event.ID, event.IncidentID, event.Cutoff, raw); err != nil {
				return e, err
			}
		} else if h.State == "FIRING" && h.LastCutoff.After(before.LastCutoff) {
			if _, err = tx.Exec(ctx, `UPDATE slo_incidents SET result=jsonb_set(result,'{last_evaluation}',to_jsonb($2::text)) WHERE id=$1`, h.IncidentID, e.ID); err != nil {
				return e, err
			}
		}
	}
	raw, _ := json.Marshal(e)
	if _, err = tx.Exec(ctx, `INSERT INTO slo_evaluations(id,slo_key,cutoff,revision,result) VALUES($1,$2,$3,$4,$5)`, e.ID, e.SLOKey, e.Cutoff, e.Revision, raw); err != nil {
		return e, err
	}
	err = tx.Commit(ctx)
	return e, err
}
func (s *Store) Read(ctx context.Context, kind, key string, limit int, after string) (any, error) {
	if kind == "definition" {
		d, e := s.Definition(ctx, key)
		if e != nil {
			return nil, e
		}
		var latest []byte
		err := s.pool.QueryRow(ctx, `SELECT result FROM slo_evaluations WHERE slo_key=$1 ORDER BY cutoff DESC,revision DESC LIMIT 1`, key).Scan(&latest)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return nil, err
		}
		var v any
		if len(latest) > 0 {
			_ = json.Unmarshal(latest, &v)
		}
		return map[string]any{"definition": d, "latest": v}, nil
	}
	if kind == "evaluation" {
		return s.Evaluation(ctx, key)
	}
	if kind == "incident" {
		var raw []byte
		e := s.pool.QueryRow(ctx, `SELECT result FROM slo_incidents WHERE id=$1`, key).Scan(&raw)
		if errors.Is(e, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		if e != nil {
			return nil, e
		}
		var incident any
		_ = json.Unmarshal(raw, &incident)
		rows, e := s.pool.Query(ctx, `SELECT result FROM slo_alert_events WHERE incident_id=$1 ORDER BY cutoff,id LIMIT 3`, key)
		if e != nil {
			return nil, e
		}
		defer rows.Close()
		events := []any{}
		for rows.Next() {
			if e = rows.Scan(&raw); e != nil {
				return nil, e
			}
			var v any
			_ = json.Unmarshal(raw, &v)
			events = append(events, v)
		}
		return map[string]any{"incident": incident, "events": events}, rows.Err()
	}
	sql := `SELECT s.key,jsonb_build_object('key',s.key,'definition',s.definition,'latest',(SELECT result FROM slo_evaluations e WHERE e.slo_key=s.key ORDER BY cutoff DESC,revision DESC LIMIT 1)) FROM slo_versions s WHERE key>$1 ORDER BY key LIMIT $2`
	args := []any{after, limit + 1}
	field := "slos"
	switch kind {
	case "evaluations":
		sql = `SELECT id,result FROM slo_evaluations WHERE slo_key=$1 AND ($2='' OR (cutoff,id)<(SELECT cutoff,id FROM slo_evaluations WHERE id=$2 AND slo_key=$1)) ORDER BY cutoff DESC,id DESC LIMIT $3`
		args = []any{key, after, limit + 1}
		field = "evaluations"
	case "alerts":
		sql = `SELECT id,result FROM slo_incidents WHERE id>$1 ORDER BY id LIMIT $2`
		field = "alerts"
	case "heads":
		sql = `SELECT fingerprint,head FROM slo_alert_heads WHERE fingerprint>$1 ORDER BY fingerprint LIMIT $2`
		field = "heads"
	case "provider":
		sql = `SELECT id,result FROM slo_evaluations WHERE result->'definition'->>'kind' IN ('provider_attempt','model_operation') AND id>$1 ORDER BY id LIMIT $2`
		field = "evaluations"
	case "slos":
	default:
		return nil, ErrNotFound
	}
	rows, err := s.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []any{}
	bytes := 0
	next, last := "", ""
	for rows.Next() {
		var id string
		var raw []byte
		if err = rows.Scan(&id, &raw); err != nil {
			return nil, err
		}
		if len(items) == limit {
			next = last
			break
		}
		bytes += len(raw)
		if bytes > 8<<20 {
			return nil, ErrTooLarge
		}
		var v any
		if err = json.Unmarshal(raw, &v); err != nil {
			return nil, err
		}
		items = append(items, v)
		last = id
	}
	return map[string]any{field: items, "next_cursor": next, "consistency": "non_snapshot"}, rows.Err()
}
