package telemetry

import (
	"context"
	"encoding/binary"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
	"testing"
)

func TestHeadSamplingDeterministicTraceIDs(t *testing.T) {
	sampler, err := SamplingProfile("parentbased_traceidratio", "0.25")
	if err != nil {
		t.Fatal(err)
	}
	selected := 0
	for i := 0; i < 256; i++ {
		id := trace.TraceID{1}
		binary.BigEndian.PutUint64(id[8:], uint64(i)<<56)
		p := sdktrace.SamplingParameters{ParentContext: context.Background(), TraceID: id}
		decision := sampler.ShouldSample(p).Decision
		if decision != sampler.ShouldSample(p).Decision {
			t.Fatal("unstable trace selection")
		}
		if decision == sdktrace.RecordAndSample {
			selected++
		}
	}
	if selected != 64 {
		t.Fatal("deterministic policy fixture", selected)
	}
}

func TestSamplingProfileAndParentPropagation(t *testing.T) {
	for _, c := range [][2]string{{"invalid", ""}, {"parentbased_traceidratio", "NaN"}, {"parentbased_traceidratio", "1.1"}, {"always_on", "0.1"}, {"parentbased_traceidratio", ""}} {
		if _, err := SamplingProfile(c[0], c[1]); err == nil {
			t.Fatal(c)
		}
	}
	always, _ := SamplingProfile("", "")
	if always.ShouldSample(sdktrace.SamplingParameters{ParentContext: context.Background()}).Decision != sdktrace.RecordAndSample {
		t.Fatal("default changed")
	}
	sampler, _ := SamplingProfile("parentbased_traceidratio", "0")
	for _, sampled := range []bool{false, true} {
		flags := trace.TraceFlags(0)
		if sampled {
			flags = trace.FlagsSampled
		}
		sc := trace.NewSpanContext(trace.SpanContextConfig{TraceID: trace.TraceID{1}, SpanID: trace.SpanID{1}, TraceFlags: flags, Remote: true})
		carrier := mapCarrier{}
		Propagator().Inject(trace.ContextWithRemoteSpanContext(context.Background(), sc), carrier)
		ctx := Propagator().Extract(context.Background(), carrier)
		got := sampler.ShouldSample(sdktrace.SamplingParameters{ParentContext: ctx, TraceID: sc.TraceID()})
		if (got.Decision == sdktrace.RecordAndSample) != sampled {
			t.Fatal("remote parent selection changed")
		}
	}
}

type mapCarrier map[string]string

func (c mapCarrier) Get(k string) string { return c[k] }
func (c mapCarrier) Set(k, v string)     { c[k] = v }
func (c mapCarrier) Keys() []string {
	out := []string{}
	for k := range c {
		out = append(out, k)
	}
	return out
}
