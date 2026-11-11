# Incident Time Machine (ITM)

**Feed ITM the logs and traces from an outage and it reconstructs what happened:**
a timeline, a service dependency graph, and a **ranked list of probable root causes with evidence**.

Symptoms are far from causes. A 500 in the edge service is three hops from a connection
pool that ran dry in a database proxy. ITM reads the raw telemetry from the incident
window, detects which services actually changed behaviour, and ranks the candidates that
best explain what the rest of the system did.

Deterministic, offline, and explainable: no runtime LLM, no network, and the same input
always produces byte-identical output.

---

## Results

**Top-1 accuracy 97.0% · Top-3 100%** on 500 held-out seeded incidents never used for tuning
(95% interval ±1.5 points).

| Metric | Value |
|---|---|
| Evaluation seeds | 5000-5499 (frozen, disjoint from tuning) |
| Scenarios | 500 |
| Top-1 accuracy | **97.0% ± 1.5** |
| Top-3 accuracy | **100%** |
| True cause missing from candidates | 0 |
| Speed, spec volume (17k logs / 54k spans) | **0.75 s** |

Per fault type, top-1 ranges from 92.7% (`bad_deploy`) to 100% (`timeout_cascade`). The
scenarios include background noise on healthy services, a distractor service that is loud in
the logs without failing, and a transient error burst in the baseline — so "earliest anomaly"
is not a free win. Full tables in [`BENCHMARK.md`](BENCHMARK.md).

**On sample size.** An earlier version reported 100% on the 20-scenario held-out set. That
was arithmetically true and misleading: 20 scenarios cannot distinguish 90% from 100%, and
the interval was ±11 points. The 500-scenario set exists because the headline number needed
enough sample to mean something.

**Honest limitation.** The simulator is written by this same project. These numbers describe
how well the method recovers a cause from *this* generator's behaviour; they are not a claim
about real production systems.

## Fixtures

Three small captures are committed in [`fixtures/`](fixtures/README.md), each readable in a
minute and each proving one behaviour:

| Fixture | Proves |
|---|---|
| [`pool-exhaustion/`](fixtures/pool-exhaustion/) | names `db-proxy` (high confidence) ahead of the two services it broke |
| [`steady-state/`](fixtures/steady-state/) | returns **insufficient evidence** rather than inventing a cause |
| [`messy-input/`](fixtures/messy-input/) | four bad lines, four distinct rejection reasons, cause still found |

```bash
go run ./cmd/itm analyze   --logs fixtures/pool-exhaustion/logs.jsonl   --spans fixtures/pool-exhaustion/spans.jsonl   --out out/pool
```

## Try it

```bash
make demo                      # simulate an incident, analyze it, write out/demo/
open out/demo/report.html      # self-contained report: no server, no network
```

Or against your own capture:

```bash
go run ./cmd/itm analyze --logs logs.jsonl --spans spans.jsonl --out out/report
```

## Requirements

- **Go 1.27** — that is the whole toolchain. No package manager, no virtualenv.
- Docker, only for the judge-style rebuild check.

There are **no third-party dependencies**: `go.mod` has no `require` block, and a test fails
if one ever appears.

## Output

One run produces, in the output directory:

| File | What it is |
|---|---|
| `report.html` | Self-contained report — inline CSS and SVG. Zero external requests, opens from `file://`. |
| `report.pdf` | The same report as a printable vector PDF (A4). |
| `charts/*.svg` | Each chart standalone: `error-rate.svg`, `timeline.svg`, `dependencies.svg`. |
| `summary.md` | Short Markdown summary for a postmortem. |
| `result.json` | Machine-readable ranked causes with evidence ids. |

PNG is deliberately not produced: raster text needs a font renderer, and vector output is
crisper, diffable and golden-testable. Export from the browser if you need a raster.
See `docs/DECISIONS.md` D-002.

## Commands

```bash
go run ./cmd/itm analyze  --logs l.jsonl --spans s.jsonl --out out/report
go run ./cmd/itm simulate --seed 7 --out out/scenario
go run ./cmd/itm bench    --seeds 1001-1020 --out BENCHMARK.md
go run ./cmd/itm demo     --out out/demo --seed 7
```

Exit codes: `0` success · `1` analysis failed or refused · `2` usage error.

## How the ranking works

Four factors, each normalized to `[0,1]`, combined with documented weights:

| Factor | Weight | Meaning |
|---|---|---|
| `onset` | 0.35 | Earlier relative to the window start scores higher |
| `strength` | 0.30 | Robust z-score of the strongest anomaly, on a log scale so a marginal signal cannot tie with a decisive one |
| `explains_downstream` | 0.25 | Fraction of its anomalous **dependents** whose onset it precedes |
| `not_explained` | 0.10 | 1.0 if no dependency of its own is anomalous and earlier |

Every candidate reports its full breakdown, so a ranking can be audited rather than trusted.
If nothing passes the anomaly threshold, the answer is **"insufficient evidence"** — never a
confident guess.

## Input format

Logs and spans are JSONL, one JSON object per line. Malformed lines are skipped and counted
by reason, never fatal.

```jsonc
// logs.jsonl
{"ts":"2026-10-16T10:00:01Z","service":"api-edge","level":"error","msg":"upstream timeout after 3000ms","trace_id":"a1b2c3","fields":{"route":"/checkout"}}

// spans.jsonl
{"trace_id":"a1b2c3","span_id":"s1","parent_span_id":null,"service":"api-edge","name":"GET /checkout","start":"2026-10-16T10:00:00Z","end":"2026-10-16T10:00:03Z","status":"error","attrs":{"http.status_code":500}}
```

## Test

```bash
make test           # full suite, offline, fast
make test-perf      # perf budget + scaling check (slow, machine-dependent)
make lint           # go vet + gofmt check
```

Both proofs that matter, run the way a reviewer runs them:

```bash
make test-offline   # GOPROXY=off CGO_ENABLED=0 — no module fetch, no C toolchain
make verify         # docker build + docker run --network none make test
```

`make test-offline` is the strong form of the offline claim: if any code path needed the
network, the build would fail before a single test ran.

## Docs

- [`docs/DECISIONS.md`](docs/DECISIONS.md) — why Go, why no PNG, why this scope.
- [`docs/spec/spec.md`](docs/spec/spec.md) — requirements with acceptance criteria.
- [`docs/spec/plan.md`](docs/spec/plan.md) — architecture and package contracts.
- [`docs/spec/constitution.md`](docs/spec/constitution.md) — the non-negotiables.
- [`docs/spec/tasks.md`](docs/spec/tasks.md) — progress.

## License

MIT — see `LICENSE`.
