# Fixtures

Three small, hand-checked captures. Each one is short enough to read in a minute and
exists to prove one specific behaviour. All of them are committed so a reviewer can see the
input format without generating anything.

Run any of them with:

```bash
go run ./cmd/itm analyze \
  --logs  fixtures/<name>/logs.jsonl \
  --spans fixtures/<name>/spans.jsonl \
  --out   out/<name>
```

---

## `pool-exhaustion/` — a real incident with a known cause

**90 log lines, 30 spans, ~2 minutes.** A `db-proxy` connection pool runs dry at
`10:01:00`; `api` starts timing out three seconds later and `web` three seconds after that.

Why it is useful: the causal chain is written into the timestamps by hand, so the ranking can
be checked against something other than the tool's own opinion.

| Time | Service | What happened |
|---|---|---|
| 10:00:00 – 10:00:54 | all | clean, one request every 6 s |
| 10:01:00 | `db-proxy` | first `connection pool exhausted` |
| 10:01:03 | `api` | first `upstream timeout after 3000ms` |
| 10:01:06 | `web` | first `upstream timeout after 3000ms` |

**Expected result:**

| Service | Score | Confidence | Why |
|---|---|---|---|
| `db-proxy` | 1.00 | high | earliest onset, strongest anomaly, explains both dependents, nothing explains it |
| `web` | 0.75 | medium | real victim: its dependency `api` is anomalous and earlier |
| `api` | 0.65 | medium | middle of the chain, penalised on both sides |

Detected window: baseline `10:00:00 → 10:01:00`, incident `10:01:00 → 10:01:57`.

---

## `steady-state/` — nothing is wrong

**36 log lines, 12 spans, ~3 minutes.** Uniform traffic, one slow request, no error spike.

Why it is useful: the tool must say **"insufficient evidence"** rather than invent a cause.
A detector that always finds something is not a detector.

**Expected result:** `insufficient evidence`, no candidates.

---

## `messy-input/` — a dirty capture

**37 log lines with exactly four bad ones**, one of each kind, plus a clean incident.

| Line | Problem | Counted as |
|---|---|---|
| `this line is not JSON at all` | not JSON | `invalid_json` |
| `{"ts":...,"level":"error"}` | no `msg` | `missing_required_field` |
| `{"ts":"not-a-timestamp",...}` | bad timestamp | `unparseable_timestamp` |
| *(blank line)* | empty | `empty_line` |

Why it is useful: it proves the four rejection paths are distinguished from each other, not
lumped into one "skipped" number. A capture with bad data must still be analyzed.

**Expected result:** 37 read, 33 accepted, 4 skipped with four distinct reasons; root cause
`api` found despite the dirty input.

---

## Changing a fixture

These files are the contract for the "expected result" column above. If a change to the
analysis alters one of these outcomes, that is either a regression or a deliberate decision —
and the difference has to be argued in the commit message, not discovered later.

`fixtures_test.go` runs all three and asserts the outcomes, so an accidental change fails the
build rather than silently updating a report.
