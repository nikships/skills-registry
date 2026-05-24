# syntax=docker/dockerfile:1.7

# Multi-stage build: pin Python on a slim base, install with uv for a
# reproducible wheel + dep tree, then copy to a minimal runtime layer.
FROM python:3.12-slim AS builder

ENV PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1 \
    PIP_DISABLE_PIP_VERSION_CHECK=1 \
    UV_LINK_MODE=copy \
    UV_PROJECT_ENVIRONMENT=/opt/venv

# Build-time deps for cryptography wheels (most are cached but cffi sometimes
# needs gcc). Kept minimal — runtime image won't carry these.
RUN apt-get update \
    && apt-get install -y --no-install-recommends build-essential \
    && rm -rf /var/lib/apt/lists/*

RUN pip install --no-cache-dir uv

WORKDIR /app

# Copy only what affects the dep set first so `uv sync` is cache-friendly.
COPY pyproject.toml README.md ./
COPY src ./src

# `uv sync --no-dev --frozen` would be ideal but we don't ship the lockfile to
# the registry build (it's in-repo but the wheel resolver doesn't need it).
RUN uv venv /opt/venv \
    && uv pip install --python /opt/venv/bin/python .

# ---------------------------------------------------------------- runtime
FROM python:3.12-slim AS runtime

ENV PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1 \
    PATH="/opt/venv/bin:$PATH" \
    FASTMCP_STORAGE_DIR=/data/oauth \
    HOST=0.0.0.0 \
    PORT=8000

# Non-root user; Railway is fine with this and it lets us drop privileges.
RUN groupadd --system app && useradd --system --gid app --create-home app

COPY --from=builder /opt/venv /opt/venv

# /data is the mount target for the Railway volume that persists OAuth +
# linking state. Create it eagerly so the first boot has somewhere to write
# even if the volume hasn't been attached yet (the app will then crash with
# a clear error message instead of segfaulting).
RUN mkdir -p /data/oauth && chown -R app:app /data

USER app
WORKDIR /home/app

EXPOSE 8000

# `skill-registry-mcp` is the console script from pyproject.toml.
CMD ["skill-registry-mcp"]
