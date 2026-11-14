package reliability

import (
	"context"
	"log"
	"strings"
	"time"
)

type Clock interface {
	Now() time.Time
	Wait(context.Context, time.Duration) bool
}
type RealClock struct{}

func (RealClock) Now() time.Time { return time.Now().UTC() }
func (RealClock) Wait(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

type Evaluator interface {
	Definition(context.Context, string) (Definition, error)
	Evaluate(context.Context, Source, Definition, time.Time, int) (Evaluation, error)
}

func Run(ctx context.Context, clock Clock, store Evaluator, source Source, keys []string, cadence, delay time.Duration) {
	if len(keys) == 0 || len(keys) > 8 || cadence < time.Second || cadence > 5*time.Minute || delay < 0 || delay > time.Minute {
		log.Print("reliability scheduler disabled: empty or invalid bounded configuration")
		return
	}
	for {
		cutoff := clock.Now().Add(-delay).Truncate(cadence).UTC()
		for _, key := range keys {
			if ctx.Err() != nil {
				return
			}
			evalCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
			d, e := store.Definition(evalCtx, strings.TrimSpace(key))
			if e == nil {
				_, e = store.Evaluate(evalCtx, source, d, cutoff, 1)
			}
			cancel()
			if e != nil {
				log.Print("reliability evaluation unavailable; no incident recovery inferred")
			}
		}
		if !clock.Wait(ctx, cadence) {
			return
		}
	}
}
