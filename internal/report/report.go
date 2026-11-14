// Package report writes the analysis artifacts.
//
// One run produces:
//
//	report.html   self-contained: inline CSS, JS and SVG, zero external requests
//	report.pdf    vector, printable, written as PDF operators
//	charts/*.svg  each chart standalone, for a postmortem or a slide
//	summary.md    short Markdown summary
//	result.json   machine-readable ranked causes with evidence ids
//
// Every writer takes the same model.Report, so the three formats cannot disagree.
package report

import (
	"encoding/json"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/RocketifyLabs/traceline/internal/model"
	"github.com/RocketifyLabs/traceline/internal/svg"
)

// Files names the artifacts produced by a run.
const (
	HTMLName  = "report.html"
	PDFName   = "report.pdf"
	MDName    = "summary.md"
	JSONName  = "result.json"
	ChartsDir = "charts"
)

// Options carries metadata that is not part of the analysis.
type Options struct {
	// Title is shown at the top; defaults to "Incident report".
	Title string
	// Source describes the input, e.g. a fixture name or a scenario seed.
	Source string
	// Version is the traceline version, normalized in golden tests.
	Version string
}

// WriteAll writes every artifact into dir.
func WriteAll(dir string, rep model.Report, opts Options) error {
	if err := os.MkdirAll(filepath.Join(dir, ChartsDir), 0o755); err != nil {
		return err
	}
	charts := buildCharts(rep)

	if err := writeFile(filepath.Join(dir, HTMLName), HTML(rep, opts, charts)); err != nil {
		return err
	}
	if err := writeFile(filepath.Join(dir, PDFName), PDF(rep, opts, charts)); err != nil {
		return err
	}
	for name, doc := range charts.standalone {
		if err := writeFile(filepath.Join(dir, ChartsDir, name), doc); err != nil {
			return err
		}
	}
	if err := writeFile(filepath.Join(dir, MDName), Markdown(rep, opts)); err != nil {
		return err
	}
	return writeFile(filepath.Join(dir, JSONName), JSON(rep, opts))
}

func writeFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o644)
}

// charts bundles the rendered SVGs.
type charts struct {
	errorRate string
	timeline  string
	graph     string
	// standalone maps a filename to its SVG document.
	standalone map[string]string
}

// buildCharts renders every chart from the report.
func buildCharts(rep model.Report) charts {
	// Error rate per service, from the candidates (which is what the ranking
	// used) rather than recomputing from raw logs.
	bars := make([]svg.Bar, 0, len(rep.Candidates))
	top := ""
	if len(rep.Candidates) > 0 {
		top = rep.Candidates[0].Service
	}
	for _, c := range rep.Candidates {
		bars = append(bars, svg.Bar{
			Label:  c.Service,
			Value:  c.Score,
			Accent: c.Service == top,
			Note:   fmt.Sprintf("%.2f", c.Score),
		})
	}

	// Timeline: first-error volume per service is not available here, so the
	// chart plots anomaly strength over time, which is what the timeline is for.
	series := []svg.Series{}
	if len(rep.Anomalies) > 0 {
		pts := make([]svg.Point, 0, len(rep.Anomalies))
		for _, a := range rep.Anomalies {
			if a.Onset.IsZero() || rep.IncidentStart.IsZero() {
				continue
			}
			pts = append(pts, svg.Point{
				At:    a.Onset.Sub(rep.IncidentStart).Seconds(),
				Value: a.Strength,
			})
		}
		sort.Slice(pts, func(i, j int) bool { return pts[i].At < pts[j].At })
		if len(pts) > 0 {
			series = append(series, svg.Series{Label: "anomaly strength", Points: pts})
		}
	}

	// Dependency graph: layered by BFS depth from the entry services.
	nodes, edges := graphView(rep, top)

	return charts{
		errorRate: svg.BarChart("candidate scores", bars, 760, 60+len(bars)*30),
		timeline:  svg.LineChart("anomaly strength over the incident window", series, 760, 300, 0, "window start"),
		graph:     svg.DependencyGraph(nodes, edges, 760, 320),
		standalone: map[string]string{
			"error-rate.svg":   svg.BarChart("candidate scores", bars, 760, 60+len(bars)*30),
			"timeline.svg":     svg.LineChart("anomaly strength over the incident window", series, 760, 300, 0, "window start"),
			"dependencies.svg": svg.DependencyGraph(nodes, edges, 760, 320),
		},
	}
}

// graphView computes layer and error rate per service.
func graphView(rep model.Report, accent string) ([]svg.Node, []svg.EdgeView) {
	// Error rate per service from the edge statistics.
	errByService := map[string]float64{}
	callsByService := map[string]int{}
	errsByService := map[string]int{}
	adj := map[string][]string{}
	callers := map[string][]string{}

	for _, e := range rep.Edges {
		callsByService[e.Caller] += e.Calls
		errsByService[e.Caller] += e.Errors
		callsByService[e.Callee] += e.Calls
		errsByService[e.Callee] += e.Errors
		adj[e.Caller] = append(adj[e.Caller], e.Callee)
		callers[e.Callee] = append(callers[e.Callee], e.Caller)
	}
	for svc, calls := range callsByService {
		if calls > 0 {
			errByService[svc] = float64(errsByService[svc]) / float64(calls)
		}
	}

	// Layer: shortest distance from a root (a service nobody calls).
	layer := map[string]int{}
	queue := []string{}
	for svc := range callsByService {
		if len(callers[svc]) == 0 {
			layer[svc] = 0
			queue = append(queue, svc)
		}
	}
	// Sorted queue seed: map iteration must not decide anything.
	sort.Strings(queue)
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		next := append([]string(nil), adj[cur]...)
		sort.Strings(next)
		for _, n := range next {
			if _, seen := layer[n]; !seen {
				layer[n] = layer[cur] + 1
				queue = append(queue, n)
			}
		}
	}

	// Nodes, in sorted name order so the SVG is stable.
	names := make([]string, 0, len(callsByService))
	for svc := range callsByService {
		names = append(names, svc)
	}
	sort.Strings(names)

	nodes := make([]svg.Node, 0, len(names))
	for _, n := range names {
		nodes = append(nodes, svg.Node{
			Name:   n,
			Layer:  layer[n],
			Error:  errByService[n],
			Accent: n == accent,
		})
	}
	// Edges in sorted order.
	edges := make([]svg.EdgeView, 0, len(rep.Edges))
	sortedEdges := append([]model.Edge(nil), rep.Edges...)
	sort.Slice(sortedEdges, func(i, j int) bool {
		if sortedEdges[i].Caller != sortedEdges[j].Caller {
			return sortedEdges[i].Caller < sortedEdges[j].Caller
		}
		return sortedEdges[i].Callee < sortedEdges[j].Callee
	})
	for _, e := range sortedEdges {
		edges = append(edges, svg.EdgeView{From: e.Caller, To: e.Callee, Errors: e.Errors})
	}
	return nodes, edges
}

// HTML renders the self-contained report.
//
// Everything is inline. A test asserts there is no http(s) reference, so this
// function must never grow a <link>, a remote <script> or a background URL.
func HTML(rep model.Report, opts Options, ch charts) string {
	title := opts.Title
	if title == "" {
		title = "Incident report"
	}

	var b strings.Builder
	b.WriteString("<!DOCTYPE html>\n<html lang=\"en\"><head><meta charset=\"utf-8\">")
	fmt.Fprintf(&b, "<meta name=\"viewport\" content=\"width=device-width, initial-scale=1\">")
	fmt.Fprintf(&b, "<title>%s</title>\n", html.EscapeString(title))
	b.WriteString("<style>")
	b.WriteString(css)
	b.WriteString("</style></head><body>\n")

	b.WriteString("<main>\n")
	fmt.Fprintf(&b, "<header><h1>%s</h1>\n", html.EscapeString(title))
	if opts.Source != "" {
		fmt.Fprintf(&b, "<p class=\"src\">source: <code>%s</code></p>\n", html.EscapeString(opts.Source))
	}
	fmt.Fprintf(&b, "<p class=\"win\">baseline %s → %s &nbsp;·&nbsp; incident %s → %s</p>\n",
		fmtTime(rep.BaselineStart), fmtTime(rep.BaselineEnd),
		fmtTime(rep.IncidentStart), fmtTime(rep.IncidentEnd))
	b.WriteString("</header>\n")

	// Verdict first: during an incident the reader wants the answer, not the
	// method.
	if rep.Insufficient {
		b.WriteString("<section class=\"verdict insufficient\">")
		b.WriteString("<h2>Insufficient evidence</h2>")
		b.WriteString("<p>No anomaly passed the detection threshold in this capture, so no root cause is ")
		b.WriteString("named. This is a deliberate answer, not a failure: a confidently wrong cause during ")
		b.WriteString("an incident is worse than no answer.</p></section>\n")
	} else if len(rep.Candidates) > 0 {
		top := rep.Candidates[0]
		fmt.Fprintf(&b, "<section class=\"verdict\"><h2>Most likely root cause</h2>")
		fmt.Fprintf(&b, "<p class=\"cause\"><code>%s</code><span class=\"conf conf-%s\">%s confidence</span></p>",
			html.EscapeString(top.Service), html.EscapeString(top.Confidence), html.EscapeString(top.Confidence))
		fmt.Fprintf(&b, "<p class=\"score\">score %.2f</p></section>\n", top.Score)
	}

	// Ranked causes with breakdown.
	b.WriteString("<section><h2>Ranked causes</h2>\n")
	if len(rep.Candidates) == 0 {
		b.WriteString("<p class=\"empty\">No candidates.</p>\n")
	} else {
		b.WriteString("<table><thead><tr><th>#</th><th>service</th><th>score</th><th>confidence</th>")
		b.WriteString("<th>onset</th><th>strength</th><th>explains</th><th>not explained</th><th>evidence</th>")
		b.WriteString("</tr></thead><tbody>\n")
		for i, c := range rep.Candidates {
			fmt.Fprintf(&b, "<tr><td>%d</td><td><code>%s</code></td><td>%.2f</td><td>%s</td>",
				i+1, html.EscapeString(c.Service), c.Score, html.EscapeString(c.Confidence))
			fmt.Fprintf(&b, "<td>%.2f</td><td>%.2f</td><td>%.2f</td><td>%.2f</td>",
				c.Breakdown.Onset, c.Breakdown.Strength,
				c.Breakdown.ExplainsDownstream, c.Breakdown.NotExplained)
			fmt.Fprintf(&b, "<td class=\"ids\">%s</td></tr>\n", html.EscapeString(joinIDs(c.EvidenceIDs)))
		}
		b.WriteString("</tbody></table>\n")
		b.WriteString("<p class=\"note\">Every score is a weighted sum of the four factors shown, so a ranking ")
		b.WriteString("can be audited rather than trusted. Evidence ids address the exact log lines and spans.</p>\n")
	}
	b.WriteString("</section>\n")

	// Charts.
	b.WriteString("<section><h2>Charts</h2>\n")
	b.WriteString("<div class=\"chart\">")
	b.WriteString(ch.errorRate)
	b.WriteString("</div>\n<div class=\"chart\">")
	b.WriteString(ch.graph)
	b.WriteString("</div>\n<div class=\"chart\">")
	b.WriteString(ch.timeline)
	b.WriteString("</div>\n</section>\n")

	// Timeline.
	b.WriteString("<section><h2>Timeline</h2>\n")
	if len(rep.Timeline) == 0 {
		b.WriteString("<p class=\"empty\">No events in this window.</p>\n")
	} else {
		b.WriteString("<table><thead><tr><th>time</th><th>kind</th><th>service</th><th>detail</th></tr></thead><tbody>\n")
		for _, e := range rep.Timeline {
			fmt.Fprintf(&b, "<tr><td>%s</td><td>%s</td><td><code>%s</code></td><td>%s</td></tr>\n",
				fmtTime(e.At), html.EscapeString(e.Kind),
				html.EscapeString(e.Service), html.EscapeString(e.Detail))
		}
		b.WriteString("</tbody></table>\n")
	}
	b.WriteString("</section>\n")

	// Dependency edges.
	b.WriteString("<section><h2>Dependencies</h2>\n")
	if len(rep.Edges) == 0 {
		b.WriteString("<p class=\"empty\">No caller→callee edges found in the spans.</p>\n")
	} else {
		b.WriteString("<table><thead><tr><th>caller</th><th></th><th>callee</th><th>calls</th><th>errors</th><th>err rate</th><th>p50</th><th>p95</th></tr></thead><tbody>\n")
		for _, e := range rep.Edges {
			fmt.Fprintf(&b, "<tr><td><code>%s</code></td><td>→</td><td><code>%s</code></td><td>%d</td><td>%d</td><td>%.0f%%</td><td>%s</td><td>%s</td></tr>\n",
				html.EscapeString(e.Caller), html.EscapeString(e.Callee),
				e.Calls, e.Errors, e.ErrorRate()*100, fmtDur(e.P50), fmtDur(e.P95))
		}
		b.WriteString("</tbody></table>\n")
	}
	b.WriteString("</section>\n")

	// Data quality: what was skipped and why. Never hidden.
	b.WriteString("<section><h2>Data quality</h2>\n<table><tbody>\n")
	fmt.Fprintf(&b, "<tr><td>log lines read</td><td>%d</td></tr>\n", rep.Quality.LogLinesRead)
	fmt.Fprintf(&b, "<tr><td>logs accepted</td><td>%d</td></tr>\n", rep.Quality.LogsAccepted)
	fmt.Fprintf(&b, "<tr><td>span lines read</td><td>%d</td></tr>\n", rep.Quality.SpanLinesRead)
	fmt.Fprintf(&b, "<tr><td>spans accepted</td><td>%d</td></tr>\n", rep.Quality.SpansAccepted)
	fmt.Fprintf(&b, "<tr><td>skipped</td><td>%d</td></tr>\n", rep.Quality.Skipped)
	if len(rep.Quality.SkipReasons) > 0 {
		reasons := make([]string, 0, len(rep.Quality.SkipReasons))
		for r := range rep.Quality.SkipReasons {
			reasons = append(reasons, r)
		}
		sort.Strings(reasons)
		for _, r := range reasons {
			fmt.Fprintf(&b, "<tr><td class=\"reason\">%s</td><td>%d</td></tr>\n",
				html.EscapeString(r), rep.Quality.SkipReasons[r])
		}
	}
	if rep.Quality.ClockSkewLines > 0 {
		fmt.Fprintf(&b, "<tr><td>clock-skewed lines</td><td>%d</td></tr>\n", rep.Quality.ClockSkewLines)
	}
	if rep.Quality.OrphanSpans > 0 {
		fmt.Fprintf(&b, "<tr><td>orphan spans</td><td>%d</td></tr>\n", rep.Quality.OrphanSpans)
	}
	b.WriteString("</tbody></table></section>\n")

	if opts.Version != "" {
		fmt.Fprintf(&b, "<footer>generated by traceline %s</footer>\n", html.EscapeString(opts.Version))
	}
	b.WriteString("</main>\n</body></html>\n")
	return b.String()
}

const css = `
:root{color-scheme:dark}
*{box-sizing:border-box}
body{margin:0;background:#0f1117;color:#e6e9ef;
  font:15px/1.6 -apple-system,Segoe UI,Roboto,Helvetica,Arial,sans-serif}
main{max-width:900px;margin:0 auto;padding:40px 24px 80px}
header h1{font-size:26px;margin:0 0 8px;letter-spacing:-.02em}
.src,.win{color:#8b93a7;font-size:13px;margin:2px 0}
code{font-family:ui-monospace,SFMono-Regular,Menlo,Consolas,monospace;font-size:.95em;
  background:#181b24;padding:1px 5px;border-radius:4px}
section{margin:34px 0}
h2{font-size:13px;text-transform:uppercase;letter-spacing:.08em;color:#8b93a7;
  margin:0 0 12px;font-weight:600}
.verdict{background:#181b24;border:1px solid #252a36;border-left:3px solid #e5484d;
  border-radius:6px;padding:18px 20px}
.verdict.insufficient{border-left-color:#f5a623}
.verdict h2{margin-bottom:10px}
.cause{margin:0;font-size:22px;display:flex;align-items:center;gap:12px;flex-wrap:wrap}
.score{margin:6px 0 0;color:#8b93a7;font-size:13px}
.conf{font-size:11px;text-transform:uppercase;letter-spacing:.06em;padding:3px 8px;border-radius:999px}
.conf-high{background:#1c3a24;color:#3fb950}
.conf-medium{background:#3a3018;color:#f5a623}
.conf-low{background:#2a2d38;color:#8b93a7}
table{width:100%;border-collapse:collapse;font-size:13px}
th{text-align:left;color:#8b93a7;font-weight:600;font-size:11px;text-transform:uppercase;
  letter-spacing:.06em;border-bottom:1px solid #252a36;padding:7px 10px 7px 0}
td{padding:7px 10px 7px 0;border-bottom:1px solid #1c2029;vertical-align:top}
tr:last-child td{border-bottom:none}
.reason{color:#8b93a7}
.ids{color:#8b93a7;font-family:ui-monospace,monospace;font-size:11px;word-break:break-all;max-width:220px}
.note,.empty{color:#8b93a7;font-size:12px;margin:10px 0 0}
.chart{background:#181b24;border:1px solid #252a36;border-radius:6px;padding:10px;
  margin-bottom:14px;overflow:hidden}
.chart svg{display:block;width:100%;height:auto}
footer{margin-top:50px;color:#5c6478;font-size:11px;border-top:1px solid #1c2029;padding-top:16px}
`

func joinIDs(ids []string) string {
	if len(ids) == 0 {
		return "—"
	}
	if len(ids) > 6 {
		return strings.Join(ids[:6], " ") + " …"
	}
	return strings.Join(ids, " ")
}

func fmtTime(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	return t.UTC().Format("15:04:05.000")
}

func fmtDur(d time.Duration) string {
	if d == 0 {
		return "0"
	}
	if d < time.Millisecond {
		return fmt.Sprintf("%dµs", d.Microseconds())
	}
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	return fmt.Sprintf("%.2fs", d.Seconds())
}

// Markdown renders the short summary.
func Markdown(rep model.Report, opts Options) string {
	title := opts.Title
	if title == "" {
		title = "Incident report"
	}

	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", title)
	if opts.Source != "" {
		fmt.Fprintf(&b, "Source: `%s`\n\n", opts.Source)
	}
	fmt.Fprintf(&b, "Baseline %s → %s · incident %s → %s\n\n",
		fmtTime(rep.BaselineStart), fmtTime(rep.BaselineEnd),
		fmtTime(rep.IncidentStart), fmtTime(rep.IncidentEnd))

	if rep.Insufficient {
		b.WriteString("## Verdict\n\n**Insufficient evidence.** No anomaly passed the threshold, so no root\n")
		b.WriteString("cause is named. This is deliberate: a confidently wrong cause is worse than\n")
		b.WriteString("no answer.\n\n")
	} else if len(rep.Candidates) > 0 {
		top := rep.Candidates[0]
		fmt.Fprintf(&b, "## Verdict\n\n**Most likely root cause: `%s`** (score %.2f, %s confidence)\n\n",
			top.Service, top.Score, top.Confidence)
	}

	b.WriteString("## Ranked causes\n\n")
	if len(rep.Candidates) == 0 {
		b.WriteString("_No candidates._\n\n")
	} else {
		b.WriteString("| # | service | score | confidence | onset | strength | explains | not explained |\n")
		b.WriteString("|--:|---|--:|---|---:|--:|--:|--:|\n")
		for i, c := range rep.Candidates {
			fmt.Fprintf(&b, "| %d | `%s` | %.2f | %s | %.2f | %.2f | %.2f | %.2f |\n",
				i+1, c.Service, c.Score, c.Confidence,
				c.Breakdown.Onset, c.Breakdown.Strength,
				c.Breakdown.ExplainsDownstream, c.Breakdown.NotExplained)
		}
		b.WriteString("\n")
	}

	b.WriteString("## Timeline\n\n")
	if len(rep.Timeline) == 0 {
		b.WriteString("_No events._\n\n")
	} else {
		for _, e := range rep.Timeline {
			fmt.Fprintf(&b, "- `%s` **%s** `%s` — %s\n", fmtTime(e.At), e.Kind, e.Service, e.Detail)
		}
		b.WriteString("\n")
	}

	b.WriteString("## Data quality\n\n")
	fmt.Fprintf(&b, "| metric | value |\n|---|---:|\n")
	fmt.Fprintf(&b, "| logs accepted | %d / %d read |\n", rep.Quality.LogsAccepted, rep.Quality.LogLinesRead)
	fmt.Fprintf(&b, "| spans accepted | %d / %d read |\n", rep.Quality.SpansAccepted, rep.Quality.SpanLinesRead)
	fmt.Fprintf(&b, "| skipped | %d |\n", rep.Quality.Skipped)
	if rep.Quality.ClockSkewLines > 0 {
		fmt.Fprintf(&b, "| clock-skewed lines | %d |\n", rep.Quality.ClockSkewLines)
	}
	if rep.Quality.OrphanSpans > 0 {
		fmt.Fprintf(&b, "| orphan spans | %d |\n", rep.Quality.OrphanSpans)
	}
	return b.String()
}

// jsonReport is the stable JSON shape. It is a separate struct, not the model, so
// an internal rename cannot silently break a consumer's parser.
type jsonReport struct {
	Version       string          `json:"version"`
	Source        string          `json:"source,omitempty"`
	BaselineStart string          `json:"baseline_start"`
	BaselineEnd   string          `json:"baseline_end"`
	IncidentStart string          `json:"incident_start"`
	IncidentEnd   string          `json:"incident_end"`
	Insufficient  bool            `json:"insufficient_evidence"`
	Candidates    []jsonCandidate `json:"candidates"`
	Timeline      []jsonEvent     `json:"timeline"`
	Edges         []jsonEdge      `json:"edges"`
	Quality       jsonQuality     `json:"data_quality"`
}

type jsonCandidate struct {
	Service     string        `json:"service"`
	Score       float64       `json:"score"`
	Confidence  string        `json:"confidence"`
	Breakdown   jsonBreakdown `json:"breakdown"`
	EvidenceIDs []string      `json:"evidence_ids"`
}

type jsonBreakdown struct {
	Onset              float64 `json:"onset"`
	Strength           float64 `json:"strength"`
	ExplainsDownstream float64 `json:"explains_downstream"`
	NotExplained       float64 `json:"not_explained"`
}

type jsonEvent struct {
	At      string `json:"at"`
	Kind    string `json:"kind"`
	Service string `json:"service"`
	Detail  string `json:"detail"`
}

type jsonEdge struct {
	Caller    string  `json:"caller"`
	Callee    string  `json:"callee"`
	Calls     int     `json:"calls"`
	Errors    int     `json:"errors"`
	ErrorRate float64 `json:"error_rate"`
	P50MS     float64 `json:"p50_ms"`
	P95MS     float64 `json:"p95_ms"`
}

type jsonQuality struct {
	LogLinesRead  int            `json:"log_lines_read"`
	LogsAccepted  int            `json:"logs_accepted"`
	SpanLinesRead int            `json:"span_lines_read"`
	SpansAccepted int            `json:"spans_accepted"`
	Skipped       int            `json:"skipped"`
	SkipReasons   map[string]int `json:"skip_reasons,omitempty"`
	ClockSkew     int            `json:"clock_skewed_lines,omitempty"`
	OrphanSpans   int            `json:"orphan_spans,omitempty"`
}

// JSON renders the machine-readable result.
//
// Maps are converted to sorted output by encoding/json (it sorts map keys), and
// every slice is already ordered by the analysis, so the bytes are stable.
func JSON(rep model.Report, opts Options) string {
	jr := jsonReport{
		Version:       opts.Version,
		Source:        opts.Source,
		BaselineStart: isoTime(rep.BaselineStart),
		BaselineEnd:   isoTime(rep.BaselineEnd),
		IncidentStart: isoTime(rep.IncidentStart),
		IncidentEnd:   isoTime(rep.IncidentEnd),
		Insufficient:  rep.Insufficient,
		Candidates:    make([]jsonCandidate, 0, len(rep.Candidates)),
		Timeline:      make([]jsonEvent, 0, len(rep.Timeline)),
		Edges:         make([]jsonEdge, 0, len(rep.Edges)),
		Quality: jsonQuality{
			LogLinesRead:  rep.Quality.LogLinesRead,
			LogsAccepted:  rep.Quality.LogsAccepted,
			SpanLinesRead: rep.Quality.SpanLinesRead,
			SpansAccepted: rep.Quality.SpansAccepted,
			Skipped:       rep.Quality.Skipped,
			SkipReasons:   rep.Quality.SkipReasons,
			ClockSkew:     rep.Quality.ClockSkewLines,
			OrphanSpans:   rep.Quality.OrphanSpans,
		},
	}
	for _, c := range rep.Candidates {
		ids := c.EvidenceIDs
		if ids == nil {
			ids = []string{}
		}
		jr.Candidates = append(jr.Candidates, jsonCandidate{
			Service:    c.Service,
			Score:      round4(c.Score),
			Confidence: c.Confidence,
			Breakdown: jsonBreakdown{
				Onset:              round4(c.Breakdown.Onset),
				Strength:           round4(c.Breakdown.Strength),
				ExplainsDownstream: round4(c.Breakdown.ExplainsDownstream),
				NotExplained:       round4(c.Breakdown.NotExplained),
			},
			EvidenceIDs: ids,
		})
	}
	for _, e := range rep.Timeline {
		jr.Timeline = append(jr.Timeline, jsonEvent{
			At: isoTime(e.At), Kind: e.Kind, Service: e.Service, Detail: e.Detail,
		})
	}
	for _, e := range rep.Edges {
		jr.Edges = append(jr.Edges, jsonEdge{
			Caller: e.Caller, Callee: e.Callee, Calls: e.Calls, Errors: e.Errors,
			ErrorRate: round4(e.ErrorRate()),
			P50MS:     round4(float64(e.P50.Microseconds()) / 1000),
			P95MS:     round4(float64(e.P95.Microseconds()) / 1000),
		})
	}
	blob, err := json.MarshalIndent(jr, "", "  ")
	if err != nil {
		// Marshalling a struct of plain values cannot fail; if it ever does, an
		// empty object is better than a panic in a report writer.
		return "{}\n"
	}
	return string(blob) + "\n"
}

// round4 keeps JSON free of float noise that would differ between platforms.
func round4(v float64) float64 {
	if v != v { // NaN
		return 0
	}
	scale := 10000.0
	if v*scale > 9.2e18 || v*scale < -9.2e18 {
		return v
	}
	return float64(int64(v*scale+sign(v)*0.5)) / scale
}

func sign(v float64) float64 {
	if v < 0 {
		return -1
	}
	return 1
}

func isoTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}
