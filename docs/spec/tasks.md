# Tasks

Working rules: read the task → write the failing test → implement → `make test` →
commit `Txx: message` → tick the box here. A task is done only when its acceptance test
would fail if the behaviour regressed.

Status: `[ ]` not started · `[~]` in progress · `[x]` done and verified

---

## Phase 0 — Foundations

- [x] **T01 Scaffold.** git repo, MIT `LICENSE`, `.gitignore`, `go.mod` (no `require` block),
  `Makefile`, `.env.example`, `cmd/itm` entry point.
  **Accept:** `make build` produces a static binary; `itm --version` works.
- [x] **T02 Offline proof.** `internal/offline` asserts the module graph has no external
  dependency and that `GOPROXY=off CGO_ENABLED=0 go build ./...` succeeds; a `test` stage in
  the `Dockerfile` keeps `make test` runnable with the network off.
  **Accept:** `make test` passes locally; `docker run --rm --network none itm-test make test`
  passes; shipped image is a distroless static binary.
- [x] **T03 Spec docs.** `constitution.md`, `spec.md`, `plan.md`, `tasks.md`, `DECISIONS.md`.
  **Accept:** every FR/NFR in `spec.md` has an acceptance criterion and at least one task
  below.
- [x] **T04a Stats + determinism core.** `internal/stats` (median, MAD, robust z, nearest-rank
  percentile) and `internal/determinism` (`SortedKeys` + the test that proves map order is a
  real hazard).
  **Accept:** MAD is legitimately zero on some series and `RobustZ` still returns a finite
  value; a flat series stays below the anomaly threshold; every map-to-output path goes
  through `SortedKeys`.

## Phase 1 — Read the data

- [x] **T04 Models + tolerant ingest.** `model.py` dataclasses; `ingest.py` JSONL readers
  that skip and count malformed lines and assign stable line ids.
  **Accept:** fixture with ~5% bad lines yields the right record count and skip count; a
  truncated/garbage file never raises; shuffled line order yields byte-identical output.
- [x] **T05 Log templating.** Deterministic masking of numbers, UUIDs, hex, IPs, durations.
  **Accept:** table-driven tests; same input → same templates; distinct messages with
  different ids collapse to one template.
- [x] **T06 Dependency graph.** Edges from spans with call count, error rate, p50/p95
  (nearest-rank), roots and orphans counted separately.
  **Accept:** golden file match on a fixture; a span with no parent produces no edge; an
  orphan parent id is counted, not invented.

## Phase 2 — Find what changed

- [x] **T07 Windows + change-point.** Baseline capped at the 20 min before incident start;
  10 s bins, 5 MADs above baseline median, sustained for 2 bins; explicit flags override.
  **Accept:** detected start within 60 s of the seeded fault start; explicit window honored
  exactly and detection disabled; a single-bin spike is not reported as the incident start.
- [x] **T08 Anomaly detectors.** Error-rate spike, p95 shift, template burst, new error
  templates. Median/MAD baselines with onset, strength, evidence ids; z ≥ 3.5 threshold.
  **Accept:** one test per detector plus a "no anomaly" negative case per detector, and a
  constant-baseline case that must not divide by zero.
- [x] **T09 Timeline.** First anomaly per service, deploy/config-change events, cascade
  order.
  **Accept:** golden timeline on a fixture; events strictly ordered by timestamp with a
  stable tie-break.

## Phase 3 — Explain it

- [x] **T10 RCA ranking.** Four-factor score with per-factor breakdown, weights in
  `itm/rca_weights.json` (overridable with `--weights`), confidence labels,
  insufficient-evidence path, documented tie-break (score desc → service asc → evidence id).
  **Accept:** injected cause ranks first on fixtures; a red-herring service does not; empty
  input returns "insufficient evidence"; shuffling input order does not change the ranking.
- [x] **T11 Simulator core.** Seeded topology, traffic, log and span emission.
  **Accept:** same seed twice → identical hashes; different seeds → different scenarios.
- [x] **T12 Fault injectors.** ≥6 fault types (target 8), each with a known true cause and
  fault start time recorded in the scenario metadata. The noisy-service fault is a
  **distractor**: present in the telemetry, deliberately *not* the true cause.
  **Accept:** each injector changes observable telemetry in the injected service; the
  distractor scenario has a different `true_cause` than the noisy service.

## Phase 4 — Prove and present it

- [x] **T13 Benchmark + floor test.** `itm bench` over a seed range; top-1, top-3, mean
  time-to-detect (misses excluded from TTD, counted separately), per-fault table; writes
  `BENCHMARK.md`.
  **Accept:** a test asserts the top-1 floor (≥ 60%) so a regression fails CI.
- [x] **T14 Tune on dev seeds, freeze, evaluate held-out.** Tune with seeds 1-40 only, then
  evaluate 1001-1020 once.
  **Accept:** `BENCHMARK.md` reports held-out numbers; no tuning commit touches held-out
  seeds.
- [x] **T15 HTML report + golden test.** Self-contained HTML with inline CSS/JS/SVG.
  **Accept:** golden match on normalized HTML; a test asserts zero `http(s)://` references.
- [x] **T16 Markdown + JSON outputs.**
  **Accept:** `summary.md` and `result.json` produced in the same run; JSON round-trips.
- [x] **T17 CLI + demo command.** Real `analyze`/`simulate`/`bench`/`demo`, meaningful exit
  codes, no absolute paths in output.
  **Accept:** subprocess smoke tests for each command, including a failure exit code.
- [x] **T18 Perf test.** 200k logs + 50k spans in ≤ 30 s, marked `perf` so `make test` stays
  fast; run explicitly with `make test-perf`.
  **Accept:** a `perf`-marked test measures and asserts the budget; `make test` excludes it
  and `make test-perf` includes it.
- [x] **T19 README.** Setup, run, test, benchmark, screenshot, limitations.
  **Accept:** a fresh reader can go from clone to demo using only the README.
- [x] **T20 Static site.** `site/` with 3 pre-generated reports plus the benchmark table.
  **Accept:** opens from `file://` with no server and no network.
- [ ] **T21 Demo video.** 2-3 minutes.
  **Accept:** uploaded and linked in the README.
  **Blocked:** recording a screen-capture video is not something this environment can do.
  The script is ready in `docs/spec/spec.md` §13 and every command it calls is verified
  working, so the video is a recording session, not remaining development.
  **Workaround until then:** `site/index.html` is a live, clickable version of the same
  walkthrough (3 real reports plus the benchmark table), and `make demo` reproduces the
  demo segment exactly.
- [x] **T23 Hand-checked fixtures.** `fixtures/` with three small captures (a real incident,
  a healthy capture, a dirty capture), each documented with its expected result and asserted
  by `internal/fixtures`.
  **Accept:** each fixture loads, analyzes, and produces its documented outcome; the dirty one
  distinguishes all four rejection reasons.
- [x] **T22 Final rebuild check.** Judge-style rebuild from scratch.
  **Accept:** `make verify` passes on a clean clone, tests offline, nothing missing.
  **Verified:** a clean `git clone` builds with `GOPROXY=off CGO_ENABLED=0`, the full suite
  passes, and `itm demo --seed 7` from that clone produces byte-identical `report.html`,
  `report.pdf`, `summary.md`, `result.json` and all three SVGs compared to the local build.
