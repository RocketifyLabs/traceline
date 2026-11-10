package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/andrelbuzzo/itm"
)

// run is the helper every test uses: it drives Run with captured writers.
func run(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errb bytes.Buffer
	code = Run(context.Background(), args, Env{Stdout: &out, Stderr: &errb})
	return code, out.String(), errb.String()
}

func TestNoArgsPrintsUsageAndExits2(t *testing.T) {
	code, _, stderr := run(t)
	if code != ExitUsage {
		t.Fatalf("exit = %d, want %d", code, ExitUsage)
	}
	if !strings.Contains(stderr, "Usage:") {
		t.Fatalf("stderr missing usage: %q", stderr)
	}
}

func TestVersionExits0(t *testing.T) {
	for _, flag := range []string{"-v", "--version", "version"} {
		code, stdout, _ := run(t, flag)
		if code != ExitOK {
			t.Fatalf("%s: exit = %d, want 0", flag, code)
		}
		if !strings.Contains(stdout, itm.Version) {
			t.Fatalf("%s: stdout %q missing version %q", flag, stdout, itm.Version)
		}
	}
}

func TestHelpExits0AndListsEveryCommand(t *testing.T) {
	for _, flag := range []string{"-h", "--help", "help"} {
		code, stdout, _ := run(t, flag)
		if code != ExitOK {
			t.Fatalf("%s: exit = %d, want 0", flag, code)
		}
		for _, command := range []string{"analyze", "simulate", "bench", "demo"} {
			if !strings.Contains(stdout, command) {
				t.Errorf("%s: help output missing command %q", flag, command)
			}
		}
	}
}

func TestEachCommandHasHelp(t *testing.T) {
	for _, command := range []string{"analyze", "simulate", "bench", "demo"} {
		code, stdout, _ := run(t, command, "--help")
		if code != ExitOK {
			t.Errorf("%s --help: exit = %d, want 0", command, code)
		}
		if !strings.Contains(stdout, "Usage: itm "+command) {
			t.Errorf("%s --help: stdout %q missing usage line", command, stdout)
		}
	}
}

func TestUnknownCommandExits2(t *testing.T) {
	code, _, stderr := run(t, "explode")
	if code != ExitUsage {
		t.Fatalf("exit = %d, want %d", code, ExitUsage)
	}
	if !strings.Contains(stderr, "unknown command") {
		t.Fatalf("stderr = %q, want mention of unknown command", stderr)
	}
}

// Stubs must fail loudly: a stub returning 0 is indistinguishable from a
// working feature in a smoke test.
func TestUnimplementedCommandsExit1(t *testing.T) {
	cases := [][]string{
		{"analyze", "--logs", "l.jsonl", "--spans", "s.jsonl", "--out", "out"},
		{"simulate", "--seed", "1", "--out", "out"},
		{"bench"},
		{"demo"},
	}
	for _, args := range cases {
		code, _, stderr := run(t, args...)
		if code != ExitError {
			t.Errorf("%v: exit = %d, want %d", args, code, ExitError)
		}
		if !strings.Contains(stderr, "not implemented yet") {
			t.Errorf("%v: stderr = %q, want 'not implemented yet'", args, stderr)
		}
		if !strings.Contains(stderr, "T1") && !strings.Contains(stderr, "T0") {
			t.Errorf("%v: stderr should name the task that implements it: %q", args, stderr)
		}
	}
}

func TestAnalyzeMissingRequiredFlagsExits2(t *testing.T) {
	code, _, stderr := run(t, "analyze", "--logs", "l.jsonl")
	if code != ExitUsage {
		t.Fatalf("exit = %d, want %d", code, ExitUsage)
	}
	if !strings.Contains(stderr, "required") {
		t.Fatalf("stderr = %q, want a required-flags message", stderr)
	}
}

func TestSimulateRequiresSeedEvenWhenZero(t *testing.T) {
	// --seed 0 must be accepted: zero is a legitimate seed.
	code, _, _ := run(t, "simulate", "--seed", "0", "--out", "out")
	if code != ExitError {
		t.Fatalf("--seed 0: exit = %d, want %d (accepted, then stub)", code, ExitError)
	}
	// Omitting --seed entirely is a usage error.
	code, _, stderr := run(t, "simulate", "--out", "out")
	if code != ExitUsage {
		t.Fatalf("missing --seed: exit = %d, want %d", code, ExitUsage)
	}
	if !strings.Contains(stderr, "--seed") {
		t.Fatalf("stderr = %q, want mention of --seed", stderr)
	}
}

func TestUnknownFlagExits2(t *testing.T) {
	code, _, _ := run(t, "demo", "--nope")
	if code != ExitUsage {
		t.Fatalf("exit = %d, want %d", code, ExitUsage)
	}
}

func TestBenchDefaultsAreDocumented(t *testing.T) {
	// bench has no required flags; with defaults it should reach the stub, not
	// a usage error.
	code, _, stderr := run(t, "bench")
	if code != ExitError {
		t.Fatalf("exit = %d, want %d", code, ExitError)
	}
	if !strings.Contains(stderr, "not implemented yet") {
		t.Fatalf("stderr = %q", stderr)
	}
}
