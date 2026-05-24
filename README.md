<div align="center">

<img src="docs/img/banner-v2.jpg" alt="skills-registry — One GitHub repo. Every AI agent. Skills fetched on demand." width="100%">

# skills-registry

**One GitHub repo, every AI agent. Skills fetched on demand — not auto-loaded into every startup context.**

[![CI](https://github.com/anand-92/skills-registry/actions/workflows/ci.yml/badge.svg)](https://github.com/anand-92/skills-registry/actions/workflows/ci.yml)
[![Python](https://img.shields.io/badge/python-3.10%2B-blue.svg)](https://www.python.org/downloads/)
[![License](https://img.shields.io/badge/license-Apache--2.0-green.svg)](LICENSE)
[![MCP](https://img.shields.io/badge/MCP-compatible-purple.svg)](https://modelcontextprotocol.io)
[![Built with FastMCP](https://img.shields.io/badge/built%20with-FastMCP-orange.svg)](https://github.com/jlowin/fastmcp)
[![Stars](https://img.shields.io/github/stars/anand-92/skills-registry?style=social)](https://github.com/anand-92/skills-registry/stargazers)

<!-- TODO(maintainer): drop in a TUI screenshot or short GIF here. Suggestion: `skill-registry list` mid-fuzzy-filter, saved as docs/img/hero.png. -->
<img src="docs/img/hero.png" alt="skill-registry TUI" width="720">

</div>

---

## What it does

Your AI tools — Claude Code, Cursor, Codex, Goose, Windsurf, all of them — auto-load every skill you've installed into the agent's startup context. That's tokens you pay for whether the agent uses the skill or not.

`skills-registry` flips the model: skills live in **one GitHub repo you own**, and agents fetch them on demand through an MCP server. The only thing each agent auto-loads is a tiny pointer file that teaches it *how* to fetch the rest.

**You get:**

- 🪶 **Lighter agent startup.** A directory of `SKILL.md` files no longer balloons every conversation's context window. Agents pull what they need, when they need it.
- 🏠 **One home for your skills.** No more keeping `~/.claude/skills`, `~/.cursor/skills`, and `~/.factory/skills` in sync by hand. Edit once, every agent sees it.
- 🚀 **Share and version like code.** Your registry is a Git repo. Branch it, PR it, fork your teammate's, restore old versions, the works.

---

## What's a "skill"?

A skill is a folder containing a `SKILL.md` (Markdown with optional YAML frontmatter) plus any supporting files the agent might need.

```markdown
---
name: PDF Processing
description: Extract and summarize PDF documents
---

# PDF Processing

When the user asks about a PDF, do the following:
1. Read the file with the pdf-text tool
2. Summarize section by section
...
```

That's it — one file, plus whatever reference docs or examples the agent should be able to see. Most modern AI coding tools already understand this format; `skills-registry` lets you keep them all in one place.

---

## Quick start

> **You need:** [GitHub CLI](https://cli.github.com/) installed and authenticated (`gh auth status` should succeed) and `git` on `PATH` (only required the first time, for the bulk push). No Python or `uv` required.

```bash
curl -fsSL https://raw.githubusercontent.com/anand-92/skills-registry/main/install.sh | sh
skill-registry
```

The installer drops the `skill-registry` Go binary into `~/.local/bin/`. The bare `skill-registry` invocation routes you to the right place automatically:

- **First-time users** land in the **onboarding wizard** (alt-screen TUI). Steps: scan dot-folders → pick repo name/visibility → push every skill with a single `git push` → pick agents to wire up → optionally delete the now-redundant local copies → install the `skill-registry-mcp` entry point → print the MCP JSON snippet.
- **Returning users** land in the **dashboard hub** with cards for Browse / Sync / Add / Publish / Remove / Settings.
- **Piped / `--json` invocations** print usage text instead of starting a TUI (so the binary is safe to drop into scripts).

The wizard auto-installs `skill-registry-mcp` (the Python FastMCP server) via `uv tool install` → `pipx install` → `pip install --user`, in that order. The first one that succeeds wins. Total failure prints a manual hint and continues — you'll still get the bootstrap, you'll just have to install the entry point yourself before the MCP server can launch. Opt out entirely with `SKILLS_SKIP_INSTALL=1`.

<!-- TODO(maintainer): capture an asciinema of the wizard end-to-end and embed/link here. -->

After it finishes, paste the printed JSON into your MCP client config, reload, and ask your agent something like:

> *"What skills do I have available?"*
> *"Get the `code-review` skill and use it on this PR."*

The agent calls `list_skills` and `get_skill` automatically — you never touch the MCP tools directly.

---

## Daily use

Once you're set up, run a bare `skill-registry` to open the dashboard hub, or use the explicit subcommands:

| What you want | Command |
|---|---|
| Open the dashboard | `skill-registry` |
| Browse what's in your registry | `skill-registry list` |
| Pull one skill into the current folder | `skill-registry get <slug>` |
| Push skills sitting in `.claude/skills` etc. into the registry | `skill-registry sync` |
| Pull a skill from someone else's repo into yours | `skill-registry add <owner/repo>` |
| Publish a new skill from a local folder | `skill-registry publish <path>` |
| Delete a skill from the registry + cache + agent dot-folders | `skill-registry remove <slug>` |
| Re-run the wizard / bootstrap (idempotent) | `skill-registry bootstrap` |

<!-- TODO(maintainer): drop a short GIF of `skill-registry sync` here — the multi-select TUI sells the experience. -->
<img src="docs/img/sync.gif" alt="skill-registry sync" width="640">

Most users only ever touch `list`, `get`, and `publish`. The TUI is fuzzy-filterable; press `/` to search and Enter to preview.

### `remove`: delete a skill end-to-end

```bash
skill-registry remove code-review
```

`remove` is destructive. It deletes the slug from three places in one go:

1. The GitHub registry repo — single atomic commit via the Git Data API.
2. The Python MCP server's local cache (`~/.cache/skills-mcp/skills/<slug>/` + `<slug>.meta.json`).
3. Every known AI tool dot-folder copy (`~/.claude/skills/<slug>/`, `~/.factory/skills/<slug>/`, `.agents/skills/<slug>/`, …).

Interactive runs surface a confirmation prompt before any of it fires. Pass `--yes` to skip the prompt for scripted use, or `--json` (which implies `--yes`) for machine-readable output. Removing a slug that isn't in the registry exits 1 cleanly — nothing destructive runs.

### Programmatic use — `--json`

Every subcommand accepts a persistent `--json` flag. With it set, the CLI suppresses every TUI and prompt and emits a single JSON payload to stdout. Errors land as `{"error": "..."}` and the process exits non-zero. This is the right flag when an agent (or any script) is driving the binary itself.

| Command | Payload shape |
|---|---|
| `skill-registry list --json` | `[{"slug", "name", "description"}, …]` |
| `skill-registry get <slug> --json` | `{"slug", "path"}` (on-disk dest) |
| `skill-registry publish <path> --json` | `{"slug", "sha", "url"}` |
| `skill-registry sync --json` | `{"pushed": [...slugs], "skipped": [...slugs]}` |
| `skill-registry remove <slug> --json` | `{"slug", "repo", "sha", "removed_from": [...]}` |

Destructive commands (`sync`, `remove`) auto-promote `--yes` when `--json` is set, so a piped invocation never hangs on a Bubble Tea prompt that can't render.

---

## vs. the alternatives

|  | Local dot-folders | Dotfiles repo | **skills-registry** |
|---|:---:|:---:|:---:|
| One home for all your agents | ❌ duplicated | ✅ | ✅ |
| Fetched on demand (no startup tokens) | ❌ | ❌ | ✅ |
| Versioned + branchable | ❌ | ✅ | ✅ |
| Works in every MCP client | partial | ❌ | ✅ |
| Share / fork between users | ❌ | clunky | ✅ (just clone the repo) |
| No shell or SSH config needed | ✅ | ❌ | ✅ |

---

## Configuration

Most people never touch these — the wizard sets up sensible defaults. Override them via your shell or MCP client environment when you need to:

| Variable | Default | What it does |
|---|---|---|
| `SKILLS_REGISTRY` | (from config) | Point at a different registry for one command: `owner/repo` or `owner/repo@branch`. Great for browsing a teammate's. |
| `SKILLS_LOG_LEVEL` | `INFO` | Bump to `DEBUG` if something's misbehaving. |
| `SKILLS_SKIP_INSTALL` | unset | Set to `1` to keep the wizard from auto-installing `skill-registry-mcp`. Useful when you manage the entry point yourself. |
| `SKILLS_REGISTRY_VERSION` | `latest` | Pin `install.sh` to a specific release tag (`v0.5.1`, etc.). |
| `SKILLS_BIN_DIR` | `~/.local/bin` | Where `install.sh` drops the `skill-registry` binary. |
| `XDG_CONFIG_HOME` / `XDG_CACHE_HOME` | OS default | Where the registry config and skill cache live. |

The registry repo URL itself is stored in `~/.config/skills-mcp/registry.toml`.

---

## Troubleshooting

<details>
<summary><strong>"gh not found" or exit code 3</strong></summary>

Install GitHub CLI from <https://cli.github.com/> and run `gh auth login`. `skills-registry` deliberately uses `gh` for every GitHub call — no SSH key shenanigans, no `git config user.email` required — so it has to be on your `PATH` (or in `~/.local/bin`, `/opt/homebrew/bin`, `/usr/local/bin`, or `/usr/bin`).
</details>

<details>
<summary><strong>"No registry configured"</strong></summary>

You haven't run the wizard yet, or your config file at `~/.config/skills-mcp/registry.toml` is missing. Run `skill-registry` (which opens the onboarding wizard the first time), or set `SKILLS_REGISTRY=owner/repo` directly.
</details>

<details>
<summary><strong>The MCP server doesn't show up in my client</strong></summary>

Make sure you pasted the JSON snippet the wizard printed at the end of onboarding (the absolute path to `skill-registry-mcp` matters — desktop MCP clients don't inherit your shell `PATH`). Then fully restart the client (not just reload). If the wizard couldn't auto-install the entry point, install it yourself with `uv tool install skills-registry` or `pipx install skills-registry` and re-run `skill-registry` to refresh the printed path.
</details>

<details>
<summary><strong>Multiple GitHub accounts</strong></summary>

`skills-registry` uses whichever account `gh auth status` says is active. Use `gh auth switch` before running `skill-registry` to pick the right one.
</details>

<details>
<summary><strong>"git not found" during onboarding</strong></summary>

The first-time bulk push uses a single `git push` to dodge GitHub's secondary rate limit. Install git (macOS: `brew install git`; Linux: `apt install git` / `dnf install git`; Windows: https://git-scm.com/downloads) and re-run `skill-registry`. After onboarding, `git` is no longer needed — the MCP server and the single-skill `publish` / `remove` commands all route through `gh api`.
</details>

---

## Manual MCP client config

The wizard prints platform-correct JSON at the end of onboarding, but if you prefer to set it up by hand:

<details>
<summary>Claude Code / Claude Desktop / Cursor / VS Code (<code>mcp.json</code>)</summary>

```json
{
  "mcpServers": {
    "skill-registry": {
      "command": "/Users/you/.local/bin/skill-registry-mcp"
    }
  }
}
```
</details>

<details>
<summary>Codex (<code>~/.codex/config.toml</code>)</summary>

```toml
[mcp_servers.skill-registry]
command = "/Users/you/.local/bin/skill-registry-mcp"
```
</details>

---

## Project status

`skills-registry` is at **v0.5** — usable day-to-day but pre-1.0. The MCP tool surface (`list_skills`, `get_skill`, `publish_skill`) is stable. The CLI commands are stable. Internals may shift between minor versions; pin to a specific version if that worries you.

Found a bug? Have an idea? [Open an issue](https://github.com/anand-92/skills-registry/issues). PRs welcome — see [`CONTRIBUTING.md`](CONTRIBUTING.md).

---

## More

- 📖 [`docs/registry.md`](docs/registry.md) — architecture deep dive (Git Data API, caching, atomic publish)
- 🛡️ [`SECURITY.md`](SECURITY.md) — threat model and reporting
- 🤖 [`AGENTS.md`](AGENTS.md) — contributor notes for AI assistants working in this repo

---

[Apache-2.0](LICENSE) · made by [@anand-92](https://github.com/anand-92)
