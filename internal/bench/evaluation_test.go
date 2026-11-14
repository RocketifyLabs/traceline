package bench

import (
	"testing"

	"github.com/RocketifyLabs/traceline/internal/analyze"
	"github.com/RocketifyLabs/traceline/internal/sim"
)

// The evaluation sets must stay frozen and disjoint. A seed that appears in both
// the tuning set and an evaluation set turns the benchmark into a memory test.
func TestSeedSetsAreDisjoint(t *testing.T) {
	seen := map[int64]string{}
	for name, r := range map[string]SeedRange{
		"dev":      DevSeeds,
		"heldout":  HeldOutSeeds,
		"extended": ExtendedSeeds,
	} {
		for _, s := range r.Seeds() {
			if prev, dup := seen[s]; dup {
				t.Fatalf("seed %d appears in both %q and %q", s, prev, name)
			}
			seen[s] = name
		}
	}
	if len(seen) == 0 {
		t.Fatal("no seeds in any set")
	}
}

// ExtendedSeeds must be large enough to give the headline number meaning.
func TestExtendedSetIsLargeEnough(t *testing.T) {
	if n := len(ExtendedSeeds.Seeds()); n < 200 {
		t.Fatalf("extended set has %d seeds; a headline accuracy needs at least 200 "+
			"to keep the confidence interval within a few points", n)
	}
}

// The extended evaluation is the headline number, so it gets its own floor.
//
// This is a slow test (500 scenarios) and is skipped in -short mode; it runs in
// `make test-all` and `make test-perf`.
func TestExtendedTop1Floor(t *testing.T) {
	if testing.Short() {
		t.Skip("extended evaluation skipped in -short mode")
	}

	res := Run(ExtendedSeeds, sim.DefaultConfig(), analyze.DefaultOptions())
	t.Logf("extended (%d scenarios): top1=%.1f%% top3=%.1f%% misses=%d insufficient=%d",
		len(res.Outcomes), res.Top1*100, res.Top3*100, res.Misses, res.NoEvidence)

	for _, f := range res.ByFault() {
		t.Logf("  %-18s n=%-3d top1=%5.1f%% top3=%5.1f%%", f.Fault, f.Scenarios, f.Top1*100, f.Top3*100)
	}

	if res.Top1 < 0.90 {
		t.Fatalf("extended top-1 %.1f%% is below the 90%% floor", res.Top1*100)
	}
	if res.Top3 < 0.98 {
		t.Fatalf("extended top-3 %.1f%% is below the 98%% floor", res.Top3*100)
	}
	// Every fault type must be solvable at all: a fault the method can never find
	// is a scenario defect, not a difficulty.
	for _, f := range res.ByFault() {
		if f.Top1 < 0.80 {
			t.Errorf("fault %q top-1 is only %.1f%%, which suggests the scenario is "+
				"unfair rather than hard", f.Fault, f.Top1*100)
		}
	}
}

// Realistic scenarios must be harder than clean ones, or the noise is not doing
// its job and the benchmark is flattering itself.
func TestRealisticIsHarderThanClean(t *testing.T) {
	if testing.Short() {
		t.Skip("comparison skipped in -short mode")
	}

	clean := Run(SeedRange{From: 200, To: 259}, sim.EasyConfig(), analyze.DefaultOptions())
	realistic := Run(SeedRange{From: 200, To: 259}, sim.DefaultConfig(), analyze.DefaultOptions())

	t.Logf("clean top1=%.1f%%  realistic top1=%.1f%% (same 60 seeds)",
		clean.Top1*100, realistic.Top1*100)

	if realistic.Top1 > clean.Top1+0.15 {
		t.Fatalf("realistic scenarios are not harder (%.1f%% vs %.1f%%); the background "+
			"noise may not be reaching the analysis", realistic.Top1*100, clean.Top1*100)
	}
	if clean.Top1 < 0.95 {
		t.Fatalf("clean scenarios score only %.1f%%, which suggests a regression in the "+
			"core method rather than difficulty", clean.Top1*100)
	}
}
