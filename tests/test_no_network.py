"""NFR: the product must never touch the network, at runtime or in tests.

The blocking itself lives in ``conftest.py`` so it applies to every test. This
module proves the guard is actually armed — a suite that cannot fetch anything
is the only evidence that the tools above are offline.
"""

from __future__ import annotations

import socket

import pytest


def test_socket_is_blocked_inside_tests() -> None:
    with pytest.raises(AssertionError, match="must run fully offline"):
        socket.create_connection(("example.com", 80))


def test_dns_resolution_is_blocked_inside_tests() -> None:
    with pytest.raises(AssertionError, match="must run fully offline"):
        socket.getaddrinfo("example.com", 80)


def test_no_runtime_dependency_outside_stdlib() -> None:
    """The runtime imports nothing but the standard library (pyproject: dependencies = [])."""
    import tomllib
    from pathlib import Path

    root = Path(__file__).resolve().parent.parent
    pyproject = tomllib.loads((root / "pyproject.toml").read_text(encoding="utf-8"))
    assert pyproject["project"]["dependencies"] == []
