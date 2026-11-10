// Package cli is the `itm` command line.
//
// Exit codes are part of the contract (docs/spec/spec.md FR-11):
//
//	0  success
//	1  analysis failed or refused (unreadable input, no data, internal error)
//	2  usage error (bad or missing arguments)
//
// Subcommands are wired progressively; until a task implements one, it reports
// honestly that it is not implemented and exits 1, so a stub can never be
// mistaken for a result by a test or a script.
package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/andrelbuzzo/itm"
)

// Exit codes.
const (
	ExitOK    = 0
	ExitError = 1
	ExitUsage = 2
)

const usage = `itm — Incident Time Machine

Reconstruct what happened during an outage: a timeline, a dependency graph, and
a ranked list of probable root causes with evidence.

Usage:
  itm <command> [flags]

Commands:
  analyze    analyze captured logs and spans
  simulate   generate a seeded fault-injection scenario
  bench      run the benchmark suite over a seed range
  demo       simulate an incident and analyze it end to end

Flags:
  -h, --help       show help for itm or for a command
  -v, --version    print the version

Run "itm <command> --help" for the flags of a command.

Examples:
  itm analyze --logs logs.jsonl --spans spans.jsonl --out out/report
  itm simulate --seed 7 --out out/scenario
  itm bench --seeds 1001-1020 --out BENCHMARK.md
  itm demo --out out/demo
`

// Env carries everything the commands need, so tests can drive the CLI without
// a real process. Nothing here touches globals.
type Env struct {
	Stdout io.Writer
	Stderr io.Writer
}

// Run executes the CLI and returns the process exit code.
func Run(ctx context.Context, args []string, env Env) int {
	if env.Stdout == nil {
		env.Stdout = io.Discard
	}
	if env.Stderr == nil {
		env.Stderr = io.Discard
	}

	if len(args) == 0 {
		fmt.Fprint(env.Stderr, usage)
		return ExitUsage
	}

	switch args[0] {
	case "-h", "--help", "help":
		fmt.Fprint(env.Stdout, usage)
		return ExitOK
	case "-v", "--version", "version":
		fmt.Fprintf(env.Stdout, "itm %s\n", itm.Version)
		return ExitOK
	}

	command, rest := args[0], args[1:]
	switch command {
	case "analyze":
		return runAnalyze(ctx, rest, env)
	case "simulate":
		return runSimulate(ctx, rest, env)
	case "bench":
		return runBench(ctx, rest, env)
	case "demo":
		return runDemo(ctx, rest, env)
	default:
		fmt.Fprintf(env.Stderr, "itm: unknown command %q\n\n", command)
		fmt.Fprint(env.Stderr, usage)
		return ExitUsage
	}
}

// notImplemented is the honest stub: it names the task that will implement the
// command and exits non-zero, so a stub is never mistaken for a result.
func notImplemented(env Env, command, task string) int {
	fmt.Fprintf(env.Stderr, "itm %s: not implemented yet (see docs/spec/tasks.md %s)\n", command, task)
	return ExitError
}

// newFlagSet returns a FlagSet that prints its own usage and never calls
// os.Exit, so Run stays testable.
func newFlagSet(name string, env Env) *flag.FlagSet {
	fs := flag.NewFlagSet("itm "+name, flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	fs.Usage = func() {
		fmt.Fprintf(env.Stdout, "Usage: itm %s [flags]\n\nFlags:\n", name)
		fs.PrintDefaults()
	}
	return fs
}

// parse parses args, mapping -h/--help to a successful help print and any other
// error to exit code 2. The bool result says whether the command should continue.
func parse(fs *flag.FlagSet, args []string, env Env, name string) (int, bool) {
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			fs.Usage()
			return ExitOK, false
		}
		fmt.Fprintf(env.Stderr, "itm %s: %v\n", name, err)
		fs.Usage()
		return ExitUsage, false
	}
	return ExitOK, true
}

func flagWasSet(fs *flag.FlagSet, name string) bool {
	set := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == name {
			set = true
		}
	})
	return set
}

func runAnalyze(ctx context.Context, args []string, env Env) int {
	fs := newFlagSet("analyze", env)
	logs := fs.String("logs", "", "JSONL log file (required)")
	spans := fs.String("spans", "", "JSONL span file (required)")
	out := fs.String("out", "", "output directory (required)")
	incStart := fs.String("incident-start", "", "ISO 8601 window start (disables detection)")
	incEnd := fs.String("incident-end", "", "ISO 8601 window end (disables detection)")
	weights := fs.String("weights", "", "scoring config JSON (default: built-in)")
	if code, ok := parse(fs, args, env, "analyze"); !ok {
		return code
	}
	_ = ctx
	_ = weights
	if *logs == "" || *spans == "" || *out == "" {
		fmt.Fprint(env.Stderr, "itm analyze: --logs, --spans and --out are required\n")
		return ExitUsage
	}
	if *incStart != "" || *incEnd != "" {
		fmt.Fprint(env.Stderr, "itm analyze: an explicit incident window is not implemented yet (T07)\n")
		return ExitError
	}
	return notImplemented(env, "analyze", "T17")
}

func runSimulate(ctx context.Context, args []string, env Env) int {
	fs := newFlagSet("simulate", env)
	seed := fs.Int("seed", 0, "deterministic random seed (required)")
	fault := fs.String("fault", "", "fault type (default: derived from the seed)")
	out := fs.String("out", "", "output directory (required)")
	if code, ok := parse(fs, args, env, "simulate"); !ok {
		return code
	}
	_ = ctx
	_ = fault
	_ = *seed
	if *out == "" || !flagWasSet(fs, "seed") {
		fmt.Fprint(env.Stderr, "itm simulate: --seed and --out are required\n")
		return ExitUsage
	}
	return notImplemented(env, "simulate", "T17")
}

func runBench(ctx context.Context, args []string, env Env) int {
	fs := newFlagSet("bench", env)
	seeds := fs.String("seeds", "1001-1020", "seed range, e.g. 1001-1020")
	out := fs.String("out", "BENCHMARK.md", "output markdown file")
	if code, ok := parse(fs, args, env, "bench"); !ok {
		return code
	}
	_ = ctx
	_ = *seeds
	_ = *out
	return notImplemented(env, "bench", "T17")
}

func runDemo(ctx context.Context, args []string, env Env) int {
	fs := newFlagSet("demo", env)
	out := fs.String("out", "out/demo", "output directory")
	seed := fs.Int("seed", 7, "scenario seed")
	if code, ok := parse(fs, args, env, "demo"); !ok {
		return code
	}
	_ = ctx
	_ = *out
	_ = *seed
	return notImplemented(env, "demo", "T17")
}

// Main is the entry point used by cmd/itm.
func Main(ctx context.Context, args []string) int {
	return Run(ctx, args, Env{Stdout: os.Stdout, Stderr: os.Stderr})
}
