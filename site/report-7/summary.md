# Incident report — seed 7

Source: `simulated scenario seed=7 fault=partition`

Baseline 09:43:18.000 → 10:03:18.000 · incident 10:03:18.000 → 10:06:00.000

## Verdict

**Most likely root cause: `orders`** (score 0.95, high confidence)

## Ranked causes

| # | service | score | confidence | onset | strength | explains | not explained |
|--:|---|--:|---|---:|--:|--:|--:|
| 1 | `orders` | 0.95 | high | 1.00 | 0.82 | 1.00 | 1.00 |
| 2 | `auth` | 0.76 | medium | 1.00 | 0.52 | 1.00 | 0.00 |
| 3 | `edge` | 0.63 | medium | 0.94 | 1.00 | 0.00 | 0.00 |

## Timeline

- `10:03:18.166` **cascade** `orders` — first error
- `10:03:18.166` **first_anomaly** `auth` — error_rate
- `10:03:18.166` **first_anomaly** `dbproxy` — template_burst
- `10:03:18.166` **first_anomaly** `orders` — error_rate
- `10:03:18.166` **first_anomaly** `payments` — template_burst
- `10:03:18.666` **cascade** `auth` — first error
- `10:03:19.166` **cascade** `edge` — first error
- `10:03:19.499` **first_anomaly** `edge` — template_burst
- `10:03:20.666` **cascade** `dbproxy` — first error
- `10:03:22.499` **cascade** `payments` — first error

## Data quality

| metric | value |
|---|---:|
| logs accepted | 0 / 0 read |
| spans accepted | 0 / 0 read |
| skipped | 0 |
