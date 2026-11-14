# Traceline desktop GUI

A native window around the same analysis the CLI runs: choose or drop
`logs.jsonl` / `spans.jsonl`, press **Analyze**, see the ranked causes and the
timeline on screen, then open the generated HTML report in a browser.

![Traceline logo](../assets/traceline.png)

## Why this is a separate module

The window needs a GPU surface. Fyne binds GLFW and OpenGL through CGO, so
building it requires a C toolchain, and Fyne alone pulls ~65 modules into the
dependency graph.

The core module's strongest claim is the opposite: `go.mod` has no `require`
block, and `make test-offline` proves `GOPROXY=off CGO_ENABLED=0 go build ./...`
works with no C compiler and no module fetch. Putting the GUI in the core module
would destroy that proof — a test that says "standard library only" cannot
coexist with a dependency graph full of GL bindings.

So: two modules, one direction.

```
gui/            own go.mod — Fyne, CGO, platform graphics
  imports →     github.com/RocketifyLabs/traceline  (the core, as a library)
                via the public API in api.go — never internal/
```

`gui/go.mod` carries a `replace` to `../`, so a clone builds without publishing
the core first. For a real release, drop the `replace` and pin a version.

## How it talks to the core

Through the small public API in [`../api.go`](../api.go):

```go
rep, err := traceline.AnalyzeFiles(logsPath, spansPath, outDir, "Traceline incident report")
rep, err := traceline.AnalyzeScenario(7, outDir)   // seeded demo, ground truth known
html := traceline.ReportPath(outDir)
```

It does **not** shell out to the CLI, and it does **not** import `internal/`.
Both of those would be ways for the window to drift from what the CLI and the
benchmark actually do — the failure mode this project cares about most is a
second analysis path that quietly disagrees with the one being measured.

## Build and run

A C compiler is required. On Windows that means MinGW-w64 on `PATH`:

```bash
# Windows (MinGW-w64, e.g. winlibs or MSYS2)
export PATH="/c/msys64/mingw64/bin:$PATH"   # or wherever gcc.exe lives
cd gui
go build -o build/traceline-gui.exe .
./build/traceline-gui.exe
```

```bash
# Linux — needs the GL/X11 headers for CGO
sudo apt install gcc libgl1-mesa-dev xorg-dev
cd gui && go build -o build/traceline-gui .
```

```bash
# macOS — Xcode command line tools
cd gui && go build -o build/traceline-gui .
```

Cross-compiling a CGO GUI is not practical from one machine: each OS needs its
own native toolchain. Windows builds locally; Linux and macOS builds want CI
runners. That is a property of native GUIs, not of this code.

## What the window does

| Control | Behaviour |
|---|---|
| `logs.jsonl` / `spans.jsonl` rows | Browse for a file, or type a path. Clear with the × button. |
| Drop area | Dropping anywhere on the window routes the file by name (`span`/`trace` → spans, `log` → logs) and, for an unrecognized name, by sniffing the first 8 KB for `span_id`. |
| Output directory | Where the five report artifacts are written. Defaults to `<home>/Documents/traceline-report` on Windows, `<home>/traceline/traceline-report` elsewhere. |
| **Analyze** | Runs the real pipeline. Disabled while running; the work happens off the UI goroutine so a large capture does not freeze the window. |
| **Generate demo** | Simulates an incident with the core's own simulator (seed 7), analyzes it, and shows HIT/MISS against the known true cause. |
| **Open report** | Opens the generated `report.html` — the same file the CLI writes. |

The right pane leads with the verdict (top candidate, score, confidence), then the
ranked list with the full score breakdown next to each row, then the timeline. A
ranking that cannot be audited is a ranking that gets trusted by mistake.

`Insufficient evidence` is displayed as a first-class answer, not as an error:
when nothing clears the anomaly threshold the window says so, because inventing a
cause is the failure this tool exists to avoid.

## Test

The GUI has no tests of its own, and that is a deliberate limit rather than an
oversight: driving a real window needs a display server, which would make
`make test` fail on a headless machine. Everything with logic in it — ingest,
window detection, anomaly scoring, ranking, report writing — lives in the core
module and is tested there. The GUI is glue: file pickers, labels and buttons.

The one piece of GUI-specific logic that could be tested (routing a dropped file
by name and content) is small on purpose, and its failure mode is visible
immediately: the file lands in the wrong row, on screen, before Analyze is
pressed.
