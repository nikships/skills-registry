# Systems

Active contributors: Nik Anand

## What this section covers

The `systems` pages document the cross-cutting machinery that isn't owned by any one app. The Go CLI and the Python MCP server both lean on these modules: the registry client that wraps `gh api`, the on-disk cache the MCP server uses to short-circuit downloads, the agent dot-folder catalogue both sides consult to decide where skills live on the filesystem, and the install / output plumbing that makes the wizard's auto-install and `--json` output predictable.

A few of these pages have direct counterparts in Python and Go — the registry client is the obvious one — and the page treats them as a single contract with two implementations. Where one side is canonical (the agent catalogue lives only in Go now), the page says so.

## The systems

| Page | What it is | Source files |
| --- | --- | --- |
| [Registry client](registry-client.md) | The Git Data API client used for `list_skills`, `get_skill`, single-skill `publish`, and `remove`. Six-call atomic publish with retries; 8-way parallel blob upload in Go. | `src/skills_mcp/registry_api.py`, `cli/internal/registry/registry.go` |
| [Bootstrap push](bootstrap-push.md) | The bulk-import path: one `git push` for the whole tree, sidestepping GitHub's per-file secondary rate limit. Used only by the wizard / `bootstrap` subcommand. | `cli/internal/registry/registry.go` (`PushTreeViaGit`) |
| [Caching](caching.md) | The Python MCP server's on-disk cache (`~/.cache/skills-mcp/skills/<slug>/`) with tree-SHA invalidation. The Go side mirrors the path for the Settings TUI display only. | `src/skills_mcp/cache.py`, `cli/internal/cache/cache.go` |
| [Agent catalogue](agent-catalogue.md) | The 56-entry single source of truth for every known AI tool dot-folder, with display names and install-path resolution. | `cli/internal/agents/agents.go` |
| [JSON output](json-output.md) | The persistent `--json` flag and the `Print` / `PrintError` helpers every CLI subcommand uses to emit machine-readable output. Includes the auto-`--yes` behavior for destructive commands. | `cli/internal/jsonout/jsonout.go` |
| [MCP entry-point install](mcp-entry-point-install.md) | The `uv tool install` → `pipx install` → `pip install --user` cascade the wizard runs to put `skills-registry-mcp` on disk for desktop MCP clients. | `cli/internal/bootstrap/mcp_install.go`, `src/skills_mcp/init.py` |

## Where they fit

The two registry paths (REST blob upload vs single `git push`) sit at the centre of the architecture; everything else in this section is a supporting module. See [overview/architecture](../overview/architecture.md) for how they connect, [apps/cli/index](../apps/cli/index.md) for how subcommands consume them, and [apps/mcp-server](../apps/mcp-server.md) for how the FastMCP tools wrap the registry client.
