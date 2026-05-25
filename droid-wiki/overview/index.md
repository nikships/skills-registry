# skills-registry

`skills-registry` is a GitHub-backed registry for AI agent skills. Skills (Markdown `SKILL.md` files plus supporting assets) live in one GitHub repository you own, and agents fetch them on demand through an MCP server instead of auto-loading every skill into the conversation's startup context.

The project ships as three coordinated deliverables out of a single repository: a POSIX installer, a Go CLI that owns every interactive surface, and a Python FastMCP server that exposes the registry to agents over stdio.

## The problem

AI tools like Claude Code, Cursor, Codex, Goose, and Windsurf each scan a local dot-folder (`~/.claude/skills`, `~/.cursor/skills`, `~/.factory/skills`, …) and load every `SKILL.md` they find into the agent's startup context. That has two costs:

- **Tokens.** Every skill is paid for on every conversation, whether the agent uses it or not.
- **Drift.** Editing a skill in one dot-folder leaves the others stale. There's no shared source of truth.

`skills-registry` flips the model. Skills live in one GitHub repository. The only thing auto-loaded into each agent is a tiny `SKILL.md` pointer that teaches the agent *how* to fetch the rest via the MCP tools.

## The three deliverables

| Deliverable | Language | Distribution | Job |
| --- | --- | --- | --- |
| `install.sh` | POSIX `sh` | `curl … \| sh` from raw GitHub content | Detect OS/arch, download the matching Go tarball from the latest release, drop the binary into `~/.local/bin/skills-registry`. |
| `skills-registry` | Go 1.24+ | GitHub Releases tarballs for darwin/linux/windows × amd64/arm64 | Charmbracelet TUI + headless commands. Bare invocation routes to the wizard, the hub, or a help dump. |
| `skills-registry-mcp` | Python 3.10+ | PyPI wheel (`skills-registry`) | FastMCP server with three tools: `list_skills`, `get_skill`, `publish_skill`. |

The Go binary auto-installs the Python entry point during onboarding via `uv tool install` → `pipx install` → `pip install --user`, so a user typing `curl … | sh` never has to touch Python directly.

## What you can do with it

- **`skills-registry`** (bare invocation) — Opens the [first-run onboarding wizard](../apps/cli/wizard-and-hub.md) (no config yet) or the [dashboard hub](../apps/cli/wizard-and-hub.md) (config exists).
- **`skills-registry list / get / sync / add / publish / remove`** — [Headless subcommands](../apps/cli/subcommands.md) for day-to-day management.
- **MCP client** (Claude Desktop, Cursor, VS Code, Codex) — Asks `list_skills` and `get_skill` through stdio. The agent fetches skills on demand instead of preloading them.

Every CLI subcommand accepts a persistent `--json` flag for [scripted use](../systems/json-output.md). Destructive commands (`sync`, `remove`) auto-promote `--yes` when `--json` is set so a piped invocation never hangs on a TUI prompt.

## Quick links

- [Architecture overview](architecture.md) — system diagrams, two upload paths, MCP boot flow
- [Getting started](getting-started.md) — install, set up, run the wizard
- [Glossary](glossary.md) — project vocabulary
- [Apps](../apps/index.md) — per-deliverable deep dives
- [Systems](../systems/index.md) — cross-cutting concerns (registry client, caching, JSON output)
- [API](../api/index.md) — MCP tools + CLI commands user-visible surface
- [Background](../background/design-decisions.md) — why this is built the way it is

## Project status

`skills-registry` is at v0.5 — usable day-to-day but pre-1.0. The MCP tool surface (`list_skills`, `get_skill`, `publish_skill`) and the CLI commands are considered stable. Internals may shift between minor versions. The PyPI package is `skills-registry`; the Python module path is `skills_mcp` (renaming would be churn without payoff). License: Apache-2.0.
