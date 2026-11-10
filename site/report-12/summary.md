# Incident report — seed 12

Source: `simulated scenario seed=12 fault=pool_exhaustion`

Baseline 09:43:18.000 → 10:03:18.000 · incident 10:03:18.000 → 10:06:00.000

## Verdict

**Most likely root cause: `orders`** (score 0.97, high confidence)

## Ranked causes

| # | service | score | confidence | onset | strength | explains | not explained |
|--:|---|--:|---|---:|--:|--:|--:|
| 1 | `orders` | 0.97 | high | 1.00 | 0.91 | 1.00 | 1.00 |
| 2 | `auth` | 0.54 | low | 1.00 | 0.63 | 0.00 | 0.00 |
| 3 | `edge` | 0.53 | low | 1.00 | 0.61 | 0.00 | 0.00 |

## Timeline

- `10:03:18.166` **cascade** `orders` — first error
- `10:03:18.166` **first_anomaly** `orders` — error_rate
- `10:03:18.166` **first_anomaly** `payments` — template_burst
- `10:03:18.333` **first_anomaly** `edge` — error_rate
- `10:03:18.499` **cascade** `auth` — first error
- `10:03:18.499` **cascade** `payments` — first error
- `10:03:18.499` **first_anomaly** `auth` — error_rate
- `10:03:18.999` **cascade** `edge` — first error

## Data quality

| metric | value |
|---|---:|
| logs accepted | 0 / 0 read |
| spans accepted | 0 / 0 read |
| skipped | 0 |
