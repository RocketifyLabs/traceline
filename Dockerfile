# syntax=docker/dockerfile:1

# ITM rebuilds from this file on a clean Linux machine with no network at run
# time. Dev dependencies (pytest, ruff) are installed at BUILD time so that
#   docker run --rm --network none itm make test
# passes with the network switched off.
FROM python:3.12-slim

ENV PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1 \
    PIP_NO_CACHE_DIR=1 \
    PIP_DISABLE_PIP_VERSION_CHECK=1 \
    PYTHONPATH=/app

WORKDIR /app

# `make` is the single documented test entry point, so the image must ship it.
RUN apt-get update \
 && apt-get install -y --no-install-recommends make \
 && rm -rf /var/lib/apt/lists/*

# 1. Dev tooling first, so this layer caches.
COPY pyproject.toml uv.lock ./
RUN python -m pip install --upgrade pip \
 && python -m pip install --no-cache-dir "pytest>=8.0" "ruff>=0.6"

# 2. Package source. The runtime itself has zero third-party dependencies.
COPY itm/ ./itm/
COPY README.md LICENSE ./
RUN python -m pip install --no-cache-dir --no-deps -e .

# 3. Everything the test suite and the generated outputs need.
COPY tests/ ./tests/
COPY fixtures/ ./fixtures/
COPY docs/ ./docs/
COPY Makefile ./

RUN python -c "import itm, sys; print('itm', itm.__version__, 'on Python', sys.version.split()[0])"

CMD ["make", "test"]
