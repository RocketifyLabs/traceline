package bench

import (
	"testing"

	"github.com/andrelbuzzo/itm/internal/analyze"
	"github.com/andrelbuzzo/itm/internal/sim"
)

// TestParseSeedRange covers the CLI's seed syntax.
func TestParseSeedRange(t *testing.T) {
	cases := []struct {
		in   string
		want SeedRange
		bad  bool
	}{
		{"1001-1020", SeedRange{1001, 1020}, false},
		{"7", SeedRange{7, 7}, false},
		{" 1-40 ", SeedRange{1, 40}, false},
		{"20-1", SeedRange{}, true},
		{"abc", SeedRange{}, true},
		{"", SeedRange{}, true},
	}
	for _, c := range cases {
		got, err := ParseSeedRange(c.in)
		if c.bad {
			if err == nil {
				t.Errorf("ParseSeedRange(%q) accepted an invalid range", c.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseSeedRange(%q): %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("ParseSeedRange(%q) = %+v, want %+v", c.in, got, c.want)
		}
	}
}

func TestSeedRangeExpands(t *testing.T) {
	r := SeedRange{From: 5, To: 8}
	got := r.Seeds()
	if len(got) != 4 || got[0] != 5 || got[3] != 8 {
		t.Fatalf("Seeds() = %v", got)
	}
	if len((SeedRange{From: 3, To: 1}).Seeds()) != 0 {
		t.Error("inverted range produced seeds")
	}
}

// The held-out range must be a fixed constant, not something derived.
func TestHeldOutSeedsAreFrozen(t *testing.T) {
	if HeldOutSeeds.From != 1001 || HeldOutSeeds.To != 1020 {
		t.Fatalf("held-out range moved: %+v", HeldOutSeeds)
	}
	if DevSeeds.From != 1 || DevSeeds.To != 40 {
		t.Fatalf("dev range moved: %+v", DevSeeds)
	}
}

// A small smoke run: the pipeline must produce outcomes and not crash.
func TestRunSmallSmoke(t *testing.T) {
	res := Run(SeedRange{From: 1, To: 3}, sim.DefaultConfig(), analyze.DefaultOptions())
	if len(res.Outcomes) != 3 {
		t.Fatalf("got %d outcomes, want 3", len(res.Outcomes))
	}
	for _, o := range res.Outcomes {
		if o.TrueCause == "" {
			t.Errorf("seed %d: empty true cause", o.Seed)
		}
		if o.Candidates == 0 && !o.Insufficient {
			t.Errorf("seed %d: no candidates but not marked insufficient", o.Seed)
		}
	}
}

// FR-10: the floor test. A regression below this fails the build.
//
// The floor is deliberately below the target so ordinary noise in a small
// synthetic set does not fail CI, while a real regression still does.
func TestTop1Floor(t *testing.T) {
	res := Run(HeldOutSeeds, sim.DefaultConfig(), analyze.DefaultOptions())
	t.Logf("held-out: top1=%.1f%% top3=%.1f%% misses=%d noevidence=%d",
		res.Top1*100, res.Top3*100, res.Misses, res.NoEvidence)
	if res.Top1 < 0.60 {
		t.Fatalf("top-1 accuracy %.1f%% is below the 60%% CI floor", res.Top1*100)
	}
}

func TestMarkdownContainsRequiredSections(t *testing.T) {
	res := Run(SeedRange{From: 1, To: 2}, sim.DefaultConfig(), analyze.DefaultOptions())
	md := res.Markdown("test")
	for _, want := range []string{
		"# Benchmark",
		"Limitation, stated first",
		"Top-1 accuracy",
		"## Per fault type",
		"## Per scenario",
	} {
		if !containsStr(md, want) {
			t.Errorf("BENCHMARK.md missing %q", want)
		}
	}
}

func TestByFaultIsSortedAndComplete(t *testing.T) {
	res := Run(SeedRange{From: 1, To: 12}, sim.DefaultConfig(), analyze.DefaultOptions())
	rows := res.ByFault()
	if len(rows) == 0 {
		t.Fatal("no per-fault rows")
	}
	total := 0
	for _, r := range rows {
		total += r.Scenarios
	}
	if total != len(res.Outcomes) {
		t.Fatalf("per-fault scenarios sum to %d, want %d", total, len(res.Outcomes))
	}
	for i := 1; i < len(rows); i++ {
		if rows[i-1].Fault > rows[i].Fault {
			t.Fatalf("per-fault rows not sorted: %s before %s", rows[i-1].Fault, rows[i].Fault)
		}
	}
}

func containsStr(haystack, needle string) bool {
	return len(needle) == 0 || (len(haystack) >= len(needle) && indexOfStr(haystack, needle) >= 0)
}

func indexOfStr(h, n string) int {
	for i := 0; i+len(n) <= len(h); i++ {
		if h[i:i+len(n)] == n {
			return i
		}
	}
	return -1
}
