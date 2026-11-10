package rca

import (
	"math"
	"testing"
	"time"

	"github.com/andrelbuzzo/itm/internal/graph"
	"github.com/andrelbuzzo/itm/internal/model"
)

func t0() time.Time { return time.Date(2026, 10, 16, 10, 0, 0, 0, time.UTC) }

func anomaly(svc, kind string, onset time.Time, strength float64, ids ...string) model.Anomaly {
	return model.Anomaly{Service: svc, Kind: kind, Onset: onset, Strength: strength, LogIDs: ids}
}

func TestRankEmptyIsInsufficientEvidence(t *testing.T) {
	got, insufficient := Rank(Input{}, DefaultWeights(), DefaultConfidence())
	if !insufficient {
		t.Fatal("empty anomalies must be insufficient evidence")
	}
	if len(got) != 0 {
		t.Fatalf("got candidates from no anomalies: %+v", got)
	}
}

func TestRankInjectedCauseFirst(t *testing.T) {
	// db-proxy breaks first and is the deepest anomalous service.
	in := Input{
		Anomalies: []model.Anomaly{
			anomaly("db-proxy", "error_rate", t0(), 9.0, "e1"),
			anomaly("api-edge", "error_rate", t0().Add(30*time.Second), 6.0, "e2"),
			anomaly("web", "error_rate", t0().Add(45*time.Second), 5.0, "e3"),
		},
		IncidentStart: t0(),
		IncidentEnd:   t0().Add(5 * time.Minute),
	}
	got, insufficient := Rank(in, DefaultWeights(), DefaultConfidence())
	if insufficient {
		t.Fatal("unexpected insufficient evidence")
	}
	if got[0].Service != "db-proxy" {
		t.Fatalf("top candidate = %s, want db-proxy (all: %+v)", got[0].Service, got)
	}
	if got[0].Confidence != "high" && got[0].Confidence != "medium" {
		t.Errorf("confidence = %q, want high or medium", got[0].Confidence)
	}
}

// A noisy red herring that explains nothing downstream must not win.
func TestRankRedHerringDoesNotWin(t *testing.T) {
	g := graph.Result{Edges: []model.Edge{
		{Caller: "api-edge", Callee: "db-proxy"},
		{Caller: "web", Callee: "api-edge"},
	}}
	in := Input{
		Anomalies: []model.Anomaly{
			// The real cause: earliest, part of the call chain.
			anomaly("db-proxy", "error_rate", t0(), 8.0, "e1"),
			// The red herring: very strong but isolated and later.
			anomaly("analytics", "latency_p95", t0().Add(90*time.Second), 12.0, "e2"),
			anomaly("api-edge", "error_rate", t0().Add(20*time.Second), 6.0, "e3"),
		},
		Graph:         g,
		IncidentStart: t0(),
		IncidentEnd:   t0().Add(5 * time.Minute),
	}
	got, _ := Rank(in, DefaultWeights(), DefaultConfidence())
	if got[0].Service == "analytics" {
		t.Fatalf("red herring ranked first: %+v", got)
	}
	// analytics must still appear, just not first.
	var found bool
	for _, c := range got {
		if c.Service == "analytics" {
			found = true
		}
	}
	if !found {
		t.Error("red herring missing from candidates entirely")
	}
}

func TestRankVictimIsPenalisedByUpstreamAnomaly(t *testing.T) {
	g := graph.Result{Edges: []model.Edge{
		{Caller: "api-edge", Callee: "db-proxy"},
	}}
	in := Input{
		Anomalies: []model.Anomaly{
			anomaly("db-proxy", "error_rate", t0(), 8.0, "e1"),
			anomaly("api-edge", "error_rate", t0().Add(10*time.Second), 8.0, "e2"),
		},
		Graph:         g,
		IncidentStart: t0(),
		IncidentEnd:   t0().Add(5 * time.Minute),
	}
	got, _ := Rank(in, DefaultWeights(), DefaultConfidence())
	if got[0].Service != "db-proxy" {
		t.Fatalf("top = %s, want db-proxy", got[0].Service)
	}
	var edge model.Candidate
	for _, c := range got {
		if c.Service == "api-edge" {
			edge = c
		}
	}
	if edge.Breakdown.NotExplained != 0 {
		t.Errorf("api-edge NotExplained = %v, want 0 (its dependency is anomalous and earlier)", edge.Breakdown.NotExplained)
	}
}

func TestRankBreakdownIsPresentAndBounded(t *testing.T) {
	in := Input{
		Anomalies:     []model.Anomaly{anomaly("a", "error_rate", t0(), 20.0, "e1")},
		IncidentStart: t0(),
		IncidentEnd:   t0().Add(time.Minute),
	}
	got, _ := Rank(in, DefaultWeights(), DefaultConfidence())
	b := got[0].Breakdown
	for name, v := range map[string]float64{
		"onset": b.Onset, "strength": b.Strength,
		"explains_downstream": b.ExplainsDownstream, "not_explained": b.NotExplained,
	} {
		if v < 0 || v > 1 {
			t.Errorf("%s = %v, out of [0,1]", name, v)
		}
	}
	// A single service with a huge z-score and immediate onset should be strong.
	if got[0].Score < 0.5 {
		t.Errorf("score = %v, want >= 0.5 for an immediate, strong, unexplained service", got[0].Score)
	}
}

func TestRankTieBreakIsServiceName(t *testing.T) {
	// Two identical candidates: the alphabetical one must win.
	in := Input{
		Anomalies: []model.Anomaly{
			anomaly("zebra", "error_rate", t0(), 5.0, "e1"),
			anomaly("alpha", "error_rate", t0(), 5.0, "e2"),
		},
		IncidentStart: t0(),
		IncidentEnd:   t0().Add(time.Minute),
	}
	got, _ := Rank(in, DefaultWeights(), DefaultConfidence())
	if got[0].Service != "alpha" {
		t.Fatalf("tie-break put %s first, want alpha", got[0].Service)
	}
}

func TestRankDeterministicUnderShuffle(t *testing.T) {
	base := []model.Anomaly{
		anomaly("db", "error_rate", t0(), 7.0, "e1"),
		anomaly("api", "error_rate", t0().Add(10*time.Second), 5.0, "e2"),
		anomaly("web", "latency_p95", t0().Add(20*time.Second), 4.0, "e3"),
	}
	in := func(as []model.Anomaly) Input {
		return Input{Anomalies: as, IncidentStart: t0(), IncidentEnd: t0().Add(5 * time.Minute)}
	}
	want, _ := Rank(in(base), DefaultWeights(), DefaultConfidence())

	shuffles := [][]int{{2, 0, 1}, {1, 2, 0}, {0, 2, 1}}
	for _, order := range shuffles {
		as := make([]model.Anomaly, len(order))
		for i, idx := range order {
			as[i] = base[idx]
		}
		got, _ := Rank(in(as), DefaultWeights(), DefaultConfidence())
		for i := range want {
			if got[i].Service != want[i].Service {
				t.Fatalf("order %v changed ranking: %s vs %s", order, got[i].Service, want[i].Service)
			}
			if math.Abs(got[i].Score-want[i].Score) > 1e-12 {
				t.Fatalf("order %v changed score of %s: %v vs %v", order, got[i].Service, got[i].Score, want[i].Score)
			}
		}
	}
}

func TestRankRespectsTopK(t *testing.T) {
	var as []model.Anomaly
	for _, svc := range []string{"a", "b", "c", "d", "e", "f", "g"} {
		as = append(as, anomaly(svc, "error_rate", t0(), 4.0, "e-"+svc))
	}
	in := Input{Anomalies: as, IncidentStart: t0(), IncidentEnd: t0().Add(time.Minute), TopK: 3}
	got, _ := Rank(in, DefaultWeights(), DefaultConfidence())
	if len(got) != 3 {
		t.Fatalf("got %d candidates, want 3", len(got))
	}
}

func TestRankConfidenceLabels(t *testing.T) {
	// One dominant candidate, one far weaker: high confidence.
	in := Input{
		Anomalies: []model.Anomaly{
			anomaly("dominant", "error_rate", t0(), 15.0, "e1"),
			anomaly("weak", "error_rate", t0().Add(4*time.Minute), 1.0, "e2"),
		},
		IncidentStart: t0(),
		IncidentEnd:   t0().Add(5 * time.Minute),
	}
	got, _ := Rank(in, DefaultWeights(), DefaultConfidence())
	if got[0].Confidence != "high" {
		t.Errorf("dominant candidate confidence = %q, want high (score %v)", got[0].Confidence, got[0].Score)
	}
}

func TestRankEvidenceIsCarried(t *testing.T) {
	in := Input{
		Anomalies:     []model.Anomaly{anomaly("a", "error_rate", t0(), 6.0, "log-1", "log-2")},
		IncidentStart: t0(),
		IncidentEnd:   t0().Add(time.Minute),
	}
	got, _ := Rank(in, DefaultWeights(), DefaultConfidence())
	if len(got[0].EvidenceIDs) != 2 {
		t.Fatalf("evidence ids = %v, want 2", got[0].EvidenceIDs)
	}
}

func TestWeightsSumToOne(t *testing.T) {
	w := DefaultWeights()
	total := w.Onset + w.Strength + w.ExplainsDownstream + w.NotExplained
	if math.Abs(total-1.0) > 1e-9 {
		t.Fatalf("weights sum to %v, want 1.0", total)
	}
}
