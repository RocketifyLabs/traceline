package report

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RocketifyLabs/traceline/internal/model"
)

func t0() time.Time { return time.Date(2026, 10, 16, 10, 0, 0, 0, time.UTC) }

func fixture() model.Report {
	return model.Report{
		BaselineStart: t0().Add(-20 * time.Minute),
		BaselineEnd:   t0(),
		IncidentStart: t0(),
		IncidentEnd:   t0().Add(5 * time.Minute),
		Candidates: []model.Candidate{
			{
				Service: "db-proxy", Score: 0.92, Confidence: "high",
				Breakdown:   model.ScoreBreakdown{Onset: 1.0, Strength: 0.9, ExplainsDownstream: 0.85, NotExplained: 1.0},
				EvidenceIDs: []string{"a1b2", "c3d4", "e5f6"},
			},
			{
				Service: "api-edge", Score: 0.61, Confidence: "medium",
				Breakdown:   model.ScoreBreakdown{Onset: 0.6, Strength: 0.7, ExplainsDownstream: 0.5, NotExplained: 0},
				EvidenceIDs: []string{"dead"},
			},
		},
		Timeline: []model.TimelineEvent{
			{At: t0(), Kind: "deploy", Service: "db-proxy", Detail: "v42"},
			{At: t0().Add(30 * time.Second), Kind: "first_anomaly", Service: "db-proxy", Detail: "error_rate"},
		},
		Edges: []model.Edge{
			{Caller: "api-edge", Callee: "db-proxy", Calls: 100, Errors: 40, P50: 12 * time.Millisecond, P95: 90 * time.Millisecond},
		},
		Anomalies: []model.Anomaly{
			{Service: "db-proxy", Kind: "error_rate", Onset: t0(), Strength: 9.5},
			{Service: "api-edge", Kind: "error_rate", Onset: t0().Add(10 * time.Second), Strength: 6.2},
		},
		Quality: model.DataQuality{
			LogLinesRead: 1000, LogsAccepted: 995, SpanLinesRead: 400, SpansAccepted: 400,
			Skipped: 5, SkipReasons: map[string]int{"invalid_json": 3, "empty_line": 2},
			ClockSkewLines: 1, OrphanSpans: 2,
		},
	}
}

func opts() Options {
	return Options{Title: "Incident report", Source: "seed 1001", Version: "0.1.0"}
}

func TestWriteAllProducesEveryArtifact(t *testing.T) {
	dir := t.TempDir()
	if err := WriteAll(dir, fixture(), opts()); err != nil {
		t.Fatalf("WriteAll: %v", err)
	}
	for _, name := range []string{HTMLName, PDFName, MDName, JSONName} {
		path := filepath.Join(dir, name)
		info, err := os.Stat(path)
		if err != nil {
			t.Errorf("%s not written: %v", name, err)
			continue
		}
		if info.Size() == 0 {
			t.Errorf("%s is empty", name)
		}
	}
	charts, err := os.ReadDir(filepath.Join(dir, ChartsDir))
	if err != nil {
		t.Fatalf("charts dir: %v", err)
	}
	if len(charts) != 3 {
		t.Errorf("got %d charts, want 3", len(charts))
	}
}

// FR-8: the HTML must contain no external resource reference.
//
// The SVG namespace URI (http://www.w3.org/2000/svg) is an identifier, not a
// fetch: browsers resolve it internally and never issue a request. It is the one
// http:// string allowed in the output, so the check excludes it explicitly
// rather than loosening the rule.
func TestHTMLHasNoExternalReferences(t *testing.T) {
	doc := HTML(fixture(), opts(), buildCharts(fixture()))
	scrubbed := strings.ReplaceAll(doc, `xmlns="http://www.w3.org/2000/svg"`, "")

	for _, bad := range []string{"http://", "https://", "<link", "<script"} {
		if strings.Contains(scrubbed, bad) {
			t.Errorf("HTML contains external reference %q", bad)
		}
	}
	// Every external reference that remains must be the SVG namespace only.
	if n := strings.Count(doc, "http://"); n != strings.Count(doc, "http://www.w3.org/2000/svg") {
		t.Errorf("found %d http:// strings but only %d are the SVG namespace",
			n, strings.Count(doc, "http://www.w3.org/2000/svg"))
	}
}

func TestHTMLContainsRequiredSections(t *testing.T) {
	doc := HTML(fixture(), opts(), buildCharts(fixture()))
	for _, want := range []string{
		"Most likely root cause", "db-proxy", "Ranked causes", "Timeline",
		"Dependencies", "Data quality", "Charts",
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("HTML missing %q", want)
		}
	}
}

func TestHTMLInsufficientEvidence(t *testing.T) {
	rep := fixture()
	rep.Candidates = nil
	rep.Insufficient = true
	doc := HTML(rep, opts(), buildCharts(rep))
	if !strings.Contains(doc, "Insufficient evidence") {
		t.Error("insufficient evidence not stated in HTML")
	}
}

// A log-derived string must never inject markup.
func TestHTMLEscapesLogContent(t *testing.T) {
	rep := fixture()
	rep.Candidates[0].Service = `<script>alert(1)</script>`
	rep.Timeline[0].Detail = `<img src=x onerror=alert(1)>`
	doc := HTML(rep, opts(), buildCharts(rep))
	if strings.Contains(doc, "<script>alert(1)</script>") {
		t.Error("service name was not escaped")
	}
	if strings.Contains(doc, "<img src=x") {
		t.Error("timeline detail was not escaped")
	}
}

func TestMarkdownContainsVerdict(t *testing.T) {
	md := Markdown(fixture(), opts())
	for _, want := range []string{"# Incident report", "Most likely root cause", "db-proxy", "## Timeline", "## Data quality"} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown missing %q", want)
		}
	}
}

func TestJSONRoundTrips(t *testing.T) {
	blob := JSON(fixture(), opts())
	var decoded map[string]any
	if err := json.Unmarshal([]byte(blob), &decoded); err != nil {
		t.Fatalf("JSON is not valid: %v", err)
	}
	cands, ok := decoded["candidates"].([]any)
	if !ok || len(cands) != 2 {
		t.Fatalf("candidates = %v", decoded["candidates"])
	}
	first := cands[0].(map[string]any)
	if first["service"] != "db-proxy" {
		t.Errorf("first candidate = %v", first["service"])
	}
	if first["confidence"] != "high" {
		t.Errorf("confidence = %v", first["confidence"])
	}
	ids, ok := first["evidence_ids"].([]any)
	if !ok || len(ids) != 3 {
		t.Errorf("evidence_ids = %v", first["evidence_ids"])
	}
	if decoded["insufficient_evidence"] != false {
		t.Errorf("insufficient_evidence = %v", decoded["insufficient_evidence"])
	}
}

func TestJSONIsDeterministic(t *testing.T) {
	rep := fixture()
	first := JSON(rep, opts())
	for i := 0; i < 10; i++ {
		if got := JSON(rep, opts()); got != first {
			t.Fatal("JSON output is not stable across runs")
		}
	}
}

// A structurally valid PDF: header, one xref, a trailer, and the text present.
func TestPDFStructure(t *testing.T) {
	doc := PDF(fixture(), opts(), buildCharts(fixture()))
	if !strings.HasPrefix(doc, "%PDF-1.7") {
		t.Error("missing PDF header")
	}
	if !strings.Contains(doc, "%%EOF") {
		t.Error("missing EOF marker")
	}
	if !strings.Contains(doc, "xref") {
		t.Error("missing xref table")
	}
	if !strings.Contains(doc, "trailer") {
		t.Error("missing trailer")
	}
	if !strings.Contains(doc, "/Type /Catalog") {
		t.Error("missing catalog")
	}
	// The verdict text must be in the content stream.
	if !strings.Contains(doc, "db-proxy") {
		t.Error("root cause not rendered in the PDF")
	}
}

// The offsets in the xref must point at the objects they claim.
func TestPDFCrossReferenceOffsets(t *testing.T) {
	doc := PDF(fixture(), opts(), buildCharts(fixture()))

	xrefAt := strings.LastIndex(doc, "\nxref")
	if xrefAt < 0 {
		t.Fatal("no xref section")
	}
	lines := strings.Split(doc[xrefAt+1:], "\n")
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], "\r")
	}
	if len(lines) < 3 {
		t.Fatalf("malformed xref: %q", lines)
	}
	if strings.TrimSpace(lines[0]) != "xref" {
		t.Fatalf("first xref line = %q, want \"xref\"", lines[0])
	}

	// The header line is "<start> <count>".
	var start, count int
	if _, err := fmt.Sscanf(strings.TrimSpace(lines[1]), "%d %d", &start, &count); err != nil {
		t.Fatalf("xref header %q: %v", lines[1], err)
	}
	_ = start
	if count < 4 {
		t.Fatalf("xref claims %d objects, expected at least 4", count)
	}

	// lines[0] = "xref", lines[1] = "<start> <count>", lines[2] = object 0 (free).
	// So object N's entry is at lines[N+2].
	for i := 1; i < count && i+2 < len(lines); i++ {
		var off int
		if _, err := fmt.Sscanf(strings.TrimSpace(lines[i+2]), "%d", &off); err != nil {
			continue
		}
		if off <= 0 || off >= len(doc) {
			t.Errorf("object %d offset %d out of range", i, off)
			continue
		}
		want := fmt.Sprintf("%d 0 obj", i)
		tail := doc[off:]
		if len(tail) > len(want) {
			tail = tail[:len(want)]
		}
		if tail != want {
			t.Errorf("object %d offset %d points at %q, want %q", i, off, tail, want)
		}
	}
}

func TestPDFEscapesSpecialCharacters(t *testing.T) {
	rep := fixture()
	rep.Candidates[0].Service = "a(b)c\\d"
	doc := PDF(rep, opts(), buildCharts(rep))
	if !strings.Contains(doc, `a\(b\)c\\d`) {
		t.Error("parentheses or backslash not escaped in PDF string")
	}
}

func TestChartsAreValidXML(t *testing.T) {
	ch := buildCharts(fixture())
	for name, doc := range ch.standalone {
		if err := xml.Unmarshal([]byte(doc), new(any)); err != nil {
			t.Errorf("%s is not valid XML: %v", name, err)
		}
	}
}

// The golden normalization must be limited to the documented fields, so this
// asserts the HTML output is byte-stable for a fixed report.
func TestHTMLIsDeterministic(t *testing.T) {
	rep := fixture()
	ch := buildCharts(rep)
	first := HTML(rep, opts(), ch)
	for i := 0; i < 10; i++ {
		if got := HTML(rep, opts(), buildCharts(rep)); got != first {
			t.Fatal("HTML output is not stable across runs")
		}
	}
}
