// Package bench measures how often the analysis finds the true root cause.
//
// It is the honest core of the project: without a measured hit rate, "it ranks
// root causes" is an unfalsifiable claim. The protocol is fixed:
//
//   - Development seeds 1-40, used for tuning.
//   - Held-out seeds 1001-1020, frozen and never used for tuning.
//
// The simulator is authored by this same project, so the numbers describe the
// method's behaviour on these scenarios, not real production systems. That
// limitation is stated in BENCHMARK.md and the README.
package bench

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/RocketifyLabs/traceline/internal/analyze"
	"github.com/RocketifyLabs/traceline/internal/determinism"
	"github.com/RocketifyLabs/traceline/internal/sim"
)

// Development and held-out seed ranges. Held-out is a constant, not a parameter,
// so no code path can accidentally tune against it.
var (
	DevSeeds     = SeedRange{From: 1, To: 40}
	HeldOutSeeds = SeedRange{From: 1001, To: 1020}
	// ExtendedSeeds is a larger evaluation set, disjoint from both of the above
	// and from anything used for tuning.
	//
	// A 20-scenario held-out set cannot distinguish 90% from 100%: the confidence
	// interval is roughly +/- 11 points. Reporting 100% on n=20 would be honest
	// arithmetic and a misleading claim, so the extended set exists to give the
	// headline number enough sample to mean something.
	ExtendedSeeds = SeedRange{From: 5000, To: 5499}
)

// SeedRange is an inclusive range of seeds.
type SeedRange struct {
	From int64
	To   int64
}

// Seeds expands the range.
func (r SeedRange) Seeds() []int64 {
	if r.To < r.From {
		return nil
	}
	out := make([]int64, 0, r.To-r.From+1)
	for s := r.From; s <= r.To; s++ {
		out = append(out, s)
	}
	return out
}

// ParseSeedRange parses "1001-1020" or a single "7".
func ParseSeedRange(s string) (SeedRange, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return SeedRange{}, fmt.Errorf("empty seed range")
	}
	if i := strings.Index(s, "-"); i > 0 {
		var from, to int64
		if _, err := fmt.Sscanf(s, "%d-%d", &from, &to); err != nil {
			return SeedRange{}, fmt.Errorf("seed range %q: %w", s, err)
		}
		if to < from {
			return SeedRange{}, fmt.Errorf("seed range %q is inverted", s)
		}
		return SeedRange{From: from, To: to}, nil
	}
	var single int64
	if _, err := fmt.Sscanf(s, "%d", &single); err != nil {
		return SeedRange{}, fmt.Errorf("seed %q: %w", s, err)
	}
	return SeedRange{From: single, To: single}, nil
}

// Outcome is one scenario's result.
type Outcome struct {
	Seed      int64
	Fault     string
	TrueCause string
	// Rank is the 1-based position of the true cause, or 0 when it was absent
	// from the candidates entirely.
	Rank int
	// TopScore is the score of the winning candidate.
	TopScore float64
	// Seconds is the detected-minus-actual start, when the cause was found.
	Seconds float64
	// Insufficient is true when the analysis declined to name a cause.
	Insufficient bool
	Candidates   int
}

// Hit reports whether the true cause was ranked first.
func (o Outcome) Hit() bool { return o.Rank == 1 }

// HitTop3 reports whether the true cause was in the top three.
func (o Outcome) HitTop3() bool { return o.Rank >= 1 && o.Rank <= 3 }

// Result is the full benchmark outcome.
type Result struct {
	Range      SeedRange
	Outcomes   []Outcome
	Top1       float64
	Top3       float64
	MeanTTD    float64 // seconds, over scenarios where the cause was found in top 3
	Misses     int
	NoEvidence int
}

// Run executes the benchmark over a seed range.
func Run(r SeedRange, simCfg sim.Config, opts analyze.Options) Result {
	out := Result{Range: r}
	seeds := r.Seeds()

	var ttdSum float64
	var ttdCount int

	for _, seed := range seeds {
		sc := sim.Generate(seed, "", simCfg)
		rep := analyze.RunScenario(analyze.ScenarioInput{
			Logs:       sc.Logs,
			Spans:      sc.Spans,
			FaultStart: sc.FaultStart,
			FaultEnd:   sc.FaultEnd,
		}, opts)

		o := Outcome{
			Seed:         seed,
			Fault:        sc.Fault,
			TrueCause:    sc.TrueCause,
			Insufficient: rep.Insufficient,
			Candidates:   len(rep.Candidates),
		}
		for i, c := range rep.Candidates {
			if c.Service == sc.TrueCause {
				o.Rank = i + 1
				break
			}
		}
		if len(rep.Candidates) > 0 {
			o.TopScore = rep.Candidates[0].Score
		}
		if o.Rank == 1 || (o.Rank >= 2 && o.Rank <= 3) {
			// Time to detect: how far the detected start sits from the real one.
			// In this pipeline the window is given, so TTD measures the window,
			// not detection; it is reported so the number has a definition rather
			// than being invented later.
			o.Seconds = rep.IncidentStart.Sub(sc.FaultStart).Seconds()
			ttdSum += o.Seconds
			ttdCount++
		}
		if o.Rank == 0 {
			out.Misses++
		}
		if rep.Insufficient {
			out.NoEvidence++
		}
		out.Outcomes = append(out.Outcomes, o)
	}

	total := len(out.Outcomes)
	if total > 0 {
		var top1, top3 int
		for _, o := range out.Outcomes {
			if o.Hit() {
				top1++
			}
			if o.HitTop3() {
				top3++
			}
		}
		out.Top1 = float64(top1) / float64(total)
		out.Top3 = float64(top3) / float64(total)
	}
	if ttdCount > 0 {
		out.MeanTTD = ttdSum / float64(ttdCount)
	}
	return out
}

// ByFault groups outcomes by fault type, in a stable order.
func (r Result) ByFault() []FaultResult {
	groups := map[string][]Outcome{}
	for _, o := range r.Outcomes {
		groups[o.Fault] = append(groups[o.Fault], o)
	}
	out := make([]FaultResult, 0, len(groups))
	for _, fault := range determinism.SortedKeys(groups) {
		os := groups[fault]
		var top1, top3 int
		var ttdSum float64
		var ttdCount int
		for _, o := range os {
			if o.Hit() {
				top1++
			}
			if o.HitTop3() {
				top3++
				ttdSum += o.Seconds
				ttdCount++
			}
		}
		fr := FaultResult{Fault: fault, Scenarios: len(os)}
		if len(os) > 0 {
			fr.Top1 = float64(top1) / float64(len(os))
			fr.Top3 = float64(top3) / float64(len(os))
		}
		if ttdCount > 0 {
			fr.MeanTTD = ttdSum / float64(ttdCount)
		}
		out = append(out, fr)
	}
	return out
}

// FaultResult is the per-fault-type table row.
type FaultResult struct {
	Fault     string
	Scenarios int
	Top1      float64
	Top3      float64
	MeanTTD   float64
}

// Markdown renders BENCHMARK.md.
//
// The limitation paragraph is not optional: a benchmark whose caveats live only
// in a README is a benchmark that will be quoted without them.
//
// Sample size is reported next to every accuracy, because "100% on 20 scenarios"
// and "97% on 500" are not the same claim and the first one is arithmetic
// theatre.
func (r Result) Markdown(generatedFor string) string {
	var b strings.Builder
	b.WriteString("# Benchmark\n\n")
	b.WriteString("How often Traceline ranks the true root cause first, measured on seeded synthetic\n")
	b.WriteString("incidents generated by this project's own simulator.\n\n")

	b.WriteString("**Limitation, stated first.** The simulator is written by the same project that\n")
	b.WriteString("does the analysis. These numbers describe how well the method recovers a cause\n")
	b.WriteString("from *this* generator's behaviour; they are not a claim about real production\n")
	b.WriteString("systems. Tuning used development seeds only; the evaluation ranges below are\n")
	b.WriteString("frozen and were not used for tuning.\n\n")

	fmt.Fprintf(&b, "## Summary\n\n")
	fmt.Fprintf(&b, "| Metric | Value |\n|---|---|\n")
	fmt.Fprintf(&b, "| Seed range | %d-%d |\n", r.Range.From, r.Range.To)
	fmt.Fprintf(&b, "| Scenarios | %d |\n", len(r.Outcomes))
	fmt.Fprintf(&b, "| **Top-1 accuracy** | **%.1f%%** |\n", r.Top1*100)
	fmt.Fprintf(&b, "| Top-3 accuracy | %.1f%% |\n", r.Top3*100)
	fmt.Fprintf(&b, "| 95%% confidence interval on top-1 | ±%.1f points |\n", r.ConfidenceInterval95()*100)
	fmt.Fprintf(&b, "| Mean time-to-detect | %.1fs |\n", r.MeanTTD)
	fmt.Fprintf(&b, "| True cause not in candidates | %d |\n", r.Misses)
	fmt.Fprintf(&b, "| Insufficient evidence returned | %d |\n\n", r.NoEvidence)

	if len(r.Outcomes) < 100 {
		fmt.Fprintf(&b, "> **Sample-size caveat.** %d scenarios gives a confidence interval of roughly\n"+
			"> ±%.0f points, so this percentage should be read as a range. The extended\n"+
			"> evaluation set (500 scenarios) exists for the headline number.\n\n",
			len(r.Outcomes), r.ConfidenceInterval95()*100)
	}

	b.WriteString("## Per fault type\n\n")
	b.WriteString("| Fault | Scenarios | Top-1 | Top-3 | Mean TTD |\n")
	b.WriteString("|---|---:|---:|---:|---:|\n")
	for _, fr := range r.ByFault() {
		fmt.Fprintf(&b, "| `%s` | %d | %.1f%% | %.1f%% | %.1fs |\n",
			fr.Fault, fr.Scenarios, fr.Top1*100, fr.Top3*100, fr.MeanTTD)
	}

	b.WriteString("\n## Per scenario\n\n")
	b.WriteString("| Seed | Fault | True cause | Rank | Top score | Insufficient |\n")
	b.WriteString("|---:|---|---|---:|---:|---|\n")
	for _, o := range r.Outcomes {
		rank := "miss"
		if o.Rank > 0 {
			rank = fmt.Sprintf("%d", o.Rank)
		}
		fmt.Fprintf(&b, "| %d | `%s` | `%s` | %s | %.3f | %v |\n",
			o.Seed, o.Fault, o.TrueCause, rank, o.TopScore, o.Insufficient)
	}
	if generatedFor != "" {
		fmt.Fprintf(&b, "\n---\n\nGenerated from %s.\n", generatedFor)
	}
	return b.String()
}

// ConfidenceInterval95 is the half-width of the 95% interval on Top-1 accuracy,
// using the normal approximation for a proportion.
//
// Reported because a percentage without its sample size invites a claim the data
// cannot support.
func (r Result) ConfidenceInterval95() float64 {
	n := len(r.Outcomes)
	if n == 0 {
		return 0
	}
	p := r.Top1
	return 1.96 * math.Sqrt(p*(1-p)/float64(n))
}

// sortedOutcomes returns outcomes ordered by seed, for reproducible output.
func (r Result) sortedOutcomes() []Outcome {
	out := append([]Outcome(nil), r.Outcomes...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Seed < out[j].Seed })
	return out
}

// Timestamps used in output, kept here so the format is fixed in one place.
const tsLayout = time.RFC3339
