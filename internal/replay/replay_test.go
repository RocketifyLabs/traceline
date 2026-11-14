package replay

import (
	"strings"
	"testing"

	"github.com/RocketifyLabs/traceline/internal/analyze"
	"github.com/RocketifyLabs/traceline/internal/ingest"
)

// fixture loads the hand-checked pool-exhaustion capture, which has a known
// answer: db-proxy is the cause.
func fixture(t *testing.T) analyze.Input {
	t.Helper()
	logs, err := ingest.Logs("../../fixtures/pool-exhaustion/logs.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	spans, err := ingest.Spans("../../fixtures/pool-exhaustion/spans.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	return analyze.Input{Logs: logs.Records, Spans: spans.Records}
}

// healthy loads the steady-state capture, whose documented answer is
// "insufficient evidence" — the fixture that exists to prove the tool refuses to
// invent a cause.
func healthy(t *testing.T) analyze.Input {
	t.Helper()
	logs, err := ingest.Logs("../../fixtures/steady-state/logs.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	spans, err := ingest.Spans("../../fixtures/steady-state/spans.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	return analyze.Input{Logs: logs.Records, Spans: spans.Records}
}

// Running the same options twice must produce no diff at all. The pipeline is
// deterministic, so a difference here would mean replay itself is lying.
func TestIdenticalOptionsProduceNoChange(t *testing.T) {
	in := fixture(t)
	opts := analyze.DefaultOptions()
	d := Compare(in, opts, opts)

	if d.Changed() {
		t.Errorf("identical options reported a change:\n%s", d.Summary())
	}
	if !d.SameTop || !d.SameVerdict {
		t.Errorf("same options, but SameTop=%v SameVerdict=%v", d.SameTop, d.SameVerdict)
	}
	if got := d.A.Report.Candidates[0].Service; got != "db-proxy" {
		t.Fatalf("fixture answer changed: top-1 = %s, want db-proxy", got)
	}
}

// Reweighting until onset is nearly ignored leaves the same ranking on this
// fixture: db-proxy wins on every factor, so the test asserts that a score can
// move while the order does not — which is what "identical ranking" means and is
// distinct from "identical numbers".
func TestReweightingMovesScoresWithoutMovingRank(t *testing.T) {
	in := fixture(t)
	optsA := analyze.DefaultOptions()
	optsB := analyze.DefaultOptions()
	optsB.Weights.Onset = 0.05
	optsB.Weights.Strength = 0.85
	optsB.Weights.ExplainsDownstream = 0.05
	optsB.Weights.NotExplained = 0.05

	d := Compare(in, optsA, optsB)
	if d.SameTop != true {
		t.Fatalf("ranking changed unexpectedly:\n%s", d.Summary())
	}
	if d.Changed() {
		t.Errorf("ranking did not change, but Changed() said it did:\n%s", d.Summary())
	}

	// The scores must actually differ, or the reweighting silently did nothing.
	var moved bool
	for _, m := range d.Moved {
		if m.ScoreA != m.ScoreB {
			moved = true
		}
	}
	if !moved {
		t.Error("no score changed after reweighting; the options were probably ignored")
	}
}

// Two runs that found nothing must agree, not report a phantom change. This is
// the bug the first version had: comparing two empty top-1 values as different.
func TestBothInsufficientIsNotAChange(t *testing.T) {
	// The steady-state fixture is a healthy capture: the documented answer is
	// "insufficient evidence", so both runs must agree there is nothing to see.
	// A threshold trick would not do the job — a baseline with MAD 0 turns one
	// error into a ~1e17-sigma event, so no finite ZThreshold suppresses it
	// (docs/HANDOFF.md trap 3).
	in := healthy(t)
	opts := analyze.DefaultOptions()

	d := Compare(in, opts, opts)
	if !d.A.Report.Insufficient || !d.B.Report.Insufficient {
		t.Fatalf("expected both runs to be insufficient, got A=%v B=%v",
			d.A.Report.Insufficient, d.B.Report.Insufficient)
	}
	if !d.SameTop {
		t.Errorf("two empty rankings were reported as different")
	}
	if d.Changed() {
		t.Errorf("two insufficient runs reported a change:\n%s", d.Summary())
	}
	if !strings.Contains(d.Summary(), "identical ranking") {
		t.Errorf("summary should say there was no change:\n%s", d.Summary())
	}
}

func TestSummaryNamesBothSides(t *testing.T) {
	inA, inB := fixture(t), healthy(t)
	opts := analyze.DefaultOptions()

	d := CompareBoth(inA, opts, inB, opts)
	d.A.Label = "A (defaults)"
	d.B.Label = "B (strict)"

	s := d.Summary()
	for _, want := range []string{"A (defaults)", "B (strict)", "CHANGED"} {
		if !strings.Contains(s, want) {
			t.Errorf("summary missing %q:\n%s", want, s)
		}
	}
}

// The Markdown output must be a valid table a postmortem can paste, so it needs
// the header, the separator and the changed rows.
func TestMarkdownHasTableAndMarksChange(t *testing.T) {
	inA, inB := fixture(t), healthy(t)
	opts := analyze.DefaultOptions()

	d := CompareBoth(inA, opts, inB, opts)
	md := d.Markdown()
	for _, want := range []string{"## Replay", "| Service |", "|---|", "**moved**", "Ranking changed"} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown missing %q:\n%s", want, md)
		}
	}
}

// Determinism: the same comparison run twice must render identically. Map
// iteration is the hazard, so this is the test that proves the ordering fix.
func TestSummaryIsDeterministic(t *testing.T) {
	in := fixture(t)
	optsA, optsB := analyze.DefaultOptions(), analyze.DefaultOptions()
	optsB.Weights.NotExplained = 0.10
	optsB.Weights.Onset = 0.30
	optsB.Weights.Strength = 0.35

	first := Compare(in, optsA, optsB).Summary()
	for i := 0; i < 5; i++ {
		if got := Compare(in, optsA, optsB).Summary(); got != first {
			t.Fatalf("summary differs on run %d:\n--- first ---\n%s\n--- got ---\n%s", i, first, got)
		}
	}
}
