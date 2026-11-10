package sim

import (
	"testing"
)

// The distractor must be a DIFFERENT service from the real cause, and the real
// cause must actually receive the fault.
//
// An earlier version picked the loud service as the target and then chose the
// "true cause" from the leftovers, so the labelled cause got no fault at all and
// only inherited collateral damage. The scenario had no correct answer, and every
// ranking was scored against a wrong label.
func TestNoisyNeighbourSeparatesNoisyFromCause(t *testing.T) {
	for seed := int64(1); seed <= 30; seed++ {
		sc := Generate(seed, FaultNoisyNeighbour, DefaultConfig())

		if sc.TrueCause == "" {
			t.Fatalf("seed %d: no true cause", seed)
		}
		// The true cause must produce real errors, not just noise.
		var causeErrors int
		for _, l := range sc.Logs {
			if l.Service == sc.TrueCause && l.Level.IsError() {
				causeErrors++
			}
		}
		if causeErrors == 0 {
			t.Fatalf("seed %d: true cause %q emits no errors, so it cannot be found",
				seed, sc.TrueCause)
		}
	}
}

// The real cause must be measurably worse than the healthy services, or the
// scenario is not solvable by any method.
func TestNoisyNeighbourHasAnIdentifiableCause(t *testing.T) {
	for seed := int64(1); seed <= 30; seed++ {
		sc := Generate(seed, FaultNoisyNeighbour, DefaultConfig())

		type counts struct{ total, errs int }
		per := map[string]*counts{}
		for _, l := range sc.Logs {
			c := per[l.Service]
			if c == nil {
				c = &counts{}
				per[l.Service] = c
			}
			c.total++
			if l.Level.IsError() {
				c.errs++
			}
		}

		cause := per[sc.TrueCause]
		if cause == nil || cause.total == 0 {
			t.Fatalf("seed %d: no telemetry for the true cause %q", seed, sc.TrueCause)
		}
		causeRate := float64(cause.errs) / float64(cause.total)

		// Every other service must be less error-prone than the cause. If any is
		// worse, the labelled cause is not the worst offender and the scenario
		// gives the analysis no way to identify it.
		for svc, c := range per {
			if svc == sc.TrueCause || c.total == 0 {
				continue
			}
			rate := float64(c.errs) / float64(c.total)
			if rate >= causeRate {
				t.Fatalf("seed %d: service %q (%.2f) errors at least as much as the "+
					"labelled cause %q (%.2f)",
					seed, svc, rate, sc.TrueCause, causeRate)
			}
		}
	}
}

// Background noise must actually be present, or the benchmark is measuring a
// scenario no real system produces.
func TestBackgroundNoiseIsEmitted(t *testing.T) {
	sc := Generate(11, FaultLatency, DefaultConfig())

	// Services other than the cause must have some errors before the fault.
	before := map[string]int{}
	for _, l := range sc.Logs {
		if l.TS.Before(sc.FaultStart) && l.Level.IsError() && l.Service != sc.TrueCause {
			before[l.Service]++
		}
	}
	if len(before) == 0 {
		t.Fatal("no background errors on healthy services before the fault")
	}
}

// EasyConfig must produce a quieter scenario than the default one, so tests that
// need a clear signal can opt out of the realistic noise.
//
// It does not produce zero background errors: the fault profile itself has a
// natural error base (1%), which every service inherits even when healthy. That
// is deliberate — a perfectly clean capture is not realistic either.
func TestEasyConfigIsQuieterThanDefault(t *testing.T) {
	noisy := Generate(11, FaultLatency, DefaultConfig())
	clean := Generate(11, FaultLatency, EasyConfig())

	count := func(sc Scenario) int {
		n := 0
		for _, l := range sc.Logs {
			if l.TS.Before(sc.FaultStart) && l.Level.IsError() && l.Service != sc.TrueCause {
				n++
			}
		}
		return n
	}

	noisyN, cleanN := count(noisy), count(clean)
	if cleanN >= noisyN {
		t.Fatalf("EasyConfig is not quieter: %d background errors vs %d with noise",
			cleanN, noisyN)
	}
	// The difference must be substantial, not incidental.
	if noisyN < cleanN*3 {
		t.Fatalf("background noise is barely visible: %d vs %d", noisyN, cleanN)
	}
}

// Noise must not break determinism.
func TestNoiseKeepsDeterminism(t *testing.T) {
	for _, cfg := range []Config{DefaultConfig(), EasyConfig()} {
		a := Generate(123, FaultTimeoutCascade, cfg)
		b := Generate(123, FaultTimeoutCascade, cfg)
		if hash(a) != hash(b) {
			t.Fatalf("scenario with noise is not deterministic (noise=%v)", cfg.BackgroundNoise)
		}
	}
}

// The baseline burst must exist and must be in the baseline, not during the fault.
func TestNoiseBurstIsInTheBaseline(t *testing.T) {
	sc := Generate(21, FaultLatency, DefaultConfig())

	var burst []int
	for i, l := range sc.Logs {
		if l.Msg == "transient upstream reset" {
			burst = append(burst, i)
			if !l.TS.Before(sc.FaultStart) {
				t.Fatalf("burst log at %v is not before the fault start %v", l.TS, sc.FaultStart)
			}
		}
	}
	if len(burst) == 0 {
		t.Fatal("no noise burst emitted with NoiseBurst enabled")
	}
}
