# Validating Traceline against real incidents

The README's headline number — **top-1 97.0% ±1.5** — comes from this project's
own simulator. That is stated plainly in the README as a limitation, and this
document is what to do about it.

The question it answers: *does the method recover the cause on captures it did
not generate?* Until that has an answer, the 97% describes the method's fit to a
generator, not its usefulness in production.

This is a **research task, not a code task**. Expect a few evenings. Nothing here
blocks shipping; the tool is useful today, with its stated limit.

---

## The honest starting position

| Claim | Status |
|---|---|
| Ranks the true cause first on 500 held-out simulated seeds | **measured**: 97.0% ±1.5 |
| Ranks the true cause first on real outages | **unmeasured** |
| Says "insufficient evidence" instead of guessing on healthy data | measured on one hand-built healthy capture |

There are two distinct risks in the simulator, and they pull in opposite
directions:

1. **Too easy.** The generator may produce signals cleaner than reality — exact
   timestamps, one fault at a time, no clock skew beyond what it injects. High
   accuracy would then say more about the generator than the method.
2. **Too hard to generalize.** Tuning may have fitted the generator's specific
   shapes (the 10 s bin size, the 20-minute baseline cap) to its fault injectors.

Both are answered the same way: run it on captures nobody here wrote.

---

## Option A — RCAEval (recommended first)

**What it is.** An open benchmark for root-cause analysis on microservice
systems, covering several public datasets with labelled ground truth (RE1, RE2,
RE3, and others), plus reference implementations of common baselines.

**Why first.** It ships datasets *and* labels in a consistent shape, and other
methods publish numbers on it — so a result is comparable to something other than
itself. That comparability is the whole point; a bespoke dataset gives you a
number nobody can place.

**What you would do.**

1. Find it: search `RCAEval` on GitHub. Read the README for the supported systems
   and the licence of each dataset — some upstream datasets are research-only,
   which matters because Traceline is AGPL and you may want to publish results.
2. Pick **one** dataset to start. RE1 or RE2 are usually the recommended entry
   points; do not try to cover everything.
3. Get the raw telemetry into Traceline's input shape: JSONL logs
   (`ts`, `service`, `level`, `msg`, and `trace_id` if present) and JSONL spans
   (`trace_id`, `span_id`, `parent_span_id`, `service`, `name`, `start`, `end`,
   `status`). `fixtures/README.md` has the exact schema with real examples.
4. The conversion is the real work. Expect to write a small throwaway Go or
   Python script per dataset — that script is **not** a deliverable for the repo,
   because it encodes one dataset's quirks.
5. Run `traceline analyze` per labelled incident and score top-1/top-3 against
   the ground-truth service.
6. Report the result **with its confidence interval and the sample size**, the
   same way `BENCHMARK.md` does. A 20-incident subset cannot distinguish 90% from
   100% and must not be quoted as if it could.

**What counts as a win.** Not "97% again". A plausible outcome is 55–75% top-1,
because real incidents have multi-cause chain reactions, missing telemetry, and
noisy neighbours that the simulator does not model. A number in that range,
measured honestly, is far more valuable than the current headline — and much more
interesting to write about.

**What would mean the method is wrong.** Top-1 near chance, or top-3 accuracy
that does not separate from top-1 (the ranking would then be noise with a lucky
first pick).

---

## Option B — the AIOps Challenge datasets

**What they are.** Yearly competitions (CCF AIOps, 2020–2022 and later) that
publish real production telemetry from Chinese internet companies — multi-modal
(metrics, traces, logs), with labelled faults. Heavier and messier than RCAEval.

**Why maybe.** They are as close to "real production" as public data gets. But:
the label granularity varies by year, the winning approaches are metric-heavy
while Traceline is log/span-driven, and the volume can be large enough that ingest
becomes the bottleneck.

**Do this only after Option A**, and only if Option A's interaction with a public
dataset went smoothly.

---

## Option C — your own incident

**The most convincing test, and the only one that cannot be faked.** Take one
real outage you lived through, export the logs and traces from that window, and
see what Traceline says. You know the answer; the tool does not.

**Why it matters.** Every other option is somebody else's data with somebody
else's idea of what the "cause" was. Yours has an authority nobody can argue with.

**Caveats that will bite:**

- **Retention.** Most systems keep 7–30 days. If the incident is older than that,
  the capture is gone. Check before planning around it.
- **PII and secrets.** Production logs contain both. Redact before the capture
  leaves the environment — Traceline is offline, but the file still exists.
- **A "cause" is a judgement.** Was it the code change, or the deployment process,
  or the missing capacity alarm? Write down the answer *before* running the tool,
  or you will unconsciously accept whatever it says.
- **You will want to tune to it.** One incident is one data point. If you tune
  weights until it gets that one right, you have learned nothing. Run it first,
  publish that number, and only then consider changes.

---

## How to report whatever you find

Follow the pattern already in `BENCHMARK.md`:

1. **The limitation, first.** Which dataset, what it does and does not model, and
   that this project wrote none of it.
2. **The number with its interval and n.** `top-1 62% (n=120, ±8.7 at 95%)` — not
   `62%`.
3. **Per-category breakdown**, because an aggregate hides that the method is good
   at one fault type and useless at another.
4. **The failures, listed.** Which incidents it got wrong and the plausible reason.
   This section is what a skeptical reader trusts.
5. **Where it is measured, not where it is claimed.** "On RE2's 2022 subset" beats
   "in production" every time.

Add the result to `BENCHMARK.md` as a second section rather than replacing the
simulator numbers: both are real, and they answer different questions.

---

## Things that will probably need code, in order of likelihood

Do not start here. This list exists so the scope is not a surprise once you have
data in hand.

1. **Ingest variants.** Real exports are rarely one JSON object per line: gzipped
   files, JSON arrays, CSV, timestamps with odd offsets. `internal/ingest` would
   need a small decoder per shape. This is the most likely first change.
2. **Service name mapping.** Real captures name services inconsistently across
   logs and spans (`api-gateway` vs `api.gateway.internal`). Today the graph keys
   on exact strings, so a mismatch silently splits one service into two.
   See `internal/graph`.
3. **Multiple simultaneous faults.** The simulator injects one. Real incidents
   chain. The ranker returns top-K, which partly covers this, but the
   "insufficient evidence" threshold was tuned on single-fault data.
4. **Metric inputs.** Traceline reads logs and spans only. Many teams will hand
   you a metrics CSV first. That is a genuine feature, not a bug fix.
5. **A scoring harness.** A small command that takes a directory of labelled
   cases and prints the accuracy table, so the comparison is reproducible instead
   of hand-tallied. Natural home: `internal/bench`, next to the simulator path.

---

## Definition of done

- [ ] One public dataset ingested end to end, with the conversion script kept
      outside the repo and documented in one paragraph.
- [ ] Top-1 and top-3 measured with n and a 95% interval.
- [ ] A second section in `BENCHMARK.md` that states the limitation first.
- [ ] The failure cases written down, not omitted.
- [ ] The README's "Honest limitation" paragraph updated to point at the real-data
      result, keeping the simulator numbers visibly separate.

Until then, the README's existing wording is correct and should not be softened:
the method recovers a cause it can see in telemetry, and how well that transfers
to production is unmeasured.
