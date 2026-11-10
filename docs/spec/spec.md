# Specification

**Product:** Incident Time Machine (ITM)
**One-liner:** Feed ITM the logs and traces from an outage and it reconstructs what
happened: a timeline, a dependency graph, and a ranked list of probable root causes with
evidence.

**Problem.** During an incident, engineers stare at thousands of log lines spread across
services. Symptoms (errors at the edge) are far from causes (a pool exhausted three hops
down). Postmortem timelines get reconstructed by hand, hours later, from memory.

**Users.** On-call engineers, SREs, platform teams, anyone who has to write a postmortem.

**Differentiators.**

- **Measurable** — "finds the true root cause in X of 20 held-out seeded incidents",
  reproducible by anyone with one command.
- **Explainable** — every ranked cause shows its score breakdown and links to the exact
  log lines and spans behind it.
- **Deterministic and offline** — no runtime LLM, no network, byte-identical output.
- **Honest** — says "insufficient evidence" instead of inventing a cause.

---

## Scope (MoSCoW)

| | Items |
|---|---|
| **Must** | JSONL log + span ingest · dependency graph · anomaly detection · root-cause ranking · self-contained HTML report · seeded simulator with ≥6 fault types · benchmark command · Dockerfile · offline tests · README |
| **Should** | Markdown + JSON outputs · log templating · static site with pre-generated reports · 8 fault types |
| **Could** | `docker compose` live fault-injection demo · OpenTelemetry JSON import · metrics input |
| **Won't** | any runtime LLM/generative AI · auth · database · streaming/live tailing · a backend server |

---

## Functional requirements

### FR-1 Ingest
Read JSONL logs `{ts (RFC 3339), service, level, msg, trace_id?, fields?}` and JSONL spans
`{trace_id, span_id, parent_span_id?, service, name, start, end, status: ok|error, attrs?}`.

**Acceptance:** a fixture with ~5% malformed lines analyzes successfully; the skipped-line
count appears in the report; bad input never crashes the process; **shuffling the input line
order produces byte-identical output**, because record ids derive from content, not from file
position.

Clock skew is reported, never repaired: a span whose `end` precedes its `start`, or a log line
whose timestamp falls outside the observed range of the file by more than 10 minutes, is kept
and counted in the data-quality section. Records are never silently dropped for looking odd.

### FR-2 Dependency graph
Build caller→callee edges from spans with call count, error rate, p50/p95 latency.
Percentiles use the **nearest-rank** method on sorted durations (no interpolation), so a
given multiset of spans always yields the same p50/p95 on every platform. A span with no
`parent_span_id` is a root and produces no edge; a span whose parent id never appears in the
input is counted as an **orphan** and reported in the data-quality section, not guessed at.

**Acceptance:** the graph for a fixture equals a golden file; orphan and root spans are
counted separately and neither produces a bogus edge.

### FR-3 Windows
Compare a baseline window to an incident window. Auto-detect the incident window via
error-rate change-point, or accept `--incident-start` / `--incident-end`.

Defaults, so the behaviour is unambiguous: the **baseline is the pre-incident window, capped
at the 20 minutes immediately before the detected (or given) incident start**, or the whole
pre-incident span if it is shorter. Change-point detection buckets log records into **10-second
bins** by error rate and picks the earliest bin whose error rate exceeds the baseline median by
**more than 5 MADs** and stays elevated for at least **two consecutive bins** (a single spike is
noise, not an incident). The reported start is the first timestamp in that bin.

**Acceptance:** auto-detected start is within a tolerance (default 60 s) of the seeded
fault start on simulator scenarios; an explicit `--incident-start/--incident-end` is honored
exactly and disables detection.

### FR-4 Anomaly detection (per service)
Error-rate spike, p95 latency shift, log-template burst, newly appearing error templates.
Robust statistics (median/MAD). Each anomaly carries onset time, strength, and evidence
references (log line ids, span ids).

Strength is a **robust z-score**: `0.6745 * (x - median) / MAD`, with MAD floored at a small
epsilon so a perfectly constant baseline does not divide by zero. An anomaly is emitted only
when the z-score is `>= 3.5` (configurable in `itm/rca_weights.json`); below that the detector
returns nothing rather than a weak signal. A service is "anomalous" for ranking purposes if it
has at least one anomaly above threshold.

**Acceptance:** one unit test per detector on synthetic series, including a "no anomaly"
negative case, plus a constant-baseline case that must not divide by zero.

### FR-5 Log templating
Deterministically mask numbers, UUIDs, hex, IPs and durations so similar messages group.

**Acceptance:** table-driven tests; identical input always yields identical templates.

### FR-6 Timeline
Chronological events: first anomaly per service, deploy/config-change events
(`fields.event` = `deploy` | `config_change`), error cascade order.

**Acceptance:** golden timeline for a fixture.

### FR-7 Root-cause ranking
Score candidates on: earliest onset, anomaly strength, number of downstream anomalous
dependents explained, and "not explained by an anomalous dependency". Weights live in a
config file. Output the top-k with a per-factor breakdown and a confidence label. If no
anomaly passes threshold, output **insufficient evidence**.

The weights file is `itm/rca_weights.json`, overridable with `--weights <path>` on
`itm analyze` (a path, never a credential). Defaults and the exact formula are in
`plan.md`. A **red herring** — a service that is noisy but whose anomaly neither precedes
nor explains any downstream anomaly — must score below the true cause; it is a non-fault
used to test discrimination, not a label the tool is told about.

**Acceptance:** on fixtures the injected cause ranks first; a red-herring noisy service does
not; the empty case returns "insufficient evidence".

### FR-8 Report
One self-contained HTML file (inline CSS/JS/SVG, zero external requests, opens from
`file://`), plus a Markdown summary and a JSON result. Sections: summary, timeline,
dependency graph (SVG), ranked causes with evidence, data-quality notes.

**Acceptance:** golden-file test on normalized HTML; a test asserts no `http(s)://`
resource references. Normalization replaces, before comparison: every absolute path, the
`itm` version string, any timestamp rendered as "generated at", and whitespace-only runs.
Nothing else is normalized — if the analysis changes, the golden test must catch it.

### FR-9 Simulator
Seeded generator of a service topology plus traffic, logs and spans, with fault injection.

**Acceptance:** determinism test — run twice, compare hashes.

### FR-10 Benchmark
`itm bench` runs N seeded scenarios and reports top-1, top-3, mean time-to-detect and a
per-fault-type table. Writes `BENCHMARK.md`.

**Time-to-detect** is defined as `detected incident start − seeded fault start`, in seconds,
averaged over scenarios where the true cause was ranked in the top 3. A scenario where the
true cause was missed is excluded from TTD and counted in the miss column instead — averaging
it in as zero would flatter the number.

**Acceptance:** a test asserts the floor below, so regressions fail CI.

### FR-11 CLI
`itm analyze`, `itm simulate`, `itm bench`, `itm demo`. Clear `--help`, meaningful exit
codes, no absolute paths in output.

Exit codes: **0** success · **1** analysis failed or refused (unreadable input, no data,
internal error) · **2** usage error (bad or missing arguments, via argparse). Nothing else is
returned, so a script can branch on the code without parsing text.

**Acceptance:** CLI smoke tests run through a subprocess: each command's `--help` exits 0, a
missing required argument exits 2, and a genuine failure exits 1.

### FR-12 Demo and live site
`itm demo` simulates an incident, analyzes it, and writes the report to `./out/demo/`. A
static `site/` (3 pre-generated incident reports plus the benchmark table) deploys to
GitHub Pages.

**Acceptance:** the site opens with no server and no network.

---

## Fault types (simulator)

Must have ≥6, target 8:
crash/restart loop · latency injection · dependency timeout cascade · connection-pool
exhaustion · bad deploy (errors right after a deploy event) · retry storm (amplification) ·
noisy unrelated service (red herring) · partial network partition.
Optional: slow-burn resource leak.

## Benchmark protocol

- Development seeds `1-40`; **held-out seeds `1001-1020`, never used for tuning**.
- Targets: top-3 ≥ 90%, top-1 ≥ 70% on held-out. CI floor: **top-1 ≥ 60%**.
- The README states the limitation plainly: the simulator is authored by this same
  project, so results are indicative, not a claim about real-world systems.

---

## Non-functional requirements

- **Deterministic** — sorted iteration, stable tie-breaks by service name, fixed seeds, no
  wall-clock or locale dependence in any output.
- **Offline** — no network at runtime or in tests (enforced by a socket-blocking autouse
  fixture).
- **Performance** — 200k log lines + 50k spans analyzed in ≤ 30 s inside the container,
  verified by a `perf`-marked test that is **excluded from `make test`** (a judge's run must
  stay fast and deterministic) and executed by `make test-perf`. The target moves only with a
  recorded measurement.
- **Portable** — no absolute or user-specific paths anywhere.
- **Safe** — never executes input data; all log content HTML-escaped in the report.
- **License** — MIT.
