# Incident report — seed 7

Source: `simulated scenario seed=7 fault=partition`

Baseline 09:43:18.000 → 10:03:18.000 · incident 10:03:18.000 → 10:06:00.000

## Verdict

**Most likely root cause: `orders`** (score 1.00, medium confidence)

## Ranked causes

| # | service | score | confidence | onset | strength | explains | not explained |
|--:|---|--:|---|---:|--:|--:|--:|
| 1 | `orders` | 1.00 | medium | 1.00 | 1.00 | 1.00 | 1.00 |
| 2 | `auth` | 0.90 | medium | 0.99 | 1.00 | 1.00 | 0.00 |
| 3 | `edge` | 0.64 | medium | 0.98 | 1.00 | 0.00 | 0.00 |

## Timeline

- `10:03:18.166` **cascade** `orders` — first error
- `10:03:18.166` **first_anomaly** `dbproxy` — template_burst
- `10:03:18.166` **first_anomaly** `orders` — error_rate
- `10:03:18.166` **first_anomaly** `payments` — template_burst
- `10:03:18.333` **first_anomaly** `auth` — template_burst
- `10:03:19.999` **cascade** `auth` — first error
- `10:03:20.666` **first_anomaly** `edge` — template_burst
- `10:03:21.333` **cascade** `edge` — first error
- `10:03:21.833` **cascade** `dbproxy` — first error
- `10:03:28.333` **cascade** `payments` — first error

## Data quality

| metric | value |
|---|---:|
| logs accepted | 0 / 0 read |
| spans accepted | 0 / 0 read |
| skipped | 0 |
