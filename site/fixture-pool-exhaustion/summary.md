# Incident report

Source: `logs.jsonl + spans.jsonl`

Baseline 10:00:00.000 → 10:01:00.000 · incident 10:01:00.000 → 10:01:57.000

## Verdict

**Most likely root cause: `db-proxy`** (score 1.00, high confidence)

## Ranked causes

| # | service | score | confidence | onset | strength | explains | not explained |
|--:|---|--:|---|---:|--:|--:|--:|
| 1 | `db-proxy` | 1.00 | high | 1.00 | 1.00 | 1.00 | 1.00 |
| 2 | `web` | 0.75 | medium | 1.00 | 1.00 | 0.00 | 1.00 |
| 3 | `api` | 0.65 | medium | 1.00 | 1.00 | 0.00 | 0.00 |

## Timeline

- `10:01:00.000` **cascade** `db-proxy` — first error
- `10:01:00.000` **first_anomaly** `db-proxy` — error_rate
- `10:01:00.000` **first_anomaly** `web` — latency_p95
- `10:01:00.010` **first_anomaly** `api` — latency_p95
- `10:01:03.000` **cascade** `api` — first error
- `10:01:06.000` **cascade** `web` — first error

## Data quality

| metric | value |
|---|---:|
| logs accepted | 90 / 90 read |
| spans accepted | 30 / 30 read |
| skipped | 0 |
