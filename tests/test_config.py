"""The scoring config is part of the product's contract, so it is tested.

If a weight or threshold disappears, the ranking silently changes meaning. These tests
pin the documented defaults so a drift is a failing test instead of a surprise in a
benchmark number.
"""

from __future__ import annotations

import json
from pathlib import Path

import pytest

CONFIG = Path(__file__).resolve().parent.parent / "itm" / "rca_weights.json"


@pytest.fixture(scope="module")
def config() -> dict:
    return json.loads(CONFIG.read_text(encoding="utf-8"))


def test_weights_sum_to_one(config: dict) -> None:
    total = sum(config["weights"].values())
    assert total == pytest.approx(1.0, abs=1e-9)


def test_weights_cover_the_four_documented_factors(config: dict) -> None:
    assert set(config["weights"]) == {
        "onset",
        "strength",
        "explains_downstream",
        "not_explained",
    }


def test_weights_are_not_negative(config: dict) -> None:
    assert all(w >= 0 for w in config["weights"].values())


def test_documented_thresholds_match_the_spec(config: dict) -> None:
    assert config["anomaly"]["z_threshold"] == 3.5
    assert config["windows"]["change_point_mad_k"] == 5.0
    assert config["windows"]["baseline_cap_seconds"] == 1200
    assert config["windows"]["detect_tolerance_seconds"] == 60


def test_no_secrets_in_config(config: dict) -> None:
    """The project folder is public: config holds paths and numbers only."""
    text = CONFIG.read_text(encoding="utf-8").lower()
    for marker in ("api_key", "apikey", "secret", "password", "token", "bearer"):
        assert marker not in text
