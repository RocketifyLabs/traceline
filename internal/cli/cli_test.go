package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RocketifyLabs/traceline"
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
		if !strings.Contains(stdout, traceline.Version) {
			t.Fatalf("%s: stdout %q missing version %q", flag, stdout, traceline.Version)
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
		if !strings.Contains(stdout, "Usage: traceline "+command) {
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

func TestAnalyzeMissingRequiredFlagsExits2(t *testing.T) {
	code, _, stderr := run(t, "analyze", "--logs", "l.jsonl")
	if code != ExitUsage {
		t.Fatalf("exit = %d, want %d", code, ExitUsage)
	}
	if !strings.Contains(stderr, "required") {
		t.Fatalf("stderr = %q, want a required-flags message", stderr)
	}
}

func TestAnalyzeMissingFileExits1(t *testing.T) {
	dir := t.TempDir()
	code, _, stderr := run(t, "analyze",
		"--logs", filepath.Join(dir, "nope.jsonl"),
		"--spans", filepath.Join(dir, "nope.jsonl"),
		"--out", dir)
	if code != ExitError {
		t.Fatalf("exit = %d, want %d (unreadable input is a failure)", code, ExitError)
	}
	if !strings.Contains(stderr, "traceline analyze:") {
		t.Errorf("stderr = %q, want a prefixed error", stderr)
	}
}

func TestAnalyzeEmptyInputExits1(t *testing.T) {
	dir := t.TempDir()
	logs := filepath.Join(dir, "logs.jsonl")
	spans := filepath.Join(dir, "spans.jsonl")
	if err := os.WriteFile(logs, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(spans, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	code, _, stderr := run(t, "analyze", "--logs", logs, "--spans", spans, "--out", dir)
	if code != ExitError {
		t.Fatalf("exit = %d, want %d for empty input", code, ExitError)
	}
	if !strings.Contains(stderr, "no usable records") {
		t.Errorf("stderr = %q", stderr)
	}
}

func TestSimulateRequiresSeedAndOut(t *testing.T) {
	code, _, stderr := run(t, "simulate", "--out", "out")
	if code != ExitUsage {
		t.Fatalf("missing --seed: exit = %d, want %d", code, ExitUsage)
	}
	if !strings.Contains(stderr, "--seed") {
		t.Fatalf("stderr = %q, want mention of --seed", stderr)
	}
}

func TestSimulateWritesScenarioFiles(t *testing.T) {
	dir := t.TempDir()
	code, stdout, stderr := run(t, "simulate", "--seed", "5", "--out", dir)
	if code != ExitOK {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	for _, name := range []string{"logs.jsonl", "spans.jsonl", "scenario.yaml"} {
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil {
			t.Errorf("%s not written: %v", name, err)
			continue
		}
		if info.Size() == 0 {
			t.Errorf("%s is empty", name)
		}
	}
	if !strings.Contains(stdout, "true_cause=") {
		t.Errorf("stdout should report the ground truth: %q", stdout)
	}
}

// --seed 0 is a legitimate seed and must be accepted.
func TestSimulateAcceptsSeedZero(t *testing.T) {
	dir := t.TempDir()
	code, _, stderr := run(t, "simulate", "--seed", "0", "--out", dir)
	if code != ExitOK {
		t.Fatalf("--seed 0 rejected: exit = %d, stderr = %q", code, stderr)
	}
}

func TestSimulateIsDeterministic(t *testing.T) {
	d1, d2 := t.TempDir(), t.TempDir()
	if code, _, _ := run(t, "simulate", "--seed", "42", "--out", d1); code != ExitOK {
		t.Fatal("first run failed")
	}
	if code, _, _ := run(t, "simulate", "--seed", "42", "--out", d2); code != ExitOK {
		t.Fatal("second run failed")
	}
	a, err := os.ReadFile(filepath.Join(d1, "logs.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(d2, "logs.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a, b) {
		t.Fatal("same seed produced different logs.jsonl")
	}
}

func TestBenchWritesMarkdown(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "BENCHMARK.md")
	code, stdout, stderr := run(t, "bench", "--seeds", "1-2", "--out", out)
	if code != ExitOK {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	if !strings.Contains(stdout, "top-1") {
		t.Errorf("stdout should report the metrics: %q", stdout)
	}
	blob, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("BENCHMARK.md not written: %v", err)
	}
	if !strings.Contains(string(blob), "Top-1 accuracy") {
		t.Error("BENCHMARK.md missing the summary")
	}
}

func TestBenchRejectsBadSeedRange(t *testing.T) {
	code, _, stderr := run(t, "bench", "--seeds", "20-1")
	if code != ExitUsage {
		t.Fatalf("exit = %d, want %d", code, ExitUsage)
	}
	if !strings.Contains(stderr, "inverted") {
		t.Errorf("stderr = %q", stderr)
	}
}

func TestDemoWritesEveryArtifact(t *testing.T) {
	dir := t.TempDir()
	code, stdout, stderr := run(t, "demo", "--out", dir, "--seed", "7")
	if code != ExitOK {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	for _, name := range []string{"report.html", "report.pdf", "summary.md", "result.json"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("%s not written: %v", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "charts", "dependencies.svg")); err != nil {
		t.Errorf("charts/dependencies.svg not written: %v", err)
	}
	if !strings.Contains(stdout, "true cause:") {
		t.Errorf("demo should state the ground truth for comparison: %q", stdout)
	}
	if !strings.Contains(stdout, "HIT") && !strings.Contains(stdout, "MISS") {
		t.Errorf("demo should say whether the ranking matched: %q", stdout)
	}
}

func TestDemoIsDeterministic(t *testing.T) {
	d1, d2 := t.TempDir(), t.TempDir()
	if code, _, _ := run(t, "demo", "--out", d1, "--seed", "3"); code != ExitOK {
		t.Fatal("first demo failed")
	}
	if code, _, _ := run(t, "demo", "--out", d2, "--seed", "3"); code != ExitOK {
		t.Fatal("second demo failed")
	}
	for _, name := range []string{"report.html", "summary.md", "result.json", "report.pdf"} {
		a, err := os.ReadFile(filepath.Join(d1, name))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		b, err := os.ReadFile(filepath.Join(d2, name))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !bytes.Equal(a, b) {
			t.Errorf("%s differs between two runs of the same seed", name)
		}
	}
}

// End to end: simulate to disk, then analyze those files back.
func TestAnalyzeRoundTripsSimulatedFiles(t *testing.T) {
	src := t.TempDir()
	if code, _, stderr := run(t, "simulate", "--seed", "12", "--out", src); code != ExitOK {
		t.Fatalf("simulate failed: %s", stderr)
	}
	out := t.TempDir()
	code, stdout, stderr := run(t, "analyze",
		"--logs", filepath.Join(src, "logs.jsonl"),
		"--spans", filepath.Join(src, "spans.jsonl"),
		"--out", out)
	if code != ExitOK {
		t.Fatalf("analyze exit = %d, stderr = %q", code, stderr)
	}
	if !strings.Contains(stdout, "root cause:") && !strings.Contains(stdout, "insufficient evidence") {
		t.Errorf("analyze printed neither a cause nor insufficient evidence: %q", stdout)
	}
	for _, name := range []string{"report.html", "result.json"} {
		if _, err := os.Stat(filepath.Join(out, name)); err != nil {
			t.Errorf("%s not written: %v", name, err)
		}
	}
}

func TestUnknownFlagExits2(t *testing.T) {
	code, _, _ := run(t, "demo", "--nope")
	if code != ExitUsage {
		t.Fatalf("exit = %d, want %d", code, ExitUsage)
	}
}
