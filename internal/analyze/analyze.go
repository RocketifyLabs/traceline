// Package analyze wires the whole analysis together: ingest → windows →
// anomaly → graph → timeline → rca.
//
// It exists so the CLI, the benchmark and the demo all run exactly the same
// pipeline. A second implementation for the demo would eventually disagree with
// the one being benchmarked, and the benchmark would stop meaning anything.
package analyze

import (
	"time"

	"github.com/andrelbuzzo/itm/internal/anomaly"
	"github.com/andrelbuzzo/itm/internal/graph"
	"github.com/andrelbuzzo/itm/internal/model"
	"github.com/andrelbuzzo/itm/internal/rca"
	"github.com/andrelbuzzo/itm/internal/timeline"
	"github.com/andrelbuzzo/itm/internal/windows"
)

// Input is either a file-based analysis or a simulated scenario.
type Input struct {
	Logs   []model.LogLine
	Spans  []model.Span
	Window windows.Result // optional: explicit window
	// HasWindow is true when Window came from the user and must not be detected.
	HasWindow bool
}

// Options tunes the analysis.
type Options struct {
	Window  windows.Config
	Anomaly anomaly.Config
	Weights rca.Weights
	Conf    rca.Confidence
	TopK    int
}

// DefaultOptions returns the documented defaults.
func DefaultOptions() Options {
	return Options{
		Window:  windows.DefaultConfig(),
		Anomaly: anomaly.DefaultConfig(),
		Weights: rca.DefaultWeights(),
		Conf:    rca.DefaultConfidence(),
		TopK:    5,
	}
}

// Run executes the full pipeline and returns one Report.
func Run(in Input, opts Options) model.Report {
	if opts.Window.BinSize == 0 {
		opts = DefaultOptions()
	}

	rep := model.Report{}

	// 1. Window: explicit or detected.
	var win windows.Result
	if in.HasWindow {
		win = in.Window
	} else {
		win = windows.Detect(in.Logs, opts.Window)
	}
	rep.BaselineStart = win.BaselineStart
	rep.BaselineEnd = win.BaselineEnd
	rep.IncidentStart = win.IncidentStart
	rep.IncidentEnd = win.IncidentEnd

	// 2. Split the telemetry.
	baseLogs, incLogs := windows.Split(in.Logs, win)
	baseSpans, incSpans := windows.SplitSpans(in.Spans, win)

	// 3. Graph over the whole capture: a dependency that only appears in the
	//    baseline is still a dependency, and dropping it would hide the path from
	//    symptom to cause.
	//
	//    Built once and reused for the ranking. Building it a second time was
	//    both wasteful and superlinear in wall-clock terms: every span was walked
	//    and every edge recomputed for the same answer.
	g := graph.Build(in.Spans)
	rep.Edges = g.Edges

	// 4. Anomalies between the windows.
	rep.Anomalies = anomaly.Detect(anomaly.Input{
		BaselineLogs:  baseLogs,
		IncidentLogs:  incLogs,
		BaselineSpans: baseSpans,
		IncidentSpans: incSpans,
		IncidentStart: win.IncidentStart,
	}, opts.Anomaly)

	// 5. Timeline.
	rep.Timeline = timeline.Build(rep.Anomalies, incLogs, win.IncidentStart, win.IncidentEnd)

	// 6. Ranking, including the insufficient-evidence path.
	candidates, insufficient := rca.Rank(rca.Input{
		Anomalies:     rep.Anomalies,
		Graph:         g,
		IncidentStart: win.IncidentStart,
		IncidentEnd:   win.IncidentEnd,
		TopK:          opts.TopK,
	}, opts.Weights, opts.Conf)
	rep.Candidates = candidates
	rep.Insufficient = insufficient

	return rep
}

// RunScenario analyzes a simulated scenario using its own recorded fault start
// as the window, which makes benchmark scoring a pure comparison of ranking
// against ground truth rather than a test of detection as well.
func RunScenario(sc ScenarioInput, opts Options) model.Report {
	win := windows.FromExplicit(sc.FaultStart, sc.FaultEnd, opts.Window)
	return Run(Input{
		Logs:      sc.Logs,
		Spans:     sc.Spans,
		Window:    win,
		HasWindow: true,
	}, opts)
}

// ScenarioInput is the subset of a simulated scenario the analysis needs, kept
// as its own type so this package does not depend on internal/sim.
type ScenarioInput struct {
	Logs       []model.LogLine
	Spans      []model.Span
	FaultStart time.Time
	FaultEnd   time.Time
}
