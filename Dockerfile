# syntax=docker/dockerfile:1

# ITM rebuilds from this file on a clean Linux machine with no network at run
# time. The exact same command the reviewer runs:
#   docker build -t itm .
#   docker run --rm --network none itm make test
FROM python:3.12-slim

ENV PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1 \
    PIP_NO_CACHE_DIR=1 \
    PIP_DISABLE_PIP_VERSION_CHECK=1 \
    UV_LINK_MODE=copy \
    UV_PYTHON_DOWNLOADS=never \
    PATH="/app/.venv/bin:$PATH"

WORKDIR /app

# `make` is the single documented test entry point, so the image must ship it.
RUN apt-get update \
 && apt-get install -y --no-install-recommends make \
 && rm -rf /var/lib/apt/lists/*

# uv is a BUILD-time tool only: it materialises .venv straight from uv.lock, so the
# image gets exactly the versions the author tested. Installing "pytest>=8" with pip
# would let a rebuild quietly pick a newer release than the lockfile records.
RUN python -m pip install --no-cache-dir "uv==0.13.7"

# Copy everything the build needs before syncing: the project itself is part of the
# lock, and `uv sync --locked` fails if pyproject and uv.lock disagree.
COPY pyproject.toml uv.lock ./
COPY README.md LICENSE ./
COPY itm/ ./itm/
COPY tests/ ./tests/
COPY fixtures/ ./fixtures/
COPY docs/ ./docs/
COPY Makefile ./

RUN uv sync --locked --no-progress \
 && python -c "import itm, pytest, sys; print('itm', itm.__version__, '| pytest', pytest.__version__, '| Python', sys.version.split()[0])"

CMD ["make", "test"]
