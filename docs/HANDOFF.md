# Handoff — continue in a new session

Written 2026-11-14. Everything below is verified against the working tree and the
published repo, not recalled from conversation.

**Read this first, then `docs/DECISIONS.md` and `docs/PROGRESS.md`.**

---

## Current state

| Item | Value |
|---|---|
| Working directory | `D:\Projetos\omnirush-buildhaton-guide` |
| Branch / remote | `master` → `origin` = `github.com/RocketifyLabs/traceline` |
| Repo | public, AGPL-3.0 (GitHub identifies it as `AGPL-3.0`) |
| Tasks done | 27 of 28 delivered; T21 (demo video) was **deselected**, not pending |
| Tests | 163, 17 packages, green |
| Verification | `go test -count=1 -short ./...` and `GOPROXY=off CGO_ENABLED=0 go build ./...` |
| Binary | 5.2 MB static, zero external Go dependencies |
| GUI | `gui/`, own module, Fyne 2.8.2, CGO — builds to a 45.8 MB window binary on Windows |
| Benchmark | top-1 97.0% ±1.5, top-3 100%, 500 held-out seeds |

**Nothing is a stub.** All four CLI commands do real work, and the GUI runs the
same pipeline. No mocks. See `docs/PROGRESS.md`.

---

## Decisions already made — do not reopen

| Decision | Detail |
|---|---|
| **Language** | Go 1.27, standard library only. `go.mod` has no `require` block; a test fails if one appears. Evidence: `docs/DECISIONS.md` D-001 |
| **Graphical output** | HTML (inline SVG) + vector PDF + standalone SVGs. **No PNG** as a report format — D-002. The logo is a PNG asset; that is not a report format |
| **Product name** | **Traceline** (was ITM / Incident Time Machine) |
| **Repo slug** | `traceline`, matching the module — D-004 explains why it deviates from the earlier `incident-time-machine` |
| **GitHub org** | `RocketifyLabs` (exact slug — no hyphen before "Labs") |
| **Copyright holder** | `André Luiz Buzzo de Souza` (CPF-level name, not the defunct Rocketify Consultoria CNPJ) |
| **License** | **AGPL-3.0-or-later with dual commercial licensing** — D-005 |
| **GUI architecture** | Separate `gui/` module with its own `go.mod`, importing the core as a library. Core stays zero-dependency |
| **GUI scope** | Native per OS. Demo button, drag-drop `logs.jsonl` + `spans.jsonl`, result on screen, button to open the generated HTML |
| **Replay scope** | Analysis replay (re-run with different parameters, diff two `result.json`) + temporal scrubbing. **State rewind explicitly out of scope** |

### Licensing nuance the user must not be misled about

The user originally asked for "open source but nobody may use it commercially". Those are
contradictory: the OSI definition forbids licenses that discriminate by field of use. AGPL-3.0
was chosen because it *is* genuinely open source, permits commercial use, but forces anyone
offering it as a network service to publish their own source under AGPL — which is what makes
a paid commercial license worth buying. **Do not describe the project as
"non-commercial" anywhere.** The README and LICENSE must say AGPL-3.0 plus a note that a
commercial license is available.

---

## Done in this session (2026-11-14)

1. **Renamed and published.** Module `github.com/RocketifyLabs/traceline`, `cmd/traceline`,
   binary `traceline`, every import and doc updated, site regenerated from the renamed binary.
   `internal/offline/offline_test.go` asserted the old module path and was updated — otherwise it
   would have kept passing for the wrong reason.
2. **License.** MIT → AGPL-3.0-or-later, plus `LICENSING.md` explaining the dual offer.
   The copyright notice sits at the **end** of `LICENSE`: with it at the top, GitHub reported
   `NOASSERTION / Other` instead of AGPL-3.0.
3. **Logo.** `time-machine.png` → `assets/traceline.png`, committed and used at the top of the
   README. `gui/assets/icon.png` is the same file for the window icon (embedded, so the GUI
   binary stays single-file).
4. **Repo created and pushed.** `gh repo create RocketifyLabs/traceline --public`, topics set,
   `master` pushed and tracking.
5. **GUI.** `gui/` built with Fyne 2.8.2. It reaches the core through the public API added in
   `api.go` (`AnalyzeFiles`, `AnalyzeScenario`, `ReportPath`) — **not** `internal/`, which a
   separate module cannot import, and **not** by shelling out to the CLI.
6. **Verified on Windows:** the window opened, **Generate demo** ran, and it produced the ranked
   causes (top: `orders`, 0.95, high), HIT against the simulator's ground truth, and the report
   at `C:\Users\andre\Documents\traceline-report`. Confirmed by the user running it.

---

## Work not started

### 1. Replay + scrubbing

Both are small because the pipeline is pure and deterministic.

- **Analysis replay:** run `analyze.Run` twice with different `Options` / window / weights,
  then diff the two `model.Report` values. Natural home: `internal/replay`.
- **Temporal scrubbing:** the report already contains `Timeline` with timestamps and `Edges`
  with per-edge timing. Filtering the timeline and graph by an instant is a rendering concern,
  so it belongs in `internal/report` (or the GUI), not in the analysis.
- CLI surface to add: `traceline replay` with `--window-a`, `--window-b`, `--weights-a`,
  `--weights-b`.

### 2. GUI on Linux and macOS

Windows is built and verified. Linux needs `gcc libgl1-mesa-dev xorg-dev`, macOS needs the Xcode
command line tools; neither was built here. Cross-compiling a CGO GUI from Windows is not
practical, so those want CI runners. `gui/README.md` carries the per-OS commands.

---

## Where to look

| File | Why |
|---|---|
| `docs/DECISIONS.md` | D-001 Go, D-002 no PNG, D-003 scope, D-004 name/slug, D-005 license |
| `docs/PROGRESS.md` | Task table, measured results, the 15 bugs found during the build |
| `docs/spec/constitution.md` | Non-negotiables: determinism, offline, stdlib-only, no black box |
| `docs/spec/spec.md` | FR-1..FR-12 with acceptance criteria and non-functional requirements |
| `docs/spec/tasks.md` | T01..T24 ticked; T21 deselected with the reason recorded |
| `api.go` | The public library API the GUI calls |
| `gui/README.md` | The window: why it is a second module, how to build it per OS |
| `fixtures/README.md` | Three hand-checked captures with expected results |

## Traps already hit — do not repeat

1. **`make` does not exist on this Windows host.** Use `go run ./cmd/traceline ...` and
   `go test`. The `Makefile` is for Linux/Docker.
2. **Go randomizes map iteration.** Every map reaching an output must go through
   `internal/determinism.SortedKeys`. A test proves the hazard is real.
3. **MAD of zero makes any change score ~1e17 sigma.** Detectors apply absolute gates
   (`MinErrorRateDelta`, `MinLatencyRatio`) on top of the z-score. Never rely on z alone.
4. **`template_burst` must never create a candidate.** Healthy services burst templates at the
   fault boundary; letting bursts create candidates made a healthy service score 1.000.
5. **Window detection must reference the lower half of bin rates**, not all bins. With half
   the capture incident the median lands mid-shift and nothing can be flagged.
6. **A 20-scenario accuracy is meaningless** (±11 points). Report the confidence interval and
   use the 500-scenario set for any headline number.
7. **A module path that does not match its repo breaks `go get`/`go install`.** Repo, module and
   binary are all `traceline` for that reason (D-004).
8. **GitHub reads the license from the top of `LICENSE`.** Copyright notice at the end, or the
   repo shows `NOASSERTION`.
9. **A separate Go module cannot import `internal/`.** The GUI had to go through `api.go`. If a
   new capability is needed in the window, extend `api.go` — do not move code into `gui/` that
   belongs in the core.
10. **Fyne needs CGO.** `CGO_ENABLED=0` fails on `go-gl/gl` build constraints. On this machine
    MinGW-w64 must be on `PATH`; without a C compiler the first Fyne build also takes several
    minutes of cgo compilation, so do not read a slow first build as a hang.
11. **`write_file` refuses to overwrite files it has not read** in the current session. Read
    first, or `rm` then write.
12. **A blind global rename corrupts its own documentation.** The phrase "Incident Time Machine"
    in this file became "Traceline" in a sentence that was about the *old* name. Rewrite docs
    that talk about the rename by hand; do not let a search-and-replace touch them.

## Demo video — deselected, not pending

**T21 was closed on 2026-11-14.** The buildathon deadline has passed, so a submission video has
no consumer; the owner will record one in the future only if a concrete need appears. Do not
treat it as backlog and do not re-propose it.

Nothing is blocked by it, and no walkthrough script is kept for it: the commands a recording
would show are the ones in the README, all verified. `site/index.html` is a live clickable
walkthrough, and the GUI's **Generate demo** button produces a full run in one click.
