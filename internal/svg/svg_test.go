package svg

import (
	"encoding/xml"
	"strings"
	"testing"
)

func TestBarChartIsWellFormedXML(t *testing.T) {
	doc := BarChart("error rate by service", []Bar{
		{Label: "db-proxy", Value: 0.94, Accent: true, Note: "94%"},
		{Label: "api-edge", Value: 0.31, Note: "31%"},
		{Label: "web", Value: 0.02, Note: "2%"},
	}, 600, 220)
	if err := xml.Unmarshal([]byte(doc), new(any)); err != nil {
		t.Fatalf("bar chart is not well-formed XML: %v\n%s", err, doc)
	}
	for _, want := range []string{"<svg", "</svg>", "db-proxy", "94%"} {
		if !strings.Contains(doc, want) {
			t.Errorf("missing %q in output", want)
		}
	}
}

func TestLineChartIsWellFormedXML(t *testing.T) {
	doc := LineChart("error rate over time", []Series{{
		Label: "api-edge",
		Points: []Point{
			{At: 0, Value: 0.01}, {At: 30, Value: 0.02},
			{At: 60, Value: 0.55}, {At: 90, Value: 0.51},
		},
		Marker: 60,
	}}, 700, 280, 1.0, "fault")
	if err := xml.Unmarshal([]byte(doc), new(any)); err != nil {
		t.Fatalf("line chart is not well-formed XML: %v", err)
	}
	if !strings.Contains(doc, "fault") {
		t.Error("marker label missing")
	}
}

func TestDependencyGraphIsWellFormedXML(t *testing.T) {
	doc := DependencyGraph([]Node{
		{Name: "web", Layer: 0, Error: 0.3},
		{Name: "api-edge", Layer: 1, Error: 0.4},
		{Name: "db-proxy", Layer: 2, Error: 0.9, Accent: true},
	}, []EdgeView{
		{From: "web", To: "api-edge", Errors: 5},
		{From: "api-edge", To: "db-proxy", Errors: 20},
	}, 700, 320)
	if err := xml.Unmarshal([]byte(doc), new(any)); err != nil {
		t.Fatalf("dependency graph is not well-formed XML: %v", err)
	}
	// The accented node draws a halo circle behind it, so the count is nodes+1.
	if got := strings.Count(doc, "<circle"); got != 4 {
		t.Errorf("expected 4 circles (3 nodes + 1 accent halo), got %d", got)
	}
}

// A log message containing markup must not break the document.
func TestLabelsAreEscaped(t *testing.T) {
	doc := BarChart(`title <script>alert(1)</script>`, []Bar{
		{Label: `a<b>&"c`, Value: 1},
	}, 400, 160)
	if strings.Contains(doc, "<script>") {
		t.Error("unescaped <script> in output")
	}
	if err := xml.Unmarshal([]byte(doc), new(any)); err != nil {
		t.Fatalf("escaped content broke XML: %v", err)
	}
}

func TestDeterministic(t *testing.T) {
	bars := []Bar{{Label: "a", Value: 0.5}, {Label: "b", Value: 0.25}}
	first := BarChart("t", bars, 400, 160)
	for i := 0; i < 20; i++ {
		if got := BarChart("t", bars, 400, 160); got != first {
			t.Fatal("bar chart output is not stable")
		}
	}
}

func TestEmptyInputsRenderPlaceholders(t *testing.T) {
	for name, doc := range map[string]string{
		"bars":  BarChart("t", nil, 400, 160),
		"lines": LineChart("t", nil, 400, 160, 1, ""),
		"graph": DependencyGraph(nil, nil, 400, 160),
	} {
		if err := xml.Unmarshal([]byte(doc), new(any)); err != nil {
			t.Errorf("%s: empty input produced invalid XML: %v", name, err)
		}
		if !strings.Contains(doc, "no ") {
			t.Errorf("%s: empty input should say why it is empty", name)
		}
	}
}

func TestNumFormatterAvoidsExponent(t *testing.T) {
	cases := map[float64]string{0: "0", 1: "1", 0.5: "0.5", 100: "100", 0.004: "0"}
	for in, want := range cases {
		got := num(in)
		if strings.ContainsAny(got, "eE") {
			t.Errorf("num(%v) = %q uses exponent notation", in, got)
		}
		if got != want && in != 0.004 {
			t.Errorf("num(%v) = %q, want %q", in, got, want)
		}
	}
}
