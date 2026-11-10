# Plan

## Stack

- **Python 3.12**, pinned in `.python-version`.
- **Standard library only at runtime.** `pyproject.toml` keeps `dependencies = []`.
- **uv** for the dev environment: `uv sync` from `uv.lock`; `pytest` and `ruff` are
  dev-group only.
- **Docker** `python:3.12-slim`, dev deps installed at build time so tests run with
  `--network none`.

Rationale: the smallest possible image, a trivial offline rebuild, and no dependency that
can break between the author's machine and a reviewer's clean Linux box.

## Module layout

Pure functions and dataclasses. No global mutable state, no module-level caches, nothing
that makes a second run differ from the first.

| Module | Responsibility |
|---|---|
| `itm/model.py` | Dataclasses: `LogLine`, `Span`, `Edge`, `Anomaly`, `TimelineEvent`, `Candidate`, `Report`. Frozen where cheap. |
| `itm/ingest.py` | Tolerant JSONL readers. Skips and counts malformed lines, assigns stable line ids. |
| `itm/templates.py` | Deterministic log templating: mask numbers, UUIDs, hex, IPs, durations, quoted strings. |
| `itm/graph.py` | Caller→callee edges from spans; call count, error rate, p50/p95. |
| `itm/windows.py` | Baseline vs incident window selection; error-rate change-point detection. |
| `itm/anomaly.py` | Four detectors over median/MAD baselines, each returning onset, strength, evidence ids. |
| `itm/timeline.py` | Chronological events: first anomaly per service, deploy/config changes, cascade order. |
| `itm/rca.py` | Candidate scoring with a per-factor breakdown, confidence labels, insufficient-evidence path. |
| `itm/report.py` | Self-contained HTML (inline CSS/JS/SVG), Markdown summary, JSON result. |
| `itm/sim.py` | Seeded topology, traffic, logs and spans generator plus fault injectors. |
| `itm/bench.py` | Runs scenarios over a seed range; top-1, top-3, mean TTD, per-fault table. |
| `itm/cli.py` | `analyze` / `simulate` / `bench` / `demo`, exit codes, no absolute paths. |

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
| `onset` | 0.35 | Earlier onset relative to the incident window start scores higher. |
| `strength` | 0.30 | Robust z-score of the strongest anomaly on this service. |
| `explains_downstream` | 0.25 | Fraction of downstream anomalous services whose anomaly this candidate precedes. |
| `not_explained` | 0.10 | 1.0 if no anomalous dependency precedes it, else 0.0. |

Weights live in `itm/rca_weights.json` (loaded at startup, overridable by flag), so tuning
is a data change, not a code change. Confidence labels: `high` (top score ≥ 0.60 and margin
over second ≥ 0.15), `medium` (top ≥ 0.40), `low` (anything above threshold), and
`insufficient evidence` when nothing passes the anomaly threshold.

## Test strategy

- **Unit** — one per detector, templater, ranker, graph builder.
- **Golden files** — timeline, graph, report (with volatile fields normalized).
- **Determinism** — same seed → identical bytes; shuffled input order → identical output.
- **Negative** — malformed lines, empty input, no-anomaly input, clock skew.
- **Benchmark floor** (FR-10), **perf** (NFR), **no-network** (socket blocked in every test).
- **One command** — `make test`, and it must pass under `--network none`.

## Risks

| Risk | Mitigation |
|---|---|
| Scope creep | MoSCoW is binding. Cut order: extra fault types → Markdown/JSON extras → site gallery → perf tuning. Never cut tests, Docker rebuild, README, demo. |
| Overfitting the benchmark | Held-out seeds frozen before evaluation; limitation stated in the README. |
| Flaky tests | No clocks, no network, no unseeded randomness, stable ordering, sorted iteration. |
| Report depends on external assets | Golden test asserts zero `http(s)://` references. |
| Perf target missed | Perf test measures; the target moves only with a recorded measurement. |
