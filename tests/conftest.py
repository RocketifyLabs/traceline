"""Shared fixtures.

The whole suite runs with network access blocked: ITM must work fully offline,
so any accidental fetch fails the tests instead of silently passing on a
machine that happens to have connectivity. There is no wall clock or random
state in tests either — determinism is a product requirement (NFR), not a
testing convenience.
"""

from __future__ import annotations

import socket

import pytest


@pytest.fixture(autouse=True)
def _forbid_network(monkeypatch: pytest.MonkeyPatch) -> None:
    def blocked(*args: object, **kwargs: object):
        raise AssertionError("network access attempted: ITM must run fully offline")

    monkeypatch.setattr(socket, "socket", blocked)
    monkeypatch.setattr(socket, "create_connection", blocked)
    monkeypatch.setattr(socket, "getaddrinfo", blocked)


@pytest.fixture
def tmp_out(tmp_path):
    """A clean output directory."""
    d = tmp_path / "out"
    d.mkdir()
    return d
