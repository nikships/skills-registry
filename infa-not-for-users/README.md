# infa-not-for-users — hosted MCP server (maintainer only)

> **End users don't install or run anything in this folder.** Skip it.
> Point your MCP client at `https://mcp.skills-registry.dev/mcp` and read
> the root `README.md` for the user-facing flow.

This subtree holds the source + deploy artifacts for the hosted FastMCP server
that runs at `https://mcp.skills-registry.dev`. It exists only so the
maintainer can iterate on the server in the same repo as the CLI. Railway
auto-deploys this folder on every push to `main`.

## What's in here

| File / folder | Role |
|---|---|
| `skills_mcp/` | Python package (FastMCP server, GitHub App client, link store, webhooks, OAuth setup routes, frontmatter parser). |
| `tests/` | `pytest` suite for the server. |
| `pyproject.toml` | Server build manifest (hatchling, static placeholder version — the server is never released or tagged). |
| `ruff.toml` | Lint / format config — server-only. |
| `uv.lock` | Lock file for `uv sync --group dev`. |
| `Dockerfile` | Multi-stage build that bakes the wheel into a slim Python runtime. |
| `railway.json` | Railway service config (DOCKERFILE builder, healthcheck, restart policy). |
| `.dockerignore` | Build-context filter. |
| `.env.example` | Required env vars at boot (`FASTMCP_*`, `GITHUB_APP_*`, `JWT_SIGNING_KEY`, `STORAGE_ENCRYPTION_KEY`) plus optional `POSTHOG_PROJECT_TOKEN` / `POSTHOG_HOST` for analytics. |

## Production safeguards

The hosted server runs with a fixed middleware stack and a few in-process
caches. Everything is wired in `skills_mcp/remote_server.py:build_server`
and the per-piece details live next to the code:

| Safeguard | Where | What it does |
|---|---|---|
| Error masking | `mask_error_details=True` on `FastMCP(...)` + `ErrorHandlingMiddleware` | Strips raw exception text from MCP responses so a `GitHubAppError("404 …")` never reaches the LLM. Use `ToolError` to surface user-actionable messages on purpose. |
| Per-user rate limit | `skills_mcp/middleware.py` (`RateLimitingMiddleware`) | Token bucket keyed on the GitHub OAuth `sub` claim. **5 req/s sustained, 15-request burst, per user.** Constants are hardcoded; tuning them is a code review, not a Railway env-var flip. |
| Structured request logs | `StructuredLoggingMiddleware` | JSON request/response log per accepted call (client id, method, duration). Honors `SKILLS_LOG_LEVEL`. |
| GitHub fan-out cap | `skills_mcp/github_api.py` (`_FANOUT_CONCURRENCY = 8`) | Bounds concurrent SKILL.md fetches per `search_skills` so a 500-folder registry doesn't trip GitHub's secondary rate limit. |
| Installation-token cache | `skills_mcp/github_app.py` (per-process dict + `asyncio.Lock`) | Caches installation access tokens until 60 s before `expires_at`. Cuts roughly half the GitHub round-trips out of the hot path. |
| Webhook replay protection | `skills_mcp/linking.py:DeliveryStore` + `webhooks.py` | Dedupes by `X-GitHub-Delivery` within a 25-hour window so a captured signed payload (or a legitimate GitHub redelivery) can't re-mutate link state. |
| PostHog analytics (optional) | `skills_mcp/analytics.py` | Shared client built once at import. Emits product-usage events (`search_skills_called`, `get_skill_called`, `user_not_linked`, `repo_linked`, `repo_unlinked`, `webhook_deduped`, `webhook_rejected`) plus PostHog SDK exception autocapture. Falls back to a no-op stub when `POSTHOG_PROJECT_TOKEN` is unset, so dev / CI never need it. See `docs/registry.md`. |

**Why these numbers and not knobs.** Both read tools fan out to GitHub, so
even a low MCP request rate maps to many GitHub calls. The 5-RPS limit is
the largest value that still lets us serve all current users without
threatening GitHub's per-installation REST allowance, and the 15-burst
budget covers the typical agent opening (`search_skills` + a handful of
`get_skill` calls back-to-back). If a user routinely hits these limits,
that's a signal to add caching or pagination, not raise the cap.

**Single-instance assumption.** This deployment runs as one Railway
container. Two pieces of in-process state assume that:

* `FileTreeStore` (OAuth state + link store + `webhook_deliveries`) is
  backed by the Railway volume at `FASTMCP_STORAGE_DIR`. Multiple
  instances would each see only their local file tree.
* The installation-token cache in `GitHubAppClient` lives in Python
  memory and cannot be shared across processes.

Going horizontal therefore means swapping in a shared backend for both —
e.g. Redis via `py-key-value-aio`'s Redis store, with a small migration
of the linking dataclasses. Until then, scale vertically.

## Local development

```bash
cd infa-not-for-users
uv sync --group dev
uv run ruff check .
uv run ruff format --check .
uv run pytest -q
```

## Deploy

Railway is wired to the GitHub repo. Pushing to `main` triggers a build off
this folder. The Railway service must have its **Root Directory** set to
`infa-not-for-users` in the dashboard so the `Dockerfile` + `railway.json`
resolve correctly, and a volume must be mounted at `/data` for the OAuth
state and link table to persist across deploys. There is no release tag
for the server — it just deploys on push. Required boot-time env vars are
listed in `.env.example`.
