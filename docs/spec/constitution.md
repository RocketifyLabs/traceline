# Constitution

Non-negotiable principles for this project. Every change is measured against them; a
change that violates one is rejected regardless of how useful it looks.

## 1. Deterministic

Same input, same bytes. Sorted iteration everywhere, stable tie-breaks (by service name,
then by id), fixed seeds, no wall clock, no locale. A test that runs twice and compares hashes
is the enforcement mechanism, not a convention.

**Go-specific rule.** Map iteration order is randomized by design — ranging over a map can
emit a different order on every execution, and no amount of code review catches it. Every map
that can reach an output MUST be iterated through `determinism.SortedKeys`. Ranging over the
map directly is a bug even when the current output happens to look sorted. The hazard is
proven, not assumed: `TestMapRangeOrderIsNotStable` demonstrates unstable raw iteration and
stable sorted iteration in the same test.

**Why:** a tool that ranks root causes differently on every run cannot be trusted,
benchmarked, or golden-tested.

## 2. Offline, always

No network at runtime and no network in tests.

In Go this is stronger than a runtime guard, because it is enforced at build time: `go.mod`
carries no `require` block, and `internal/offline` fails the suite if a non-stdlib package
appears in the module graph. The build itself is the proof —
`GOPROXY=off CGO_ENABLED=0 go build ./...` — and `make test-offline` runs it.

**Why:** reviewers rebuild on a clean machine with `--network none`. Anything that needs the
network to build or pass tests is unbuildable for them.

## 3. Standard library only

`go.mod` has no dependencies. A test asserts that. A dependency has to replace something the
standard library cannot do — none has qualified, including graphics (SVG and PDF are text
formats) and statistics (median/MAD/percentiles are a few dozen lines).

**Why:** an 8 MB static image, trivial offline rebuild, nothing to break, no supply-chain
surface.

## 4. Tests first, one task at a time

Each task in `tasks.md` starts with a failing test. Implement until green. Commit with the
task id. Tick the box. No task is "done" without an acceptance test that would fail if the
behaviour regressed.

**Why:** the acceptance criteria are the specification; a task without a test is an
unverified claim.

## 5. Never a black box

Every ranked root cause shows its per-factor score breakdown and references the exact log
line ids and span ids that produced it. Evidence is addressable, not summarized away.

**Why:** an on-call engineer will not act on "service X, confidence high" from a tool they
cannot audit.

## 6. Honest when the data is thin

When no anomaly passes the threshold, the output says **insufficient evidence**. It never
invents a cause, never silently upgrades weak evidence to a confident label, and always
reports how many input lines were skipped as malformed.

**Why:** a confidently wrong root cause during an incident is worse than no answer.

## 7. Small, working, portable

Hand-checkable fixtures stay tiny. No absolute or user-specific paths anywhere. Outputs
open from `file://` with zero external requests. Small and polished beats big and broken.

## 8. Nothing secret in the repository

No keys, tokens, credentials or private data. The project folder and the agent sessions
that produced it are public. `.env.example` documents the expected (empty) configuration.

## 9. Safety of input data

Input is data, never code. All log content is HTML-escaped before it reaches the report.
No `eval`, no `exec`, no shell interpolation of user data.
