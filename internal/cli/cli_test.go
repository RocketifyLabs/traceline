package cli

import (
	"bytes"
	"context"
	"encoding/json"
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

// writeScenario generates a small scenario into logs and spans paths, so a test
// that needs a real pipeline run does not depend on a fixture's exact numbers.
func writeScenario(t *testing.T, logs, spans string) {
	t.Helper()
	src := t.TempDir()
	if code, _, stderr := run(t, "simulate", "--seed", "12", "--out", src); code != ExitOK {
		t.Fatalf("simulate failed: exit %d, %s", code, stderr)
	}
	for _, pair := range [][2]string{
		{filepath.Join(src, "logs.jsonl"), logs},
		{filepath.Join(src, "spans.jsonl"), spans},
	} {
		blob, err := os.ReadFile(pair[0])
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(pair[1], blob, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// A --weights file that does not parse must be a usage error, not a silent
// fallback: running with built-in weights when the operator asked for their own
// would attribute the result to tuning that was never applied.
func TestAnalyzeRejectsBadWeightsFile(t *testing.T) {
	dir := t.TempDir()
	logs := filepath.Join(dir, "logs.jsonl")
	spans := filepath.Join(dir, "spans.jsonl")
	writeScenario(t, logs, spans)

	bad := filepath.Join(dir, "typo.json")
	// "z_treshold" is the realistic mistake: one letter short, and the value it
	// was meant to set would have stayed at its default.
	if err := os.WriteFile(bad, []byte(`{"anomaly":{"z_treshold":9.0}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	code, _, stderr := run(t, "analyze", "--logs", logs, "--spans", spans,
		"--out", filepath.Join(dir, "out"), "--weights", bad)
	if code != ExitUsage {
		t.Fatalf("exit = %d, want %d", code, ExitUsage)
	}
	if !strings.Contains(stderr, "z_treshold") {
		t.Errorf("stderr should name the unknown field, got: %q", stderr)
	}
}

// The shipped rca_weights.json must produce byte-identical output to a run with
// no --weights at all. This is the whole claim of the flag: the file documents
// the defaults, so passing it cannot change the answer.
func TestShippedWeightsMatchDefaultRun(t *testing.T) {
	dir := t.TempDir()
	logs := filepath.Join(dir, "logs.jsonl")
	spans := filepath.Join(dir, "spans.jsonl")
	writeScenario(t, logs, spans)

	shipped := filepath.Join("..", "..", "rca_weights.json")
	if _, err := os.Stat(shipped); err != nil {
		t.Fatalf("rca_weights.json is missing from the repo root: %v", err)
	}

	outA, outB := filepath.Join(dir, "a"), filepath.Join(dir, "b")
	if code, _, stderr := run(t, "analyze", "--logs", logs, "--spans", spans, "--out", outA); code != ExitOK {
		t.Fatalf("default run failed: exit %d, %s", code, stderr)
	}
	if code, _, stderr := run(t, "analyze", "--logs", logs, "--spans", spans,
		"--out", outB, "--weights", shipped); code != ExitOK {
		t.Fatalf("run with shipped weights failed: exit %d, %s", code, stderr)
	}

	a, err := os.ReadFile(filepath.Join(outA, "result.json"))
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(outB, "result.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a, b) {
		t.Errorf("rca_weights.json changed the result; it must document the defaults exactly\n%s", firstDiff(string(a), string(b)))
	}
}

// firstDiff reports the first differing region, so a failure says what changed
// instead of dumping two long JSON documents.
func firstDiff(a, b string) string {
	if a == b {
		return "(identical)"
	}
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		if a[i] != b[i] {
			lo := i - 60
			if lo < 0 {
				lo = 0
			}
			hi := i + 60
			if hi > n {
				hi = n
			}
			return "at byte " + itoa(i) + ":\n  default: ..." + a[lo:hi] + "...\n  weights: ..." + b[lo:hi] + "..."
		}
	}
	return "one document is a prefix of the other"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var d []byte
	for n > 0 {
		d = append([]byte{byte(48 + n%10)}, d...)
		n /= 10
	}
	return string(d)
}

// A simulated demo must report its real record counts. Before this test the
// report claimed "logs accepted 0 / 0 read" for a 9k-line capture — a false
// statement about the data the ranking was built from, and the kind of number a
// reader uses to decide whether to trust the result.
func TestDemoReportsRealDataQuality(t *testing.T) {
	dir := t.TempDir()
	code, stdout, stderr := run(t, "demo", "--out", dir, "--seed", "7")
	if code != ExitOK {
		t.Fatalf("demo exit = %d, stderr = %q", code, stderr)
	}

	blob, err := os.ReadFile(filepath.Join(dir, "result.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		DataQuality struct {
			LogLinesRead  int `json:"log_lines_read"`
			LogsAccepted  int `json:"logs_accepted"`
			SpanLinesRead int `json:"span_lines_read"`
			SpansAccepted int `json:"spans_accepted"`
		} `json:"data_quality"`
	}
	if err := json.Unmarshal(blob, &doc); err != nil {
		t.Fatal(err)
	}

	q := doc.DataQuality
	if q.LogLinesRead == 0 || q.SpanLinesRead == 0 {
		t.Fatalf("demo reported zero input lines: %+v (stdout: %q)", q, stdout)
	}
	if q.LogsAccepted != q.LogLinesRead {
		t.Errorf("logs accepted %d != read %d; a simulated scenario cannot skip lines",
			q.LogsAccepted, q.LogLinesRead)
	}
	if q.SpansAccepted != q.SpanLinesRead {
		t.Errorf("spans accepted %d != read %d", q.SpansAccepted, q.SpanLinesRead)
	}

	// The Markdown must agree with the JSON: two artifacts of one run contradicting
	// each other is worse than either being wrong alone.
	md, err := os.ReadFile(filepath.Join(dir, "summary.md"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(md), "0 / 0 read") {
		t.Errorf("summary.md still claims zero lines read:\n%s", md)
	}
}
