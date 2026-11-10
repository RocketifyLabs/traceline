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
count appears in the report; bad input never crashes the process.

### FR-2 Dependency graph
Build caller→callee edges from spans with call count, error rate, p50/p95 latency.

**Acceptance:** the graph for a fixture equals a golden file.

### FR-3 Windows
Compare a baseline window to an incident window. Auto-detect the incident window via
error-rate change-point, or accept `--incident-start` / `--incident-end`.

**Acceptance:** auto-detected start is within a tolerance (default 60 s) of the seeded
fault start on simulator scenarios.

### FR-4 Anomaly detection (per service)
Error-rate spike, p95 latency shift, log-template burst, newly appearing error templates.
Robust statistics (median/MAD). Each anomaly carries onset time, strength, and evidence
references (log line ids, span ids).

**Acceptance:** one unit test per detector on synthetic series, including a "no anomaly"
negative case.

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

**Acceptance:** on fixtures the injected cause ranks first; a red-herring noisy service does
not; the empty case returns "insufficient evidence".

### FR-8 Report
One self-contained HTML file (inline CSS/JS/SVG, zero external requests, opens from
`file://`), plus a Markdown summary and a JSON result. Sections: summary, timeline,
dependency graph (SVG), ranked causes with evidence, data-quality notes.

**Acceptance:** golden-file test on normalized HTML; a test asserts no `http(s)://`
resource references.

### FR-9 Simulator
Seeded generator of a service topology plus traffic, logs and spans, with fault injection.

**Acceptance:** determinism test — run twice, compare hashes.

### FR-10 Benchmark
`itm bench` runs N seeded scenarios and reports top-1, top-3, mean time-to-detect and a
per-fault-type table. Writes `BENCHMARK.md`.

**Acceptance:** a test asserts the floor below, so regressions fail CI.

### FR-11 CLI
`itm analyze`, `itm simulate`, `itm bench`, `itm demo`. Clear `--help`, meaningful exit
codes, no absolute paths in output.

**Acceptance:** CLI smoke tests run through a subprocess.

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
- **Performance** — 200k log lines + 50k spans analyzed in ≤ 30 s inside the container
  (verified by a perf test; the target moves only with a measured reason).
- **Portable** — no absolute or user-specific paths anywhere.
- **Safe** — never executes input data; all log content HTML-escaped in the report.
- **License** — MIT.
