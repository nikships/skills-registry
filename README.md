<div align="center">

<img src="docs/img/banner-v2.jpg" alt="skills-registry — One GitHub repo. Every AI agent. Skills fetched on demand." width="100%">

# skills-registry

**One GitHub repo, every AI agent. Skills fetched on demand — not auto-loaded into every startup context.**

[![CI](https://github.com/anand-92/skills-registry/actions/workflows/ci.yml/badge.svg)](https://github.com/anand-92/skills-registry/actions/workflows/ci.yml)
[![License](https://img.shields.io/badge/license-Apache--2.0-green.svg)](LICENSE)
[![MCP](https://img.shields.io/badge/MCP-compatible-purple.svg)](https://modelcontextprotocol.io)
[![Built with FastMCP](https://img.shields.io/badge/built%20with-FastMCP-orange.svg)](https://github.com/jlowin/fastmcp)
[![Stars](https://img.shields.io/github/stars/anand-92/skills-registry?style=social)](https://github.com/anand-92/skills-registry/stargazers)

<!-- TODO(maintainer): drop in a TUI screenshot or short GIF here. Suggestion: `skills-registry list` mid-fuzzy-filter, saved as docs/img/hero.png. -->
<img src="docs/img/hero.png" alt="skills-registry TUI" width="720">

</div>

---

## What it does

AI tools like Claude Code, Cursor, Codex, Goose, and Windsurf auto-load every installed skill into the agent's startup context — tokens you pay for whether the agent uses them or not.

`skills-registry` flips this: skills live in **one GitHub repo you own**, and agents fetch them on demand through a hosted MCP server. Each agent auto-loads only a tiny pointer file telling it *how* to fetch the rest.

**You get:**

- 🪶 **Lighter agent startup.** Skills no longer balloon every conversation's context window. Agents pull what they need, when they need it.
- 🏠 **One home for your skills.** Stop syncing `~/.claude/skills`, `~/.cursor/skills`, and `~/.factory/skills` by hand. Edit once, every agent sees it.
- 🚀 **Share and version like code.** Your registry is a Git repo — branch it, PR it, fork a teammate's, restore old versions.

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

One file, plus any reference docs or examples. Most modern AI coding tools already understand this format; `skills-registry` keeps them in one place.

---

## Quick start

> **You need:** [GitHub CLI](https://cli.github.com/) installed and authenticated (`gh auth status` succeeds), and `git` on `PATH` (only for the first-time bulk push). No Python — the MCP server is hosted.

```bash
curl -fsSL https://raw.githubusercontent.com/anand-92/skills-registry/main/install.sh | sh
skills-registry
```

The installer drops the `skills-registry` Go binary into `~/.local/bin/`. Bare `skills-registry` routes automatically:

- **First-time users** → **onboarding wizard** (alt-screen TUI): scan dot-folders → pick repo name/visibility → push every skill with one `git push` → pick agents to wire up → optionally delete the now-redundant local copies → print the hosted-MCP JSON snippet.
- **Returning users** → **dashboard hub** with cards for Browse / Sync / Add / Publish / Remove / Settings.
- **Piped / `--json` invocations** → usage text instead of a TUI (safe to drop into scripts).

The wizard ends with a JSON snippet for your MCP client config, pointing at `https://mcp.skills-registry.dev/mcp`. Your client handles the GitHub OAuth on first connect; the server can then read and (with permission) write to your registry repo.

<!-- TODO(maintainer): capture an asciinema of the wizard end-to-end and embed/link here. -->

Paste the printed JSON into your MCP client config, reload, and ask:

> *"What skills do I have available?"*
> *"Get the `code-review` skill and use it on this PR."*

The agent calls `search_skills` and `get_skill` automatically — you never touch the MCP tools directly.

---

## Daily use

Run `skills-registry` for the dashboard, or use subcommands directly:

| What you want | Command |
|---|---|
| Open the dashboard | `skills-registry` |
| Browse what's in your registry as an interactive list | `skills-registry list` |
| Fuzzy-search your registry returning top 10 matches | `skills-registry search [QUERY]` |
| Pull one skill into the current folder | `skills-registry get <slug>` |
| Push skills sitting in `.claude/skills` etc. into the registry | `skills-registry sync` |
| Pull a skill from someone else's repo into yours | `skills-registry add <owner/repo>` |
| Publish a new skill from a local folder | `skills-registry publish <path>` |
| Delete a skill from the registry + cache + agent dot-folders | `skills-registry remove <slug>` |
| Update the installed binary to the latest release | `skills-registry update` |
| Re-run the wizard / bootstrap (idempotent) | `skills-registry bootstrap` |

<!-- TODO(maintainer): drop a short GIF of `skills-registry sync` here — the multi-select TUI sells the experience. -->
<img src="docs/img/sync.gif" alt="skills-registry sync" width="640">

Most users only touch `list`, `get`, and `publish`. The TUI is fuzzy-filterable; press `/` to search, Enter to preview.

### `remove`: delete a skill end-to-end

```bash
skills-registry remove code-review
```

`remove` is destructive. It deletes the slug from three places at once:

1. The GitHub registry repo — single atomic commit via the Git Data API.
2. The local cache (`~/.cache/skills-mcp/skills/<slug>/` + `<slug>.meta.json`).
3. Every known AI tool dot-folder copy (`~/.claude/skills/<slug>/`, `~/.factory/skills/<slug>/`, `.agents/skills/<slug>/`, …).

Interactive runs prompt for confirmation first. Pass `--yes` to skip it, or `--json` (which implies `--yes`) for machine-readable output. Removing a slug that isn't in the registry exits 1 cleanly — nothing destructive runs.

### `update`: self-update the installed binary

```bash
skills-registry update                  # pull the newest GitHub release
skills-registry update --dry-run        # show what would change, write nothing
skills-registry update --version v0.6.0 # pin a specific tag
skills-registry update --force          # reinstall even if you're already current
```

`update` mirrors `install.sh` — it hits `api.github.com` to resolve the latest tag, downloads `skills-registry_<os>_<arch>.tar.gz` directly from GitHub Releases, and atomically swaps the binary in place. No `gh` required, no auth, no shell state. Supports `darwin/linux × amd64/arm64`; Windows users still use `install.sh` via WSL. Set `SKILLS_REGISTRY_AUTO_UPDATE=1` in your shell to check for updates automatically right before the hub opens.

### Programmatic use — `--json`

Every subcommand accepts a persistent `--json` flag. With it, the CLI suppresses TUIs and prompts and emits a single JSON payload to stdout. Errors land as `{"error": "..."}` with a non-zero exit. Use this when an agent or script drives the binary.

| Command | Payload shape |
|---|---|
| `skills-registry list --json` | `[{"slug", "name", "description"}, …]` |
| `skills-registry search [QUERY] --json` | `[{"slug", "name", "description"}, …]` |
| `skills-registry get <slug> --json` | `{"slug", "path"}` (on-disk dest) |
| `skills-registry publish <path> --json` | `{"slug", "sha", "url"}` |
| `skills-registry sync --json` | `{"pushed": [...slugs], "skipped": [...slugs]}` |
| `skills-registry remove <slug> --json` | `{"slug", "repo", "sha", "removed_from": [...]}` |
| `skills-registry update --json` | `{"updated", "version", "asset", "path", "message"}` |

Destructive commands (`sync`, `remove`) auto-promote `--yes` when `--json` is set, so piped invocations never hang on a Bubble Tea prompt that can't render.

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

The wizard sets sensible defaults. Override via shell env when needed:

| Variable | Default | What it does |
|---|---|---|
| `SKILLS_REGISTRY` | (from config) | Point at a different registry for one command: `owner/repo` or `owner/repo@branch`. Great for browsing a teammate's. |
| `SKILLS_LOG_LEVEL` | `INFO` | Bump to `DEBUG` when debugging. |
| `SKILLS_REGISTRY_VERSION` | `latest` | Pin `install.sh` to a release tag (`v0.7.0`, etc.). |
| `SKILLS_BIN_DIR` | `~/.local/bin` | Where `install.sh` drops the `skills-registry` binary. |
| `SKILLS_REGISTRY_AUTO_UPDATE` | unset | Set to `1`/`true`/`yes` to opportunistically run `skills-registry update` before opening the hub. Errors are warning-logged, never fatal. |
| `XDG_CONFIG_HOME` / `XDG_CACHE_HOME` | OS default | Where the registry config and skill cache live. |

The registry repo URL itself lives in `~/.config/skills-mcp/registry.toml`.

---

## Troubleshooting

<details>
<summary><strong>"gh not found" or exit code 3</strong></summary>

Install GitHub CLI from <https://cli.github.com/> and run `gh auth login`. `skills-registry` uses `gh` for every GitHub call — no SSH keys, no `git config user.email` required — so it must be on your `PATH` (or in `~/.local/bin`, `/opt/homebrew/bin`, `/usr/local/bin`, or `/usr/bin`).
</details>

<details>
<summary><strong>"No registry configured"</strong></summary>

The wizard hasn't run yet, or `~/.config/skills-mcp/registry.toml` is missing. Run `skills-registry` (it opens the wizard first run), or set `SKILLS_REGISTRY=owner/repo` directly.
</details>

<details>
<summary><strong>The MCP server doesn't show up in my client</strong></summary>

Paste the wizard's JSON snippet into your client's MCP config and fully restart the client (not just reload). On first connect the client opens a browser to authorize the Skills Registry GitHub App on your registry repo — accept it. If the server says "no repo linked yet", install the GitHub App via the link in the error and retry; the webhook auto-links within seconds.
</details>

<details>
<summary><strong>Multiple GitHub accounts</strong></summary>

`skills-registry` uses whichever account `gh auth status` reports active. Use `gh auth switch` to pick the right one.
</details>

<details>
<summary><strong>"git not found" during onboarding</strong></summary>

The first-time bulk push uses a single `git push` to dodge GitHub's secondary rate limit. Install git (macOS: `brew install git`; Linux: `apt install git` / `dnf install git`; Windows: https://git-scm.com/downloads) and re-run. After onboarding, `git` is no longer needed — `publish` and `remove` route through `gh api`.
</details>

---

## Manual MCP client config

The wizard prints this JSON; if you prefer wiring it up by hand:

```json
{
  "mcpServers": {
    "skills-registry": {
      "url": "https://mcp.skills-registry.dev/mcp"
    }
  }
}
```

Drop it into your client's `mcp.json` (Claude Code, Claude Desktop, Cursor, VS Code+Copilot all use the same shape). On first connect, your client opens a browser to authorize the Skills Registry GitHub App on your registry repo. After that, every `search_skills` / `get_skill` call goes through the hosted server — no local binary required.

> **Codex.** Codex's TOML config only accepts stdio MCPs (`command = "..."`), and the hosted server speaks Streamable HTTP. Not supported yet — use the CLI directly (`skills-registry list`, `skills-registry get <slug>`).

---

## Project status

`skills-registry` is at **v0.7** — usable day-to-day but pre-1.0. The hosted MCP read tools (`search_skills`, `get_skill`) and the CLI commands (`list` / `get` / `sync` / `add` / `publish` / `remove` / `search`) are stable. Internals may shift between minor versions; pin a CLI release with `SKILLS_REGISTRY_VERSION` if needed.

Found a bug? Have an idea? [Open an issue](https://github.com/anand-92/skills-registry/issues). PRs welcome — see [`CONTRIBUTING.md`](CONTRIBUTING.md).

---

## Repo layout

- [`cli/`](cli) — the Go binary users install (TUI + headless subcommands).
- [`install.sh`](install.sh) — POSIX one-shot installer for the Go binary.
- [`docs/`](docs) — architecture deep-dive (`registry.md`) and supporting docs.
- [`infa-not-for-users/`](infa-not-for-users) — the hosted MCP server (Python + FastMCP), Dockerfile, and Railway config. **Maintainer-only**; see its README for deployment details.
- [`website/`](website) — Next.js landing page (`skills-registry.dev`).

---

## More

- 📖 [`docs/registry.md`](docs/registry.md) — architecture deep dive (Git Data API, caching, atomic publish)
- 🛡️ [`SECURITY.md`](SECURITY.md) — threat model and reporting
- 🤖 [`AGENTS.md`](AGENTS.md) — contributor notes for AI assistants working in this repo

---

[Apache-2.0](LICENSE) · made by [@anand-92](https://github.com/anand-92)
