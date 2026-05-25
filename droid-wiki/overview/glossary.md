# Glossary

Terms specific to this project (and one or two from neighboring ecosystems that show up constantly).

## Brand and packaging

- **skills-registry** — The project brand. Plural. Used in prose, the PyPI package name, the GitHub repository name (URL `anand-92/skills-registry`), and the README. The Python module path is still `skills_mcp` (a rename would be churn without payoff).
- **skills-registry** — Singular. The literal CLI binary name (`~/.local/bin/skills-registry`), the FastMCP server name registered in `registry_server.py:build_server`, the directory written into agent dot-folders (`<dot>/skills/skills-registry/SKILL.md`), the release artifact prefix. Keep singular when quoting commands or config snippets users type.
- **skills-registry-mcp** — The Python console script that desktop MCP clients launch. Installed via `uv tool install skills-registry` / `pipx install skills-registry` / `pip install --user skills-registry`.

## Skills

- **Skill** — A folder containing a `SKILL.md` (Markdown plus optional YAML frontmatter) and any supporting assets (`scripts/`, `assets/`, `resources/`). The unit of distribution.
- **Slug** — The filesystem-safe identifier derived from a skill's `name`. Slugify is `lower → replace non-alphanumeric with _ → strip surrounding _`. Both Python (`src/skills_mcp/registry_api.py:slugify`) and Go (`cli/internal/scan/scan.go:Slugify`) implement the same algorithm.
- **Dot-folder** — A per-agent directory under `$HOME` or the project's cwd that historically held skills: `~/.claude/skills`, `~/.cursor/skills`, `~/.factory/skills`, `.agents/skills`, etc. The 56-entry catalogue lives in `cli/internal/agents/agents.go`. After bootstrap, dot-folders only hold the tiny pointer `SKILL.md` written by `bootstrap.InstallSkillMd`.
- **Registry** — The GitHub repository that owns the canonical copies of every skill. One per user (today). Configured in `~/.config/skills-mcp/registry.toml`.

## MCP

- **MCP** — Model Context Protocol. The stdio-based RPC layer between MCP clients (Claude Desktop, Cursor, VS Code, Codex, …) and MCP servers. `skills-registry-mcp` is one such server.
- **MCP tool** — A function exposed by a server. The registry server registers three: `list_skills`, `get_skill`, `publish_skill`. Annotations on each tool (`readOnlyHint`, `destructiveHint`, `openWorldHint`) help the client decide which calls need user confirmation.
- **FastMCP** — The Python library that implements the MCP server side. `skills-registry-mcp` uses FastMCP 3.x — `FastMCP(name, instructions=..., version=__version__)` plus `@server.tool(...)` decorators.

## GitHub plumbing

- **`gh`** — The GitHub CLI. The only thing that touches GitHub credentials in this project. The MCP server runs every read and write through `gh api`. The CLI bootstrap also uses `gh repo create` and `gh auth setup-git`.
- **`gh api`** — `gh api <endpoint>` proxies to the GitHub REST API with the user's authenticated token baked in. The MCP server's `RegistryClient` is essentially a typed wrapper around it.
- **Git Data API** — The low-level GitHub endpoints for blobs, trees, commits, and refs. `RegistryClient.publish_skill` and `Client.Publish` / `Client.Delete` (Go) build atomic commits this way: list base tree → upload blobs → create new tree → create commit → fast-forward ref.
- **`PushTreeViaGit`** — The bulk-import path used only by the CLI bootstrap (`registry.Client.PushTreeViaGit`). One `git push` over HTTPS, credentials wired via `gh auth setup-git`. Bypasses GitHub's secondary rate limit on the Git Data API blob endpoint.
- **Tree SHA** — The SHA of a Git tree object. Used as the cache invalidation key: `~/.cache/skills-mcp/skills/<slug>.meta.json` records the tree SHA at fetch time, and `get_skill` re-fetches whenever the registry-reported SHA differs.
- **Secondary rate limit** — GitHub's per-endpoint quota (~80 POSTs/minute on `git/blobs`) that the Git Data API blob path trips when uploading 100+ files at once. The whole reason `PushTreeViaGit` exists.

## Internal types and routes

- **`bareRouteDecision`** — The pure routing function in `cli/cmd/skills-registry/main.go`. Maps `(isTTY, --json, configLoadErr)` to one of `bareRouteHelp` / `bareRouteWizard` / `bareRouteHub` / `bareRouteError`.
- **Wizard** — The 8-step alt-screen Bubble Tea program in `cli/cmd/skills-registry/wizard.go`. The first-run onboarding UX.
- **Hub** — The dashboard for returning users in `cli/cmd/skills-registry/hub.go`. A launch loop that runs the alt-screen `tui.HubModel`, dispatches the picked card to a per-action helper, and seeds the next frame with a result toast.
- **Toast** — A one-line success/error caption surfaced above the hub footer between iterations. `hubToast{text, ok, fatal}` in `cli/cmd/skills-registry/hub.go`.
- **`jsonout.Enabled`** — Returns `true` when `--json` was supplied. Every subcommand branches on this to skip the TUI and emit a single JSON payload.
- **Atomic publish** — A `publish_skill` / `remove` operation that replaces (or removes) the entire `<slug>/` subtree in one commit. Implemented via the Git Data API tree payload — null SHAs delete entries; new blob SHAs add/overwrite them.
- **Universal agent** — An agent target marked `Universal: true` in the agents catalogue. Always selected in the bootstrap multi-select; currently just `.agents/` (project-local, picked up by most agents).

## CLI behavior

- **`--json` mode** — Persistent flag bound at the root cobra command via `jsonout.BindFlag`. Suppresses every Bubble Tea program and prompt; emits a single JSON payload to stdout. Errors land as `{"error": "..."}` with a non-zero exit code.
- **Auto-yes** — Destructive subcommands (`sync`, `remove`) auto-promote `--yes` when `--json` is set AND stdin is not a TTY. Prevents a piped agent invocation from hanging on a confirmation prompt that can't render. See `shouldAutoYes()` in `cli/cmd/skills-registry/list.go`.
- **Bootstrap** — The standalone subcommand (`skills-registry bootstrap`) that runs the same flow the wizard does, but inline in the terminal instead of inside an alt-screen. Used for scripted invocations and as a fallback when the wizard can't render.

## Config and cache

- **`registry.toml`** — `~/.config/skills-mcp/registry.toml`. Stores the active registry's `owner/repo` and `default_branch`. Hand-editable; written by the wizard.
- **`SKILLS_REGISTRY`** — Environment variable override for the active registry. Takes precedence over `registry.toml`. Format: `owner/repo` or `owner/repo@branch`.
- **Cache root** — `~/.cache/skills-mcp/skills/`. Each downloaded skill lives in `<slug>/` with a sibling `<slug>.meta.json` recording the tree SHA at fetch time. `cache.lookup`, `cache.reserve`, `cache.commit` in `src/skills_mcp/cache.py`. The Go side mirrors the path in `cli/internal/cache/cache.go` for the Settings TUI.
