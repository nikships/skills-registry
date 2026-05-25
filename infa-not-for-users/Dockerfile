# syntax=docker/dockerfile:1.7

# Multi-stage build: pin Python on a slim base, install with uv for a
# reproducible wheel + dep tree, then copy to a minimal runtime layer.
FROM python:3.12-slim AS builder

ENV PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1 \
    PIP_DISABLE_PIP_VERSION_CHECK=1 \
    UV_LINK_MODE=copy

# Build-time deps for cryptography wheels (cffi sometimes needs gcc). Kept
# minimal — runtime image doesn't carry these.
RUN apt-get update \
    && apt-get install -y --no-install-recommends build-essential \
    && rm -rf /var/lib/apt/lists/* \
    && pip install --no-cache-dir uv

WORKDIR /app

# Copy manifests first so the install layer caches when only skills_mcp/ changes.
COPY pyproject.toml README.md ./
COPY skills_mcp ./skills_mcp

# pyproject.toml carries a static version; no hatch-vcs, no .git probing, no
# SETUPTOOLS_SCM_PRETEND_VERSION env shim required.
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

COPY --from=builder /opt/venv /opt/venv

# /data is the mount target for the Railway volume that persists OAuth +
# linking state. We run as root because Railway volumes mount with root
# ownership; trying to chown them at build time gets shadowed by the mount.
# The container is a single-tenant FastMCP server with no untrusted local
# input — running as root inside the container is the standard Railway
# pattern for volume-backed services.
RUN mkdir -p /data/oauth

WORKDIR /root

EXPOSE 8000

# Ensure the volume's oauth subdir exists on every boot (mount may shadow
# the build-time mkdir if the volume is empty).
CMD mkdir -p /data/oauth && exec skill-registry-mcp
