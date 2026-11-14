package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/RocketifyLabs/traceline"
	"github.com/RocketifyLabs/traceline/internal/analyze"
	"github.com/RocketifyLabs/traceline/internal/config"
	"github.com/RocketifyLabs/traceline/internal/ingest"
	"github.com/RocketifyLabs/traceline/internal/replay"
	"github.com/RocketifyLabs/traceline/internal/windows"
)

// runReplayReal implements `traceline replay`: run the same capture twice under
// different option sets and report how the answer moved.
//
// The value is answering "would I get a different cause if I assumed the incident
// started earlier, or trusted latency more than onset?" — on the same data, with
// nothing re-collected.
func runReplayReal(ctx context.Context, args []string, env Env) int {
	fs := newFlagSet("replay", env)
	logsPath := fs.String("logs", "", "JSONL log file (required)")
	spansPath := fs.String("spans", "", "JSONL span file (required)")

	startA := fs.String("incident-start-a", "", "run A: ISO 8601 window start")
	endA := fs.String("incident-end-a", "", "run A: ISO 8601 window end")
	weightsA := fs.String("weights-a", "", "run A: config JSON")

	startB := fs.String("incident-start-b", "", "run B: ISO 8601 window start")
	endB := fs.String("incident-end-b", "", "run B: ISO 8601 window end")
	weightsB := fs.String("weights-b", "", "run B: config JSON")

	markdownPath := fs.String("markdown", "", "also write the comparison as Markdown to this path")

	if code, ok := parse(fs, args, env, "replay"); !ok {
		return code
	}
	if *logsPath == "" || *spansPath == "" {
		fmt.Fprint(env.Stderr, "traceline replay: --logs and --spans are required\n")
		return ExitUsage
	}
	_ = ctx

	cfgA, err := config.Load(*weightsA)
	if err != nil {
		fmt.Fprintf(env.Stderr, "traceline replay: run A: %v\n", err)
		return ExitUsage
	}
	cfgB, err := config.Load(*weightsB)
	if err != nil {
		fmt.Fprintf(env.Stderr, "traceline replay: run B: %v\n", err)
		return ExitUsage
	}

	logRes, err := ingest.Logs(*logsPath)
	if err != nil {
		fmt.Fprintf(env.Stderr, "traceline replay: %v\n", err)
		return ExitError
	}
	spanRes, err := ingest.Spans(*spansPath)
	if err != nil {
		fmt.Fprintf(env.Stderr, "traceline replay: %v\n", err)
		return ExitError
	}
	if len(logRes.Records) == 0 && len(spanRes.Records) == 0 {
		fmt.Fprint(env.Stderr, "traceline replay: no usable records in the input\n")
		return ExitError
	}

	// Both runs see the same records; they differ in the options they apply to
	// them. That is the whole point: one capture, two readings.
	in := analyze.Input{Logs: logRes.Records, Spans: spanRes.Records}

	winA, code := explicitWindow(env, *startA, *endA, cfgA)
	if code != ExitOK {
		return code
	}
	winB, code := explicitWindow(env, *startB, *endB, cfgB)
	if code != ExitOK {
		return code
	}

	// The window belongs to the input, not the options, so each side gets its own
	// input carrying its window. The records are shared.
	inA, inB := in, in
	if winA != nil {
		inA.Window, inA.HasWindow = *winA, true
	}
	if winB != nil {
		inB.Window, inB.HasWindow = *winB, true
	}

	diff := replay.CompareBoth(inA, options(cfgA), inB, options(cfgB))
	diff.A.Label = labelFor("A", *startA, *endA, *weightsA)
	diff.B.Label = labelFor("B", *startB, *endB, *weightsB)

	fmt.Fprint(env.Stdout, diff.Summary())

	if *markdownPath != "" {
		if err := os.WriteFile(*markdownPath, []byte(diff.Markdown()), 0o644); err != nil {
			fmt.Fprintf(env.Stderr, "traceline replay: %v\n", err)
			return ExitError
		}
		fmt.Fprintf(env.Stdout, "\nwrote %s\n", *markdownPath)
	}
	return ExitOK
}

// options turns a loaded config into the analysis options it implies, applying
// an explicit window when one was given.
func options(c config.Settings) analyze.Options {
	o := analyze.Options{
		Window:  c.Windows,
		Anomaly: c.Anomaly,
		Weights: c.Weights,
		Conf:    c.Confidence,
		TopK:    5,
	}
	return o
}

// explicitWindow parses one side's --incident-start/--incident-end pair. It
// returns nil when neither was given (detection stays on).
func explicitWindow(env Env, startISO, endISO string, cfg config.Settings) (*windows.Result, int) {
	if startISO == "" && endISO == "" {
		return nil, ExitOK
	}
	if startISO == "" || endISO == "" {
		fmt.Fprint(env.Stderr, "traceline replay: --incident-start-* and --incident-end-* must both be given\n")
		return nil, ExitUsage
	}
	start, err1 := time.Parse(time.RFC3339, startISO)
	end, err2 := time.Parse(time.RFC3339, endISO)
	if err1 != nil || err2 != nil {
		fmt.Fprint(env.Stderr, "traceline replay: window bounds must be valid ISO 8601\n")
		return nil, ExitUsage
	}
	if !end.After(start) {
		fmt.Fprint(env.Stderr, "traceline replay: window end must be after its start\n")
		return nil, ExitUsage
	}
	w := windows.FromExplicit(start, end, cfg.Windows)
	return &w, ExitOK
}

// labelFor names a run by what differs, so the printed comparison says which
// knob was turned instead of just "A" and "B".
func labelFor(base, start, end, weights string) string {
	var parts []string
	if start != "" || end != "" {
		parts = append(parts, "window "+start+" → "+end)
	}
	if weights != "" {
		parts = append(parts, "weights "+filepath.Base(weights))
	}
	if len(parts) == 0 {
		return base + " (defaults)"
	}
	return base + " (" + joinComma(parts) + ")"
}

func joinComma(parts []string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += ", "
		}
		out += p
	}
	return out
}

var _ = traceline.Version
