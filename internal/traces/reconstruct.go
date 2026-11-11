package traces

import (
	"bytes"
	"encoding/json"
	"io"
	"sort"
	"time"
)

// CanonicalJSON ignores insignificant object-key order and whitespace without
// converting numeric JSON values through float64. Arrays retain their order.
func CanonicalJSON(raw json.RawMessage, array bool) (json.RawMessage, error) {
	if len(raw) == 0 {
		if array {
			return json.RawMessage("[]"), nil
		}
		return json.RawMessage("{}"), nil
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	var v any
	if err := d.Decode(&v); err != nil {
		return nil, ErrCorrupt
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return nil, ErrCorrupt
	}
	if array {
		if _, ok := v.([]any); !ok {
			return nil, ErrCorrupt
		}
	} else {
		if _, ok := v.(map[string]any); !ok {
			return nil, ErrCorrupt
		}
	}
	b, e := json.Marshal(v)
	if e != nil {
		return nil, ErrCorrupt
	}
	return b, nil
}

func normalize(r Record) (Record, string, error) {
	if !ValidID(r.TraceID, 16) || !ValidID(r.SpanID, 8) || (r.ParentSpanID != "" && !ValidID(r.ParentSpanID, 8)) {
		return Record{}, "", ErrCorrupt
	}
	// Positive signed nanosecond times match the Session 2 storage contract.
	if r.StartTime.Before(time.Unix(0, 1)) || r.EndTime.Before(time.Unix(0, 1)) || r.StartTime.After(time.Unix(0, 1<<63-1)) || r.EndTime.After(time.Unix(0, 1<<63-1)) {
		return Record{}, "", ErrCorrupt
	}
	r.StartTime = r.StartTime.UTC()
	r.EndTime = r.EndTime.UTC()
	r.IngestedAt = r.IngestedAt.UTC()
	for _, item := range []struct {
		p     *json.RawMessage
		array bool
	}{{&r.ResourceAttributes, true}, {&r.SpanAttributes, true}, {&r.Events, true}, {&r.Links, true}, {&r.Resource, false}, {&r.Scope, false}, {&r.Span, false}} {
		v, e := CanonicalJSON(*item.p, item.array)
		if e != nil {
			return Record{}, "", e
		}
		*item.p = v
	}
	content := r
	content.IngestedAt = time.Time{}
	b, e := json.Marshal(content)
	if e != nil {
		return Record{}, "", ErrCorrupt
	}
	return r, string(b), nil
}

// Reconstruct retains every canonical span in a finite forest. Canonical
// conflicts prefer earliest ingestion, then lexicographically smallest complete
// normalized payload; neither preference establishes which observation is true.
func Reconstruct(traceID string, rows []Record, maxRows int) (*Detail, error) {
	if maxRows < 1 || maxRows > HardMaxRows || len(rows) > maxRows {
		return nil, ErrTooLarge
	}
	if !ValidID(traceID, 16) {
		return nil, ErrCorrupt
	}
	d := &Detail{TraceID: traceID, RootSpans: []*Node{}, OrphanSpans: []*Node{}, DisconnectedSpans: []*Node{}, ObservedServices: []string{}, Diagnostics: Diagnostics{PhysicalRows: len(rows), Conflicts: []Conflict{}, MissingParentIDs: []string{}, Cycles: [][]string{}, TimingWarnings: []TimingWarning{}, Completeness: "unknown"}}
	type group struct {
		row      Record
		content  string
		variants map[string]bool
	}
	groups := map[string]*group{}
	for _, input := range rows {
		if input.TraceID != traceID {
			return nil, ErrCorrupt
		}
		r, content, e := normalize(input)
		if e != nil {
			return nil, e
		}
		g := groups[r.SpanID]
		if g == nil {
			groups[r.SpanID] = &group{row: r, content: content, variants: map[string]bool{content: true}}
			continue
		}
		if g.variants[content] {
			d.Diagnostics.ExactDuplicateRows++
		}
		g.variants[content] = true
		if r.IngestedAt.Before(g.row.IngestedAt) || (r.IngestedAt.Equal(g.row.IngestedAt) && content < g.content) {
			g.row = r
			g.content = content
		}
	}
	d.Diagnostics.UniqueSpans = len(groups)
	nodes := map[string]*Node{}
	ordered := make([]*Node, 0, len(groups))
	services := map[string]bool{}
	for id, g := range groups {
		n := &Node{Record: g.row, Children: []*Node{}}
		nodes[id] = n
		ordered = append(ordered, n)
		services[n.ServiceName] = true
		if len(g.variants) > 1 {
			d.Diagnostics.Conflicts = append(d.Diagnostics.Conflicts, Conflict{id, len(g.variants)})
		}
		if d.Envelope == nil {
			d.Envelope = &Envelope{StartTime: n.StartTime, EndTime: n.EndTime}
		}
		if n.StartTime.Before(d.Envelope.StartTime) {
			d.Envelope.StartTime = n.StartTime
		}
		if n.EndTime.After(d.Envelope.EndTime) {
			d.Envelope.EndTime = n.EndTime
		}
	}
	less := func(a, b *Node) bool {
		if a.StartTime.Equal(b.StartTime) {
			return a.SpanID < b.SpanID
		}
		return a.StartTime.Before(b.StartTime)
	}
	sort.Slice(ordered, func(i, j int) bool { return less(ordered[i], ordered[j]) })
	sort.Slice(d.Diagnostics.Conflicts, func(i, j int) bool { return d.Diagnostics.Conflicts[i].SpanID < d.Diagnostics.Conflicts[j].SpanID })
	d.Diagnostics.ConflictingDuplicateGroups = len(d.Diagnostics.Conflicts)
	// Iterative three-state parent traversal avoids recursion even for deep graphs.
	state := map[string]uint8{}
	index := map[string]int{}
	cut := map[string]bool{}
	for _, start := range ordered {
		if state[start.SpanID] != 0 {
			continue
		}
		path := []string{}
		cur := start
		for cur != nil && state[cur.SpanID] == 0 {
			state[cur.SpanID] = 1
			index[cur.SpanID] = len(path)
			path = append(path, cur.SpanID)
			cur = nodes[cur.ParentSpanID]
		}
		if cur != nil && state[cur.SpanID] == 1 {
			cycle := append([]string{}, path[index[cur.SpanID]:]...)
			sort.Strings(cycle)
			d.Diagnostics.Cycles = append(d.Diagnostics.Cycles, cycle)
			cut[cycle[0]] = true
		}
		for _, id := range path {
			state[id] = 2
		}
	}
	sort.Slice(d.Diagnostics.Cycles, func(i, j int) bool { return d.Diagnostics.Cycles[i][0] < d.Diagnostics.Cycles[j][0] })
	d.Diagnostics.CycleCount = len(d.Diagnostics.Cycles)
	missing := map[string]bool{}
	for _, n := range ordered {
		if d.Envelope != nil {
			n.RelativeStartNS = n.StartTime.UnixNano() - d.Envelope.StartTime.UnixNano()
		}
		if n.EndTime.Before(n.StartTime) {
			d.Diagnostics.TimingWarnings = append(d.Diagnostics.TimingWarnings, TimingWarning{SpanID: n.SpanID, Code: "end_before_start"})
		} else if uint64(n.EndTime.UnixNano()-n.StartTime.UnixNano()) != n.DurationNS {
			d.Diagnostics.TimingWarnings = append(d.Diagnostics.TimingWarnings, TimingWarning{SpanID: n.SpanID, Code: "duration_mismatch"})
		}
		parent := nodes[n.ParentSpanID]
		if parent != nil {
			if n.StartTime.Before(parent.StartTime) {
				d.Diagnostics.TimingWarnings = append(d.Diagnostics.TimingWarnings, TimingWarning{n.SpanID, parent.SpanID, "child_starts_before_parent"})
			}
			if n.EndTime.After(parent.EndTime) {
				d.Diagnostics.TimingWarnings = append(d.Diagnostics.TimingWarnings, TimingWarning{n.SpanID, parent.SpanID, "child_ends_after_parent"})
			}
		}
		switch {
		case n.ParentSpanID == "":
			d.RootSpans = append(d.RootSpans, n)
		case parent == nil:
			d.OrphanSpans = append(d.OrphanSpans, n)
			missing[n.ParentSpanID] = true
		case cut[n.SpanID]:
			n.DetachedParentEdge = true
			d.DisconnectedSpans = append(d.DisconnectedSpans, n)
		default:
			parent.Children = append(parent.Children, n)
		}
	}
	// Children were appended from the globally ordered slice, hence sibling order
	// is already start-time/span-ID order without a recursive sorting pass.
	for service := range services {
		d.ObservedServices = append(d.ObservedServices, service)
	}
	sort.Strings(d.ObservedServices)
	for id := range missing {
		d.Diagnostics.MissingParentIDs = append(d.Diagnostics.MissingParentIDs, id)
	}
	sort.Strings(d.Diagnostics.MissingParentIDs)
	d.Diagnostics.MissingParentCount = len(missing)
	d.Diagnostics.OrphanSpanCount = len(d.OrphanSpans)
	d.Diagnostics.ObservedRootCount = len(d.RootSpans)
	d.Diagnostics.MultipleRoots = len(d.RootSpans) > 1
	d.Diagnostics.HasDetectedGaps = len(missing) > 0
	if d.Envelope != nil {
		d.Envelope.DurationNS = d.Envelope.EndTime.UnixNano() - d.Envelope.StartTime.UnixNano()
	}
	return d, nil
}
