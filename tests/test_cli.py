"""CLI shape tests (FR-11). Stubs must exit non-zero so a stub is never mistaken for a result."""

from __future__ import annotations

import subprocess
import sys

import pytest
from itm.cli import EXIT_ERROR, EXIT_USAGE, build_parser, main


def run_cli(*args: str) -> subprocess.CompletedProcess[str]:
    """Invoke the CLI in a fresh interpreter, the way a judge would."""
    return subprocess.run(
        [sys.executable, "-m", "itm.cli", *args],
        capture_output=True,
        text=True,
        check=False,
    )


def test_no_command_prints_help_and_returns_usage() -> None:
    assert main([]) == EXIT_USAGE
    assert build_parser().prog == "itm"


def test_version_exits_zero_via_subprocess() -> None:
    result = run_cli("--version")
    assert result.returncode == 0
    assert result.stdout.startswith("itm ")


def test_help_lists_every_required_subcommand() -> None:
    result = run_cli("--help")
    assert result.returncode == 0
    for command in ("analyze", "simulate", "bench", "demo"):
        assert command in result.stdout


def test_each_subcommand_has_help() -> None:
    for command in ("analyze", "simulate", "bench", "demo"):
        result = run_cli(command, "--help")
        assert result.returncode == 0, command
        assert "usage: itm " + command in result.stdout


@pytest.mark.parametrize(
    "argv",
    [
        ["analyze", "--logs", "l.jsonl", "--spans", "s.jsonl", "--out", "out"],
        ["simulate", "--seed", "1", "--out", "out"],
        ["bench"],
        ["demo"],
    ],
)
def test_unimplemented_commands_fail_loudly(argv: list[str]) -> None:
    assert main(argv) == EXIT_ERROR


def test_missing_required_argument_is_a_usage_error() -> None:
    result = run_cli("analyze", "--logs", "l.jsonl")
    assert result.returncode == 2
