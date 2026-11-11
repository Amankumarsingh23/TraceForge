package traces

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"reflect"
	"strings"
	"testing"
	"time"
)

const tid = "11111111111111111111111111111111"

func sid(n int) string { return fmt.Sprintf("%016x", n) }
func record(id, parent int, start, end int64) Record {
	p := ""
	if parent != 0 {
		p = sid(parent)
	}
	return Record{TraceID: tid, SpanID: sid(id), ParentSpanID: p, ServiceName: "synthetic", SpanName: fmt.Sprint(id), StartTime: time.Unix(1700000000, start).UTC(), EndTime: time.Unix(1700000000, end).UTC(), DurationNS: uint64(end - start), IngestedAt: time.Unix(1700000010, 0).UTC()}
}
func build(t *testing.T, rows []Record) *Detail {
	t.Helper()
	d, e := Reconstruct(tid, rows, HardMaxRows)
	if e != nil {
		t.Fatal(e)
	}
	return d
}
func countNodes(d *Detail) int {
	stack := append(append(append([]*Node{}, d.RootSpans...), d.OrphanSpans...), d.DisconnectedSpans...)
	seen := map[string]bool{}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if seen[n.SpanID] {
			return -1
		}
		seen[n.SpanID] = true
		stack = append(stack, n.Children...)
	}
	return len(seen)
}

func TestReconstructionShapes(t *testing.T) {
	for _, tc := range []struct {
		name                            string
		rows                            []Record
		roots, orphans, cycles, missing int
	}{
		{"empty", nil, 0, 0, 0, 0},
		{"one_root", []Record{record(1, 0, 0, 100)}, 1, 0, 0, 0},
		{"chain_out_of_order", []Record{record(3, 2, 20, 40), record(1, 0, 0, 100), record(2, 1, 10, 50)}, 1, 0, 0, 0},
		{"branching", []Record{record(3, 1, 10, 40), record(2, 1, 10, 80), record(1, 0, 0, 100)}, 1, 0, 0, 0},
		{"missing_parent", []Record{record(1, 0, 0, 100), record(2, 9, 20, 40), record(3, 9, 30, 50)}, 1, 2, 0, 1},
		{"multiple_roots", []Record{record(1, 0, 0, 100), record(2, 0, 20, 40)}, 2, 0, 0, 0},
		{"self_parent", []Record{record(1, 1, 0, 100)}, 0, 0, 1, 0},
		{"cycle_with_descendant", []Record{record(1, 2, 0, 100), record(2, 3, 0, 100), record(3, 1, 0, 100), record(4, 2, 10, 30)}, 0, 0, 1, 0},
		{"disconnected_components", []Record{record(1, 0, 0, 100), record(2, 1, 10, 20), record(3, 9, 10, 30), record(4, 3, 15, 25)}, 1, 1, 0, 1},
		{"two_cycles", []Record{record(1, 2, 0, 100), record(2, 1, 0, 100), record(3, 3, 0, 100)}, 0, 0, 2, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := build(t, tc.rows)
			a := d.Diagnostics
			if a.PhysicalRows != len(tc.rows) || a.UniqueSpans != len(tc.rows) || a.ObservedRootCount != tc.roots || a.OrphanSpanCount != tc.orphans || a.CycleCount != tc.cycles || a.MissingParentCount != tc.missing || a.HasDetectedGaps != (tc.missing > 0) || a.MultipleRoots != (tc.roots > 1) || a.Completeness != "unknown" {
				t.Fatalf("wrong diagnostics %+v", a)
			}
			if countNodes(d) != len(tc.rows) {
				t.Fatal("lost or cyclic output nodes")
			}
			if _, e := json.Marshal(d); e != nil {
				t.Fatal("non-finite output", e)
			}
			if tc.cycles > 0 && (!d.DisconnectedSpans[0].DetachedParentEdge || d.DisconnectedSpans[0].ParentSpanID == "") {
				t.Fatal("cycle cut lost recorded parent")
			}
		})
	}
}

func TestDuplicateSelection(t *testing.T) {
	a := record(1, 0, 0, 100)
	b := a
	b.IngestedAt = b.IngestedAt.Add(time.Second)
	b.Span = json.RawMessage(`{ "b":2, "a":1 }`)
	a.Span = json.RawMessage(`{"a":1,"b":2}`)
	d := build(t, []Record{b, a})
	if d.Diagnostics.ExactDuplicateRows != 1 || d.Diagnostics.ConflictingDuplicateGroups != 0 || !d.RootSpans[0].IngestedAt.Equal(a.IngestedAt) {
		t.Fatal("ingestion metadata/json formatting caused conflict")
	}
	c := a
	c.SpanName = "changed"
	c.IngestedAt = c.IngestedAt.Add(-time.Second)
	d = build(t, []Record{a, b, c, c})
	if d.Diagnostics.ExactDuplicateRows != 2 || d.Diagnostics.ConflictingDuplicateGroups != 1 || d.RootSpans[0].SpanName != "changed" || d.Diagnostics.Conflicts[0].Variants != 2 {
		t.Fatalf("wrong conflict diagnostics %+v", d.Diagnostics)
	}
	// Equal timestamps use normalized payload, not incoming row order.
	c.IngestedAt = a.IngestedAt
	x := build(t, []Record{a, c})
	y := build(t, []Record{c, a})
	if !reflect.DeepEqual(x, y) {
		t.Fatal("tie selection depends on row order")
	}
}

func TestShufflesOrderingAndNoMutation(t *testing.T) {
	rows := []Record{record(4, 2, 30, 40), record(3, 1, 10, 50), record(2, 1, 10, 60), record(1, 0, 0, 100), record(5, 9, 0, 10), record(6, 7, 0, 100), record(7, 6, 0, 100)}
	original, _ := json.Marshal(rows)
	want := build(t, rows)
	if want.RootSpans[0].Children[0].SpanID != sid(2) {
		t.Fatal("sibling tie not ordered by span ID")
	}
	rng := rand.New(rand.NewSource(42))
	for i := 0; i < 50; i++ {
		shuffled := append([]Record{}, rows...)
		rng.Shuffle(len(shuffled), func(a, b int) { shuffled[a], shuffled[b] = shuffled[b], shuffled[a] })
		if !reflect.DeepEqual(want, build(t, shuffled)) {
			t.Fatal("shuffle changed result")
		}
	}
	after, _ := json.Marshal(rows)
	if string(original) != string(after) {
		t.Fatal("input mutated")
	}
}

func TestTimingAndLinks(t *testing.T) {
	root := record(1, 0, 100, 900)
	root.Links = json.RawMessage(`[{"spanId":"some-linked-observation"}]`)
	a := record(2, 1, 90, 690)
	b := record(3, 1, 400, 900)
	d := build(t, []Record{root, a, b})
	if d.Envelope.DurationNS != 810 || d.RootSpans[0].RelativeStartNS != 10 || d.RootSpans[0].Children[0].RelativeStartNS != 0 || len(d.RootSpans[0].Children) != 2 || len(d.Diagnostics.TimingWarnings) != 1 || d.Diagnostics.TimingWarnings[0].Code != "child_starts_before_parent" {
		t.Fatal("incorrect timing/overlap handling")
	}
	if !d.RootSpans[0].StartTime.Equal(root.StartTime) || d.RootSpans[0].ParentSpanID != "" || len(d.OrphanSpans) != 0 {
		t.Fatal("rewrote time or treated link as parent")
	}
	a = record(2, 1, 50, 950)
	a.DurationNS = 1
	d = build(t, []Record{root, a})
	if len(d.Diagnostics.TimingWarnings) != 3 {
		t.Fatal("missing duration/skew diagnostics")
	}
	a.EndTime = a.StartTime.Add(-time.Nanosecond)
	d = build(t, []Record{a})
	if d.Diagnostics.TimingWarnings[0].Code != "end_before_start" {
		t.Fatal("invalid raw timing hidden")
	}
}

func TestIdentityLimitsAndCorruptContent(t *testing.T) {
	if _, e := Reconstruct(tid, []Record{record(1, 0, 0, 1), record(2, 1, 0, 1)}, 1); !errors.Is(e, ErrTooLarge) {
		t.Fatal("silent row truncation")
	}
	for _, change := range []func(*Record){func(r *Record) { r.TraceID = strings.Repeat("2", 32) }, func(r *Record) { r.SpanID = "zero" }, func(r *Record) { r.ParentSpanID = strings.Repeat("0", 16) }, func(r *Record) { r.Events = json.RawMessage(`{}`) }, func(r *Record) { r.Scope = json.RawMessage(`{"x":`) }} {
		r := record(1, 0, 0, 1)
		change(&r)
		if _, e := Reconstruct(tid, []Record{r}, 100); !errors.Is(e, ErrCorrupt) {
			t.Fatal("accepted invalid record")
		}
	}
	a := record(1, 0, 0, 1)
	b := a
	b.TraceID = strings.Repeat("2", 32)
	if _, e := Reconstruct(b.TraceID, []Record{b}, 100); e != nil {
		t.Fatal("same span ID must work in a separate trace")
	}
}

func TestDeepTraceWithoutTraversalRecursion(t *testing.T) {
	rows := make([]Record, HardMaxRows)
	for i := range rows {
		rows[i] = record(i+1, i, 0, 100)
	}
	d := build(t, rows)
	if countNodes(d) != HardMaxRows {
		t.Fatal("deep trace lost spans")
	}
	b, e := json.Marshal(d)
	if e != nil {
		t.Fatal(e)
	}
	var decoded any
	if e := json.Unmarshal(b, &decoded); e != nil {
		t.Fatal("response exceeds JSON depth support", e)
	}
}
