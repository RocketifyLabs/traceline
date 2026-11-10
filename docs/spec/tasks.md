# Tasks

Working rules: read the task → write the failing test → implement → `make test` →
commit `Txx: message` → tick the box here. A task is done only when its acceptance test
would fail if the behaviour regressed.

Status: `[ ]` not started · `[~]` in progress · `[x]` done and verified

---

## Phase 0 — Foundations

- [x] **T01 Scaffold.** git repo, MIT `LICENSE`, `.gitignore`, `.python-version` (3.12),
  `pyproject.toml` (stdlib runtime, dev group), `Makefile`, `.env.example`.
  **Accept:** `uv sync` succeeds; `uv run itm --version` works.
- [x] **T02 Docker + offline proof.** `Dockerfile` installing dev deps at build time; the
  suite blocks sockets globally.
  **Accept:** `make test` passes locally; `make verify` documented for the reviewer.
- [x] **T03 Spec docs.** `constitution.md`, `spec.md`, `plan.md`, `tasks.md`.
  **Accept:** every FR/NFR in `spec.md` has an acceptance criterion and at least one task
  below.

## Phase 1 — Read the data

- [ ] **T04 Models + tolerant ingest.** `model.py` dataclasses; `ingest.py` JSONL readers
  that skip and count malformed lines and assign stable line ids.
  **Accept:** fixture with ~5% bad lines yields the right record count and skip count; a
  truncated/garbage file never raises.
- [ ] **T05 Log templating.** Deterministic masking of numbers, UUIDs, hex, IPs, durations.
  **Accept:** table-driven tests; same input → same templates; distinct messages with
  different ids collapse to one template.
- [ ] **T06 Dependency graph.** Edges from spans with call count, error rate, p50/p95.
  **Accept:** golden file match on a fixture; a span with no parent produces no edge.

## Phase 2 — Find what changed

- [ ] **T07 Windows + change-point.** Baseline vs incident window; auto-detect via
  error-rate change-point; explicit `--incident-start/--incident-end` overrides.
  **Accept:** detected start within 60 s of the seeded fault start; explicit window is
  honored exactly.
- [ ] **T08 Anomaly detectors.** Error-rate spike, p95 shift, template burst, new error
  templates. Median/MAD baselines with onset, strength, evidence ids.
  **Accept:** one test per detector plus a "no anomaly" negative case per detector.
- [ ] **T09 Timeline.** First anomaly per service, deploy/config-change events, cascade
  order.
  **Accept:** golden timeline on a fixture; events strictly ordered by timestamp with a
  stable tie-break.

## Phase 3 — Explain it

- [ ] **T10 RCA ranking.** Four-factor score with per-factor breakdown, weights in
  `itm/rca_weights.json`, confidence labels, insufficient-evidence path.
  **Accept:** injected cause ranks first on fixtures; a red-herring service does not; empty
  input returns "insufficient evidence".
- [ ] **T11 Simulator core.** Seeded topology, traffic, log and span emission.
  **Accept:** same seed twice → identical hashes; different seeds → different scenarios.
- [ ] **T12 Fault injectors.** ≥6 fault types (target 8), each with a known true cause and
  fault start time recorded in the scenario metadata.
  **Accept:** each injector changes observable telemetry in the injected service.

## Phase 4 — Prove and present it

- [ ] **T13 Benchmark + floor test.** `itm bench` over a seed range; top-1, top-3, mean
  time-to-detect, per-fault table; writes `BENCHMARK.md`.
  **Accept:** a test asserts the top-1 floor (≥ 60%) so a regression fails CI.
- [ ] **T14 Tune on dev seeds, freeze, evaluate held-out.** Tune with seeds 1-40 only, then
  evaluate 1001-1020 once.
  **Accept:** `BENCHMARK.md` reports held-out numbers; no tuning commit touches held-out
  seeds.
- [ ] **T15 HTML report + golden test.** Self-contained HTML with inline CSS/JS/SVG.
  **Accept:** golden match on normalized HTML; a test asserts zero `http(s)://` references.
- [ ] **T16 Markdown + JSON outputs.**
  **Accept:** `summary.md` and `result.json` produced in the same run; JSON round-trips.
- [ ] **T17 CLI + demo command.** Real `analyze`/`simulate`/`bench`/`demo`, meaningful exit
  codes, no absolute paths in output.
  **Accept:** subprocess smoke tests for each command, including a failure exit code.
- [ ] **T18 Perf test.** 200k logs + 50k spans in ≤ 30 s.
  **Accept:** a `perf`-marked test measures and asserts the budget.
- [ ] **T19 README.** Setup, run, test, benchmark, screenshot, limitations.
  **Accept:** a fresh reader can go from clone to demo using only the README.
- [ ] **T20 Static site.** `site/` with 3 pre-generated reports plus the benchmark table.
  **Accept:** opens from `file://` with no server and no network.
- [ ] **T21 Demo video.** 2-3 minutes.
  **Accept:** uploaded and linked in the README.
- [ ] **T22 Final rebuild check.** Judge-style rebuild from scratch.
  **Accept:** `make verify` passes on a clean clone, tests offline, nothing missing.
