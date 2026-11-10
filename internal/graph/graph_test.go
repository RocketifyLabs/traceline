package graph

import (
	"math/rand"
	"testing"
	"time"

	"github.com/andrelbuzzo/itm/internal/model"
)

func ts(min, sec int) time.Time {
	return time.Date(2026, 10, 16, 10, min, sec, 0, time.UTC)
}

func span(trace, id, parent, svc, name string, start, end time.Time, status model.Status) model.Span {
	return model.Span{
		TraceID: trace, SpanID: id, ParentSpanID: parent,
		Service: svc, Name: name, Start: start, End: end, Status: status,
	}
}

func TestBuildCreatesEdgeFromParentChild(t *testing.T) {
	spans := []model.Span{
		span("t1", "root", "", "edge", "GET /checkout", ts(0, 0), ts(0, 2), model.StatusError),
		span("t1", "child", "root", "db", "query", ts(0, 0), ts(0, 1), model.StatusOK),
	}
	res := Build(spans)
	if len(res.Edges) != 1 {
		t.Fatalf("got %d edges, want 1: %+v", len(res.Edges), res.Edges)
	}
	e := res.Edges[0]
	if e.Caller != "edge" || e.Callee != "db" {
		t.Fatalf("edge = %s->%s, want edge->db", e.Caller, e.Callee)
	}
	if e.Calls != 1 || e.Errors != 0 {
		t.Fatalf("calls=%d errors=%d, want 1/0", e.Calls, e.Errors)
	}
	if res.Roots != 1 {
		t.Errorf("Roots = %d, want 1", res.Roots)
	}
}

// FR-2: a span with no parent produces no edge.
func TestBuildRootSpanProducesNoEdge(t *testing.T) {
	res := Build([]model.Span{
		span("t1", "root", "", "edge", "GET", ts(0, 0), ts(0, 1), model.StatusOK),
	})
	if len(res.Edges) != 0 {
		t.Fatalf("root span produced an edge: %+v", res.Edges)
	}
	if res.Roots != 1 {
		t.Errorf("Roots = %d, want 1", res.Roots)
	}
}

// FR-2: an orphan parent id is counted, not invented as an edge.
func TestBuildOrphanCountedNotInvented(t *testing.T) {
	res := Build([]model.Span{
		span("t1", "child", "ghost", "db", "query", ts(0, 0), ts(0, 1), model.StatusOK),
	})
	if len(res.Edges) != 0 {
		t.Fatalf("orphan produced an edge: %+v", res.Edges)
	}
	if res.Orphans != 1 {
		t.Errorf("Orphans = %d, want 1", res.Orphans)
	}
}

func TestBuildSameServiceSpanIsNotAnEdge(t *testing.T) {
	res := Build([]model.Span{
		span("t1", "root", "", "edge", "GET", ts(0, 0), ts(0, 2), model.StatusOK),
		span("t1", "inner", "root", "edge", "middleware", ts(0, 0), ts(0, 1), model.StatusOK),
	})
	if len(res.Edges) != 0 {
		t.Fatalf("internal span became an edge: %+v", res.Edges)
	}
}

func TestBuildAggregatesCallsAndErrors(t *testing.T) {
	var spans []model.Span
	for i := 0; i < 10; i++ {
		status := model.StatusOK
		if i < 3 {
			status = model.StatusError
		}
		spans = append(spans,
			span("t1", "root"+string(rune('a'+i)), "", "edge", "GET", ts(0, 0), ts(0, 1), model.StatusOK),
			span("t1", "child"+string(rune('a'+i)), "root"+string(rune('a'+i)), "db", "query", ts(0, 0), ts(0, 1), status),
		)
	}
	res := Build(spans)
	if len(res.Edges) != 1 {
		t.Fatalf("got %d edges, want 1", len(res.Edges))
	}
	e := res.Edges[0]
	if e.Calls != 10 || e.Errors != 3 {
		t.Fatalf("calls=%d errors=%d, want 10/3", e.Calls, e.Errors)
	}
	wantRate := 0.3
	if got := e.ErrorRate(); got < wantRate-1e-9 || got > wantRate+1e-9 {
		t.Fatalf("ErrorRate = %v, want %v", got, wantRate)
	}
}

func TestBuildPercentilesNearestRank(t *testing.T) {
	// Durations 1..10 ms across ten calls; nearest-rank p95 = 10, p50 = 5.
	var spans []model.Span
	for i := 1; i <= 10; i++ {
		spans = append(spans,
			span("t", "r"+string(rune('a'+i)), "", "edge", "GET", ts(0, 0), ts(0, 30), model.StatusOK),
			span("t", "c"+string(rune('a'+i)), "r"+string(rune('a'+i)), "db", "q",
				ts(0, 0), ts(0, 0).Add(time.Duration(i)*time.Millisecond), model.StatusOK),
		)
	}
	e := Build(spans).Edges[0]
	if e.P50 != 5*time.Millisecond {
		t.Errorf("P50 = %v, want 5ms", e.P50)
	}
	if e.P95 != 10*time.Millisecond {
		t.Errorf("P95 = %v, want 10ms", e.P95)
	}
}

func TestEdgesAreSortedAndStable(t *testing.T) {
	spans := []model.Span{
		span("t", "a", "", "zeta", "n", ts(0, 0), ts(0, 1), model.StatusOK),
		span("t", "b", "a", "alpha", "n", ts(0, 0), ts(0, 1), model.StatusOK),
		span("t", "c", "", "alpha", "n", ts(0, 0), ts(0, 1), model.StatusOK),
		span("t", "d", "c", "beta", "n", ts(0, 0), ts(0, 1), model.StatusOK),
	}
	res := Build(spans)
	if len(res.Edges) != 2 {
		t.Fatalf("got %d edges, want 2", len(res.Edges))
	}
	if res.Edges[0].Caller != "alpha" || res.Edges[0].Callee != "beta" {
		t.Errorf("edge[0] = %s->%s, want alpha->beta", res.Edges[0].Caller, res.Edges[0].Callee)
	}
	if res.Edges[1].Caller != "zeta" || res.Edges[1].Callee != "alpha" {
		t.Errorf("edge[1] = %s->%s, want zeta->alpha", res.Edges[1].Caller, res.Edges[1].Callee)
	}
}

// Shuffling the span input must not change the graph.
func TestBuildIndependentOfInputOrder(t *testing.T) {
	base := []model.Span{
		span("t", "r1", "", "edge", "GET", ts(0, 0), ts(0, 5), model.StatusError),
		span("t", "c1", "r1", "db", "q", ts(0, 0), ts(0, 2), model.StatusError),
		span("t", "r2", "", "db", "q", ts(0, 0), ts(0, 3), model.StatusOK),
		span("t", "c2", "r2", "cache", "get", ts(0, 0), ts(0, 1), model.StatusOK),
	}
	want := Build(base).Edges

	r := rand.New(rand.NewSource(11))
	for attempt := 0; attempt < 20; attempt++ {
		sh := append([]model.Span(nil), base...)
		r.Shuffle(len(sh), func(i, j int) { sh[i], sh[j] = sh[j], sh[i] })
		got := Build(sh).Edges
		if len(got) != len(want) {
			t.Fatalf("attempt %d: %d edges vs %d", attempt, len(got), len(want))
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("attempt %d: edge %d = %+v, want %+v", attempt, i, got[i], want[i])
			}
		}
	}
}

func TestServicesAndAdjacency(t *testing.T) {
	res := Build([]model.Span{
		span("t", "a", "", "edge", "n", ts(0, 0), ts(0, 1), model.StatusOK),
		span("t", "b", "a", "db", "n", ts(0, 0), ts(0, 1), model.StatusOK),
		span("t", "c", "", "db", "n", ts(0, 0), ts(0, 1), model.StatusOK),
		span("t", "d", "c", "cache", "n", ts(0, 0), ts(0, 1), model.StatusOK),
	})
	svcs := res.Services()
	want := []string{"cache", "db", "edge"}
	if len(svcs) != len(want) {
		t.Fatalf("Services = %v, want %v", svcs, want)
	}
	for i := range want {
		if svcs[i] != want[i] {
			t.Fatalf("Services = %v, want %v", svcs, want)
		}
	}
	if got := res.Callees("edge"); len(got) != 1 || got[0] != "db" {
		t.Errorf("Callees(edge) = %v, want [db]", got)
	}
	if got := res.Callers("cache"); len(got) != 1 || got[0] != "db" {
		t.Errorf("Callers(cache) = %v, want [db]", got)
	}
}

func TestBuildEmpty(t *testing.T) {
	res := Build(nil)
	if len(res.Edges) != 0 || res.Roots != 0 || res.Orphans != 0 {
		t.Fatalf("Build(nil) = %+v", res)
	}
	if len(res.Services()) != 0 {
		t.Fatal("empty graph reported services")
	}
}

func TestEdgeErrorRateWithNoCalls(t *testing.T) {
	if got := (model.Edge{}).ErrorRate(); got != 0 {
		t.Fatalf("ErrorRate of empty edge = %v, want 0", got)
	}
}
