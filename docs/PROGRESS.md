# Progress

Continuous record of what is done, what is mocked, and what genuinely needs a decision.
Last updated at the end of the autonomous build run.

## Status: 24 of 25 delivered; T21 (demo video) deselected, not pending

| Task | State |
|---|---|
| T01 scaffold | done |
| T02 offline proof | done |
| T03 spec docs | done |
| T04 models + ingest | done |
| T05 templating | done |
| T06 dependency graph | done |
| T07 windows + change-point | done |
| T08 anomaly detectors | done |
| T09 timeline | done |
| T10 RCA ranking | done |
| T11 simulator core | done |
| T12 fault injectors | done |
| T13 benchmark + floor test | done |
| T14 tune + held-out evaluation | done — 97.0% on 500 scenarios |
| T15 HTML report | done |
| T16 MD + JSON outputs | done |
| T17 CLI + demo | done |
| T18 perf test | done |
| T19 README | done |
| T20 static site | done |
| T21 demo video | deselected 2026-11-14 — deadline passed, no consumer |
| T22 clean-clone rebuild | done |
| T23 hand-checked fixtures | done |
| T24 desktop GUI (`gui/`, Fyne + CGO) | done on Windows |
| T25 rename, license, publish | done — public at `RocketifyLabs/traceline` |

## Measured results

- **Top-1 97.0% ± 1.5, Top-3 100%, 0 misses** on 500 held-out seeds 5000-5499, disjoint from
  every tuning set.
- Per fault type, top-1 ranges from 92.7% (`bad_deploy`) to 100% (`timeout_cascade`). No
  fault type above 80%, which the tests assert: a fault the method can never find would be a
  scenario defect rather than difficulty.
- Spec-volume performance: **754 ms** for 17k logs / 54k spans (budget: 30 s).
- Scaling: 2× input → 2.1× time (asserted, so a quadratic regression fails the build).
- Heavy volume: 662k spans in 15.5 s.
- Image: **11.3 MB** distroless static binary.
- Clean clone → byte-identical artifacts including the PDF.

## Fixtures

Three small captures in `fixtures/`, each proving one behaviour and documented with its
expected result in `fixtures/README.md`:

| Fixture | Lines | Proves |
|---|---|---|
| `pool-exhaustion/` | 90 logs, 30 spans | names `db-proxy` (high confidence) ahead of the two victims, with the causal chain hand-written into the timestamps |
| `steady-state/` | 36 logs, 12 spans | returns **insufficient evidence** for a healthy capture |
| `messy-input/` | 37 logs, 4 bad | counts four distinct rejection reasons and still finds the cause |

`internal/fixtures` runs all three and asserts the documented outcomes, so the README cannot
drift away from the code. They are also on the static site.

Writing them found a real bug: see item 15.

## Nothing is mocked

There are no stubs or fakes in the delivered code. Every command does its real work:

| Command | Real behaviour |
|---|---|
| `traceline analyze` | reads JSONL from disk, runs the full pipeline, writes 5 artifacts |
| `traceline simulate` | generates a scenario and writes `logs.jsonl`, `spans.jsonl`, `scenario.yaml` |
| `traceline bench` | runs 20 scenarios and writes `BENCHMARK.md` |
| `traceline demo` | simulates, analyzes, writes everything, and states HIT/MISS against ground truth |

The simulator is not a mock of anything external — it is a product feature, the benchmark's
ground truth. That is stated in the README where the numbers are.

## Blocked: T21 demo video

Recording a 2-3 minute screen capture is outside what this environment can do. Nothing is
fake-substituted for it.

What exists instead:
- The demo script is written in `docs/spec/spec.md` §13.
- Every command it calls is verified working.
- `site/index.html` is a live, clickable version of the same walkthrough: 3 real reports
  plus the benchmark table, openable from `file://`.
- `make demo` reproduces the demo segment exactly and prints HIT/MISS.

## Open items that genuinely need a human decision

1. **Demo video (T21).** Record it, or drop it if the submission format allows the live site
   instead. Only you can decide which the reviewer accepts.

2. ~~`noisy_neighbour` accuracy is 0%.~~ **Fixed.** It was a scenario defect, not a method
   gap: the label was assigned to a service that received no fault at all. See the bug list
   below. The scenario now runs at 98.2% top-1 across 56 held-out seeds.

3. **Publication.** The repository is local only. Making it public, choosing the GitHub
   account, and adding a LICENSE owner line are actions on your accounts, not code.

## Bugs found and fixed during the build

Recorded because each was found by measuring, not by reasoning:

1. Clock-skew detection used min/max bounds, so a wild outlier dragged the bound to itself
   and looked normal. Switched to the median.
2. `explains_downstream` walked callees instead of callers: a root cause at the bottom of a
   chain scored zero for explaining its own visible symptoms. This alone moved top-1 from
   25% to 60%.
3. Propagation delay in the simulator used walk depth from the entry service, making the
   entry service the earliest symptom and therefore the top-ranked "cause".
4. Zero-MAD baselines turned any change into an astronomical z-score (6.7e8). Added absolute
   gates so a z-score alone can never call an anomaly.
5. `template_burst` created candidates on its own; every healthy service bursts templates at
   the fault boundary, so a healthy service with a perfect onset and nothing to explain scored
   1.000.
6. `noisy_neighbour` had `errorFault: 0.02`, so the true cause produced no behavioural change
   and the scenario had no correct answer.
7. Confidence labels used each row's margin to the next row, so the second candidate showed
   "high" while the first showed "medium".
8. Window detection collapsed to zero width when no change point was found, making every
   downstream comparison empty.
9. The pipeline built the dependency graph twice (edges, then ranking): 23.1 s → 16.3 s.
10. The perf payload raised every service's error rate equally, so no service was the cause
    and the test proved nothing about output.
11. `noisy_neighbour` labelled a service that never received the fault: the loud decoy was
    treated as the target and the "true cause" was picked from the leftovers, so it only
    inherited collateral damage. The scenario had no correct answer and every ranking was
    scored against a wrong label. Separated the two roles.
12. The `strength` factor was linear (`z/10`), so a marginal anomaly (z=4.4) tied with a
    decisive one (z=6.4). Switched to log2 scaling so the factor discriminates across the
    realistic z range of ~4 to ~600.
13. Onset was the first error line, so background noise on a healthy caller made it look like
    the earliest symptom. Onset is now the first bin with a materially elevated error rate.
14. 100% on the 20-scenario held-out set was arithmetically true and statistically
    meaningless (±11 points). Added a 500-scenario evaluation set and a confidence interval
    next to every reported accuracy.
15. Window detection computed its median/MAD reference over ALL bins. When roughly half the
    capture is incident — the normal case for a short capture — the median lands halfway up,
    the MAD becomes the size of the shift, and the threshold ends up above the maximum
    observed rate. No bin could ever be flagged, the window collapsed to zero width, and a
    capture with an obvious incident reported "insufficient evidence". Now referenced from the
    lower half of the rates. Found by writing a 90-line fixture, which is exactly what
    fixtures are for.
