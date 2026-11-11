# Incident report

Source: `logs.jsonl + spans.jsonl`

Baseline 11:00:00.000 → 11:01:00.000 · incident 11:01:00.000 → 11:02:06.000

## Verdict

**Most likely root cause: `api`** (score 0.75, medium confidence)

## Ranked causes

| # | service | score | confidence | onset | strength | explains | not explained |
|--:|---|--:|---|---:|--:|--:|--:|
| 1 | `api` | 0.75 | medium | 1.00 | 1.00 | 0.00 | 1.00 |

## Timeline

- `11:01:00.000` **cascade** `api` — first error
- `11:01:00.000` **first_anomaly** `api` — error_rate

## Data quality

| metric | value |
|---|---:|
| logs accepted | 33 / 37 read |
| spans accepted | 9 / 9 read |
| skipped | 4 |
