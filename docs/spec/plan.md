# Plan

## Stack

- **Go 1.27**, standard library only. `go.mod` carries no `require` block, and a test fails if
  one ever appears.
- **`go test`** for tests and benchmarks (built in — nothing to install).
- **`go vet` + `go fmt`** for static checks (built in).
- **Docker**: a `golang:1.27-alpine` build stage producing a static binary, shipped in
  `distroless/static`. A separate `test` stage keeps `make test` runnable with the network off.

Rationale, and the measurements behind it: `docs/DECISIONS.md` D-001. Short version — the
shipped image is 10 MB with no interpreter, no package manager and no lockfile resolution, and
the offline rebuild is `GOPROXY=off go build`. That is the "small image, trivial offline
rebuild, nothing to break" property, achieved without a virtualenv.

**Determinism caveat, specific to Go.** Map iteration order is randomized by design, which
threatens the byte-identical-output requirement. The policy is enforced by
`internal/determinism.SortedKeys` plus a test (`TestMapRangeOrderIsNotStable`) that
demonstrates the hazard is real and that sorting removes it.

## Package layout

Pure functions and plain structs. No global mutable state, no package-level caches, nothing
that makes a second run differ from the first.

| Package | Responsibility |
|---|---|
| `traceline` (root) | Version and package documentation. |
| `internal/model` | Structs: `LogLine`, `Span`, `Edge`, `Anomaly`, `TimelineEvent`, `Candidate`, `Report`. |
| `internal/ingest` | Tolerant JSONL readers. Skips and counts malformed lines, assigns stable ids. |
| `internal/templates` | Deterministic log templating: mask numbers, UUIDs, hex, IPs, durations. |
| `internal/graph` | Caller→callee edges from spans; call count, error rate, p50/p95. |
| `internal/windows` | Baseline vs incident window selection; error-rate change-point detection. |
| `internal/anomaly` | Four detectors over median/MAD baselines, returning onset, strength, evidence ids. |
| `internal/stats` | Median, MAD, robust z-score, nearest-rank percentile. **Done (T04a).** |
| `internal/timeline` | Chronological events: first anomaly per service, deploy/config changes, cascade order. |
| `internal/rca` | Candidate scoring with per-factor breakdown, confidence labels, insufficient-evidence path. |
| `internal/report` | HTML (inline CSS/JS/SVG), PDF (vector), standalone SVGs, Markdown, JSON. |
| `internal/sim` | Seeded topology, traffic, log and span generator plus fault injectors. |
| `internal/bench` | Runs scenarios over a seed range; top-1, top-3, mean TTD, per-fault table. |
| `internal/cli` | `analyze` / `simulate` / `bench` / `demo`, exit codes, no absolute paths. |
| `internal/determinism` | `SortedKeys` and the guard that proves map order is a real hazard. |
| `internal/offline` | Asserts no external dependency and that an offline build succeeds. |
| `cmd/traceline` | Thin `main`, calls `internal/cli`. |

## Data flow

```
logs.jsonl ─┐
            ├─▶ ingest ─▶ normalize ─▶ templates ─▶ graph ─▶ windows ─▶ anomaly
spans.jsonl ┘                                                              │
                                                                           ▼
                    report ◀─ rca ◀─ timeline ◀────────────────────────────┘
                      │
                      ├─▶ report.html   (self-contained, zero external requests)
                      ├─▶ summary.md
                      └─▶ result.json   (the ranked causes, machine-readable)
```

## Determinism rules (enforced by tests, not by hope)

1. Every dict that reaches output is emitted with sorted keys.
2. Every collection that reaches output is sorted, with an explicit tie-break: score desc,
   then service name asc, then first-evidence line id asc.
3. All randomness comes from `random.Random(seed)`. No module-level `random`, no
   `time.time()`, no `datetime.now()` in any output path.
4. Timestamps are parsed to timezone-aware UTC and rendered in a fixed format.
5. The simulator writes floats through a fixed formatter so hashes match across platforms.

## Scoring model (FR-7)

Each candidate service accumulates four factors, each normalized to `[0, 1]`:

| Factor | Weight | Meaning |
|---|---|---|
| `onset` | 0.35 | Earlier onset relative to the incident window start scores higher: `1 - (onset - incident_start) / incident_duration`, clamped to `[0, 1]`. |
| `strength` | 0.30 | Robust z-score of the strongest anomaly on this service, scaled by `z / 10` and clamped to `[0, 1]`. |
| `explains_downstream` | 0.25 | Fraction of downstream anomalous services whose anomaly this candidate precedes. |
| `not_explained` | 0.10 | 1.0 if no anomalous dependency precedes it, else 0.0. |

Weights live in `traceline/rca_weights.json` (loaded at startup, overridable with `--weights`), so
tuning is a data change, not a code change. That file also holds the anomaly `z_threshold`
(default 3.5) and the window `change_point_mad_k` (default 5.0).

Score is the weighted sum. Confidence labels: `high` (top score ≥ 0.60 and margin over second
≥ 0.15), `medium` (top ≥ 0.40), `low` (anything above threshold), and
`insufficient evidence` when nothing passes the anomaly threshold.

Tie-break, in this order and never on dict order: score desc → service name asc → first
evidence line id asc.

## Test strategy

- **Unit** — one per detector, templater, ranker, graph builder.
- **Golden files** — timeline, graph, report (normalized: absolute paths, version string and
  "generated at" timestamps only).
- **Determinism** — same seed → identical bytes; shuffled input order → identical output.
- **Negative** — malformed lines, empty input, no-anomaly input, clock skew, orphan spans.
- **Benchmark floor** (FR-10) and **no-network** (socket blocked in every test) run inside
  `make test`. The **perf** budget (NFR) runs in `make test-perf`, because a judge's test run
  must stay fast and must not fail on a slow machine.
- **One command** — `make test`, and it must pass under `--network none`.

## Risks

| Risk | Mitigation |
|---|---|
| Scope creep | MoSCoW is binding. Cut order: extra fault types → Markdown/JSON extras → site gallery → perf tuning. Never cut tests, Docker rebuild, README, demo. |
| Overfitting the benchmark | Held-out seeds frozen before evaluation; limitation stated in the README. |
| Flaky tests | No clocks, no network, no unseeded randomness, stable ordering, sorted iteration. |
| Report depends on external assets | Golden test asserts zero `http(s)://` references. |
| Perf target missed | Perf test measures; the target moves only with a recorded measurement. |
