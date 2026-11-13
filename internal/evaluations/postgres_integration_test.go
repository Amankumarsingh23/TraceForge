//go:build integration

package evaluations

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5/pgconn"
	"os"
	"testing"
)

func TestPostgresRead(t *testing.T) {
	if os.Getenv("TRACEFORGE_PG_TEST") != "1" {
		t.Skip("local PostgreSQL integration requires opt-in")
	}
	s, e := Open(context.Background())
	if e != nil {
		t.Fatal("configuration unavailable")
	}
	defer s.Close()
	v, e := s.Read(context.Background(), "runs", "", "", 1, "")
	if e != nil {
		var pg *pgconn.PgError
		if errors.As(e, &pg) {
			t.Fatalf("PostgreSQL SQLSTATE %s: %s", pg.Code, pg.Message)
		}
		t.Fatalf("PostgreSQL connection/read failure type %T", e)
	}
	if v == nil {
		t.Fatal("missing result")
	}
	b, err := os.ReadFile("../../artifacts/session08/baseline.json")
	if err != nil {
		t.Fatal("generate baseline artifact first")
	}
	var baseline struct {
		RunID string `json:"run_id"`
	}
	if json.Unmarshal(b, &baseline) != nil {
		t.Fatal("invalid baseline")
	}
	if _, err = s.Read(context.Background(), "run", baseline.RunID, "", 1, ""); err != nil {
		t.Fatal("run lookup failed")
	}
	page, err := s.Read(context.Background(), "cases", baseline.RunID, "", 10, "")
	if err != nil {
		t.Fatal("case page failed")
	}
	p := page.(map[string]any)
	if len(p["cases"].([]any)) != 10 || p["next_cursor"] != "support-010" {
		t.Fatal("incorrect case pagination")
	}
	second, err := s.Read(context.Background(), "cases", baseline.RunID, "", 10, p["next_cursor"].(string))
	if err != nil || second.(map[string]any)["cases"].([]any)[0].(map[string]any)["case_id"] != "support-011" {
		t.Fatal("pagination overlap")
	}
	caseValue, err := s.Read(context.Background(), "case", baseline.RunID, "support-001", 1, "")
	if err != nil || caseValue.(map[string]any)["trace_id"] == nil {
		t.Fatal("case identity missing")
	}
	if _, err = s.Read(context.Background(), "case", baseline.RunID, "' OR 1=1 --", 1, ""); !errors.Is(err, ErrNotFound) {
		t.Fatal("parameterized lookup failed")
	}
	if _, err = s.Read(context.Background(), "run", "00000000000000000000000000000000", "", 1, ""); !errors.Is(err, ErrNotFound) {
		t.Fatal("not-found failed")
	}
	_, err = s.pool.Exec(context.Background(), "UPDATE eval_runs SET status=status WHERE false")
	var denied *pgconn.PgError
	if !errors.As(err, &denied) || denied.Code != "25006" {
		t.Fatal("query connection is not read-only")
	}
	if raw, e := os.ReadFile("../../artifacts/session09/evidence.json"); e == nil {
		var evidence struct {
			Identical struct {
				ComparisonID string `json:"comparison_id"`
				DecisionID   string `json:"decision_id"`
			} `json:"identical"`
		}
		if json.Unmarshal(raw, &evidence) != nil {
			t.Fatal("bad comparison evidence")
		}
		for _, kind := range []string{"comparison", "comparison_cases", "decision", "comparisons", "experiments"} {
			id := evidence.Identical.ComparisonID
			if kind == "decision" {
				id = evidence.Identical.DecisionID
			}
			if _, err := s.Read(context.Background(), kind, id, "", 2, ""); err != nil {
				t.Fatalf("%s unavailable", kind)
			}
		}
		page, err := s.Read(context.Background(), "experiments", "", "", 1, "")
		if err != nil {
			t.Fatal("manifest page")
		}
		cursor := page.(map[string]any)["next_cursor"].(string)
		if cursor != "" {
			if _, err := s.Read(context.Background(), "experiments", "", "", 1, cursor); err != nil {
				t.Fatal("manifest cursor")
			}
		}
	}
}
