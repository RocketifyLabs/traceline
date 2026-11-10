.PHONY: help test test-perf lint fmt demo bench site clean verify

PY ?= python

help:           ## show this help
	@grep -hE '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-12s\033[0m %s\n", $$1, $$2}'

test:           ## run the offline test suite (fast; excludes perf)
	$(PY) -m pytest -m "not perf"

test-perf:      ## run the perf budget test (slow, machine-dependent)
	$(PY) -m pytest -m perf

lint:           ## static checks
	$(PY) -m ruff check itm tests
	$(PY) -m ruff format --check itm tests

fmt:            ## auto-format
	$(PY) -m ruff format itm tests

demo:           ## simulate an incident, analyze it, write the report to out/demo
	$(PY) -m itm.cli demo --out out/demo

bench:          ## run the held-out benchmark, rewrite BENCHMARK.md
	$(PY) -m itm.cli bench --seeds 1001-1020 --out BENCHMARK.md

site:           ## regenerate the static site (3 pre-generated reports + benchmark table)
	$(PY) -m itm.cli demo --out site/report-1 --seed 11
	$(PY) -m itm.cli demo --out site/report-2 --seed 23
	$(PY) -m itm.cli demo --out site/report-3 --seed 37
	$(PY) -c "import shutil; shutil.copy('BENCHMARK.md', 'site/BENCHMARK.md')"

verify:         ## judge-style check: clean build, tests with no network
	docker build -t itm .
	docker run --rm --network none itm make test

clean:
	rm -rf out .pytest_cache .ruff_cache
	find . -name '__pycache__' -type d -prune -exec rm -rf {} +
