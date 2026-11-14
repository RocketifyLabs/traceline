package main

import (
	_ "embed"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/RocketifyLabs/traceline"
)

//go:embed assets/icon.png
var iconPNG []byte

// logo returns the window icon, embedded so the binary stays a single file.
func logo() fyne.Resource { return fyne.NewStaticResource("traceline.png", iconPNG) }

// fileRow is one capture-file input: a path entry plus browse and clear buttons.
type fileRow struct {
	title string
	hint  string
	path  string
	entry *widget.Entry
	clear *widget.Button
	owner *ui
}

func newFileRow(title, hint string, owner *ui) *fileRow {
	r := &fileRow{title: title, hint: hint, owner: owner}
	r.entry = widget.NewEntry()
	r.entry.SetPlaceHolder("no file chosen")
	r.entry.OnChanged = func(s string) { r.path = strings.TrimSpace(s) }
	r.clear = widget.NewButtonWithIcon("", theme.ContentClearIcon(), func() { r.set("") })
	r.clear.Disable()
	return r
}

func (r *fileRow) set(path string) {
	r.path = path
	r.entry.SetText(path)
	if path == "" {
		r.clear.Disable()
	} else {
		r.clear.Enable()
	}
}

func (r *fileRow) object() fyne.CanvasObject {
	title := widget.NewLabelWithStyle(r.title, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	hint := widget.NewLabel(r.hint)
	hint.TextStyle = fyne.TextStyle{Italic: true}
	hint.Wrapping = fyne.TextWrapWord

	browse := widget.NewButtonWithIcon("", theme.FolderOpenIcon(), func() {
		d := dialog.NewFileOpen(func(rc fyne.URIReadCloser, err error) {
			if err != nil || rc == nil {
				return
			}
			defer rc.Close()
			r.set(rc.URI().Path())
			r.owner.dropped.SetText(fmt.Sprintf("%s ← %s",
				strings.TrimSuffix(r.title, ".jsonl"), filepath.Base(rc.URI().Path())))
		}, r.owner.win)
		d.SetFilter(storage.NewExtensionFileFilter([]string{".jsonl", ".json", ".log", ".txt"}))
		d.Show()
	})

	return container.NewVBox(
		title, hint,
		container.NewBorder(nil, nil, nil, container.NewHBox(r.clear, browse), r.entry),
	)
}

// looksLikeSpans sniffs the first few KB: a span carries span_id. A file that
// cannot be read is treated as logs, which then produces a real error from the
// ingest step rather than a silent misroute.
func looksLikeSpans(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()

	buf := make([]byte, 8192)
	n, _ := f.Read(buf)
	head := string(buf[:n])
	return strings.Contains(head, `"span_id"`) || strings.Contains(head, `"parent_span_id"`)
}

// resultView is the right pane: the answer first, the evidence under it.
type resultView struct {
	verdict  *widget.Label
	truth    *widget.Label
	quality  *widget.Label
	ranked   *widget.List
	events   *widget.List
	tabs     *container.AppTabs
	rows     []string
	evRows   []string
	htmlPath string
}

func newResultView() *resultView {
	v := &resultView{}
	v.verdict = widget.NewLabel("No analysis yet")
	v.verdict.Wrapping = fyne.TextWrapWord
	v.truth = widget.NewLabel("")
	v.truth.Wrapping = fyne.TextWrapWord
	v.truth.TextStyle = fyne.TextStyle{Italic: true}
	v.quality = widget.NewLabel("")
	v.quality.Wrapping = fyne.TextWrapWord
	v.rows = []string{"run an analysis to see the ranking"}
	v.evRows = []string{}

	mono := func() fyne.CanvasObject {
		l := widget.NewLabel("")
		l.TextStyle = fyne.TextStyle{Monospace: true}
		return l
	}
	v.ranked = widget.NewList(
		func() int { return len(v.rows) },
		mono,
		func(i widget.ListItemID, o fyne.CanvasObject) { o.(*widget.Label).SetText(v.rows[i]) },
	)
	v.events = widget.NewList(
		func() int { return len(v.evRows) },
		mono,
		func(i widget.ListItemID, o fyne.CanvasObject) { o.(*widget.Label).SetText(v.evRows[i]) },
	)
	v.tabs = container.NewAppTabs(
		container.NewTabItem("Ranked causes", v.ranked),
		container.NewTabItem("Timeline", v.events),
	)
	return v
}

func (v *resultView) object() fyne.CanvasObject {
	head := container.NewVBox(
		widget.NewLabelWithStyle("Result", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		v.verdict, v.truth, v.quality,
	)
	return container.NewBorder(container.NewPadded(head), nil, nil, nil, v.tabs)
}

// show renders a report. The score breakdown stays visible next to each score
// because a ranking should be auditable, not trusted.
func (v *resultView) show(rep traceline.Report, outDir string) {
	v.htmlPath = traceline.ReportPath(outDir)
	v.truth.SetText("")
	if rep.TrueCause != "" {
		v.truth.SetText("Ground truth from the simulator (not from the analysis): " + rep.TrueCause)
	}

	v.rows = v.rows[:0]
	v.evRows = v.evRows[:0]

	switch {
	case rep.Insufficient:
		v.verdict.SetText("Insufficient evidence — nothing cleared the anomaly threshold. " +
			"That is the honest answer, and the report explains what was checked.")
	case len(rep.Candidates) == 0:
		v.verdict.SetText("No candidates were produced.")
	default:
		top := rep.Candidates[0]
		v.verdict.SetText(fmt.Sprintf("%s — score %.2f, %s confidence",
			top.Service, top.Score, top.Confidence))
	}

	for i, c := range rep.Candidates {
		v.rows = append(v.rows, fmt.Sprintf(
			"%d. %-14s %.2f  %-6s  onset %.2f · strength %.2f · explains %.2f · not-explained %.2f",
			i+1, c.Service, c.Score, c.Confidence,
			c.Onset, c.Strength, c.ExplainsDownstream, c.NotExplained))
	}
	if len(v.rows) == 0 {
		v.rows = append(v.rows, "no ranked causes")
	}

	for _, e := range rep.Timeline {
		v.evRows = append(v.evRows, fmt.Sprintf("%s  %-13s %-14s %s",
			e.At.Format("15:04:05"), e.Kind, e.Service, e.Detail))
	}
	if len(v.evRows) == 0 {
		v.evRows = append(v.evRows, "no timeline events")
	}

	// Say out loud what the input cost, instead of implying it was clean.
	if rep.Skipped > 0 {
		v.quality.SetText(fmt.Sprintf("%d lines read · %d skipped (%s)",
			rep.LinesRead, rep.Skipped, reasons(rep.SkipReasons)))
	} else if rep.LinesRead > 0 {
		v.quality.SetText(fmt.Sprintf("%d lines read · nothing skipped", rep.LinesRead))
	} else {
		v.quality.SetText("")
	}

	v.ranked.Refresh()
	v.events.Refresh()
	v.tabs.SelectIndex(0)
	v.verdict.Refresh()
	v.truth.Refresh()
	v.quality.Refresh()
}

// reasons renders the skip tally in a stable order so the line does not shuffle
// between runs.
func reasons(m map[string]int) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	// Deterministic order: sorting a handful of strings costs nothing and keeps
	// the display from changing for no reason.
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j] < keys[j-1]; j-- {
			keys[j], keys[j-1] = keys[j-1], keys[j]
		}
	}
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s ×%d", k, m[k]))
	}
	return strings.Join(parts, ", ")
}

// openReport opens the generated HTML in the system browser. It is the same file
// the CLI writes, so what the user sees is what a reviewer would see.
func (v *resultView) openReport() {
	if v.htmlPath == "" {
		return
	}
	var win fyne.Window
	if ws := fyne.CurrentApp().Driver().AllWindows(); len(ws) > 0 {
		win = ws[0]
	}
	if _, err := os.Stat(v.htmlPath); err != nil {
		dialog.ShowError(fmt.Errorf("report not found at %s", v.htmlPath), win)
		return
	}
	if err := openWithDesktop(v.htmlPath); err != nil {
		dialog.ShowError(err, win)
	}
}

// openWithDesktop hands a path to the platform's default handler. It uses the OS
// launcher directly instead of Fyne's OpenURL because that one takes a *url.URL,
// and a Windows path is not a URL until it is encoded — `file://C:/x/report.html`
// loses the drive colon and opens nothing.
func openWithDesktop(path string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		// cmd /c start takes the path as a plain string; the empty title arg is
		// required so a path containing spaces is not read as the window title.
		cmd = exec.Command("cmd", "/c", "start", "", path)
	case "darwin":
		cmd = exec.Command("open", path)
	default:
		cmd = exec.Command("xdg-open", path)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("could not open the report: %w", err)
	}
	// The launcher exits on its own; reaping it keeps no zombie behind.
	go func() { _ = cmd.Wait() }()
	return nil
}
