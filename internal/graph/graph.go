// Package graph builds the caller→callee dependency graph from spans.
//
// The graph answers one question during an incident: which service is calling
// which, and how is each edge behaving? Symptoms in a caller are often caused by
// a callee three hops down, and without the graph every service looks equally
// suspicious.
//
// Determinism: edges are returned in a sorted order and every duration is a
// nearest-rank percentile, so the same spans always produce the same graph.
package graph

import (
	"sort"
	"time"

	"github.com/RocketifyLabs/traceline/internal/determinism"
	"github.com/RocketifyLabs/traceline/internal/model"
	"github.com/RocketifyLabs/traceline/internal/stats"
)

// Result is the dependency graph plus the bookkeeping needed to explain what
// was ignored.
type Result struct {
	Edges []model.Edge
	// Roots and Orphans are counted, never silently merged into edges.
	Roots   int
	Orphans int
	// Unpaired counts spans whose parent exists but belongs to no service pair,
	// which happens when a trace is partially sampled.
	Unpaired int
}

// Build derives the dependency graph from spans.
//
// A caller→callee edge exists only when a span's parent is present in the input
// and the two spans belong to different services. A root span (no parent) starts
// no edge, and an orphan (parent id absent) is counted rather than guessed.
func Build(spans []model.Span) Result {
	byID := make(map[string]model.Span, len(spans))
	for _, s := range spans {
		byID[s.SpanID] = s
	}

	var res Result
	// Aggregate per edge key. A map is used for accumulation only; iteration
	// below always goes through a sorted key slice.
	type acc struct {
		caller, callee string
		durations      []float64 // milliseconds
		errors         int
	}
	accs := make(map[[2]string]*acc)

	for _, child := range spans {
		if child.IsRoot() {
			res.Roots++
			continue
		}
		parent, ok := byID[child.ParentSpanID]
		if !ok {
			res.Orphans++
			continue
		}
		if parent.Service == child.Service {
			// An internal span (same service) is not a dependency edge.
			continue
		}
		if parent.Service == "" || child.Service == "" {
			res.Unpaired++
			continue
		}
		key := [2]string{parent.Service, child.Service}
		a := accs[key]
		if a == nil {
			a = &acc{caller: parent.Service, callee: child.Service}
			accs[key] = a
		}
		a.durations = append(a.durations, float64(child.Duration())/float64(time.Millisecond))
		if child.IsError() {
			a.errors++
		}
	}

	// Sorted iteration: the map above must never decide the output order.
	keys := make([][2]string, 0, len(accs))
	for k := range accs {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i][0] != keys[j][0] {
			return keys[i][0] < keys[j][0]
		}
		return keys[i][1] < keys[j][1]
	})

	res.Edges = make([]model.Edge, 0, len(keys))
	for _, k := range keys {
		a := accs[k]
		res.Edges = append(res.Edges, model.Edge{
			Caller: a.caller,
			Callee: a.callee,
			Calls:  len(a.durations),
			Errors: a.errors,
			P50:    msToDuration(stats.Percentile(a.durations, 50)),
			P95:    msToDuration(stats.Percentile(a.durations, 95)),
		})
	}
	return res
}

// Callees returns the services a caller depends on, sorted.
func (r Result) Callees(caller string) []string {
	var out []string
	for _, e := range r.Edges {
		if e.Caller == caller {
			out = append(out, e.Callee)
		}
	}
	sort.Strings(out)
	return out
}

// Callers returns the services that depend on a callee, sorted.
func (r Result) Callers(callee string) []string {
	var out []string
	for _, e := range r.Edges {
		if e.Callee == callee {
			out = append(out, e.Caller)
		}
	}
	sort.Strings(out)
	return out
}

func msToDuration(ms float64) time.Duration {
	return time.Duration(ms * float64(time.Millisecond))
}

// Services returns every service that appears in the graph, sorted. It is the
// canonical node list for rendering: never derived from map iteration.
func (r Result) Services() []string {
	set := make(map[string]struct{})
	for _, e := range r.Edges {
		set[e.Caller] = struct{}{}
		set[e.Callee] = struct{}{}
	}
	return determinism.SortedKeys(set)
}
