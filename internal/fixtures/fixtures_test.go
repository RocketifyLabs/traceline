// Package fixtures_test runs the hand-checked captures in fixtures/ and asserts the
// documented outcomes.
//
// Each fixture exists to prove one behaviour, and the README states the expected
// result for each. Without this file those expectations would be prose that drifts
// silently away from what the code actually does.
package fixtures

import (
	"path/filepath"
	"testing"

	"github.com/RocketifyLabs/traceline/internal/analyze"
	"github.com/RocketifyLabs/traceline/internal/ingest"
	"github.com/RocketifyLabs/traceline/internal/model"
)

// fixturesDir is the repository's fixtures directory, two levels above this
// package's directory (go test runs with the package dir as the working directory).
const fixturesDir = "../../fixtures"

// load reads a fixture pair from the repository root.
func load(t *testing.T, name string) analyze.Input {
	t.Helper()
	dir := filepath.Join(fixturesDir, name)

	logs, err := ingest.Logs(filepath.Join(dir, "logs.jsonl"))
	if err != nil {
		t.Fatalf("%s: read logs: %v", name, err)
	}
	spans, err := ingest.Spans(filepath.Join(dir, "spans.jsonl"))
	if err != nil {
		t.Fatalf("%s: read spans: %v", name, err)
	}
	return analyze.Input{Logs: logs.Records, Spans: spans.Records}
}

// pool-exhaustion: the tool must name db-proxy, the service where the pool ran
// dry, ahead of the two services that merely suffered from it.
func TestPoolExhaustionFixture(t *testing.T) {
	rep := analyze.Run(load(t, "pool-exhaustion"), analyze.DefaultOptions())

	if rep.Insufficient {
		t.Fatal("reported insufficient evidence for a capture with an obvious incident")
	}
	if len(rep.Candidates) == 0 {
		t.Fatal("no candidates")
	}

	top := rep.Candidates[0]
	if top.Service != "db-proxy" {
		t.Fatalf("top candidate is %q, want db-proxy (all: %v)", top.Service, names(rep))
	}
	if top.Confidence != "high" {
		t.Errorf("confidence is %q, want high for a clear, fully-explained cause", top.Confidence)
	}
	// A perfect cause: nothing explains it, and it explains everything else.
	if top.Breakdown.NotExplained != 1 {
		t.Errorf("NotExplained = %v, want 1 (db-proxy has no anomalous dependency)",
			top.Breakdown.NotExplained)
	}
	if top.Breakdown.ExplainsDownstream == 0 {
		t.Errorf("ExplainsDownstream = %v, want > 0 (api and web depend on it)",
			top.Breakdown.ExplainsDownstream)
	}

	// The detected window must land on the real fault, not at the start of the file.
	// This asserted zero when the baseline was computed over all bins.
	incidentStart := rep.IncidentStart
	baselineWidth := rep.BaselineEnd.Sub(rep.BaselineStart)
	if baselineWidth <= 0 {
		t.Errorf("baseline has no width: %v .. %v", rep.BaselineStart, rep.BaselineEnd)
	}
	if baselineWidth < 30*1e9 {
		t.Errorf("baseline is only %v wide; the first 60s of the capture are clean", baselineWidth)
	}
	// The fault starts at 10:01:00; the window must be within 10s of that.
	if h, m := incidentStart.Hour(), incidentStart.Minute(); h != 10 || m != 1 {
		t.Errorf("incident start is %v, want around 10:01:00", incidentStart)
	}
}

// steady-state: nothing is wrong, and the tool must say so.
func TestSteadyStateFixture(t *testing.T) {
	rep := analyze.Run(load(t, "steady-state"), analyze.DefaultOptions())

	if !rep.Insufficient {
		t.Fatalf("named a root cause for a healthy capture: %v", names(rep))
	}
	if len(rep.Candidates) != 0 {
		t.Errorf("produced candidates for a healthy capture: %v", names(rep))
	}
}

// messy-input: four bad lines, one of each kind, must be counted separately and
// must not prevent the analysis.
func TestMessyInputFixture(t *testing.T) {
	dir := filepath.Join(fixturesDir, "messy-input")
	logs, err := ingest.Logs(filepath.Join(dir, "logs.jsonl"))
	if err != nil {
		t.Fatalf("dirty input must not be fatal: %v", err)
	}
	spans, err := ingest.Spans(filepath.Join(dir, "spans.jsonl"))
	if err != nil {
		t.Fatalf("dirty spans must not be fatal: %v", err)
	}

	if got := logs.Quality.LogLinesRead; got != 37 {
		t.Errorf("log lines read = %d, want 37", got)
	}
	if got := logs.Quality.Skipped; got != 4 {
		t.Errorf("skipped = %d, want 4", got)
	}

	// Each rejection path must be distinguishable, not lumped into one number.
	for _, reason := range []string{
		"invalid_json",
		"missing_required_field",
		"unparseable_timestamp",
		"empty_line",
	} {
		if logs.Quality.SkipReasons[reason] == 0 {
			t.Errorf("skip reason %q not recorded: %v", reason, logs.Quality.SkipReasons)
		}
	}

	// And the incident must still be found through the noise.
	rep := analyze.Run(analyze.Input{Logs: logs.Records, Spans: spans.Records}, analyze.DefaultOptions())
	if rep.Insufficient {
		t.Fatal("dirty but analyzable input reported insufficient evidence")
	}
	if len(rep.Candidates) == 0 || rep.Candidates[0].Service != "api" {
		t.Errorf("top candidate = %v, want api", names(rep))
	}
}

// Every fixture must load and analyze without panicking, which is the minimum a
// reviewer will try first.
func TestAllFixturesLoad(t *testing.T) {
	for _, name := range []string{"pool-exhaustion", "steady-state", "messy-input"} {
		t.Run(name, func(t *testing.T) {
			in := load(t, name)
			if len(in.Logs) == 0 {
				t.Error("no log records")
			}
			if len(in.Spans) == 0 {
				t.Error("no span records")
			}
			// Must not panic.
			_ = analyze.Run(in, analyze.DefaultOptions())
		})
	}
}

// names lists the candidate services in rank order, for failure messages.
func names(rep model.Report) []string {
	out := make([]string, 0, len(rep.Candidates))
	for _, c := range rep.Candidates {
		out = append(out, c.Service)
	}
	return out
}
