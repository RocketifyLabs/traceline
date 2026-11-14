package report

import (
	"fmt"
	"strings"
	"time"

	"github.com/RocketifyLabs/traceline/internal/model"
)

// PDF renders the report as a printable vector document.
//
// Written directly as PDF operators: a content stream is plain text, so no
// library is needed and no dependency can break the offline build. Text is placed
// with the two base-14 fonts every reader ships (Helvetica and Courier), which
// means no font embedding and no binary payload.
//
// Determinism: object order, page order and every coordinate are fixed functions
// of the report. No timestamp is embedded, so a second run produces the same
// bytes.
func PDF(rep model.Report, opts Options, ch charts) string {
	title := opts.Title
	if title == "" {
		title = "Incident report"
	}

	doc := newPDFDoc()
	pages := buildPDFPages(rep, opts, title, ch)
	for _, content := range pages {
		doc.addPage(content)
	}
	return doc.render()
}

// pdfDoc accumulates page content streams.
type pdfDoc struct {
	pages []string
}

func newPDFDoc() *pdfDoc { return &pdfDoc{} }

func (d *pdfDoc) addPage(content string) { d.pages = append(d.pages, content) }

// render assembles a valid PDF with a cross-reference table.
//
// Object layout, fixed so the bytes are stable:
//
//	1 catalog · 2 pages · 3.. Helvetica · then one object per page and per stream
func (d *pdfDoc) render() string {
	const (
		A4W = 842.0
		A4H = 595.0
	)

	// Build the object table first, then the body with byte offsets.
	objs := make([]string, 0, 8+len(d.pages)*2)

	// 1: catalog, 2: pages — placeholders filled after we know the kids.
	objs = append(objs, "") // 1 catalog
	objs = append(objs, "") // 2 pages
	objs = append(objs, "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>")
	objs = append(objs, "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica-Bold >>")
	objs = append(objs, "<< /Type /Font /Subtype /Type1 /BaseFont /Courier >>")

	kids := make([]string, 0, len(d.pages))
	for _, content := range d.pages {
		pageNum := len(objs) + 1
		streamNum := pageNum + 1
		kids = append(kids, fmt.Sprintf("%d 0 R", pageNum))
		objs = append(objs, fmt.Sprintf(
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 %.0f %.0f] "+
				"/Resources << /Font << /F1 3 0 R /F2 4 0 R /F3 5 0 R >> >> /Contents %d 0 R >>",
			A4W, A4H, streamNum))
		objs = append(objs, fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content))
	}

	objs[0] = "<< /Type /Catalog /Pages 2 0 R >>"
	objs[1] = fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>",
		strings.Join(kids, " "), len(d.pages))

	var b strings.Builder
	b.WriteString("%PDF-1.7\n")
	// A binary comment marks the file as containing binary-safe streams.
	b.WriteString("%\xe2\xe3\xcf\xd3\n")

	offsets := make([]int, len(objs)+1)
	for i, obj := range objs {
		offsets[i+1] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, obj)
	}

	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n", len(objs)+1)
	b.WriteString("0000000000 65535 f \n")
	for i := 1; i <= len(objs); i++ {
		fmt.Fprintf(&b, "%010d 00000 n \n", offsets[i])
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n",
		len(objs)+1, xref)
	return b.String()
}

// page is a content-stream builder with a simple vertical cursor.
type page struct {
	b    strings.Builder
	y    float64
	left float64
}

const (
	pageW  = 842.0
	pageH  = 595.0
	margin = 48.0
)

func newPage() *page {
	p := &page{y: pageH - margin, left: margin}
	// Dark background, matching the HTML.
	p.b.WriteString("0.059 0.067 0.090 rg\n")
	fmt.Fprintf(&p.b, "0 0 %.0f %.0f re f\n", pageW, pageH)
	return p
}

// textAt writes a single line at an absolute position.
func (p *page) textAt(x, y float64, s string, size float64, font string, rgb string) {
	fmt.Fprintf(&p.b, "%s rg\nBT /%s %.1f Tf %.2f %.2f Td (%s) Tj ET\n",
		rgb, font, size, x, y, escapePDF(s))
}

// line writes a line and advances the cursor.
func (p *page) line(s string, size float64, font string, rgb string) {
	p.y -= size + 5
	if p.y < margin {
		return // caller starts a new page; overflow is truncated, never clipped
	}
	p.textAt(p.left, p.y, s, size, font, rgb)
}

func (p *page) gap(h float64) { p.y -= h }

// rect fills a rectangle.
func (p *page) rect(x, y, w, h float64, rgb string) {
	fmt.Fprintf(&p.b, "%s rg %.2f %.2f %.2f %.2f re f\n", rgb, x, y, w, h)
}

// horizontalRule draws a thin line across the content width.
func (p *page) horizontalRule() {
	fmt.Fprintf(&p.b, "0.145 0.164 0.212 RG 0.7 w %.0f %.2f m %.0f %.2f l S\n",
		margin, p.y, pageW-margin, p.y)
	p.y -= 10
}

// Colors as PDF rgb operators.
const (
	pdfBg     = "0.059 0.067 0.090"
	pdfText   = "0.902 0.914 0.937"
	pdfMuted  = "0.545 0.576 0.655"
	pdfDanger = "0.898 0.282 0.302"
	pdfWarn   = "0.961 0.651 0.137"
	pdfOk     = "0.247 0.725 0.314"
	pdfPanel  = "0.094 0.106 0.141"
)

// buildPDFPages lays the report out, starting a new page when the cursor runs out.
func buildPDFPages(rep model.Report, opts Options, title string, ch charts) []string {
	var pages []string
	p := newPage()

	// Title block.
	p.textAt(margin, pageH-margin, title, 20, "F2", pdfText)
	p.y = pageH - margin - 22
	if opts.Source != "" {
		p.line("source: "+opts.Source, 9, "F3", pdfMuted)
	}
	p.line(fmt.Sprintf("baseline %s -> %s   incident %s -> %s",
		fmtTime(rep.BaselineStart), fmtTime(rep.BaselineEnd),
		fmtTime(rep.IncidentStart), fmtTime(rep.IncidentEnd)), 9, "F3", pdfMuted)
	p.gap(12)
	p.horizontalRule()

	// Verdict.
	if rep.Insufficient {
		p.line("INSUFFICIENT EVIDENCE", 16, "F2", pdfWarn)
		p.line("No anomaly passed the detection threshold in this capture, so no root cause is named.", 10, "F1", pdfMuted)
		p.line("This is a deliberate answer: a confidently wrong cause during an incident is worse than no answer.", 10, "F1", pdfMuted)
	} else if len(rep.Candidates) > 0 {
		top := rep.Candidates[0]
		p.line("MOST LIKELY ROOT CAUSE", 9, "F1", pdfMuted)
		p.line(fmt.Sprintf("%s   (score %.2f, %s confidence)", top.Service, top.Score, top.Confidence), 18, "F2", pdfDanger)
	}
	p.gap(10)

	// Ranked causes table, drawn as text columns.
	p.line("RANKED CAUSES", 9, "F1", pdfMuted)
	p.line(fmt.Sprintf("%-3s %-18s %6s %-8s %6s %6s %6s %6s",
		"#", "service", "score", "conf", "onset", "str", "expl", "notex"), 8, "F3", pdfText)
	for i, c := range rep.Candidates {
		if p.y < margin+40 {
			pages = append(pages, p.b.String())
			p = newPage()
		}
		p.line(fmt.Sprintf("%-3d %-18s %6.2f %-8s %6.2f %6.2f %6.2f %6.2f",
			i+1, trunc(c.Service, 18), c.Score, trunc(c.Confidence, 8),
			c.Breakdown.Onset, c.Breakdown.Strength,
			c.Breakdown.ExplainsDownstream, c.Breakdown.NotExplained), 9, "F3", pdfText)
	}
	p.gap(14)

	// Timeline.
	if p.y < margin+60 {
		pages = append(pages, p.b.String())
		p = newPage()
	}
	p.line("TIMELINE", 9, "F1", pdfMuted)
	for _, e := range rep.Timeline {
		if p.y < margin+30 {
			pages = append(pages, p.b.String())
			p = newPage()
		}
		p.line(fmt.Sprintf("%s  %-14s %-16s %s",
			fmtTime(e.At), trunc(e.Kind, 14), trunc(e.Service, 16), trunc(e.Detail, 46)), 9, "F3", pdfText)
	}
	p.gap(14)

	// Dependencies.
	if p.y < margin+60 {
		pages = append(pages, p.b.String())
		p = newPage()
	}
	p.line("DEPENDENCIES", 9, "F1", pdfMuted)
	p.line(fmt.Sprintf("%-18s %-18s %7s %7s %8s %8s",
		"caller", "callee", "calls", "errors", "p50", "p95"), 8, "F3", pdfMuted)
	for _, e := range rep.Edges {
		if p.y < margin+30 {
			pages = append(pages, p.b.String())
			p = newPage()
		}
		p.line(fmt.Sprintf("%-18s %-18s %7d %7d %8s %8s",
			trunc(e.Caller, 18), trunc(e.Callee, 18), e.Calls, e.Errors,
			fmtDur(e.P50), fmtDur(e.P95)), 9, "F3", pdfText)
	}
	p.gap(14)

	// Data quality.
	if p.y < margin+80 {
		pages = append(pages, p.b.String())
		p = newPage()
	}
	p.line("DATA QUALITY", 9, "F1", pdfMuted)
	p.line(fmt.Sprintf("logs accepted   %d / %d read", rep.Quality.LogsAccepted, rep.Quality.LogLinesRead), 9, "F3", pdfText)
	p.line(fmt.Sprintf("spans accepted  %d / %d read", rep.Quality.SpansAccepted, rep.Quality.SpanLinesRead), 9, "F3", pdfText)
	p.line(fmt.Sprintf("skipped         %d", rep.Quality.Skipped), 9, "F3", pdfText)
	if rep.Quality.ClockSkewLines > 0 {
		p.line(fmt.Sprintf("clock skew      %d", rep.Quality.ClockSkewLines), 9, "F3", pdfText)
	}
	if rep.Quality.OrphanSpans > 0 {
		p.line(fmt.Sprintf("orphan spans    %d", rep.Quality.OrphanSpans), 9, "F3", pdfText)
	}

	// Charts are vector art; the SVG versions ship alongside. Drawing them again
	// as PDF operators would duplicate the layout logic in a second language.
	p.gap(16)
	if p.y < margin+40 {
		pages = append(pages, p.b.String())
		p = newPage()
	}
	p.line("Charts: see charts/*.svg next to this file (vector, open in any browser).", 9, "F1", pdfMuted)
	if opts.Version != "" {
		p.line("generated by traceline "+opts.Version, 8, "F3", pdfMuted)
	}

	pages = append(pages, p.b.String())
	return pages
}

// escapePDF escapes the three characters that are special inside a PDF string
// and transliterates common typography to ASCII.
//
// The base-14 fonts use WinAnsi, and embedding a font to render "—" or "→" would
// add a binary payload to a report that is otherwise pure text. Transliterating
// keeps the document readable instead of printing "?" for every dash.
func escapePDF(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '\\':
			b.WriteString(`\\`)
		case '(':
			b.WriteString(`\(`)
		case ')':
			b.WriteString(`\)`)
		case '\n', '\r', '	':
			b.WriteByte(' ')
		// Typography that has an ASCII equivalent.
		case '—', '–', '−':
			b.WriteString("-")
		case '→':
			b.WriteString("->")
		case '←':
			b.WriteString("<-")
		case '·':
			b.WriteString(".")
		case '“', '”':
			b.WriteString(`"`)
		case '‘', '’':
			b.WriteString("'")
		case '…':
			b.WriteString("...")
		case 'µ':
			b.WriteString("u")
		case '≥':
			b.WriteString(">=")
		case '≤':
			b.WriteString("<=")
		default:
			if r < 32 || r > 126 {
				b.WriteByte('?')
				continue
			}
			b.WriteRune(r)
		}
	}
	return b.String()
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n <= 1 {
		return s[:n]
	}
	return s[:n-1] + "."
}

// ensure time is used even if the layout stops referencing it directly.
var _ = time.RFC3339
