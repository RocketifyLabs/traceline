# Decisions

A short, dated log of choices that were expensive to reverse. Each entry states the
decision, the measured evidence behind it, and what would have to change for it to be
revisited. This is the file to read when someone asks "why is it like this?".

---

## D-001 — Language: Go 1.27, standard library only

**Decided:** 2026-11-10 · **Status:** accepted

**Context.** The original product brief specified Python 3.12 with a standard-library-only
runtime. Before committing to it, the same workload was implemented in both languages and
measured on this machine, on identical input, doing identical work (JSONL ingest, log
templating, median/MAD per service, nearest-rank p50/p95).

| | Python 3.12 stdlib | Go 1.27 |
|---|---|---|
| 200k log lines | 1.29 s | **0.94 s** |
| 1M log lines | 6.71 s | **4.67 s** |
| Container image | 327 MB | **10.4 MB** |
| Build artifacts | uv + uv.lock + .venv | single binary, `go build` |
| Offline build | `uv sync --locked` | `GOPROXY=off go build` |
| Test runner | pytest (installed at build) | `go test` (built in) |

**Decision.** Go, not Python.

**Why the speed number alone would have been the wrong reason.** 1.4× on a 30 s budget is
noise; the performance argument does not justify a rewrite. The load-bearing argument is the
**rebuild**, which is a stated judging criterion: a 10.4 MB static binary in a distroless
image has no interpreter, no virtualenv, no lockfile resolution, and nothing to break on a
reviewer's machine. That is the "small image, trivial offline rebuild, nothing to break"
property the original brief was actually asking for, achieved better.

**Accepted cost.** Go randomizes map iteration order by design, which is a direct threat to
the byte-identical-output requirement. This is mitigated structurally: every map that reaches
any output is iterated through an explicitly sorted key slice, and this is enforced by the
rules in `constitution.md` §1 and by determinism tests per module. Verified: three
consecutive runs of the 200k-line analysis produced identical output hashes.

**Revisit if:** the project needs Python-only ecosystem tooling, or the team maintaining it is
Python-fluent and not Go-fluent. Neither applies now.

---

## D-002 — Graphical outputs: HTML + PDF + standalone SVGs, no PNG

**Decided:** 2026-11-10 · **Status:** accepted

**Context.** Three output formats were prototyped stdlib-only and judged by rendering them and
inspecting the result:

| Format | Result | Verdict |
|---|---|---|
| SVG | 1.5 KB, crisp text, correct labels and values when rendered and measured in the DOM | best |
| PDF | 1.7 KB, structurally valid, text extractable by a third-party parser | good, printable |
| PNG (Go) | 4.9 KB, **illegible** — stdlib cannot rasterize fonts | unusable |
| PNG (Python) | 2.3 KB, legible only after hand-writing a 5×7 bitmap font | poor |

**Decision.** Ship HTML (inline SVG), PDF (vector, written as PDF operators), and each chart
as a standalone `.svg`. **No PNG.**

**Why.** Raster text needs a font renderer or a hand-rolled bitmap font; both were measured as
strictly worse than vector output. SVG carries real text, scales without loss, is
golden-testable and diffs in git. Anyone who needs a raster can export it from the browser
that already renders the HTML.

**Acceptance.** A test parses `report.pdf` and asserts page count and extractable text; each
standalone SVG must parse as XML; the HTML golden test additionally asserts zero `http(s)://`
references.

---

## D-003 — Scope posture: open-source project, not a timed submission

**Decided:** 2026-11-10 · **Status:** accepted

**Context.** The brief's submission deadline (20 Oct 2026) had already passed when work began,
and the original six-day budget no longer applies.

**Decision.** Build to open-source quality rather than to a deadline: complete feature set,
documented decisions, tests that assert behaviour rather than smoke-test it, and no
"cut order" pressure. The product may later become a commercial tool.

**What this does not change.** Nothing in the acceptance criteria relaxes. Tests, offline
rebuild, README and the demo remain non-negotiable — the difference is that optional
robustness (more fault types, better PDF layout, a proper site) is now in scope instead of
being the first to be cut.

---

## D-004 — Name, module path and repository slug

**Decided:** 2026-11-13 · **Status:** accepted

**Context.** The product was called ITM (Incident Time Machine) during the build. The handoff
fixed the new product name as **Traceline** but left the repository slug as
`incident-time-machine`, and asked for the Go module to become
`github.com/RocketifyLabs/traceline`.

**Decision.** Repo, module and binary are all **`traceline`**:
`github.com/RocketifyLabs/traceline`, `cmd/traceline`, binary `traceline`.

**Why the slug changed too.** A Go module path that does not match its repository is not
merely untidy: `go get`/`go install github.com/RocketifyLabs/incident-time-machine` resolves
the module and then fails the path check, so the `go install` line in the README would not
work. Matching repo and module keeps `go install` a one-liner and avoids a permanent
redirect to explain in every issue thread.

**Cost.** The GitHub search terms "incident time machine" no longer match the repo name; the
README's first paragraph still carries the phrase, and the repo description/topics mention it
so search still finds it. Renaming a repo later is cheap on GitHub (it redirects), so this is
reversible.

---

## D-005 — License: AGPL-3.0 with a dual commercial offer

**Decided:** 2026-11-13 · **Status:** accepted

**Decision.** `LICENSE` is AGPL-3.0-or-later (copyright André Luiz Buzzo de Souza), and
`LICENSING.md` explains the optional commercial license.

**Why not a "no commercial use" license.** See `LICENSING.md`: the OSI definition forbids
field-of-use restrictions, so such a license would not be open source. AGPL is genuinely open
source, permits commercial use, and its §13 network clause is exactly the pressure that makes
a paid license worth buying for anyone who wants to keep modifications private.

**Do not** describe the project as non-commercial anywhere in the docs, the site or the repo
description.
