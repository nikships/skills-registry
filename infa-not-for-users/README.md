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
| `.env.example` | Required env vars at boot (`FASTMCP_*`, `GITHUB_APP_*`, `JWT_SIGNING_KEY`, `STORAGE_ENCRYPTION_KEY`). |

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
