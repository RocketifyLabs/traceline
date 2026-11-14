// Package replay re-runs the same capture under two different sets of analysis
// options and reports how the answer changed.
//
// Why this exists: Traceline answers "what caused this outage" once, with one set
// of tuning. The question an on-call engineer actually asks next is "would I get
// a different answer if I assumed the incident started earlier?" — or "how much
// does the ranking depend on those weights?". Replay answers that by running the
// same pure pipeline twice and diffing the two reports.
//
// State rewind is explicitly out of scope: this re-analyzes a capture, it does
// not replay the system's behaviour.
package replay

import (
	"fmt"
	"sort"
	"strings"

	"github.com/RocketifyLabs/traceline/internal/analyze"
	"github.com/RocketifyLabs/traceline/internal/model"
)

// Side is one run: the options used and the report they produced.
type Side struct {
	Label  string
	Run    analyze.Options
	Report model.Report
}

// Diff is the comparison of two runs of the same input.
type Diff struct {
	A, B Side

	// SameTop is true when both runs rank the same service first.
	SameTop bool
	// SameVerdict is true when both agree on "insufficient evidence" versus a
	// named cause. A run that names a cause where the other finds nothing has
	// changed its mind about whether an incident happened at all, which matters
	// more than a reordering.
	SameVerdict bool

	// Rankings lists the services each run ranked, in order, so a caller can show
	// the movement rather than just the winner.
	RankingsA, RankingsB []string
	// Moved contains the services whose rank changed, with 0 meaning "absent".
	Moved []RankChange
}

// RankChange is one service's movement between the two runs.
type RankChange struct {
	Service string
	RankA   int // 1-based; 0 means the service does not appear
	RankB   int
	ScoreA  float64
	ScoreB  float64
}

// Changed reports whether the two runs disagreed in any way that a human would
// notice: a different winner, a different verdict, or a reordering.
func (d Diff) Changed() bool {
	if !d.SameTop || !d.SameVerdict {
		return true
	}
	for _, m := range d.Moved {
		if m.RankA != m.RankB {
			return true
		}
	}
	return false
}

// Compare runs the same input under both option sets and diffs the results.
//
// Use this when only the tuning differs. When the two runs also disagree about
// the analysis window, use CompareBoth: the window is part of the input, so two
// different windows are two different inputs over the same records.
func Compare(in analyze.Input, a, b analyze.Options) Diff {
	return CompareBoth(in, a, in, b)
}

// CompareBoth is Compare for two inputs that share records but differ in their
// window. Passing them as separate inputs rather than mutating one keeps the
// pipeline's purity visible: analyze.Run never writes to its input, so both runs
// can share the underlying record slices.
func CompareBoth(inA analyze.Input, a analyze.Options, inB analyze.Input, b analyze.Options) Diff {
	d := Diff{
		A: Side{Label: "A", Run: a, Report: analyze.Run(inA, a)},
		B: Side{Label: "B", Run: b, Report: analyze.Run(inB, b)},
	}

	d.SameVerdict = d.A.Report.Insufficient == d.B.Report.Insufficient
	// Two runs that both found nothing agree. Comparing empty strings and calling
	// that a change would report movement where nothing happened — the noisiest
	// possible way to be wrong about a diff.
	topA, topB := topService(d.A.Report), topService(d.B.Report)
	d.SameTop = topA == topB

	d.RankingsA = ranking(d.A.Report)
	d.RankingsB = ranking(d.B.Report)

	ra, rb := rankIndex(d.RankingsA), rankIndex(d.RankingsB)
	scoresA, scoresB := scoreIndex(d.A.Report), scoreIndex(d.B.Report)

	seen := map[string]bool{}
	for _, s := range append(append([]string{}, d.RankingsA...), d.RankingsB...) {
		if seen[s] {
			continue
		}
		seen[s] = true
		d.Moved = append(d.Moved, RankChange{
			Service: s,
			RankA:   ra[s],
			RankB:   rb[s],
			ScoreA:  scoresA[s],
			ScoreB:  scoresB[s],
		})
	}
	// Sorted by service so the output is stable for a golden test; map iteration
	// above is the hazard this avoids.
	sort.Slice(d.Moved, func(i, j int) bool { return d.Moved[i].Service < d.Moved[j].Service })
	return d
}

// Summary renders the diff for a terminal: the verdict, the winner, and the
// movement, with scores to two decimals.
func (d Diff) Summary() string {
	var b strings.Builder

	fmt.Fprintf(&b, "run A (%s) vs run B (%s)\n", d.A.Label, d.B.Label)
	fmt.Fprintf(&b, "  verdict  A: %s   B: %s", verdictWord(d.A.Report), verdictWord(d.B.Report))
	if d.SameVerdict {
		b.WriteString("   (unchanged)\n")
	} else {
		b.WriteString("   CHANGED\n")
	}

	fmt.Fprintf(&b, "  top-1    A: %s   B: %s", orNone(topService(d.A.Report)), orNone(topService(d.B.Report)))
	if d.SameTop {
		b.WriteString("   (unchanged)\n")
	} else {
		b.WriteString("   CHANGED\n")
	}

	if len(d.Moved) == 0 {
		// Both runs found nothing. Say so in the same words as the changed case,
		// because "no candidates" and "identical ranking" are the same answer here
		// and the reader should not have to infer it from silence.
		b.WriteString("  no candidates in either run — identical ranking under both option sets\n")
		return b.String()
	}

	b.WriteString("\n  service          A            B\n")
	for _, m := range d.Moved {
		fmt.Fprintf(&b, "  %-15s %-12s %-12s",
			m.Service, rankScore(m.RankA, m.ScoreA), rankScore(m.RankB, m.ScoreB))
		if m.RankA != m.RankB {
			b.WriteString("  ← moved")
		}
		b.WriteString("\n")
	}

	if !d.Changed() {
		b.WriteString("\n  identical ranking under both option sets\n")
	}
	return b.String()
}

// Markdown renders the same diff as a table, for pasting into a postmortem.
func (d Diff) Markdown() string {
	var b strings.Builder
	b.WriteString("## Replay: A vs B\n\n")
	fmt.Fprintf(&b, "| | A | B | |\n|---|---|---|---|\n")
	fmt.Fprintf(&b, "| Verdict | %s | %s | %s |\n",
		verdictWord(d.A.Report), verdictWord(d.B.Report), mark(d.SameVerdict))
	fmt.Fprintf(&b, "| Top-1 | %s | %s | %s |\n",
		orNone(topService(d.A.Report)), orNone(topService(d.B.Report)), mark(d.SameTop))
	b.WriteString("\n| Service | Rank A | Score A | Rank B | Score B |\n|---|---|---|---|---|\n")
	for _, m := range d.Moved {
		rankNote := ""
		if m.RankA != m.RankB {
			rankNote = " **moved**"
		}
		fmt.Fprintf(&b, "| %s | %s | %.2f | %s | %.2f |%s\n",
			m.Service, rankWord(m.RankA), m.ScoreA, rankWord(m.RankB), m.ScoreB, rankNote)
	}
	// The closing line must reflect the whole diff, not just the verdict: a
	// reordering with the same winner is still a change worth seeing.
	if d.Changed() {
		fmt.Fprintf(&b, "\n**Ranking changed** — %d service(s) moved.\n", movedCount(d.Moved))
	} else {
		b.WriteString("\nIdentical ranking under both option sets.\n")
	}
	return b.String()
}

// movedCount counts services whose rank differs between the runs.
func movedCount(moved []RankChange) int {
	n := 0
	for _, m := range moved {
		if m.RankA != m.RankB {
			n++
		}
	}
	return n
}

func mark(same bool) string {
	if same {
		return "same"
	}
	return "**changed**"
}

func verdictWord(r model.Report) string {
	if r.Insufficient {
		return "insufficient evidence"
	}
	if len(r.Candidates) == 0 {
		return "no candidates"
	}
	return "cause named"
}

func topService(r model.Report) string {
	if len(r.Candidates) == 0 {
		return ""
	}
	return r.Candidates[0].Service
}

func ranking(r model.Report) []string {
	out := make([]string, 0, len(r.Candidates))
	for _, c := range r.Candidates {
		out = append(out, c.Service)
	}
	return out
}

func rankIndex(ranked []string) map[string]int {
	m := make(map[string]int, len(ranked))
	for i, s := range ranked {
		m[s] = i + 1
	}
	return m
}

func scoreIndex(r model.Report) map[string]float64 {
	m := make(map[string]float64, len(r.Candidates))
	for _, c := range r.Candidates {
		m[c.Service] = c.Score
	}
	return m
}

func rankScore(rank int, score float64) string {
	if rank == 0 {
		return "—"
	}
	return fmt.Sprintf("#%d %.2f", rank, score)
}

func rankWord(rank int) string {
	if rank == 0 {
		return "—"
	}
	return fmt.Sprintf("#%d", rank)
}

func orNone(s string) string {
	if s == "" {
		return "—"
	}
	return s
}
