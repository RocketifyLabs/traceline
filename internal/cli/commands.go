package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/RocketifyLabs/traceline"
	"github.com/RocketifyLabs/traceline/internal/analyze"
	"github.com/RocketifyLabs/traceline/internal/bench"
	"github.com/RocketifyLabs/traceline/internal/ingest"
	"github.com/RocketifyLabs/traceline/internal/model"
	"github.com/RocketifyLabs/traceline/internal/report"
	"github.com/RocketifyLabs/traceline/internal/sim"
	"github.com/RocketifyLabs/traceline/internal/windows"
)

// runAnalyze is the real implementation of `traceline analyze`.
func runAnalyzeReal(ctx context.Context, args []string, env Env) int {
	fs := newFlagSet("analyze", env)
	logsPath := fs.String("logs", "", "JSONL log file (required)")
	spansPath := fs.String("spans", "", "JSONL span file (required)")
	outDir := fs.String("out", "", "output directory (required)")
	incStart := fs.String("incident-start", "", "ISO 8601 window start (disables detection)")
	incEnd := fs.String("incident-end", "", "ISO 8601 window end (disables detection)")
	weights := fs.String("weights", "", "scoring config JSON (default: built-in)")
	if code, ok := parse(fs, args, env, "analyze"); !ok {
		return code
	}
	if *logsPath == "" || *spansPath == "" || *outDir == "" {
		fmt.Fprint(env.Stderr, "traceline analyze: --logs, --spans and --out are required\n")
		return ExitUsage
	}
	_ = ctx
	_ = weights

	logRes, err := ingest.Logs(*logsPath)
	if err != nil {
		fmt.Fprintf(env.Stderr, "traceline analyze: %v\n", err)
		return ExitError
	}
	spanRes, err := ingest.Spans(*spansPath)
	if err != nil {
		fmt.Fprintf(env.Stderr, "traceline analyze: %v\n", err)
		return ExitError
	}
	if len(logRes.Records) == 0 && len(spanRes.Records) == 0 {
		fmt.Fprint(env.Stderr, "traceline analyze: no usable records in the input\n")
		return ExitError
	}

	in := analyze.Input{Logs: logRes.Records, Spans: spanRes.Records}
	if *incStart != "" || *incEnd != "" {
		start, err1 := parseISO(*incStart)
		end, err2 := parseISO(*incEnd)
		if err1 != nil || err2 != nil {
			fmt.Fprint(env.Stderr, "traceline analyze: --incident-start and --incident-end must both be valid ISO 8601\n")
			return ExitUsage
		}
		cfg := windows.DefaultConfig()
		in.Window = windows.FromExplicit(start, end, cfg)
		in.HasWindow = true
	}

	rep := analyze.Run(in, analyze.DefaultOptions())
	// Merge the ingest quality into the report: the analysis cannot know how many
	// lines were skipped before it saw the data.
	rep.Quality = mergeQuality(rep.Quality, logRes.Quality, spanRes.Quality)

	if err := report.WriteAll(*outDir, rep, report.Options{
		Title:   "Incident report",
		Source:  filepath.Base(*logsPath) + " + " + filepath.Base(*spansPath),
		Version: traceline.Version,
	}); err != nil {
		fmt.Fprintf(env.Stderr, "traceline analyze: %v\n", err)
		return ExitError
	}

	printVerdict(env, rep, *outDir)
	return ExitOK
}

// runSimulateReal writes a generated scenario to disk as JSONL.
func runSimulateReal(ctx context.Context, args []string, env Env) int {
	fs := newFlagSet("simulate", env)
	seed := fs.Int("seed", 0, "deterministic random seed (required)")
	fault := fs.String("fault", "", "fault type (default: derived from the seed)")
	outDir := fs.String("out", "", "output directory (required)")
	if code, ok := parse(fs, args, env, "simulate"); !ok {
		return code
	}
	_ = ctx
	if *outDir == "" || !flagWasSet(fs, "seed") {
		fmt.Fprint(env.Stderr, "traceline simulate: --seed and --out are required\n")
		return ExitUsage
	}

	sc := sim.Generate(int64(*seed), *fault, sim.DefaultConfig())
	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		fmt.Fprintf(env.Stderr, "traceline simulate: %v\n", err)
		return ExitError
	}
	if err := writeJSONL(filepath.Join(*outDir, "logs.jsonl"), logLines(sc.Logs)); err != nil {
		fmt.Fprintf(env.Stderr, "traceline simulate: %v\n", err)
		return ExitError
	}
	if err := writeJSONL(filepath.Join(*outDir, "spans.jsonl"), spanLines(sc.Spans)); err != nil {
		fmt.Fprintf(env.Stderr, "traceline simulate: %v\n", err)
		return ExitError
	}
	meta := fmt.Sprintf("seed: %d\nfault: %s\ntrue_cause: %s\nfault_start: %s\nfault_end: %s\nlogs: %d\nspans: %d\n",
		sc.Seed, sc.Fault, sc.TrueCause,
		sc.FaultStart.Format("2006-01-02T15:04:05Z07:00"),
		sc.FaultEnd.Format("2006-01-02T15:04:05Z07:00"),
		len(sc.Logs), len(sc.Spans))
	if err := os.WriteFile(filepath.Join(*outDir, "scenario.yaml"), []byte(meta), 0o644); err != nil {
		fmt.Fprintf(env.Stderr, "traceline simulate: %v\n", err)
		return ExitError
	}

	fmt.Fprintf(env.Stdout, "%s\n", sc)
	fmt.Fprintf(env.Stdout, "wrote %s\n", *outDir)
	return ExitOK
}

// runBenchReal runs the benchmark and writes BENCHMARK.md.
func runBenchReal(ctx context.Context, args []string, env Env) int {
	fs := newFlagSet("bench", env)
	seeds := fs.String("seeds", "1001-1020", "seed range, e.g. 1001-1020")
	outPath := fs.String("out", "BENCHMARK.md", "output markdown file")
	if code, ok := parse(fs, args, env, "bench"); !ok {
		return code
	}
	_ = ctx

	r, err := bench.ParseSeedRange(*seeds)
	if err != nil {
		fmt.Fprintf(env.Stderr, "traceline bench: %v\n", err)
		return ExitUsage
	}
	if r == bench.HeldOutSeeds {
		fmt.Fprintf(env.Stdout, "running the HELD-OUT range %d-%d (never used for tuning)\n", r.From, r.To)
	} else {
		fmt.Fprintf(env.Stdout, "running seeds %d-%d\n", r.From, r.To)
	}

	res := bench.Run(r, sim.DefaultConfig(), analyze.DefaultOptions())
	md := res.Markdown(fmt.Sprintf("`traceline bench --seeds %d-%d`", r.From, r.To))
	if err := os.WriteFile(*outPath, []byte(md), 0o644); err != nil {
		fmt.Fprintf(env.Stderr, "traceline bench: %v\n", err)
		return ExitError
	}

	fmt.Fprintf(env.Stdout, "top-1 %.1f%%  top-3 %.1f%%  mean TTD %.1fs  misses %d\n",
		res.Top1*100, res.Top3*100, res.MeanTTD, res.Misses)
	fmt.Fprintf(env.Stdout, "wrote %s\n", *outPath)
	return ExitOK
}

// runDemoReal simulates an incident, analyzes it, and writes the report.
func runDemoReal(ctx context.Context, args []string, env Env) int {
	fs := newFlagSet("demo", env)
	outDir := fs.String("out", "out/demo", "output directory")
	seed := fs.Int("seed", 7, "scenario seed")
	if code, ok := parse(fs, args, env, "demo"); !ok {
		return code
	}
	_ = ctx

	sc := sim.Generate(int64(*seed), "", sim.DefaultConfig())
	rep := analyze.RunScenario(analyze.ScenarioInput{
		Logs: sc.Logs, Spans: sc.Spans,
		FaultStart: sc.FaultStart, FaultEnd: sc.FaultEnd,
	}, analyze.DefaultOptions())

	if err := report.WriteAll(*outDir, rep, report.Options{
		Title:   fmt.Sprintf("Incident report — seed %d", sc.Seed),
		Source:  fmt.Sprintf("simulated scenario seed=%d fault=%s", sc.Seed, sc.Fault),
		Version: traceline.Version,
	}); err != nil {
		fmt.Fprintf(env.Stderr, "traceline demo: %v\n", err)
		return ExitError
	}

	fmt.Fprintf(env.Stdout, "scenario: %s\n", sc)
	fmt.Fprintf(env.Stdout, "true cause: %s\n", sc.TrueCause)
	if len(rep.Candidates) > 0 {
		top := rep.Candidates[0]
		verdict := "HIT"
		if top.Service != sc.TrueCause {
			verdict = "MISS"
		}
		fmt.Fprintf(env.Stdout, "ranked #1: %s (score %.2f, %s) — %s\n",
			top.Service, top.Score, top.Confidence, verdict)
	} else {
		fmt.Fprintf(env.Stdout, "ranked: insufficient evidence\n")
	}
	fmt.Fprintf(env.Stdout, "wrote %s\n", *outDir)
	return ExitOK
}

// printVerdict writes the one-line answer a script or a human wants.
func printVerdict(env Env, rep model.Report, outDir string) {
	if rep.Insufficient {
		fmt.Fprintf(env.Stdout, "insufficient evidence: no anomaly passed the threshold\n")
	} else if len(rep.Candidates) > 0 {
		top := rep.Candidates[0]
		fmt.Fprintf(env.Stdout, "root cause: %s (score %.2f, %s confidence)\n",
			top.Service, top.Score, top.Confidence)
	}
	fmt.Fprintf(env.Stdout, "wrote %s\n", outDir)
}

// mergeQuality combines the ingest tallies with the analysis's own counts.
func mergeQuality(base, logs, spans model.DataQuality) model.DataQuality {
	out := base
	out.LogLinesRead = logs.LogLinesRead
	out.LogsAccepted = logs.LogsAccepted
	out.SpanLinesRead = spans.SpanLinesRead
	out.SpansAccepted = spans.SpansAccepted
	out.Skipped = logs.Skipped + spans.Skipped
	out.ClockSkewLines = logs.ClockSkewLines
	out.OrphanSpans = spans.OrphanSpans
	out.RootSpans = spans.RootSpans
	out.SkipReasons = map[string]int{}
	for reason, n := range logs.SkipReasons {
		out.SkipReasons[reason] += n
	}
	for reason, n := range spans.SkipReasons {
		out.SkipReasons[reason] += n
	}
	return out
}

func parseISO(s string) (t time.Time, err error) {
	return time.Parse(time.RFC3339, s)
}

var _ = strconv.Itoa
