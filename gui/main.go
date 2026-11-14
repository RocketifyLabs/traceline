// Command traceline-gui is the Traceline desktop window.
//
// It is a separate Go module on purpose. A native window needs CGO and the
// platform graphics stack (Fyne pulls in GLFW and OpenGL), while the core module
// must keep proving that `GOPROXY=off CGO_ENABLED=0 go build ./...` works with no
// C toolchain at all. Two modules, one direction of dependency: gui imports the
// core, never the reverse.
//
// It talks to the core through the public library API (the traceline package),
// not by shelling out to the CLI and not by importing internal/: the window runs
// exactly the pipeline the CLI and the benchmark run, so it cannot silently
// drift from them.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/RocketifyLabs/traceline"
)

func main() {
	a := app.NewWithID("labs.rocketify.traceline")
	a.SetIcon(logo())

	w := a.NewWindow("Traceline")
	w.Resize(fyne.NewSize(1020, 700))
	w.SetContent(newUI(a, w))
	w.ShowAndRun()
}

// ui holds every widget whose state changes, so callbacks reach them without
// package-level variables.
type ui struct {
	app    fyne.App
	win    fyne.Window
	logs   *fileRow
	spans  *fileRow
	outDir *widget.Entry

	dropped *widget.Label
	runBtn  *widget.Button
	openBtn *widget.Button
	status  *widget.Label
	result  *resultView
}

func newUI(a fyne.App, w fyne.Window) fyne.CanvasObject {
	u := &ui{app: a, win: w}

	u.logs = newFileRow("logs.jsonl", "Captured log lines, one JSON object per line.", u)
	u.spans = newFileRow("spans.jsonl", "Captured spans. A report from logs alone still works.", u)

	u.outDir = widget.NewEntry()
	u.outDir.SetText(filepath.Join(userOutputDir(), "traceline-report"))
	u.outDir.SetPlaceHolder("directory the report is written to")

	u.status = widget.NewLabel("Choose the capture files, or drop them anywhere on this window.")
	u.status.Wrapping = fyne.TextWrapWord

	u.result = newResultView()

	u.runBtn = widget.NewButtonWithIcon("Analyze", theme.MediaPlayIcon(), u.analyze)
	u.runBtn.Importance = widget.HighImportance
	u.openBtn = widget.NewButtonWithIcon("Open report", theme.DocumentIcon(), u.result.openReport)
	u.openBtn.Disable()
	demoBtn := widget.NewButtonWithIcon("Generate demo", theme.MediaSkipNextIcon(), u.demo)

	u.dropped = widget.NewLabel("Drop logs.jsonl / spans.jsonl on this window")
	u.dropped.Alignment = fyne.TextAlignCenter
	dropBox := container.NewStack(widget.NewCard("", "", container.NewPadded(
		container.NewCenter(u.dropped))))

	inputs := container.NewVBox(
		sectionTitle("Telemetry"),
		u.logs.object(),
		u.spans.object(),
		sectionTitle("Output directory"),
		container.NewBorder(nil, nil, nil,
			widget.NewButtonWithIcon("", theme.FolderIcon(), u.pickOutDir), u.outDir),
	)

	left := container.NewVBox(
		container.NewPadded(inputs),
		container.NewPadded(dropBox),
		widget.NewSeparator(),
		container.NewPadded(container.NewHBox(u.runBtn, demoBtn, u.openBtn)),
		container.NewPadded(u.status),
	)

	split := container.NewHSplit(container.NewVScroll(left), u.result.object())
	split.SetOffset(0.40)

	// Window-wide drop handling: name first, content sniffing second.
	w.SetOnDropped(func(_ fyne.Position, uris []fyne.URI) {
		for _, uri := range uris {
			if uri.Scheme() != "file" {
				continue
			}
			u.assignDropped(uri.Path())
		}
	})
	return split
}

// assignDropped routes a dropped file to the logs or spans slot. The filename is
// the fast path; an unrecognized name is sniffed by content, so `export-3.jsonl`
// still lands in the right place.
func (u *ui) assignDropped(path string) {
	base := filepath.Base(path)
	lower := strings.ToLower(base)
	switch {
	case strings.Contains(lower, "span") || strings.Contains(lower, "trace"):
		u.spans.set(path)
		u.dropped.SetText("spans ← " + base)
	case strings.Contains(lower, "log"):
		u.logs.set(path)
		u.dropped.SetText("logs ← " + base)
	case looksLikeSpans(path):
		u.spans.set(path)
		u.dropped.SetText("spans ← " + base)
	default:
		u.logs.set(path)
		u.dropped.SetText("logs ← " + base)
	}
}

// analyze is the button handler: it calls the same library entry point another
// program would use, which runs analyze.Run and report.WriteAll inside the core.
func (u *ui) analyze() {
	if u.logs.path == "" && u.spans.path == "" {
		u.setStatus("Choose at least one file — logs.jsonl is usually the one with the answers.", true)
		return
	}
	outDir := strings.TrimSpace(u.outDir.Text)
	if outDir == "" {
		u.setStatus("Choose an output directory.", true)
		return
	}

	u.runBtn.Disable()
	u.setStatus("Analyzing…", false)

	// The work runs off the UI goroutine so a large capture does not freeze the
	// window. Fyne queues widget refreshes, so updating from here is safe.
	go func() {
		defer u.runBtn.Enable()

		rep, err := traceline.AnalyzeFiles(u.logs.path, u.spans.path, outDir, "Traceline incident report")
		if err != nil {
			u.setStatus(err.Error(), true)
			return
		}

		u.result.show(rep, outDir)
		u.openBtn.Enable()
		u.setStatus(summaryLine(rep, outDir), false)
	}()
}

// demo runs a seeded incident end to end, so the window has something real to
// show on a machine with no capture on hand.
func (u *ui) demo() {
	outDir := strings.TrimSpace(u.outDir.Text)
	if outDir == "" {
		u.setStatus("Choose an output directory first.", true)
		return
	}
	u.runBtn.Disable()
	u.setStatus("Simulating an incident and analyzing it…", false)
	go func() {
		defer u.runBtn.Enable()
		rep, err := traceline.AnalyzeScenario(7, outDir)
		if err != nil {
			u.setStatus(err.Error(), true)
			return
		}
		u.result.show(rep, outDir)
		u.openBtn.Enable()
		verdict := "MISS"
		if len(rep.Candidates) > 0 && rep.Candidates[0].Service == rep.TrueCause {
			verdict = "HIT"
		}
		u.setStatus(fmt.Sprintf("Seed 7 · true cause %s · %s · %d timeline events · written to %s",
			rep.TrueCause, verdict, len(rep.Timeline), outDir), false)
	}()
}

func (u *ui) pickOutDir() {
	d := dialog.NewFolderOpen(func(lu fyne.ListableURI, err error) {
		if err != nil || lu == nil {
			return
		}
		u.outDir.SetText(filepath.Join(lu.Path(), "traceline-report"))
	}, u.win)
	d.Show()
}

func (u *ui) setStatus(msg string, isError bool) {
	u.status.SetText(msg)
	if isError {
		u.status.Importance = widget.DangerImportance
	} else {
		u.status.Importance = widget.MediumImportance
	}
	u.status.Refresh()
}

func sectionTitle(s string) *widget.Label {
	return widget.NewLabelWithStyle(s, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
}

func summaryLine(rep traceline.Report, outDir string) string {
	if rep.Insufficient {
		return "Insufficient evidence: nothing cleared the anomaly threshold. That answer is a result, " +
			"not a failure — the report says what was checked. Written to " + outDir
	}
	top := rep.Candidates[0]
	return fmt.Sprintf("Top candidate: %s (score %.2f, %s confidence) · %d timeline events · written to %s",
		top.Service, top.Score, top.Confidence, len(rep.Timeline), outDir)
}

// userOutputDir returns an output directory that exists on this platform, so the
// GUI never defaults to a path the user has to create by hand.
func userOutputDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	if runtime.GOOS == "windows" {
		return filepath.Join(home, "Documents")
	}
	return filepath.Join(home, "traceline")
}
