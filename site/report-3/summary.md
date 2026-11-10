# Incident report — seed 3

Source: `simulated scenario seed=3 fault=pool_exhaustion`

Baseline 09:43:18.000 → 10:03:18.000 · incident 10:03:18.000 → 10:06:00.000

## Verdict

**Most likely root cause: `edge`** (score 0.75, high confidence)

## Ranked causes

| # | service | score | confidence | onset | strength | explains | not explained |
|--:|---|--:|---|---:|--:|--:|--:|
| 1 | `edge` | 0.75 | high | 1.00 | 1.00 | 0.00 | 1.00 |
| 2 | `orders` | 0.27 | low | 0.01 | 0.54 | 0.00 | 1.00 |

## Timeline

- `10:03:18.166` **cascade** `edge` — first error
- `10:03:18.166` **first_anomaly** `auth` — template_burst
- `10:03:18.166` **first_anomaly** `edge` — error_rate
- `10:03:18.999` **cascade** `orders` — first error
- `10:03:19.333` **first_anomaly** `orders` — template_burst
- `10:03:20.166` **cascade** `auth` — first error

## Data quality

| metric | value |
|---|---:|
| logs accepted | 0 / 0 read |
| spans accepted | 0 / 0 read |
| skipped | 0 |
