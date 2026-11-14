package traceline

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/RocketifyLabs/traceline/internal/analyze"
	"github.com/RocketifyLabs/traceline/internal/ingest"
	"github.com/RocketifyLabs/traceline/internal/model"
	"github.com/RocketifyLabs/traceline/internal/report"
	"github.com/RocketifyLabs/traceline/internal/sim"
)

// This file is the public library API: the surface a GUI or another program uses
// so it can call the same pipeline the CLI and the benchmark call, instead of
// reimplementing one that would eventually disagree with them.
//
// It deliberately does not expose the internal model types. A caller gets plain
// structs with the fields worth showing on a screen, which keeps `internal/`
// free to change shape without breaking the GUI.

// Report is the public view of one analysis.
type Report struct {
	IncidentStart time.Time
	IncidentEnd   time.Time
	Candidates    []Candidate
	Timeline      []Event
	// Insufficient is true when nothing cleared the anomaly threshold. That is a
	// result, not an error: the tool is saying the capture does not justify a
	// confident answer.
	Insufficient bool
	// LinesRead and Skipped describe the input, so a caller can show what was
	// ignored instead of pretending the input was clean.
	LinesRead int
	Skipped   int
	// SkipReasons is the rejection tally, keyed by reason.
	SkipReasons map[string]int
	// TrueCause is set only for a simulated scenario, where the ground truth is
	// known. Empty for a real capture.
	TrueCause string
}

// Candidate is one scored root-cause hypothesis, with the breakdown that makes
// the score auditable rather than something to be trusted.
type Candidate struct {
	Service            string
	Score              float64
	Confidence         string
	Onset              float64
	Strength           float64
	ExplainsDownstream float64
	NotExplained       float64
}

// Event is one entry of the reconstructed timeline.
type Event struct {
	At      time.Time
	Kind    string
	Service string
	Detail  string
}

// AnalyzeFiles reads a capture, runs the full pipeline, and writes every report
// artifact into outDir. logsPath and spansPath may be empty; at least one is
// required. It returns an error only when the input cannot be read at all or the
// output cannot be written — "insufficient evidence" comes back as a Report with
// Insufficient set, exactly as the CLI reports it.
func AnalyzeFiles(logsPath, spansPath, outDir, title string) (Report, error) {
	if logsPath == "" && spansPath == "" {
		return Report{}, fmt.Errorf("no input: pass a logs file, a spans file, or both")
	}

	var logRes ingest.Result[model.LogLine]
	var spanRes ingest.Result[model.Span]
	var err error
	if logsPath != "" {
		if logRes, err = ingest.Logs(logsPath); err != nil {
			return Report{}, fmt.Errorf("logs %s: %w", filepath.Base(logsPath), err)
		}
	}
	if spansPath != "" {
		if spanRes, err = ingest.Spans(spansPath); err != nil {
			return Report{}, fmt.Errorf("spans %s: %w", filepath.Base(spansPath), err)
		}
	}
	if len(logRes.Records) == 0 && len(spanRes.Records) == 0 {
		return Report{}, fmt.Errorf("no usable records: every line was skipped, or the files are empty")
	}

	rep := analyze.Run(
		analyze.Input{Logs: logRes.Records, Spans: spanRes.Records},
		analyze.DefaultOptions(),
	)

	if title == "" {
		title = "Traceline incident report"
	}
	if err := report.WriteAll(outDir, rep, report.Options{
		Title:   title,
		Source:  sourceName(logsPath, spansPath),
		Version: Version,
	}); err != nil {
		return Report{}, err
	}
	return export(rep, mergeQuality(logRes.Quality, spanRes.Quality)), nil
}

// AnalyzeScenario generates a seeded incident with the project's own simulator
// and analyzes it. It exists so a GUI can show a real, complete run on a machine
// that has no capture lying around.
func AnalyzeScenario(seed int64, outDir string) (Report, error) {
	sc := sim.Generate(seed, "", sim.DefaultConfig())
	rep := analyze.RunScenario(analyze.ScenarioInput{
		Logs:       sc.Logs,
		Spans:      sc.Spans,
		FaultStart: sc.FaultStart,
		FaultEnd:   sc.FaultEnd,
	}, analyze.DefaultOptions())

	if err := report.WriteAll(outDir, rep, report.Options{
		Title:   fmt.Sprintf("Incident report — seed %d", sc.Seed),
		Source:  fmt.Sprintf("simulated scenario seed=%d fault=%s", sc.Seed, sc.Fault),
		Version: Version,
	}); err != nil {
		return Report{}, err
	}

	out := export(rep, mergeQuality(model.DataQuality{}, model.DataQuality{}))
	out.TrueCause = sc.TrueCause
	// A simulated scenario carries its own record count; the ingest counters are
	// not involved because no file was parsed.
	out.LinesRead = len(sc.Logs) + len(sc.Spans)
	out.Skipped = 0
	out.SkipReasons = map[string]int{}
	return out, nil
}

// ReportPath is the HTML artifact a caller should open after a successful run.
func ReportPath(outDir string) string { return filepath.Join(outDir, report.HTMLName) }

// export converts the internal report into the public one.
func export(rep model.Report, q model.DataQuality) Report {
	out := Report{
		IncidentStart: rep.IncidentStart,
		IncidentEnd:   rep.IncidentEnd,
		Insufficient:  rep.Insufficient,
		LinesRead:     q.LogLinesRead + q.SpanLinesRead,
		Skipped:       q.Skipped,
		SkipReasons:   q.SkipReasons,
	}
	if out.SkipReasons == nil {
		out.SkipReasons = map[string]int{}
	}

	for _, c := range rep.Candidates {
		out.Candidates = append(out.Candidates, Candidate{
			Service:            c.Service,
			Score:              c.Score,
			Confidence:         c.Confidence,
			Onset:              c.Breakdown.Onset,
			Strength:           c.Breakdown.Strength,
			ExplainsDownstream: c.Breakdown.ExplainsDownstream,
			NotExplained:       c.Breakdown.NotExplained,
		})
	}
	for _, e := range rep.Timeline {
		out.Timeline = append(out.Timeline, Event{
			At: e.At, Kind: e.Kind, Service: e.Service, Detail: e.Detail,
		})
	}
	return out
}

// mergeQuality mirrors internal/cli: skip counters are additive across the two
// files, the rest come from whichever file has them.
func mergeQuality(logs, spans model.DataQuality) model.DataQuality {
	out := model.DataQuality{
		LogLinesRead:   logs.LogLinesRead,
		LogsAccepted:   logs.LogsAccepted,
		SpanLinesRead:  spans.SpanLinesRead,
		SpansAccepted:  spans.SpansAccepted,
		Skipped:        logs.Skipped + spans.Skipped,
		ClockSkewLines: logs.ClockSkewLines,
		OrphanSpans:    spans.OrphanSpans,
		RootSpans:      spans.RootSpans,
		SkipReasons:    map[string]int{},
	}
	for reason, n := range logs.SkipReasons {
		out.SkipReasons[reason] += n
	}
	for reason, n := range spans.SkipReasons {
		out.SkipReasons[reason] += n
	}
	return out
}

func sourceName(logsPath, spansPath string) string {
	switch {
	case logsPath != "" && spansPath != "":
		return filepath.Base(logsPath) + " + " + filepath.Base(spansPath)
	case logsPath != "":
		return filepath.Base(logsPath)
	default:
		return filepath.Base(spansPath)
	}
}
