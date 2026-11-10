// Package rca ranks probable root causes.
//
// The method is deliberately simple and auditable rather than clever. Each
// candidate service accumulates four factors whose values and weights are shown
// in the report, so an on-call engineer can disagree with the ranking instead of
// having to trust it.
//
// If nothing passes the anomaly threshold, the answer is "insufficient evidence"
// — never a confident guess.
package rca

import (
	"sort"
	"time"

	"github.com/andrelbuzzo/itm/internal/graph"
	"github.com/andrelbuzzo/itm/internal/model"
)

// isMeaningful reports whether an anomaly is evidence that a service is actually
// in trouble.
//
// Only behavioural anomalies qualify. A log-template burst with no change in
// error rate or latency is a logging change — and in this simulator it is
// guaranteed noise, because every healthy service bursts its templates at the
// fault boundary. Letting a burst create a candidate is what made a healthy
// service win: it had a perfect onset, no anomalous dependency, and nothing to
// explain, which is the highest possible score.
//
// Template bursts therefore corroborate a service that is already a candidate;
// they never create one.
func isMeaningful(a model.Anomaly) bool {
	switch a.Kind {
	case "error_rate", "latency_p95", "new_error_template":
		return true
	default:
		return false
	}
}

// Weights are the factor weights. They sum to 1 so a perfect candidate scores
// exactly 1.0.
type Weights struct {
	Onset              float64 `json:"onset"`
	Strength           float64 `json:"strength"`
	ExplainsDownstream float64 `json:"explains_downstream"`
	NotExplained       float64 `json:"not_explained"`
}

// DefaultWeights matches itm/rca_weights.json.
func DefaultWeights() Weights {
	return Weights{Onset: 0.35, Strength: 0.30, ExplainsDownstream: 0.25, NotExplained: 0.10}
}

// Confidence thresholds.
type Confidence struct {
	HighScore  float64
	HighMargin float64
	Medium     float64
}

// DefaultConfidence matches itm/rca_weights.json.
func DefaultConfidence() Confidence {
	return Confidence{HighScore: 0.60, HighMargin: 0.15, Medium: 0.40}
}

// Input is everything the ranker needs.
type Input struct {
	Anomalies     []model.Anomaly
	Graph         graph.Result
	IncidentStart time.Time
	IncidentEnd   time.Time
	// TopK bounds the number of candidates returned. Zero means 5.
	TopK int
}

// Rank produces the ranked candidates.
//
// The returned slice is empty and Insufficient is true when no anomaly is strong
// enough to justify naming a cause.
func Rank(in Input, w Weights, c Confidence) (candidates []model.Candidate, insufficient bool) {
	if len(in.Anomalies) == 0 {
		return nil, true
	}
	if w.Onset+w.Strength+w.ExplainsDownstream+w.NotExplained <= 0 {
		w = DefaultWeights()
	}
	if c.HighScore <= 0 {
		c = DefaultConfidence()
	}
	topK := in.TopK
	if topK <= 0 {
		topK = 5
	}

	// Earliest onset per service, and the strongest anomaly per service.
	// Only anomalies that mean "this service is in trouble" participate; a noisy
	// log template must not make a healthy service look broken or, worse, make a
	// broken service look explained by its dependency.
	onset := map[string]time.Time{}
	strength := map[string]float64{}
	for _, a := range in.Anomalies {
		if a.Onset.IsZero() || !isMeaningful(a) {
			continue
		}
		cur, ok := onset[a.Service]
		if !ok || a.Onset.Before(cur) {
			onset[a.Service] = a.Onset
		}
		if a.Strength > strength[a.Service] {
			strength[a.Service] = a.Strength
		}
	}
	if len(onset) == 0 {
		return nil, true
	}

	services := make([]string, 0, len(onset))
	for svc := range onset {
		services = append(services, svc)
	}
	sort.Strings(services)

	span := in.IncidentEnd.Sub(in.IncidentStart)
	if span <= 0 {
		span = time.Second
	}

	// Downstream relation: for each anomalous service, which other anomalous
	// services DEPEND ON it (its transitive callers).
	//
	// Direction matters and is easy to get backwards. A root cause at the bottom
	// of a call chain has no anomalous callee; what it explains is the failure of
	// the services that call it. Counting callees would score the root cause zero
	// for explaining anything, and hand the win to its loudest victim.
	dependents := dependentMap(in.Graph, services)

	out := make([]model.Candidate, 0, len(services))
	for _, svc := range services {
		b := model.ScoreBreakdown{}

		// Factor 1: earlier onset scores higher.
		elapsed := onset[svc].Sub(in.IncidentStart)
		if elapsed < 0 {
			elapsed = 0
		}
		b.Onset = clamp01(1 - float64(elapsed)/float64(span))

		// Factor 2: stronger anomaly scores higher. z/10 saturates at a very
		// strong signal so one absurd outlier cannot dominate the ranking.
		b.Strength = clamp01(strength[svc] / 10)

		// Factor 3: how many of this service's anomalous dependents it precedes.
		deps := dependents[svc]
		if len(deps) > 0 {
			explained := 0
			for _, d := range deps {
				if onset[svc].Before(onset[d]) || onset[svc].Equal(onset[d]) {
					explained++
				}
			}
			b.ExplainsDownstream = float64(explained) / float64(len(deps))
		}

		// Factor 4: a service whose own dependency is also anomalous and earlier
		// is more likely a victim than a cause. The dependency is a CALLEE: what
		// this service calls. Callers are its consumers, and a noisy consumer
		// says nothing about whether this service caused anything.
		b.NotExplained = 1.0
		for _, dep := range in.Graph.Callees(svc) {
			if _, ok := onset[dep]; ok && !onset[dep].After(onset[svc]) {
				b.NotExplained = 0.0
				break
			}
		}

		score := w.Onset*b.Onset +
			w.Strength*b.Strength +
			w.ExplainsDownstream*b.ExplainsDownstream +
			w.NotExplained*b.NotExplained

		out = append(out, model.Candidate{
			Service:     svc,
			Score:       score,
			Breakdown:   b,
			EvidenceIDs: evidenceFor(in.Anomalies, svc),
		})
	}

	// Rank: score desc, then service asc, then first evidence id asc. A total
	// order, so equal scores never depend on input order.
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		if out[i].Service != out[j].Service {
			return out[i].Service < out[j].Service
		}
		return firstID(out[i].EvidenceIDs) < firstID(out[j].EvidenceIDs)
	})

	// Confidence labels.
	//
	// The margin only means something for the leader: it answers "is the winner
	// clearly better than the runner-up?". Applying it to every row made the
	// second candidate look more trustworthy than the first, because its gap to
	// the third happened to be large — which is meaningless.
	for i := range out {
		if i == 0 {
			margin := 0.0
			if len(out) > 1 {
				margin = out[0].Score - out[1].Score
			}
			switch {
			case out[0].Score >= c.HighScore && margin >= c.HighMargin:
				out[0].Confidence = "high"
			case out[0].Score >= c.Medium:
				out[0].Confidence = "medium"
			default:
				out[0].Confidence = "low"
			}
			continue
		}
		// Runners-up are labelled by absolute score only, and can never claim
		// high confidence: they are not the answer.
		switch {
		case out[i].Score >= c.HighScore:
			out[i].Confidence = "medium"
		case out[i].Score >= c.Medium:
			out[i].Confidence = "low"
		default:
			out[i].Confidence = "low"
		}
	}

	if len(out) > topK {
		out = out[:topK]
	}
	return out, false
}

// dependentMap returns, for each service, the anomalous services that depend on
// it — that is, its transitive CALLERS.
//
// Reaching a caller means walking the graph backwards: from a service to the
// services whose edges point at it. This is the direction that makes a deep root
// cause explain its visible symptoms.
func dependentMap(g graph.Result, candidates []string) map[string][]string {
	anomalous := make(map[string]bool, len(candidates))
	for _, s := range candidates {
		anomalous[s] = true
	}

	out := make(map[string][]string, len(candidates))
	for _, start := range candidates {
		visited := map[string]bool{}
		var reachable []string
		queue := []string{start}
		for len(queue) > 0 {
			cur := queue[0]
			queue = queue[1:]
			for _, prev := range g.Callers(cur) {
				if visited[prev] {
					continue
				}
				visited[prev] = true
				if anomalous[prev] {
					reachable = append(reachable, prev)
				}
				queue = append(queue, prev)
			}
		}
		sort.Strings(reachable)
		out[start] = reachable
	}
	return out
}

func evidenceFor(anomalies []model.Anomaly, svc string) []string {
	seen := map[string]bool{}
	var out []string
	for _, a := range anomalies {
		if a.Service != svc {
			continue
		}
		for _, id := range append(append([]string{}, a.LogIDs...), a.SpanIDs...) {
			if !seen[id] {
				seen[id] = true
				out = append(out, id)
			}
		}
	}
	sort.Strings(out)
	if len(out) > 20 {
		out = out[:20]
	}
	return out
}

func firstID(ids []string) string {
	if len(ids) == 0 {
		return ""
	}
	return ids[0]
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
