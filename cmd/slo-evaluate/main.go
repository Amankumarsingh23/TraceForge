// Local administration only: no HTTP write endpoint or external notifications.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"time"
	"traceforge/internal/reliability"
	"traceforge/internal/storage"
)

func run() error {
	file := flag.String("file", "", "bounded JSON array of immutable SLO definitions")
	key := flag.String("key", "", "evaluate only this key")
	cutoffText := flag.String("cutoff", "", "whole-second UTC cutoff (default now minus3s)")
	revision := flag.Int("revision", 1, "measurement revision1..100; never rewrites history")
	registerOnly := flag.Bool("register-only", false, "only register frozen definitions")
	flag.Parse()
	if *file == "" {
		return fmt.Errorf("provide local definition file")
	}
	input, e := os.Open(*file)
	if e != nil {
		return fmt.Errorf("definition file unavailable")
	}
	defer input.Close()
	raw, e := io.ReadAll(io.LimitReader(input, (64<<10)+1))
	if e != nil || len(raw) > 64<<10 {
		return fmt.Errorf("definition file unavailable or >64KiB")
	}
	var defs []reliability.Definition
	if e = json.Unmarshal(raw, &defs); e != nil || len(defs) == 0 || len(defs) > 8 {
		return fmt.Errorf("use1..8 validated definitions")
	}
	cutoff := time.Now().UTC().Add(-3 * time.Second).Truncate(time.Second)
	if *cutoffText != "" {
		cutoff, e = time.Parse(time.RFC3339, *cutoffText)
		if e != nil {
			return fmt.Errorf("invalid UTC cutoff")
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	pg, e := reliability.Open(ctx)
	if e != nil {
		return e
	}
	defer pg.Close()
	if e = pg.Migrate(ctx); e != nil {
		return fmt.Errorf("reliability migration unavailable")
	}
	for _, d := range defs {
		if e = pg.Register(ctx, d); e != nil {
			return e
		}
	}
	if *registerOnly {
		return nil
	}
	ch, e := storage.Open(ctx)
	var source reliability.Source = reliability.UnavailableSource{}
	if e == nil {
		defer ch.Close()
		source = reliability.Sources{Metric: reliability.MetricSource{Repo: ch}, Provider: reliability.ProviderSource{Repo: ch}}
	}
	results := []reliability.Evaluation{}
	for _, d := range defs {
		if *key != "" && d.Key() != *key {
			continue
		}
		evalCtx, c := context.WithTimeout(ctx, 20*time.Second)
		ev, err := pg.Evaluate(evalCtx, source, d, cutoff, *revision)
		c()
		if err != nil {
			return fmt.Errorf("evaluation persistence unavailable")
		}
		results = append(results, ev)
	}
	if len(results) == 0 {
		return fmt.Errorf("selected SLO key was not found in file")
	}
	return json.NewEncoder(os.Stdout).Encode(results)
}
func main() {
	if e := run(); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
