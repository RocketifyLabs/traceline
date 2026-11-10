// Package svg renders the charts as plain SVG text.
//
// SVG is a text format, so it needs no dependency, scales without loss, diffs in
// git and can be golden-tested. Every value is written with a fixed formatter and
// every label is escaped, so a log message can never break the document or the
// determinism of its hash.
package svg

import (
	"fmt"
	"html"
	"sort"
	"strings"
)

// Palette is fixed: the same input must produce the same bytes, and a random
// colour would break that. Colours are chosen for contrast on a dark background.
const (
	ColBg     = "#0f1117"
	ColPanel  = "#181b24"
	ColText   = "#e6e9ef"
	ColMuted  = "#8b93a7"
	ColGrid   = "#252a36"
	ColAccent = "#4c8dff"
	ColWarn   = "#f5a623"
	ColDanger = "#e5484d"
	ColOk     = "#3fb950"
)

// num formats a coordinate deterministically: at most two decimals, no exponent,
// no locale.
func num(v float64) string {
	s := fmt.Sprintf("%.2f", v)
	s = strings.TrimRight(s, "0")
	s = strings.TrimRight(s, ".")
	if s == "" || s == "-" {
		return "0"
	}
	return s
}

// Builder accumulates SVG elements.
type Builder struct {
	width, height int
	elems         []string
}

// New starts an SVG document of the given size.
func New(width, height int) *Builder {
	b := &Builder{width: width, height: height}
	b.rect(0, 0, float64(width), float64(height), ColBg, 0)
	return b
}

// String closes and returns the document.
func (b *Builder) String() string {
	var sb strings.Builder
	fmt.Fprintf(&sb, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %d %d" width="%d" height="%d" font-family="-apple-system, Segoe UI, Roboto, Helvetica, Arial, sans-serif">`,
		b.width, b.height, b.width, b.height)
	for _, e := range b.elems {
		sb.WriteString(e)
	}
	sb.WriteString("</svg>")
	return sb.String()
}

func (b *Builder) rect(x, y, w, h float64, fill string, radius float64) {
	if radius > 0 {
		b.elems = append(b.elems, fmt.Sprintf(
			`<rect x="%s" y="%s" width="%s" height="%s" rx="%s" fill="%s"/>`,
			num(x), num(y), num(w), num(h), num(radius), fill))
		return
	}
	b.elems = append(b.elems, fmt.Sprintf(
		`<rect x="%s" y="%s" width="%s" height="%s" fill="%s"/>`,
		num(x), num(y), num(w), num(h), fill))
}

func (b *Builder) line(x1, y1, x2, y2 float64, stroke string, width float64) {
	b.elems = append(b.elems, fmt.Sprintf(
		`<line x1="%s" y1="%s" x2="%s" y2="%s" stroke="%s" stroke-width="%s"/>`,
		num(x1), num(y1), num(x2), num(y2), stroke, num(width)))
}

// text writes a label. The content is HTML-escaped: it may come from a log line.
func (b *Builder) text(x, y float64, s string, size int, fill string, anchor string) {
	if anchor == "" {
		anchor = "start"
	}
	b.elems = append(b.elems, fmt.Sprintf(
		`<text x="%s" y="%s" font-size="%d" fill="%s" text-anchor="%s">%s</text>`,
		num(x), num(y), size, fill, anchor, html.EscapeString(s)))
}

// Path draws a polyline from points.
func (b *Builder) polyline(pts [][2]float64, stroke string, width float64) {
	if len(pts) < 2 {
		return
	}
	var d strings.Builder
	for i, p := range pts {
		if i == 0 {
			fmt.Fprintf(&d, "M %s %s", num(p[0]), num(p[1]))
			continue
		}
		fmt.Fprintf(&d, " L %s %s", num(p[0]), num(p[1]))
	}
	b.elems = append(b.elems, fmt.Sprintf(
		`<path d="%s" fill="none" stroke="%s" stroke-width="%s" stroke-linejoin="round"/>`,
		d.String(), stroke, num(width)))
}

// circle draws a node.
func (b *Builder) circle(cx, cy, r float64, fill string) {
	b.elems = append(b.elems, fmt.Sprintf(
		`<circle cx="%s" cy="%s" r="%s" fill="%s"/>`, num(cx), num(cy), num(r), fill))
}

// --- charts ---

// Bar is one bar of a bar chart.
type Bar struct {
	Label  string
	Value  float64
	Max    float64 // scale; if zero, the largest value is used
	Accent bool    // highlight this bar (the top candidate)
	Note   string  // optional value label
}

// BarChart draws a horizontal bar chart.
//
// Horizontal, not vertical: service names are long and a vertical chart forces
// either rotated labels or truncation. Readability beats convention.
func BarChart(title string, bars []Bar, width, height int) string {
	b := New(width, height)
	b.text(20, 30, title, 15, ColText, "")

	if len(bars) == 0 {
		b.text(20, 60, "no data", 13, ColMuted, "")
		return b.String()
	}

	max := 0.0
	for _, bar := range bars {
		if bar.Max > max {
			max = bar.Max
		}
		if bar.Value > max {
			max = bar.Value
		}
	}
	if max <= 0 {
		max = 1
	}

	const (
		labelW = 150.0
		top    = 50.0
		rowH   = 30.0
	)
	barW := float64(width) - labelW - 90

	for i, bar := range bars {
		y := top + float64(i)*rowH
		label := bar.Label
		if len(label) > 18 {
			label = label[:17] + "…"
		}
		b.text(labelW-12, y+18, label, 13, ColText, "end")

		w := (bar.Value / max) * barW
		if w < 1 && bar.Value > 0 {
			w = 1
		}
		col := ColAccent
		if bar.Accent {
			col = ColDanger
		}
		// Track, so a near-zero bar is still visible as a row.
		b.rect(labelW, y+6, barW, 16, ColGrid, 3)
		if w > 0 {
			b.rect(labelW, y+6, w, 16, col, 3)
		}

		note := bar.Note
		if note == "" {
			note = num(bar.Value)
		}
		b.text(labelW+barW+10, y+19, note, 12, ColMuted, "")
	}
	return b.String()
}

// Point is one sample of a time series.
type Point struct {
	At    float64 // seconds from the incident start
	Value float64
}

// Series is one line of a line chart.
type Series struct {
	Label  string
	Points []Point
	Color  string
	// Marker is drawn where the anomaly begins.
	Marker float64
}

// LineChart draws an error-rate-over-time chart with an optional incident marker.
func LineChart(title string, series []Series, width, height int, yMax float64, markerLabel string) string {
	b := New(width, height)
	b.text(20, 30, title, 15, ColText, "")

	if len(series) == 0 {
		b.text(20, 60, "no data", 13, ColMuted, "")
		return b.String()
	}

	const (
		left   = 60.0
		right  = 20.0
		top    = 55.0
		bottom = 40.0
	)
	plotW := float64(width) - left - right
	plotH := float64(height) - top - bottom

	// Time and value bounds.
	maxT := 1.0
	for _, s := range series {
		for _, p := range s.Points {
			if p.At > maxT {
				maxT = p.At
			}
		}
	}
	if yMax <= 0 {
		for _, s := range series {
			for _, p := range s.Points {
				if p.Value > yMax {
					yMax = p.Value
				}
			}
		}
	}
	if yMax <= 0 {
		yMax = 1
	}

	xFor := func(t float64) float64 { return left + (t/maxT)*plotW }
	yFor := func(v float64) float64 { return top + plotH - (v/yMax)*plotH }

	// Axes.
	b.line(left, top, left, top+plotH, ColGrid, 1)
	b.line(left, top+plotH, left+plotW, top+plotH, ColGrid, 1)
	for i := 0; i <= 4; i++ {
		v := yMax * float64(i) / 4
		y := yFor(v)
		b.line(left, y, left+plotW, y, ColGrid, 1)
		b.text(left-8, y+4, num(v), 10, ColMuted, "end")
	}
	b.text(left, top+plotH+22, "0s", 10, ColMuted, "middle")
	b.text(left+plotW, top+plotH+22, num(maxT)+"s", 10, ColMuted, "middle")

	colors := []string{ColAccent, ColWarn, ColDanger, ColOk, "#a371f7", "#39c5cf"}
	for i, s := range series {
		col := s.Color
		if col == "" {
			col = colors[i%len(colors)]
		}
		pts := make([][2]float64, 0, len(s.Points))
		for _, p := range s.Points {
			pts = append(pts, [2]float64{xFor(p.At), yFor(p.Value)})
		}
		b.polyline(pts, col, 2)
	}

	// Incident marker as a vertical dashed line.
	for _, s := range series {
		if s.Marker > 0 {
			x := xFor(s.Marker)
			b.line(x, top, x, top+plotH, ColDanger, 1)
			if markerLabel != "" {
				b.text(x+5, top+12, markerLabel, 10, ColDanger, "")
			}
			break
		}
	}

	// Legend, in series order.
	for i, s := range series {
		col := s.Color
		if col == "" {
			col = colors[i%len(colors)]
		}
		x := left + float64(i)*140
		y := float64(height) - 10
		b.rect(x, y-8, 10, 10, col, 2)
		b.text(x+16, y+1, s.Label, 11, ColMuted, "")
	}
	return b.String()
}

// Node is one service in the dependency graph.
type Node struct {
	Name string
	// Layer places the node; lower is closer to the entry service.
	Layer  int
	Error  float64 // 0..1, drives the colour
	Accent bool    // the top-ranked candidate
}

// EdgeView is a drawn dependency edge.
type EdgeView struct {
	From, To string
	Errors   int
}

// DependencyGraph draws the call graph in layers.
//
// Layered left-to-right: the entry service on the left, dependencies to the
// right. That direction matches how engineers read a call chain, and puts the
// root cause (usually deepest) on the far right where it is easy to spot.
func DependencyGraph(nodes []Node, edges []EdgeView, width, height int) string {
	b := New(width, height)
	b.text(20, 26, "service dependency graph", 15, ColText, "")

	if len(nodes) == 0 {
		b.text(20, 56, "no spans in this capture", 13, ColMuted, "")
		return b.String()
	}

	// Group by layer, sorted by name inside each layer.
	byLayer := map[int][]Node{}
	var layers []int
	for _, n := range nodes {
		if _, ok := byLayer[n.Layer]; !ok {
			layers = append(layers, n.Layer)
		}
		byLayer[n.Layer] = append(byLayer[n.Layer], n)
	}
	sort.Ints(layers)
	for _, l := range layers {
		ns := byLayer[l]
		sort.Slice(ns, func(i, j int) bool { return ns[i].Name < ns[j].Name })
		byLayer[l] = ns
	}

	const (
		topPad  = 60.0
		leftPad = 90.0
	)
	usableW := float64(width) - 2*leftPad
	usableH := float64(height) - topPad - 40
	layerGap := 0.0
	if len(layers) > 1 {
		layerGap = usableW / float64(len(layers)-1)
	}

	pos := map[string][2]float64{}
	for li, l := range layers {
		ns := byLayer[l]
		x := leftPad + float64(li)*layerGap
		gapY := 0.0
		if len(ns) > 1 {
			gapY = usableH / float64(len(ns)-1)
		} else {
			gapY = 0
		}
		for i, n := range ns {
			y := topPad + float64(i)*gapY
			if len(ns) == 1 {
				y = topPad + usableH/2
			}
			pos[n.Name] = [2]float64{x, y}
		}
	}

	// Edges first, so nodes draw on top.
	nodeRadius := 26.0
	for _, e := range edges {
		from, ok1 := pos[e.From]
		to, ok2 := pos[e.To]
		if !ok1 || !ok2 {
			continue
		}
		col := ColGrid
		width := 1.0
		if e.Errors > 0 {
			col = ColDanger
			width = 2
		}
		b.line(from[0]+nodeRadius, from[1], to[0]-nodeRadius, to[1], col, width)
		if e.Errors > 0 {
			mx := (from[0] + to[0]) / 2
			my := (from[1]+to[1])/2 - 4
			b.text(mx, my, fmt.Sprintf("%d err", e.Errors), 9, ColDanger, "middle")
		}
	}

	for _, n := range nodes {
		p, ok := pos[n.Name]
		if !ok {
			continue
		}
		col := ColOk
		switch {
		case n.Error >= 0.25:
			col = ColDanger
		case n.Error >= 0.05:
			col = ColWarn
		}
		if n.Accent {
			b.circle(p[0], p[1], nodeRadius+5, ColDanger)
		}
		b.circle(p[0], p[1], nodeRadius, col)
		// Label under the node, escaped.
		name := n.Name
		if len(name) > 12 {
			name = name[:11] + "…"
		}
		b.text(p[0], p[1]+nodeRadius+16, name, 11, ColText, "middle")
	}
	return b.String()
}
