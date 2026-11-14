# Handoff — continue in a new session

Written 2026-11-13. Everything below is verified against the working tree, not recalled
from conversation.

**Read this first, then `docs/DECISIONS.md` and `docs/PROGRESS.md`.**

---

## Current state

| Item | Value |
|---|---|
| Working directory | `D:\Projetos\omnirush-buildhaton-guide` |
| Branch / last commit | `master` @ `f272719` |
| Tasks done | 23 of 24 (`docs/spec/tasks.md`) |
| Tests | 163, 17 packages, green |
| Verification | `go test -count=1 -short ./...` and `docker run --rm --network none itm-test make test` |
| Binary | 5.2 MB static, zero external Go dependencies |
| Image | 11.3 MB distroless |
| Benchmark | top-1 97.0% ±1.5, top-3 100%, 500 held-out seeds |
| Untracked | `time-machine.png` (46 KB, 512×512) — not committed, purpose unknown |

**Nothing is a stub.** All four CLI commands do real work. No mocks. See `docs/PROGRESS.md`.

---

## Decisions already made — do not reopen

These were argued and settled. Re-asking wastes the user's time.

| Decision | Detail |
|---|---|
| **Language** | Go 1.27, standard library only. `go.mod` has no `require` block; a test fails if one appears. Evidence and rejected alternative: `docs/DECISIONS.md` D-001 |
| **Graphical output** | HTML (inline SVG) + vector PDF + standalone SVGs. **No PNG** — D-002 |
| **New product name** | **Traceline** (was ITM / Incident Time Machine) |
| **Repo slug** | `incident-time-machine` |
| **GitHub org** | `RocketifyLabs` (exact slug — no hyphen before "Labs") |
| **Copyright holder** | `André Luiz Buzzo de Souza` (CPF-level name, not the defunct Rocketify Consultoria CNPJ) |
| **License** | **AGPL-3.0 with dual commercial licensing** |
| **GUI architecture** | Option A: separate `gui/` module with its own `go.mod`, importing the core as a library. Core stays zero-dependency |
| **GUI scope** | Native per OS. "Gerar demo" button, drag-drop `logs.jsonl` + `spans.jsonl`, result shown on screen, button to open the generated HTML |
| **Replay scope** | Analysis replay (re-run with different parameters, diff two `result.json`) + temporal scrubbing of the timeline. **State rewind explicitly out of scope** |

### Licensing nuance the user must not be misled about

The user originally asked for "open source but nobody may use it commercially". Those are
contradictory: the OSI definition forbids licenses that discriminate by field of use. AGPL-3.0
was chosen because it *is* genuinely open source, permits commercial use, but forces anyone
offering it as a network service to publish their own source under AGPL — which is what makes
a paid commercial license worth buying. **Do not describe the project as
"non-commercial" anywhere.** The README and LICENSE must say AGPL-3.0 plus a note that a
commercial license is available.

---

## Work not started

### 1. Rename and publish (blocking everything else)

Nothing has been renamed or pushed. No git remote exists. `gh` is authenticated as
`andrelbuzzo` with `repo`, `workflow`, `admin:org` scopes.

Required steps:

1. Rename module `github.com/andrelbuzzo/itm` → `github.com/RocketifyLabs/traceline` in
   `go.mod`, plus every import (~14 files), `cmd/itm` → `cmd/traceline`, the binary name, the
   `Dockerfile`, `Makefile`, CLI help strings, README, and `docs/`.
2. Replace `LICENSE` (currently MIT) with AGPL-3.0, copyright
   `André Luiz Buzzo de Souza`, plus a short `LICENSING.md` explaining the dual-license offer.
3. Create the public repo: `gh repo create RocketifyLabs/incident-time-machine --public`.
4. Push.
5. Decide whether `time-machine.png` belongs in the repo.

**Watch out:** `internal/offline/offline_test.go` hardcodes the module path
`github.com/andrelbuzzo/itm` for its dependency-graph assertion. It must be updated with the
rename or the test silently passes for the wrong reason.

### 2. GUI module (Option A)

Not started. Create `gui/` with its own `go.mod`. Constraints and evidence:

- Native cross-OS GUI requires CGO + third-party deps. Verified on this machine:
  **Fyne fails to build with `CGO_ENABLED=0`** (`go-gl/gl` build constraints), and pulls
  ~34 indirect modules, 65 in the graph. Wails and Gio have the same CGO requirement.
- Therefore `gui/` must be a **separate module** so `make test-offline` on the core keeps
  proving `GOPROXY=off CGO_ENABLED=0 go build ./...`.
- Cross-compiling a CGO GUI is not practical from this machine. Windows builds locally;
  Linux and macOS builds need CI runners.
- The GUI must call `analyze.Run` and `report.WriteAll` **directly**, not shell out to the
  CLI binary.

### 3. Replay + scrubbing

Not started. Both are small because the pipeline is pure and deterministic.

- **Analysis replay:** run `analyze.Run` twice with different `Options` / window / weights,
  then diff the two `model.Report` values. Natural home: `internal/replay`.
- **Temporal scrubbing:** the report already contains `Timeline` with timestamps and `Edges`
  with per-edge timing. Filtering the timeline and graph by an instant is a rendering concern,
  so it belongs in `internal/report` (or the GUI), not in the analysis.
- CLI surface to add: `itm replay` (or `traceline replay`) with `--window-a`, `--window-b`,
  `--weights-a`, `--weights-b`.

---

## Where to look

| File | Why |
|---|---|
| `docs/DECISIONS.md` | D-001 Go, D-002 no PNG, D-003 scope. Read before proposing stack changes |
| `docs/PROGRESS.md` | Task table, measured results, the 15 bugs found during the build |
| `docs/spec/constitution.md` | Non-negotiables: determinism, offline, stdlib-only, no black box |
| `docs/spec/spec.md` | FR-1..FR-12 with acceptance criteria, §13 demo video script |
| `docs/spec/tasks.md` | T01..T23, all ticked except T21 (video) |
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
7. **`write_file` refuses to overwrite files it has not read** in the current session. Read
   first, or `rm` then write.

## Remaining open item

**T21 — demo video.** Cannot be recorded in this environment. The script is written in
`docs/spec/spec.md` §13 and every command in it is verified working. `site/index.html` is a
live clickable substitute.
