"""Command-line entry point for ITM.

Subcommands are wired progressively (T17). Until then every command reports
honestly that it is not implemented, and exits non-zero so callers (tests, CI,
the demo script) can never mistake a stub for a result.
"""

from __future__ import annotations

import argparse
import sys

from itm import __version__

EXIT_OK = 0
EXIT_ERROR = 1
EXIT_USAGE = 2


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        prog="itm",
        description="Incident Time Machine: reconstruct what happened during an outage.",
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog=(
            "examples:\n"
            "  itm analyze --logs logs.jsonl --spans spans.jsonl --out out/report\n"
            "  itm simulate --seed 7 --out out/scenario\n"
            "  itm bench --seeds 1001-1020 --out BENCHMARK.md\n"
            "  itm demo --out out/demo\n"
        ),
    )
    parser.add_argument("--version", action="version", version=f"itm {__version__}")
    sub = parser.add_subparsers(dest="command", metavar="<command>")

    p_analyze = sub.add_parser("analyze", help="analyze captured logs and spans")
    p_analyze.add_argument("--logs", required=True, help="JSONL log file")
    p_analyze.add_argument("--spans", required=True, help="JSONL span file")
    p_analyze.add_argument("--out", required=True, help="output directory")
    p_analyze.add_argument("--incident-start", default=None, help="ISO 8601 incident window start")
    p_analyze.add_argument("--incident-end", default=None, help="ISO 8601 incident window end")
    p_analyze.add_argument(
        "--weights",
        default=None,
        help="scoring config JSON (default: itm/rca_weights.json)",
    )

    p_sim = sub.add_parser("simulate", help="generate a seeded fault-injection scenario")
    p_sim.add_argument("--seed", type=int, required=True, help="deterministic random seed")
    p_sim.add_argument("--fault", default=None, help="fault type (default: derived from seed)")
    p_sim.add_argument("--out", required=True, help="output directory")

    p_bench = sub.add_parser("bench", help="run the held-out benchmark suite")
    p_bench.add_argument("--seeds", default="1001-1020", help="seed range, e.g. 1001-1020")
    p_bench.add_argument("--out", default="BENCHMARK.md", help="output markdown file")

    p_demo = sub.add_parser("demo", help="simulate an incident and analyze it end to end")
    p_demo.add_argument("--out", default="out/demo", help="output directory")
    p_demo.add_argument("--seed", type=int, default=None, help="scenario seed (default: 7)")

    return parser


def main(argv: list[str] | None = None) -> int:
    parser = build_parser()
    args = parser.parse_args(argv)

    if not args.command:
        parser.print_help()
        return EXIT_USAGE

    print(f"itm {args.command}: not implemented yet (see docs/spec/tasks.md)", file=sys.stderr)
    return EXIT_ERROR


if __name__ == "__main__":
    raise SystemExit(main())
