//go:build integration

package reliability

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"os"
	"sync"
	"testing"
	"time"
)

func TestPersistentLifecycleConcurrencyAndRevisions(t *testing.T) {
	if os.Getenv("TRACEFORGE_PG_TEST") != "1" {
		t.Skip("explicit local PostgreSQL")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancel()
	store, e := Open(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer func() { store.Close() }()
	if e = store.Migrate(ctx); e != nil {
		t.Fatal(e)
	}
	d := definition()
	d.ID = "pg-" + uuid.NewString()
	if e = store.Register(ctx, d); e != nil {
		t.Fatal(e)
	}
	changed := d
	changed.Target = "0.8"
	if store.Register(ctx, changed) == nil {
		t.Fatal("rewrote frozen target")
	}
	changed.Version = 2
	if e = store.Register(ctx, changed); e != nil {
		t.Fatal(e)
	}
	breach := sourceFunc(func(ctx context.Context, d Definition, a, b time.Time) Evidence {
		return eventEvidence(a, b, "10", "5", "5")
	})
	healthy := sourceFunc(func(ctx context.Context, d Definition, a, b time.Time) Evidence {
		return eventEvidence(a, b, "10", "10", "0")
	})
	cutoff := time.Now().UTC().Truncate(time.Second)
	var wg sync.WaitGroup
	errs := make(chan error, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, e := store.Evaluate(ctx, breach, d, cutoff, 1); errs <- e }()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	var n int
	if e = store.pool.QueryRow(ctx, `SELECT count(*) FROM slo_incidents WHERE slo_key=$1`, d.Key()).Scan(&n); e != nil || n != 1 {
		t.Fatal("concurrent duplicate incidents", n, e)
	}
	fingerprint := Hash([]any{d.Key(), Hash(d), d.Rules[0].ID, d.Rules[0].Version})
	head := func() Head {
		var raw []byte
		if e = store.pool.QueryRow(ctx, `SELECT head FROM slo_alert_heads WHERE fingerprint=$1`, fingerprint).Scan(&raw); e != nil {
			t.Fatal(e)
		}
		var h Head
		if e = json.Unmarshal(raw, &h); e != nil {
			t.Fatal(e)
		}
		return h
	}
	original := head()
	if original.State != "FIRING" {
		t.Fatal(original)
	}
	unknownSource := sourceFunc(func(ctx context.Context, d Definition, a, b time.Time) Evidence {
		return unknown(a, b, "BACKEND_UNAVAILABLE", "controlled unavailable source")
	})
	if _, e = store.Evaluate(ctx, unknownSource, d, cutoff.Add(time.Second), 1); e != nil {
		t.Fatal(e)
	}
	if h := head(); h.State != "FIRING" || h.IncidentID != original.IncidentID || h.LastDataStatus != "UNKNOWN" {
		t.Fatal("unknown resolved firing", h)
	}
	if _, e = store.Evaluate(ctx, healthy, d, cutoff.Add(-time.Second), 1); e != nil {
		t.Fatal(e)
	}
	if _, e = store.Evaluate(ctx, healthy, d, cutoff.Add(time.Second), 2); e != nil {
		t.Fatal(e)
	}
	if h := head(); h.State != "FIRING" || h.LastDataStatus != "UNKNOWN" {
		t.Fatal("stale/revision changed newer state", h)
	}
	store.Close()
	store, e = Open(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if h := head(); h.IncidentID != original.IncidentID {
		t.Fatal("restart lost state")
	}
	if _, e = store.Evaluate(ctx, healthy, d, cutoff.Add(2*time.Second), 1); e != nil {
		t.Fatal(e)
	}
	if head().State != "INACTIVE" {
		t.Fatal("no resolution")
	}
	if _, e = store.Evaluate(ctx, breach, d, cutoff.Add(3*time.Second), 1); e != nil {
		t.Fatal(e)
	}
	if h := head(); h.IncidentID == original.IncidentID {
		t.Fatal("new episode reused prior incident")
	}
	if e = store.pool.QueryRow(ctx, `SELECT count(*) FROM slo_alert_events e JOIN slo_incidents i ON e.incident_id=i.id WHERE i.slo_key=$1`, d.Key()).Scan(&n); e != nil || n != 3 {
		t.Fatal("event dedup/history", n, e)
	}
	if _, e = store.pool.Exec(ctx, `UPDATE slo_versions SET checksum='changed' WHERE key=$1`, d.Key()); e == nil {
		t.Fatal("definition trigger missing")
	}
	if _, e = store.pool.Exec(ctx, `DELETE FROM slo_evaluations WHERE slo_key=$1`, d.Key()); e == nil {
		t.Fatal("evaluation history mutable")
	}
	t.Log("12 concurrent evaluations -> one incident/event; UNKNOWN and stale/revision preserve active state; reopened pool preserves history; recovery/new episode and immutability verified")
}
