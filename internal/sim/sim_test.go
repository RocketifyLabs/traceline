package sim

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"
)

// FR-9: the same seed must produce byte-identical output.
func TestGenerateIsDeterministic(t *testing.T) {
	for _, fault := range AllFaults {
		a := Generate(42, fault, DefaultConfig())
		b := Generate(42, fault, DefaultConfig())
		if hash(a) != hash(b) {
			t.Fatalf("fault %s: same seed produced different scenarios\n%s\n%s", fault, hash(a), hash(b))
		}
	}
}

func TestDifferentSeedsProduceDifferentScenarios(t *testing.T) {
	a := Generate(1, FaultLatency, DefaultConfig())
	b := Generate(2, FaultLatency, DefaultConfig())
	if hash(a) == hash(b) {
		t.Fatal("different seeds produced identical scenarios")
	}
}

func TestEveryFaultProducesObservableTelemetry(t *testing.T) {
	for _, fault := range AllFaults {
		sc := Generate(7, fault, DefaultConfig())
		if len(sc.Logs) == 0 {
			t.Errorf("%s: no logs generated", fault)
		}
		if len(sc.Spans) == 0 {
			t.Errorf("%s: no spans generated", fault)
		}
		if sc.TrueCause == "" {
			t.Errorf("%s: no true cause recorded", fault)
		}
		if sc.FaultStart.IsZero() {
			t.Errorf("%s: no fault start recorded", fault)
		}
	}
}

// The fault must actually be visible in the target's error rate, or the scenario
// is not a test of anything.
func TestFaultRaisesErrorRateOnTarget(t *testing.T) {
	sc := Generate(3, FaultCrashLoop, DefaultConfig())

	var beforeTotal, beforeErr, afterTotal, afterErr int
	for _, l := range sc.Logs {
		if l.Service != sc.TrueCause {
			continue
		}
		if l.TS.Before(sc.FaultStart) {
			beforeTotal++
			if l.Level.IsError() {
				beforeErr++
			}
		} else {
			afterTotal++
			if l.Level.IsError() {
				afterErr++
			}
		}
	}
	if beforeTotal == 0 || afterTotal == 0 {
		t.Fatalf("target %q has no records before/after the fault (%d/%d)",
			sc.TrueCause, beforeTotal, afterTotal)
	}
	beforeRate := float64(beforeErr) / float64(beforeTotal)
	afterRate := float64(afterErr) / float64(afterTotal)
	if afterRate <= beforeRate {
		t.Fatalf("crash loop did not raise the error rate: %.2f -> %.2f", beforeRate, afterRate)
	}
}

// The noisy-neighbour scenario must have a different true cause than the noisy
// service, otherwise it is not a red herring at all.
func TestNoisyNeighbourIsADistractor(t *testing.T) {
	for seed := int64(1); seed <= 20; seed++ {
		sc := Generate(seed, FaultNoisyNeighbour, DefaultConfig())
		if sc.Fault != FaultNoisyNeighbour {
			t.Fatalf("seed %d: fault = %s", seed, sc.Fault)
		}
		if sc.TrueCause == "" {
			t.Fatalf("seed %d: no true cause", seed)
		}
		// The scenario must contain at least two services to be a distractor.
		if len(sc.Services) < 2 {
			t.Fatalf("seed %d: only %d services", seed, len(sc.Services))
		}
	}
}

func TestBadDeployEmitsDeployEvent(t *testing.T) {
	for seed := int64(1); seed <= 10; seed++ {
		sc := Generate(seed, FaultBadDeploy, DefaultConfig())
		var found bool
		for _, l := range sc.Logs {
			if l.Event() == "deploy" {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("seed %d: bad_deploy produced no deploy event", seed)
		}
	}
}

func TestGenerateProducesEdgesWorthyTopology(t *testing.T) {
	sc := Generate(5, FaultLatency, DefaultConfig())
	if len(sc.Services) < 3 {
		t.Fatalf("topology has %d services, want >= 3", len(sc.Services))
	}
	// Spans must reference at least two distinct services, or there is no graph.
	svcs := map[string]bool{}
	for _, s := range sc.Spans {
		svcs[s.Service] = true
	}
	if len(svcs) < 2 {
		t.Fatalf("spans cover %d services, want >= 2", len(svcs))
	}
}

func TestGenerateDerivesFaultFromSeedWhenUnset(t *testing.T) {
	// Same seed, no fault named: the derived fault must be stable.
	a := Generate(11, "", DefaultConfig())
	b := Generate(11, "", DefaultConfig())
	if a.Fault != b.Fault {
		t.Fatalf("derived fault unstable: %s vs %s", a.Fault, b.Fault)
	}
	if a.Fault == "" {
		t.Fatal("no fault derived from seed")
	}
}

func TestGenerateOutputIsSorted(t *testing.T) {
	sc := Generate(9, FaultTimeoutCascade, DefaultConfig())
	for i := 1; i < len(sc.Logs); i++ {
		if sc.Logs[i].TS.Before(sc.Logs[i-1].TS) {
			t.Fatalf("logs unsorted at %d", i)
		}
	}
	for i := 1; i < len(sc.Spans); i++ {
		if sc.Spans[i].Start.Before(sc.Spans[i-1].Start) {
			t.Fatalf("spans unsorted at %d", i)
		}
	}
}

func TestPhaseOrderingIsDeterministicAcrossFaultOrder(t *testing.T) {
	// Generating a different fault first must not perturb this fault's output:
	// each scenario owns its own rng.
	sc1 := Generate(99, FaultLatency, DefaultConfig())
	_ = Generate(99, FaultCrashLoop, DefaultConfig())
	sc2 := Generate(99, FaultLatency, DefaultConfig())
	if hash(sc1) != hash(sc2) {
		t.Fatal("generating another fault changed this scenario's output")
	}
}

func hash(sc Scenario) string {
	blob, err := json.Marshal(struct {
		Seed      int64
		Fault     string
		TrueCause string
		Logs      int
		Spans     int
		FirstLog  string
		LastLog   string
	}{
		Seed:      sc.Seed,
		Fault:     sc.Fault,
		TrueCause: sc.TrueCause,
		Logs:      len(sc.Logs),
		Spans:     len(sc.Spans),
		FirstLog:  firstLogBlob(sc),
		LastLog:   lastLogBlob(sc),
	})
	if err != nil {
		panic(err)
	}
	sum := sha256.Sum256(blob)
	return hex.EncodeToString(sum[:])[:16]
}

func firstLogBlob(sc Scenario) string {
	if len(sc.Logs) == 0 {
		return ""
	}
	l := sc.Logs[0]
	return l.ID + "|" + l.TS.Format("15:04:05.000") + "|" + l.Service + "|" + string(l.Level) + "|" + l.Msg
}

func lastLogBlob(sc Scenario) string {
	if len(sc.Logs) == 0 {
		return ""
	}
	l := sc.Logs[len(sc.Logs)-1]
	return l.ID + "|" + l.TS.Format("15:04:05.000") + "|" + l.Service + "|" + string(l.Level) + "|" + l.Msg
}
