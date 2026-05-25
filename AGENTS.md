<coding_guidelines>
# Agent Notes — skills-registry

A living guide for AI agents and new contributors. Captures the architecture, patterns, and trade-offs of the current (0.7.x) **GitHub-backed registry + hosted MCP** design. The CLI binary is `skill-registry`; the GitHub repo is still `skills-mcp` (org URL unchanged); the Python module is still `skills_mcp` (renaming would be churn without payoff).

> **What changed in 0.3.0:** The project pivoted from "consolidate local skills" (gather/add) to "personal GitHub registry repo, fetched on demand". `gather` and `add` were removed. A new Go CLI handles all interactive UX, and a separate Python MCP server exposes the registry as three tools.

> **What changed in 0.5.x:** The user-facing install flow is now `curl | sh` against `install.sh`, not `uvx skills-registry init`. The Python `init` subcommand is gone; everything bootstrap-related (`gh` auth check, scan/push/agent-select) lives in the Go binary. A bare `skill-registry` routes to the **onboarding wizard** (no config), the **dashboard hub** (config present), or a help dump (non-TTY / `--json`). The Go CLI gained a `remove` subcommand and a persistent `--json` flag honored by every subcommand.

> **What changed in 0.7.x:** The MCP server is no longer something users install. It's **hosted** at `https://mcp.skills-registry.dev/mcp` (Streamable HTTP, OAuth + GitHub App), deployed from a Docker image on Railway. The PyPI wheel + sdist are gone; the Python entry point (`uv/pipx/pip install skills-registry`) is gone; the Go MCP-installer (`EnsureMCPEntryPoint`) is gone. The CLI's only MCP responsibility is printing the JSON snippet that points at the hosted URL — users paste it into their client and the OAuth flow does the rest. All the Python server code, Dockerfile, and Railway config live under `infa-not-for-users/` because users never touch it.

---

## Project Overview

`skills-registry` is **two coordinated deliverables** shipped from a single repo:

| Piece | Language | Distribution | Job |
|---|---|---|---|
| `skill-registry` (Go) | Go 1.24+ | GitHub Releases tarballs, installed by `install.sh` (`curl … \| sh`) | Charmbracelet TUI + headless commands. Bare invocation routes to wizard / hub / help. Subcommands: `bootstrap`, `list`, `get`, `sync`, `add`, `publish`, `remove`. All subcommands honor a persistent `--json` flag. |
| `skill-registry-mcp` (Python, hosted) | Python 3.10+ (FastMCP) | Docker image on Railway, served at `https://mcp.skills-registry.dev/mcp` | Streamable HTTP MCP server with **2 read-only tools** (`list_skills`, `get_skill`). All writes (`publish` / `sync` / `remove`) go through the Go CLI — the hosted server never mutates the user's repo. OAuth + GitHub App on first connect. Users never install this. |

- **Build (Python, maintainer-only):** `hatchling` (PEP 517) with a static `version = "0.0.0+server"` in `pyproject.toml`. The server is never published to PyPI, never tagged, and Railway redeploys on every push to `main`, so there's no semver to derive. The wheel exists only to provide the `skill-registry-mcp` entry point inside the Docker image.
- **Package manager (Python):** `uv`
- **Test runner (Python):** `pytest` with `pytest-cov`
- **Lint/Format (Python):** `ruff`
- **Build/Test (Go):** stdlib (`go build`, `go test`, `go vet`) + `staticcheck` + `deadcode` for dead-code / unused-symbol detection
- **TUI library:** Charmbracelet (bubbletea + lipgloss + bubbles + cobra)
- **MCP transport:** Streamable HTTP via FastMCP 3.x (the hosted server). stdio is no longer supported — Codex remains unsupported because its TOML config only accepts stdio MCPs.
- **Network surface:**
  - **Hosted MCP server (Python):** every GitHub call uses an installation-scoped GitHub App token. No `gh`, no `git`, no SSH, no user shell state. The container has only what its Dockerfile installs.
  - **CLI bootstrap (Go):** the bulk initial import (wizard step 4) uses **`git push` over HTTPS** (single push for the whole tree) because per-file `POST /git/blobs` trips GitHub's secondary rate limit on registries with dozens of skills. Auth wired through `gh auth setup-git`.
  - **CLI reads (Go):** `list`, `get`, `sync` and the hub read from a **local shallow-clone mirror** at `~/.cache/skills-mcp/mirror/<owner>/<repo>/` (see `cli/internal/registry/mirror.go`). Created with `git clone --depth=1`, fast-forwarded with `git fetch --depth=1` + `git reset --hard FETCH_HEAD`. The previous `1 + N` sequential `gh api` walk dropped from ~25 s to ~0.8 s warm on a 91-skill registry. `SKILLS_MIRROR_DISABLE=1` (or no `git` on PATH) forces the original gh-api path.
  - **CLI writes (Go):** single-skill `publish` and `remove` go through `gh api` — 1–10 files, well under the rate limit, and the atomic Git Data API path keeps strict-ordering / null-SHA semantics intact.
  - **Installer (`install.sh`):** the only one-shot `curl … | sh` surface. POSIX `sh`, detects OS/arch, downloads the matching tarball, drops the binary into `~/.local/bin/skill-registry`. Never touches Python.

---

## Repository Layout

```text
install.sh               # POSIX `curl | sh` installer — the user-facing entry point.
                         # Downloads the matching skill-registry tarball from GitHub Releases.

infa-not-for-users/      # Maintainer-only. Hosted MCP server source + Docker/Railway config.
  skills_mcp/            # Python package (no `src/` layout — packages = ["skills_mcp"] in pyproject.toml)
    __init__.py          # __version__ resolved from installed package metadata
    remote_server.py     # `skill-registry-mcp` — FastMCP build_server() + main(); registers list_skills + get_skill
    github_api.py        # Token-based GitHub REST helpers: list_skill_folders, get_skill_md, repo_has_skills
    github_app.py        # GitHubAppClient: JWT minting, installation lookup, installation-token issuance, retry
    linking.py           # LinkStore + LinkedRepo: {github_user → owner/repo} persistence on the Railway volume
    setup_routes.py      # /setup/install + post-install landing routes (GitHub App install handoff)
    webhooks.py          # /webhooks/github handler: parses `installation` events and writes to LinkStore
    frontmatter.py       # parse_frontmatter / first_paragraph helpers used by github_api
  tests/                 # pytest suite (~81 tests) covering frontmatter, github_api, github_app, linking, remote_server, setup_routes, webhooks
  pyproject.toml         # hatchling + static version + fastmcp + uvicorn + httpx + PyJWT + cryptography + py-key-value-aio + starlette
  Dockerfile             # uv → build wheel → install entry point → run skill-registry-mcp
  railway.json           # Railway service definition (volume mount at /data/oauth)
  .env.example           # OAuth + GitHub App env var template (FASTMCP_*, GITHUB_APP_*, JWT_SIGNING_KEY, STORAGE_ENCRYPTION_KEY)
  README.md              # Deployment + env-var notes (maintainer-facing)

cli/                     # Separate Go module (own go.mod) — the user-facing binary.
  cmd/skill-registry/
    main.go              # Cobra root + bare-command routing (wizard / hub / help)
    wizard.go            # First-run onboarding wizard (Bubble Tea alt-screen, 8 steps)
    hub.go               # Returning-user dashboard hub (alt-screen card grid)
    bootstrap.go         # Legacy headless `bootstrap` subcommand (still useful for scripting)
    list.go / get.go / sync.go / add.go / publish.go / remove.go   # Per-subcommand handlers
  internal/
    agents/              # 56-entry KNOWN_DOT_DIRS catalogue with display names + universal flag
    bootstrap/           # SkillMd renderer + InstallSkillMd + hosted-MCP JSON snippet (HostedMCPURL constant + MCPJSONSnippet())
    cache/               # CacheRoot() helper (mirrors the legacy Python cache.py path resolution)
    config/              # ~/.config/skills-mcp/registry.toml read/save + SKILLS_REGISTRY env override
    jsonout/             # Persistent --json flag plumbing + Print / PrintError helpers
    registry/            # Go mirror of registry_api.py (gh-api client, atomic Publish/Delete, conflict retry, PushTreeViaGit, mirror)
    scan/                # Dot-folder discovery + frontmatter parsing
    tui/                 # Bubble Tea models: list, multi-select, input, choice, hub, wizard, settings, toast

docs/
  registry.md            # Architecture deep dive (refreshed for the hosted-MCP topology)
.github/workflows/
  ci.yml                 # Two parallel jobs: `server` (ruff + pytest in infa-not-for-users/) and `cli` (vet/staticcheck/deadcode/gocyclo/build/test)
  release.yml            # CLI-only path filter (cli/**, install.sh). Builds Go binaries for 5 targets; no PyPI publish, no wheel build.
website/                 # Next.js landing page (skills-registry.dev). Static; deployed separately.
```

---

## Architecture

### Two deliverables, one repo

```text
[user] → curl https://…/install.sh | sh
            └─ install.sh (POSIX)
                ├─ detect OS/arch (uname -s/-m)
                ├─ download skill-registry_<os>_<arch>.tar.gz from GitHub Releases
                └─ drop binary into ~/.local/bin/skill-registry

[user] → skill-registry
            ├─ cobra parses persistent --json flag
            ├─ runRoot → bareRouteDecision(isTTY, jsonMode, configLoadErr)
            │     ├─ non-TTY or --json     → bareRouteHelp (print usage, exit)
            │     ├─ ErrMissing + TTY      → bareRouteWizard
            │     ├─ nil load err + TTY    → bareRouteHub
            │     └─ other load err + TTY  → bareRouteError (surface to caller)
            │
            ├─ Wizard (first-run, 8 alt-screen steps):
            │     1. ensure_authed(gh) + requireGitForBootstrap
            │     2. scan dot-folders (Bubble Tea progress)
            │     3. prompt repo name + visibility
            │     4. gh repo create → PushTreeViaGit (single `git push`)
            │     5. multi-select agents → write SKILL.md to each
            │     6. offer to delete local dot-folder copies
            │     7. print hosted-MCP JSON snippet (no install, no goroutine)
            │     8. summary + "all done" caption
            │
            └─ Hub (returning user, alt-screen card grid):
                  Browse / Sync / Add / Publish / Remove / Settings
                  Each card launches the same code path the standalone subcommand
                  uses; the result is captured as a toast and seeded into the
                  next hub frame. Quit = q / esc / ctrl+c.

[MCP client] → https://mcp.skills-registry.dev/mcp (Streamable HTTP)
            ├─ OAuth handshake on first connect (browser pop-up → GitHub)
            ├─ Server resolves {github_user → owner/repo} from its repo-link table
            │     (table populated by Skills Registry GitHub App `installation` webhook)
            └─ list_skills / get_skill → GitHub REST contents API
                  via installation-scoped GitHub App token (read-only)
```

**MCP wire-up is a static URL.** `cli/internal/bootstrap/install.go` exposes `HostedMCPURL = "https://mcp.skills-registry.dev/mcp"` and `MCPJSONSnippet()` (no arguments — no binary path to compute). The wizard's step 7 (`WizardStepMCPConnect`) and the headless `bootstrap` subcommand both print this snippet. **The CLI never installs, boots, or proxies an MCP server.** Codex remains unsupported because the hosted server speaks Streamable HTTP and Codex's TOML config only accepts stdio (`command = "..."`); the wizard prints a one-line caveat instead of a Codex snippet.

### Bare-command routing (hub / wizard / help)

`cli/cmd/skill-registry/main.go:bareRouteDecision` is the single decision point for bare `skill-registry`. Pure (no I/O), so the routing matrix is unit-testable end-to-end. The four resolutions:

| isTTY | --json | config | → route | what fires |
|---|---|---|---|---|
| any | `true` | any | `bareRouteHelp` | print usage; exit 0 |
| `false` | `false` | any | `bareRouteHelp` | print usage; exit 0 |
| `true` | `false` | `ErrMissing` | `bareRouteWizard` | first-run onboarding wizard |
| `true` | `false` | nil | `bareRouteHub` | dashboard hub |
| `true` | `false` | other err | `bareRouteError` | surface the malformed-config error |

Bare invocation should always land somewhere safe: non-TTY → no Bubble Tea (can't render); `--json` → no Bubble Tea (caller asked for stdout); otherwise route by config presence.

### The dashboard hub

`cli/cmd/skill-registry/hub.go:runHub` is a launch loop. Each iteration:

1. Loads the registry config (fail-fast on read error).
2. Builds `tui.HubModel` with the repo + a closure that lazily lists the registry to populate the skill count.
3. Optionally injects a pending toast (set by the previous iteration's dispatcher).
4. Runs the Bubble Tea program with `tea.WithAltScreen()`.
5. Reads `Selection()` from the post-quit model and switches into the matching per-action helper (`runBrowseFromHub`, `runSyncFromHub`, `runAddFromHub`, `runPublishFromHub`, `runRemoveFromHub`, `runSettingsFromHub`).
6. Each helper returns a `hubToast` (text + ok/err) that's threaded into the next loop iteration.

The loop terminates on `Quit()` (q / esc / ctrl+c) or when a launcher-level error makes continuing pointless. Per-action failures land as red toasts; the user sees them on the next frame and can retry.

### Why a separate Go binary?

The `building-glamorous-tuis` skill recommends Charmbracelet (Go), which has no first-class Python equivalent. Building the bootstrap UX in Bubble Tea required a Go binary, so `install.sh` drops the Go binary directly and **the user never sees Python**. The hosted MCP server is a service users connect to, not software they install.

### Two upload paths in the CLI: `gh api` for day-to-day writes, `git push` for bootstrap

The **hosted MCP server is read-only.** It runs in a Docker container with no shell state — no `gh`, no `git`, no SSH, no `user.email` — and its only credential is an installation-scoped GitHub App token fetched per request. `list_skills` and `get_skill` are served via the GitHub Contents API; the server never invokes anything that could mutate the user's repo.

That means **every write lives in the Go CLI** (`publish`, `sync`, `add`, `remove`). The CLI has two upload paths:

1. **Single-skill `gh api` blob path** (`registry.Client.Publish` / `registry.Client.Delete`). Used by `publish`, `add`, `sync`, and `remove`. Each call walks the standard atomic-commit dance:

```text
GET  /repos/{r}/git/ref/heads/{branch}        → parent SHA
GET  /repos/{r}/git/commits/{parent}          → base tree SHA
GET  /repos/{r}/git/trees/{base}?recursive=1  → list stale files under <slug>/
POST /repos/{r}/git/blobs                     → upload each file
POST /repos/{r}/git/trees                     → new tree referencing base + blobs (+ null SHAs for deletions)
POST /repos/{r}/git/commits                   → commit pointing at new tree, parents=[parent]
PATCH /repos/{r}/git/refs/heads/{branch}      → fast-forward ref
```

Conflicts (409/422) trigger up to 3 retries with exponential backoff against the freshly-fetched HEAD. Fine for one skill: ~1–10 files, well under the secondary-rate-limit threshold. `Client.Delete` uses the same six-call sequence with null SHAs in the new tree entries to drop the slug atomically — see §3.3 of `docs/registry.md`.

2. **Bulk `git push` path** (`registry.Client.PushTreeViaGit`). The wizard's step 4 only. A first-time user typically has 30–200 skills (≈100–500 files); per-file blob POSTs trip GitHub's secondary rate limit at ~80 requests/minute. `PushTreeViaGit` sidesteps that with one `git push`:

1. `gh auth setup-git --hostname github.com` (idempotent — wires `gh` as the HTTPS credential helper).
2. `gh api user` → commit author name/email (falls back to `<login>@users.noreply.github.com`).
3. If the branch already exists upstream, shallow-clone it; otherwise `git init -b main` in a tempdir and add the remote.
4. Materialize every file in the tempdir; `git add -A`; commit; `git push -u origin main`.
5. Tempdir removed on exit; nothing persists outside the user's `~/.gitconfig` (which now references `gh` as credential helper for github.com).

Hard requirements for the bulk path: `git` on PATH and an authenticated `gh`. `cmd_bootstrap` fails fast (before any prompts) when `git` is missing, with an install hint. Outside the wizard, every CLI write stays on the `gh api` blob path — never close to the rate limit.

### Caching

The CLI's `get` writes to `~/.cache/skills-mcp/skills/<slug>/` with a sibling `<slug>.meta.json` storing the **registry tree SHA** at fetch time. Next call:
1. Ask the registry for the current `<slug>/` tree SHA.
2. SHA matches → return the cached path immediately.
3. Otherwise wipe the folder and re-download.

Force-pushes and any subtree change invalidate correctly. The hosted MCP does not cache between requests — every `get_skill` reads through to GitHub.

### Single source of truth for agent dot-folders

`cli/internal/agents/agents.go` holds the canonical 56-entry list of known AI tool dot-folders, each with a display name and a `Universal`/`UnderHome` flag. The Python side doesn't need this list — the only consumer (the legacy `gather` command, removed in 0.3.0) is gone. Go-only.

---

## Key Symbols

| Symbol | File | Role |
|---|---|---|
| `build_server()` | `infa-not-for-users/skills_mcp/remote_server.py` | Constructs the FastMCP server, validates settings at boot, and registers the two read-only tools (`list_skills`, `get_skill`). Returns `(FastMCP, LinkStore, GitHubAppClient)`. |
| `list_skill_folders` / `get_skill_md` / `repo_has_skills` | `infa-not-for-users/skills_mcp/github_api.py` | Token-based GitHub REST helpers used by the hosted server's read tools. No `gh` binary, no `git`. |
| `GitHubAppClient` | `infa-not-for-users/skills_mcp/github_app.py` | Mints the JWT, looks up the installation for a given user, and exchanges JWT → installation access token per request. Owns the retry policy. |
| `LinkStore` / `LinkedRepo` | `infa-not-for-users/skills_mcp/linking.py` | Persists `{github_user → owner/repo}` on the Railway-backed volume. Populated by the `installation` webhook (`webhooks.py`) and read on every tool call. |
| `parse_frontmatter` / `first_paragraph` | `infa-not-for-users/skills_mcp/frontmatter.py` | YAML-ish frontmatter parser + description fallback used by `github_api` to render `list_skills` rows. |
| `registry.Client` | `cli/internal/registry/registry.go` | The Go-side GitHub Git Data API client: `Publish`, `Delete`, `PushTreeViaGit`, list/get mirror operations. All CLI writes flow through here. |
| `validateRelPath` | `cli/internal/registry/registry.go` | Path-traversal guard for repo-relative paths. Rejects `..`, absolute paths, and empty strings. Applied to every file before blob upload or `git add`. |
| `bareRouteDecision` | `cli/cmd/skill-registry/main.go` | Pure routing function for `skill-registry` with no subcommand: returns `bareRouteHelp` / `bareRouteWizard` / `bareRouteHub` / `bareRouteError`. |
| `runWizard` | `cli/cmd/skill-registry/wizard.go` | First-run alt-screen Bubble Tea wizard. 8 steps, owns scan/repo-create/push/agent-install/cleanup/MCP-snippet/done. |
| `runHub` | `cli/cmd/skill-registry/hub.go` | Returning-user dashboard loop: launches `tui.HubModel`, dispatches the picked action, seeds the next frame with a toast. |
| `runBootstrap` | `cli/cmd/skill-registry/bootstrap.go` | Headless / scripted bootstrap (legacy flow, still useful for CI). |
| `HostedMCPURL` / `MCPJSONSnippet` | `cli/internal/bootstrap/install.go` | The Streamable-HTTP URL the wizard prints (`https://mcp.skills-registry.dev/mcp`) and the JSON formatter for `mcp.json`. No binary lookup; no install path. |
| `WizardStepMCPConnect` / `startMCPConnect` | `cli/internal/tui/wizard.go`, `cli/internal/tui/wizard_steps.go` | Step 7 of the wizard — purely informational. Synchronous snapshot of `MCPJSONSnippet()`, no goroutine. |
| `jsonout.BindFlag` / `Enabled` / `Print` / `PrintError` | `cli/internal/jsonout/jsonout.go` | Persistent `--json` flag plumbing. Every subcommand checks `Enabled()` and branches into a JSON-only code path. |
| `Client.Delete` | `cli/internal/registry/registry.go` | Atomic `<slug>/` removal via the Git Data API. Mirrors `Publish` but builds a tree with null-SHA entries. Used by `remove` (and the hub's Remove card). |
| `FindGH` | `cli/internal/registry/registry.go` | PATH + fallback lookup for the `gh` CLI. CLI-side only — the hosted server doesn't shell out to anything. |
| `MultiSelectModel` | `cli/internal/tui/multiselect.go` | Fuzzy-searchable multi-select with locked-universal section. |
| `SkillMd` | `cli/internal/bootstrap/skillmd.go` | Sole source of the generated `skill-registry/SKILL.md` template (CLI-only; written into each agent dot-folder by Go bootstrap). Documents both the hosted MCP (preferred for reads) and the CLI (writes + fallback reads). |
| `scan.Discover` | `cli/internal/scan/scan.go` | Local skill discovery + frontmatter parsing. Used by `sync`, `add`, `bootstrap`. |

---

## Testing

- **Python (hosted server):** 81 tests covering `frontmatter`, `github_api`, `github_app`, `linking`, `remote_server`, `setup_routes`, `webhooks`. GitHub REST calls are stubbed with `httpx.MockTransport`; OAuth + GitHub App flows use scripted JWT fixtures. Run from `infa-not-for-users/`:
  ```bash
  cd infa-not-for-users && uv run pytest -v --cov=skills_mcp --cov-report=term-missing
  ```
- **Go:** Tests for `agents`, `bootstrap`, `config`, `scan`, `registry`, `tui` (also uses a `gh` shim invoked via `/bin/sh` → `python3`). Run with `cd cli && go test ./...`.
- Run everything:
  ```bash
  (cd infa-not-for-users && uv run pytest -v --cov=skills_mcp --cov-report=term-missing)
  (cd cli && go vet ./... && staticcheck ./... && deadcode -test ./... && gocyclo -over 15 -ignore "_test" . && go test ./...)
  ```
- **Dead-code detection (Go):** CI runs `staticcheck ./...` (scoped via `cli/staticcheck.conf` to disable the noisy `ST*`/`QF*` style families while keeping every unused-symbol/correctness check) plus `deadcode -test ./...` for reachability-based unused-function analysis. Both must be green to merge. See **How to Work on This Repo** below for pinned install commands.
- **Cyclomatic-complexity ceilings:** Python: ruff's `C90` (mccabe) with `max-complexity = 12` in `infa-not-for-users/ruff.toml`. Go: CI runs `gocyclo -over 15 -ignore "_test"` on `cli/` — the industry standard for Go production code (test files excluded because table-driven tests inflate complexity naturally). Both are enforced in `ci.yml` and `release.yml`. Never raise them casually; extract helpers if a new function exceeds the limit.

---

## Known Issues & Improvement Opportunities

### Outstanding

1. **No `update` command.** `remove` shipped in F4.1; in-place `update` would still be useful (today users `publish` from a folder, which works but doesn't surface "what changed").
2. **No multi-registry support.** Config is one-repo. A `[registries]` array + `connect <owner/repo>` would let an agent see several side-by-side.
3. **Browsing third-party public registries** isn't a first-class flow. The read tools (`list_skills`, `get_skill`) don't require write access — wiring them to an arbitrary `owner/repo` would be a few lines.
4. **Windows installer.** `install.sh` is POSIX-only. The Go binary builds for `windows/amd64`, but Windows users need an `install.ps1` (and `gh.exe` lookup in `FindGH`) for the same one-shot experience.
5. **`get_skill_md` does no schema validation** of the SKILL.md it serves. Malformed skills are silently skipped by `list_skills`; a verbose-mode error log in the hosted server would help diagnose user reports.
6. **No server-side cache.** Every `get_skill` reads through to GitHub. A short-TTL in-process cache keyed on tree SHA would cut latency for hot slugs.
7. **Codex unsupported by the hosted MCP.** Codex's TOML config only accepts stdio MCPs. Either Codex needs Streamable HTTP, or we'd ship a stdio→HTTP shim for Codex specifically.

### Carried over from the previous design

- **Frontmatter parser is YAML-ish.** Both Python and Go avoid a real YAML dep; multi-line values, lists, and nested keys are silently dropped. Fine for the current scope.

---

## CI / CD

- `.github/workflows/ci.yml` — two parallel jobs: `server` (ruff lint + format + pytest with coverage from `infa-not-for-users/`) and `cli` (vet + staticcheck + deadcode + gocyclo + build + test from `cli/`). Both must be green to merge.
- `.github/workflows/release.yml` — **auto-releases on every push to `main` touching `cli/**` or `install.sh`**. Path filter is the release decision; commits that only touch the hosted server (`infa-not-for-users/`), docs, workflows, or the website do not release. The hosted server is redeployed by Railway directly from `main` — no PyPI publish, no wheel build, no version tag for the server.
  1. Tests gate (go vet + staticcheck + deadcode + gocyclo + go test) — must pass.
  2. Tag job computes the next semver from the latest `vX.Y.Z` tag, then pushes a lightweight tag on the triggering commit. CI never commits version bumps back to `main`.
  3. Builds the Go CLI for `darwin/amd64`, `darwin/arm64`, `linux/amd64`, `linux/arm64`, `windows/amd64`.
  4. Creates one GitHub Release `vX.Y.Z` containing 4 tarballs + 1 zip. `install.sh` downloads from this same "latest" release.
- Force a non-patch bump with `gh workflow run release.yml -f bump=minor` (or `major`).
- **Gaps:** No Python version matrix for the server, no OS matrix for server tests, no Dependabot, no codecov upload (coverage XML is generated but not uploaded), no integration tests that actually call GitHub. The Go test gate in `release.yml` is a near-duplicate of `ci.yml`'s `cli` job; change one, check the other.

---

## Security Notes

- **Hosted MCP server (Python):** runs in a Docker container with no `gh`, no `git`, no SSH, no user shell state. All GitHub I/O uses installation-scoped GitHub App tokens fetched per request via `GitHubAppClient`. OAuth state + repo-link table live on a Railway-backed volume at `/data/oauth/`. The server is **read-only** — it never mutates the user's repo, so it has no need for write-path safety checks.
- **CLI writes (Go):** `registry.Client.Publish` and `registry.Client.Delete` shell out to `gh api` (no token in argv/env/disk — `gh` resolves credentials from its own store). `registry.Client.PushTreeViaGit` shells out to `git` over HTTPS with credentials resolved by `gh auth setup-git`. The bootstrap tempdir is `os.RemoveAll`'d on exit.
- `subprocess.run()` (Python) and `exec.CommandContext()` (Go) are used with list args; no `shell=True` / `sh -c`.
- **Path-traversal guard (Go):** `validateRelPath` rejects `..`, `../`, `/../`, absolute paths, and empty strings; it normalizes separators and re-checks via `filepath.Clean`. Applied to every file before blob upload (`Publish`) and before `git add` (`PushTreeViaGit`).
- `Client.Publish` skips dotfiles (`.git`, `.DS_Store`, …) and `__pycache__` directories so accidental upload of editor or build artifacts can't slip through.
- A per-file size cap (`SKILLS_MAX_FILE_BYTES`, default 2 MiB) prevents accidental upload of huge binaries.

---

## How to Work on This Repo

```bash
# Setup — CLI (the user-facing piece)
(cd cli && go mod download)

# Setup — hosted MCP server (maintainer-only)
(cd infa-not-for-users && uv sync --group dev)

# Install Go dead-code analyzers (versions pinned to match CI; see
# .github/workflows/ci.yml — bump in lockstep)
go install honnef.co/go/tools/cmd/staticcheck@2025.1.1
go install golang.org/x/tools/cmd/deadcode@v0.45.0
go install github.com/fzipp/gocyclo/cmd/gocyclo@v0.6.0

# Run all tests
(cd infa-not-for-users && uv run pytest -v --cov=skills_mcp --cov-report=term-missing)
(cd cli && go vet ./... && go test ./...)

# Dead-code detection (Go)
(cd cli && staticcheck ./... && deadcode -test ./...)

# Cyclomatic-complexity ceiling (Go)
(cd cli && gocyclo -over 15 -ignore "_test" .)

# Lint & format Python
(cd infa-not-for-users && uv run ruff check . && uv run ruff format .)

# Smoke-test the Go binary locally
(cd cli && go build -o /tmp/skill-registry ./cmd/skill-registry && /tmp/skill-registry --help)

# Build & run the hosted server in Docker locally (maintainer-only)
(cd infa-not-for-users && docker build -t skill-registry-mcp:dev . && docker run --rm -p 8000:8000 skill-registry-mcp:dev)
```

When making changes:
- **FastMCP server conventions.** Construct servers with `FastMCP(name, instructions=..., version=__version__)`. Register every tool through `@server.tool(...)` with an `annotations={...}` dict carrying client-gating safety hints — `readOnlyHint` / `destructiveHint` / `openWorldHint`. Use `Args:` docstring sections only when per-parameter descriptions add real value (e.g. mutually-exclusive params); single-arg tools don't need them. Don't pass `title` or `idempotentHint` without a concrete consumer.
- **Naming conventions are enforced by lint.** Authoritative table in `CONTRIBUTING.md` ("Naming conventions"). Summary:
  - **Python:** `snake_case` for functions/vars/modules, `CapWords` for classes, `UPPER_SNAKE_CASE` for module constants, leading underscore for private. Enforced by ruff's `N` rule set (`infa-not-for-users/ruff.toml`).
  - **Go (`cli/`):** packages short, lowercase, no underscores; exported `PascalCase`, unexported `camelCase`; acronyms keep case (`URL`, `SHA`, `MCP`, `ID`); receivers 1–2 letter abbreviations. Enforced by `gofmt -l` + `go vet` (both gate CI) plus code review.
  When you add a construct existing rules don't cover, expand both the linter config and the `CONTRIBUTING.md` table in the same PR — do not silently introduce a new style.
- **Keep Python and Go in sync on the shared contract.** The hosted server and the CLI both call the GitHub Contents API for reads — if you change skill-folder discovery or slug derivation (`infa-not-for-users/skills_mcp/github_api.py:slugify` ↔ `cli/internal/scan` / `cli/internal/registry`), update both implementations and both test suites in the same PR. The write surface (`registry.Client.Publish` / `Delete` / `PushTreeViaGit`) is Go-only and has no Python mirror. The `skill-registry/SKILL.md` template is Go-only at `cli/internal/bootstrap/skillmd.go`.
- **Do not reintroduce the local MCP-install path.** The hosted MCP is the only MCP server users connect to. The CLI must never shell out to `uv tool install` / `pipx install` / `pip install` for any MCP-related purpose. The wizard's step 7 is a static snippet; nothing more.
- Do not add new mandatory runtime dependencies without justification. The hosted server has one (`fastmcp`); the Go side has cobra + bubbletea/bubbles/lipgloss + yaml.v3.
- Update `README.md` and `docs/registry.md` for any user-visible change.
- Add or update tests for any behavior change. Untested behavior is undefined.
- Use conventional-commit prefixes (`feat:`, `fix:`, `docs:`, `refactor:`, `test:`, `chore:`).
- **Hosted server safety:** any new server code that talks to GitHub MUST route through `GitHubAppClient` so it gets an installation-scoped token (with the right retry policy) for the requesting user. Never assume `git`, `ssh`, `gh`, or `user.name`/`user.email` are configured — the container has none of them. The server stays read-only; if a feature genuinely needs to write, route it through the CLI instead.
</coding_guidelines>
