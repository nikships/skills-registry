# Agent Notes — skills-registry

This file is a living guide for AI agents and new contributors. It captures the architecture, patterns, and trade-offs of the current (0.5.x) **GitHub-backed registry** design. The PyPI package is `skills-registry`; the GitHub repo is still named `skills-mcp` (org URL unchanged); the Python module path is still `skills_mcp` (renaming would be a churn-without-payoff refactor).

> **What changed in 0.3.0:** The project pivoted from "consolidate local skills" (gather/add) to "personal GitHub registry repo, fetched on demand". `gather` and `add` were removed. A new Go CLI handles all interactive UX, and a separate Python MCP server exposes the registry as three tools.

> **What changed in 0.5.x:** The user-facing install flow is now `curl | sh` against `install.sh`, not `uvx skills-registry init`. The Python `init` subcommand is gone; everything bootstrap-related (`gh` auth check, MCP entry-point install, scan/push/agent-select) lives in the Go binary. A bare `skill-registry` routes to the **onboarding wizard** (no config), the **dashboard hub** (config present), or a help dump (non-TTY / `--json`). The Go CLI gained a `remove` subcommand and a persistent `--json` flag honored by every subcommand.

---

## Project Overview

`skills-registry` is now three coordinated deliverables shipped from a single repo:

| Piece | Language | Distribution | Job |
|---|---|---|---|
| `skill-registry` (Go) | Go 1.24+ | GitHub Releases tarballs, installed by `install.sh` (`curl … \| sh`) | Charmbracelet TUI + headless commands. Bare invocation routes to wizard / hub / help. Subcommands: `bootstrap`, `list`, `get`, `sync`, `add`, `publish`, `remove`. All subcommands honor a persistent `--json` flag. |
| `skill-registry-mcp` (Python) | Python 3.10+ | PyPI (`uv tool install skills-registry` / `pipx install skills-registry` / `pip install --user skills-registry`) | FastMCP server with 3 tools (`list_skills`, `get_skill`, `publish_skill`). The wizard auto-installs this on first run; users can also install it manually. |
| `skills-registry` (Python) | Python 3.10+ | Same wheel as the MCP server (`[project.scripts]`) | Provides only the `skill-registry-mcp` entry point. The legacy `skills-registry init` console script is deprecated; the wizard inside the Go binary owns the bootstrap flow now. |

- **Build (Python):** `hatchling` + `hatch-vcs` (PEP 517; version from `vX.Y.Z` tags)
- **Package manager (Python):** `uv`
- **Test runner (Python):** `pytest` with `pytest-cov`
- **Lint/Format (Python):** `ruff`
- **Build/Test (Go):** stdlib (`go build`, `go test`, `go vet`) + `staticcheck` + `deadcode` for dead-code / unused-symbol detection
- **TUI library:** Charmbracelet (bubbletea + lipgloss + bubbles + cobra)
- **MCP transport:** stdio via FastMCP 3.x
- **Network surface:**
  - **MCP server (Python):** every GitHub call goes through `gh api`. No `git`, no SSH, no embedded HTTP client. The server must work in the stripped environment Claude Desktop / Cursor / VS Code give an MCP subprocess.
  - **CLI bootstrap (Go):** the bulk initial import (wizard step 4) uses **`git push` over HTTPS** (single push for the whole tree) because the per-file `POST /git/blobs` path trips GitHub's secondary rate limit on registries with dozens of skills. Auth is wired through `gh auth setup-git`. Everything else the CLI does (list, get, publish a single skill, sync, remove) still goes through `gh api`.
  - **Installer (`install.sh`):** the only one-shot `curl … | sh` surface. POSIX `sh`, detects OS/arch, downloads the matching tarball from the latest GitHub Release, drops the binary into `~/.local/bin/skill-registry`. Replaces the old `uvx skills-registry init` flow.

---

## Repository Layout

```text
install.sh               # POSIX `curl | sh` installer — the user-facing entry point.
                         # Downloads the matching skill-registry tarball from GitHub Releases.

src/skills_mcp/
  __init__.py            # __version__ resolved from installed package metadata
  __main__.py            # legacy `skills-registry` console script (init); deprecated in favor of `install.sh`
  init.py                # legacy `skills-registry init` — left in for users who still `uvx` it
  registry_server.py     # `skill-registry-mcp` — FastMCP with list_skills / get_skill / publish_skill
  registry_api.py        # RegistryClient: gh-api wrapper, atomic Git-Data-API publish/delete with retry
  gh.py                  # find_gh() PATH+fallback lookup, ensure_authed(), gh_api() helper
  config.py              # ~/.config/skills-mcp/registry.toml read/save + SKILLS_REGISTRY env override
  cache.py               # ~/.cache/skills-mcp/skills/<slug>/ with tree-SHA meta files
  frontmatter.py         # parse_frontmatter / first_paragraph helpers used by registry_api

cli/                     # Separate Go module (own go.mod) — the user-facing binary.
  cmd/skill-registry/
    main.go              # Cobra root + bare-command routing (wizard / hub / help)
    wizard.go            # First-run onboarding wizard (Bubble Tea alt-screen, 8 steps)
    hub.go               # Returning-user dashboard hub (alt-screen card grid)
    bootstrap.go         # Legacy headless `bootstrap` subcommand (still useful for scripting)
    list.go / get.go / sync.go / add.go / publish.go / remove.go   # Per-subcommand handlers
  internal/
    agents/              # 56-entry KNOWN_DOT_DIRS catalogue with display names + universal flag
    bootstrap/           # SkillMd renderer + InstallSkillMd + MCP/Codex snippets + MCP-entry-point installer (uv→pipx→pip)
    cache/               # CacheRoot() helper (mirrors Python cache.py path resolution)
    config/              # Go mirror of Python config.py (TOML round-trip)
    jsonout/             # Persistent --json flag plumbing + Print / PrintError helpers
    registry/            # Go mirror of registry_api.py (gh-api client, atomic Publish/Delete, conflict retry, PushTreeViaGit)
    scan/                # Dot-folder discovery + frontmatter parsing
    tui/                 # Bubble Tea models: list, multi-select, input, choice, hub, wizard, settings, toast

tests/                   # 139 Python tests (pytest)
docs/
  registry.md            # Architecture deep dive
.github/workflows/
  ci.yml                 # Python (lint/format/test) + Go (vet/build/test) matrix
  release.yml            # Source-change auto-release: test, tag, build, GH release, PyPI
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
            │     7. bootstrap.EnsureMCPEntryPoint (uv → pipx → pip; in-Go now)
            │     8. print MCP JSON snippet + "all done" caption
            │
            └─ Hub (returning user, alt-screen card grid):
                  Browse / Sync / Add / Publish / Remove / Settings
                  Each card launches the same code path the standalone subcommand
                  uses; the result is captured as a toast and seeded into the
                  next hub frame. Quit = q / esc / ctrl+c.
```

**MCP entry-point install lives in Go now.** `cli/internal/bootstrap/mcp_install.go` exposes `EnsureMCPEntryPoint(ctx)`. It probes the same curated fallback dirs as `locateMCPBinary` (`~/.local/bin`, `~/.local/share/uv/tools/skills-registry/bin`, `/opt/homebrew/bin`, `/usr/local/bin`) and, when nothing is found, runs `uv tool install --force skills-registry` → `pipx install --force skills-registry` → `python3 -m pip install --user --upgrade skills-registry` in order. First non-zero exit + on-disk presence wins. Total failure prints a manual-install hint and continues (the bootstrap never fails because of this). Opt out with `SKILLS_SKIP_INSTALL=1`. The legacy Python `_ensure_mcp_entry_point` in `init.py` is still present but no longer the canonical path.

### Bare-command routing (hub / wizard / help)

`cli/cmd/skill-registry/main.go:bareRouteDecision` is the single decision point for `skill-registry` invoked with no subcommand. It's a pure function (no I/O) so the routing matrix is unit-testable end-to-end. The four resolutions:

| isTTY | --json | config | → route | what fires |
|---|---|---|---|---|
| any | `true` | any | `bareRouteHelp` | print usage; exit 0 |
| `false` | `false` | any | `bareRouteHelp` | print usage; exit 0 |
| `true` | `false` | `ErrMissing` | `bareRouteWizard` | first-run onboarding wizard |
| `true` | `false` | nil | `bareRouteHub` | dashboard hub |
| `true` | `false` | other err | `bareRouteError` | surface the malformed-config error |

The contract is "bare invocation should always land somewhere safe": non-TTY → no Bubble Tea (it can't render); `--json` → no Bubble Tea (the caller asked for stdout); otherwise route based on whether config exists.

### The dashboard hub

`cli/cmd/skill-registry/hub.go:runHub` is a launch loop. Each iteration:

1. Loads the registry config (fail-fast on read error).
2. Builds `tui.HubModel` with the repo + a closure that lazily lists the registry to populate the skill count.
3. Optionally injects a pending toast (set by the previous iteration's dispatcher).
4. Runs the Bubble Tea program with `tea.WithAltScreen()`.
5. Reads `Selection()` from the post-quit model and switches into the matching per-action helper (`runBrowseFromHub`, `runSyncFromHub`, `runAddFromHub`, `runPublishFromHub`, `runRemoveFromHub`, `runSettingsFromHub`).
6. Each helper returns a `hubToast` (text + ok/err) that's threaded into the next loop iteration.

The loop terminates on `Quit()` (q / esc / ctrl+c) or when a launcher-level error makes continuing pointless. Per-action failures land as red toasts; the user just sees them on the next frame and can retry.

### Why a separate Go binary?

The user-facing `building-glamorous-tuis` skill recommends Charmbracelet (Go). Charmbracelet has no first-class Python equivalent. Building the bootstrap UX in Bubble Tea required a Go binary regardless, so `install.sh` now drops the Go binary directly and **the user never sees Python during onboarding**. The Python wheel is still PyPI-published — but only as the host for `skill-registry-mcp` (the FastMCP server), which is installed in the background by the Go wizard via `uv tool install` / `pipx install` / `pip install --user`.

### Two upload paths: `gh api` for the MCP server + day-to-day commands, `git push` for bootstrap

Desktop MCP clients (Claude Desktop, Cursor, VS Code/Copilot) spawn the MCP server with a stripped environment:
- `PATH` doesn't include your shell extensions.
- `SSH_AUTH_SOCK` is unset.
- `git config user.email` may be missing.

So the **MCP server** (`registry_api.RegistryClient.publish_skill`) never touches `git`/SSH. Every write goes through the GitHub Git Data API, called via `gh api`. The sequence (mirrored in Go's `registry.Client.Publish`):

```text
GET  /repos/{r}/git/ref/heads/{branch}        → parent SHA
GET  /repos/{r}/git/commits/{parent}          → base tree SHA
GET  /repos/{r}/git/trees/{base}?recursive=1  → list stale files under <slug>/
POST /repos/{r}/git/blobs                     → upload each file
POST /repos/{r}/git/trees                     → new tree referencing base + blobs (+ null SHAs for deletions)
POST /repos/{r}/git/commits                   → commit pointing at new tree, parents=[parent]
PATCH /repos/{r}/git/refs/heads/{branch}      → fast-forward ref
```

Conflicts (409/422) trigger up to 3 retries with exponential backoff against the freshly-fetched HEAD. This is fine for `publish_skill` because a single skill is ~1–10 files; well under the secondary-rate-limit threshold. `remove` (Python `delete_skill` / Go `Client.Delete`) uses the same six-call sequence but with null SHAs in the new tree entries to drop the slug atomically — see §6 of `docs/registry.md`.

The **CLI bootstrap** flow is different. A first-time user typically has 30–200 skills (≈100–500 files), and the per-file blob POSTs trip GitHub's secondary rate limit at ~80 requests/minute. `registry.Client.PushTreeViaGit` sidesteps that with one `git push`:

1. `gh auth setup-git --hostname github.com` (idempotent — wires `gh` as the HTTPS credential helper).
2. `gh api user` → commit author name/email (falls back to `<login>@users.noreply.github.com`).
3. If the branch already exists upstream, shallow-clone it; otherwise `git init -b main` in a tempdir and add the remote.
4. Materialize every file under the tempdir; `git add -A`; commit; `git push -u origin main`.
5. Tempdir is removed on exit; nothing persists outside the user's `~/.gitconfig` (which now references `gh` as its credential helper for github.com).

Hard requirements for the bootstrap path: `git` on PATH and an authenticated `gh`. `cmd_bootstrap` fails fast (before any prompts) when `git` is missing, with an install hint. Single-skill `publish` from the CLI still uses the `gh api` blob path — it's never close to the rate limit.

### Caching

`get_skill` writes to `~/.cache/skills-mcp/skills/<slug>/` with a sibling `<slug>.meta.json` storing the **registry tree SHA** at fetch time. The next call:
1. Asks the registry for the current `<slug>/` tree SHA.
2. Returns the cached path immediately if the SHA matches.
3. Otherwise wipes the folder and re-downloads.

Force-pushes and any subtree change correctly invalidate.

### Single source of truth for agent dot-folders

`cli/internal/agents/agents.go` holds the canonical 56-entry list of known AI tool dot-folders, each annotated with a display name and a `Universal`/`UnderHome` flag. The Python side doesn't need this list any more (the legacy `gather` command was the only consumer); for the new flow it's Go-only.

---

## Key Symbols

| Symbol | File | Role |
|---|---|---|
| `RegistryClient` | `src/skills_mcp/registry_api.py` | Python: `list_skills` / `download_skill` / `publish_skill`. Owns Git Data API logic + retry. |
| `registry.Client` | `cli/internal/registry/registry.go` | Go mirror of `RegistryClient`. Same endpoints, same order, same retries. Also exposes `PushTreeViaGit` (bulk bootstrap path) and `Delete` (slug-level atomic remove). |
| `build_server()` | `src/skills_mcp/registry_server.py` | Constructs the FastMCP server. Validates auth + config at boot. |
| `bareRouteDecision` | `cli/cmd/skill-registry/main.go` | Pure routing function for `skill-registry` with no subcommand: returns `bareRouteHelp` / `bareRouteWizard` / `bareRouteHub` / `bareRouteError`. |
| `runWizard` | `cli/cmd/skill-registry/wizard.go` | First-run alt-screen Bubble Tea wizard. 8 steps, owns scan/repo-create/push/agent-install/cleanup/MCP-install/done. |
| `runHub` | `cli/cmd/skill-registry/hub.go` | Returning-user dashboard loop: launches `tui.HubModel`, dispatches the picked action, seeds the next frame with a toast. |
| `runBootstrap` | `cli/cmd/skill-registry/bootstrap.go` | Headless / scripted bootstrap (legacy flow, still useful for CI). |
| `EnsureMCPEntryPoint` | `cli/internal/bootstrap/mcp_install.go` | Go port of the Python `_ensure_mcp_entry_point`: tries `uv tool install` → `pipx install` → `pip install --user`. |
| `jsonout.BindFlag` / `Enabled` / `Print` / `PrintError` | `cli/internal/jsonout/jsonout.go` | Persistent `--json` flag plumbing. Every subcommand checks `Enabled()` and branches into a JSON-only code path. |
| `Client.Delete` | `cli/internal/registry/registry.go` | Atomic `<slug>/` removal via the Git Data API. Mirrors `Publish` but builds a tree with null-SHA entries. Used by `remove` (and the hub's Remove card). |
| `find_gh` / `FindGH` | `src/skills_mcp/gh.py`, `cli/internal/registry/registry.go` | PATH + fallback lookup (`~/.local/bin`, `/opt/homebrew/bin`, `/usr/local/bin`, `/usr/bin`). |
| `MultiSelectModel` | `cli/internal/tui/multiselect.go` | Fuzzy-searchable multi-select with locked-universal section. |
| `parse_frontmatter` / `first_paragraph` | `src/skills_mcp/frontmatter.py` | Tiny YAML-ish frontmatter parser + description fallback used by `registry_api`. |
| `SkillMd` | `cli/internal/bootstrap/skillmd.go` | Sole source of the generated `skill-registry/SKILL.md` template (CLI-only; written into each agent dot-folder by Go bootstrap). Now documents `--json` + `remove` + the `curl \| sh` install. |
| `scan.Discover` | `cli/internal/scan/scan.go` | Local skill discovery + frontmatter parsing. Used by `sync`, `add`, `bootstrap`. |

---

## Testing

- **Python:** focused suite covering `cache`, `config`, `frontmatter`, `gh`, `init`, `registry_api`, and `registry_server`. The `registry_api` suite stubs `gh` with a Python shim that replays scripted JSON responses based on argv substring matches.
- **Go:** Tests for `agents`, `bootstrap`, `config`, `scan`, and `registry` (also uses a `gh` shim invoked via `/bin/sh` → `python3`). Run with `cd cli && go test ./...`.
- Run everything:
  ```bash
  uv run pytest -v --cov=skills_mcp --cov-report=term-missing
  (cd cli && go vet ./... && staticcheck ./... && deadcode -test ./... && gocyclo -over 15 -ignore "_test" . && go test ./...)
  ```
- **Dead-code detection (Go):** CI runs `staticcheck ./...` (scoped via `cli/staticcheck.conf` to disable the noisy `ST*`/`QF*` style families while keeping every unused-symbol/correctness check) plus `deadcode -test ./...` for reachability-based unused-function analysis. Both must be green to merge. See the **How to Work on This Repo** section below for the pinned install commands.
- **Cyclomatic-complexity ceilings:** Python: ruff's `C90` (mccabe) rule is enabled in `ruff.toml` with `max-complexity = 12`. Go: CI runs `gocyclo -over 15 -ignore "_test"` on `cli/` — the industry-standard ceiling for Go production code (test files are excluded because table-driven tests naturally inflate complexity). Both ceilings are enforced in `ci.yml` and `release.yml`. Never raise them casually; if a new function exceeds the limit, extract helpers.

---

## Known Issues & Improvement Opportunities

### Outstanding

1. **No `update` command.** `remove` shipped in F4.1; an in-place `update` would still be useful (today users `publish` from a folder, which works but doesn't surface "what changed").
2. **No multi-registry support.** Config is one-repo. Adding a `[registries]` array + a `connect <owner/repo>` CLI command would let an agent see several registries side-by-side.
3. **Browsing third-party public registries** is not yet a first-class flow. The read tools (`list_skills`, `get_skill`) don't require write access — wiring them to an arbitrary `owner/repo` would be a few lines.
4. **Windows installer.** `install.sh` is POSIX-only. The Go binary builds for `windows/amd64`, but Windows users need a PowerShell `install.ps1` (and `gh.exe` lookup in `FindGH`) to get the same one-shot install experience.
5. **`build_server()` does no schema validation** of the SKILL.md it serves. A malformed skill makes `list_skills` skip it silently; a verbose-mode error log would help debugging.
6. **No `mcp_install` Python parity.** Now that the Go `EnsureMCPEntryPoint` is the canonical path, the legacy Python helper in `init.py` is dead weight for new users. Removing it would let us also drop the `[project.scripts] skills-registry` entry; the wheel would then host only `skill-registry-mcp`.

### Carried over from the previous design

- **Frontmatter parser is YAML-ish.** Both Python and Go avoid a real YAML dep; multi-line values, lists, and nested keys are silently dropped. Fine for the current scope.

---

## CI / CD

- `.github/workflows/ci.yml` — runs the Python job (ruff lint + format + pytest with coverage) **and** the Go job (vet + staticcheck + deadcode + build + test) in parallel on every push/PR. Both must be green to merge.
- `.github/workflows/release.yml` — **auto-releases on every push to `main` that touches `src/`, `cli/`, `tests/`, or `pyproject.toml`**. The path filter is the release decision; commits that only touch docs, workflow files, or other non-source paths do not release.
  1. Tests gate (ruff + pytest + go vet + staticcheck + deadcode + go test) — must pass.
  2. Tag job computes the next semver from the latest `vX.Y.Z` tag, then pushes a lightweight tag on the triggering commit. CI never commits version bumps back to `main`.
  3. Python package version is dynamic via `hatch-vcs`; building from the release tag produces the matching wheel/sdist version.
  4. Builds the Python wheel + sdist (`uv build`) and the Go CLI for `darwin/amd64`, `darwin/arm64`, `linux/amd64`, `linux/arm64`, `windows/amd64`.
  5. Creates a single GitHub Release `vX.Y.Z` containing all 7 assets (wheel, sdist, 4 Go tarballs, 1 Go zip). `skills-registry init` downloads the Go binary from this same "latest" release.
  6. Publishes the wheel to PyPI via the `pypi` environment using `PYPI_API_TOKEN`.
- Force a non-patch bump with `gh workflow run release.yml -f bump=minor` (or `major`).
- **Gaps:** No Python version matrix yet, no OS matrix for the Python tests, no Dependabot, no codecov upload (coverage XML is generated but not uploaded), no integration tests that actually call GitHub. The test gate inside `release.yml` is a near-duplicate of `ci.yml`; if you change one, check the other.

---

## Security Notes

- **MCP server (Python):** no `git` shell-out, no SSH agent dependency, no embedded HTTP client. All GitHub I/O routes through the user's authenticated `gh` CLI.
- **CLI bootstrap (Go):** the `PushTreeViaGit` path shells out to `git` over HTTPS, with credentials resolved by `gh auth setup-git` (which writes a credential-helper entry to the user's `~/.gitconfig` pointing at `gh`). Token never appears in argv, env, or on disk. The temp working directory used for the push is `os.RemoveAll`-ed on exit.
- `subprocess.run()` (Python) and `exec.CommandContext()` (Go) are used with list args; no `shell=True`/`sh -c`.
- `RegistryClient.publish_skill` rejects paths containing `..` segments and skips dotfiles (`.git`, `.DS_Store`, …) and `__pycache__`.
- `_normalize_rel_path` rejects backslash-encoded traversals and absolute-path injection.
- `PushTreeViaGit` applies the same traversal rejection (`..`, `../`, `/../`) before writing any file to disk.
- A per-file size cap (`SKILLS_MAX_FILE_BYTES`, default 2 MiB) prevents accidental upload of huge binaries.
- The Go binary uses identical validation paths for the REST blob upload.

---

## How to Work on This Repo

```bash
# Setup
uv sync --group dev
(cd cli && go mod download)
# Install Go dead-code analyzers (versions pinned to match CI; see
# .github/workflows/ci.yml — bump in lockstep)
go install honnef.co/go/tools/cmd/staticcheck@2025.1.1
go install golang.org/x/tools/cmd/deadcode@v0.45.0
go install github.com/fzipp/gocyclo/cmd/gocyclo@v0.6.0

# Run all tests (Python + Go)
uv run pytest -v --cov=skills_mcp --cov-report=term-missing
(cd cli && go vet ./... && go test ./...)

# Dead-code detection (Go)
(cd cli && staticcheck ./... && deadcode -test ./...)

# Cyclomatic-complexity ceiling (Go)
(cd cli && gocyclo -over 15 -ignore "_test" .)

# Lint & format Python
uv run ruff check .
uv run ruff format .

# Install pre-commit hooks
uv run pre-commit install

# Smoke-test the Go binary locally
(cd cli && go build -o /tmp/skill-registry ./cmd/skill-registry && /tmp/skill-registry --help)
```

When making changes:
- **FastMCP server conventions.** Construct servers with `FastMCP(name, instructions=..., version=__version__)`. Register every tool through `@server.tool(...)` and pass an `annotations={...}` dict carrying the safety hints that matter for client gating — `readOnlyHint` / `destructiveHint` / `openWorldHint`. Use `Args:` docstring sections only when per-parameter descriptions add real value (e.g. mutually-exclusive params); single-arg tools don't need them. Don't pass `title` or `idempotentHint` unless you have a concrete consumer.
- **Naming conventions are enforced by lint.** The authoritative table lives in `CONTRIBUTING.md` ("Naming conventions"). Summary:
  - **Python:** `snake_case` for functions/vars/modules, `CapWords` for classes, `UPPER_SNAKE_CASE` for module constants, leading underscore for private names. Enforced by ruff's `N` rule set (`ruff.toml`).
  - **Go (`cli/`):** packages are short, lowercase, no underscores; exported identifiers `PascalCase`, unexported `camelCase`; acronyms keep their case (`URL`, `SHA`, `MCP`, `ID`); receivers are 1–2 letter abbreviations. Enforced by `gofmt -l` + `go vet` (both gate CI) plus code review.
  When you add a construct that the existing rules don't cover, expand both the linter config and the table in `CONTRIBUTING.md` in the same PR — do not silently introduce a new style.
- **Keep Python and Go in sync.** If you change the registry contract (`registry_api.py` ↔ `registry.go`), update both implementations and both test suites in the same PR. The `skill-registry/SKILL.md` template is Go-only and lives in `cli/internal/bootstrap/skillmd.go`.
- Do not add new mandatory runtime dependencies without justification. The Python side has exactly one (`fastmcp`); the Go side has cobra + bubbletea/bubbles/lipgloss + yaml.v3.
- Update `README.md` and `docs/registry.md` if you change anything user-visible.
- Add or update tests for any behavior change. Untested behavior is treated as undefined.
- Use conventional-commit prefixes (`feat:`, `fix:`, `docs:`, `refactor:`, `test:`, `chore:`).
- **GUI environment safety:** any new code that talks to GitHub MUST go through `gh api` (or `gh release download` / `gh repo create`). Never assume `git`, `ssh`, or `user.name`/`user.email` are configured.
