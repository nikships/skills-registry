# Agent Notes — skills-registry

This file is a living guide for AI agents and new contributors. It captures the architecture, patterns, and trade-offs of the current (0.5.x) **GitHub-backed registry** design. The PyPI package is `skills-registry`; the GitHub repo is still named `skills-mcp` (org URL unchanged); the Python module path is still `skills_mcp` (renaming would be a churn-without-payoff refactor).

> **What changed in 0.3.0:** The project pivoted from "consolidate local skills" (gather/add) to "personal GitHub registry repo, fetched on demand". `gather` and `add` were removed. A new Go CLI handles all interactive UX, and a separate Python MCP server exposes the registry as three tools.

---

## Project Overview

`skills-registry` is now three coordinated deliverables shipped from a single repo:

| Piece | Language | Distribution | Job |
|---|---|---|---|
| `skills-registry` (Python) | Python 3.10+ | PyPI (`pip install skills-registry` / `uvx`) | Thin bootstrap (`skills-registry init`) only. |
| `skill-registry-mcp` (Python) | Python 3.10+ | Same wheel, second `[project.scripts]` entry point | FastMCP server with 3 tools (`list_skills`, `get_skill`, `publish_skill`). |
| `skill-registry` (Go) | Go 1.24+ | GitHub Releases tarballs (built by `.github/workflows/release.yml`) | Charmbracelet TUI: `bootstrap`, `list`, `get`, `sync`, `add`, `publish`. |

- **Build (Python):** `hatchling` + `hatch-vcs` (PEP 517; version from `vX.Y.Z` tags)
- **Package manager (Python):** `uv`
- **Test runner (Python):** `pytest` with `pytest-cov`
- **Lint/Format (Python):** `ruff`
- **Build/Test (Go):** stdlib (`go build`, `go test`, `go vet`) + `staticcheck` + `deadcode` for dead-code / unused-symbol detection
- **TUI library:** Charmbracelet (bubbletea + lipgloss + bubbles + cobra)
- **MCP transport:** stdio via FastMCP 3.x
- **Network surface:**
  - **MCP server (Python):** every GitHub call goes through `gh api`. No `git`, no SSH, no embedded HTTP client. The server must work in the stripped environment Claude Desktop / Cursor / VS Code give an MCP subprocess.
  - **CLI bootstrap (Go):** the bulk initial import uses **`git push` over HTTPS** (single push for the whole tree) because the per-file `POST /git/blobs` path trips GitHub's secondary rate limit on registries with dozens of skills. Auth is wired through `gh auth setup-git`. Everything else the CLI does (list, get, publish a single skill, sync) still goes through `gh api`.

---

## Repository Layout

```
src/skills_mcp/
  __init__.py            # __version__ resolved from installed package metadata
  __main__.py            # `skills-registry` console script: just wires the `init` subcommand
  init.py                # `skills-registry init` — thin bootstrap: gh check + Go binary download + os.execv
  registry_server.py     # `skill-registry-mcp` — FastMCP with list_skills / get_skill / publish_skill
  registry_api.py        # RegistryClient: gh-api wrapper, atomic Git-Data-API publish with retry
  gh.py                  # find_gh() PATH+fallback lookup, ensure_authed(), gh_api() helper
  config.py              # ~/.config/skills-mcp/registry.toml read/save + SKILLS_REGISTRY env override
  cache.py               # ~/.cache/skills-mcp/skills/<slug>/ with tree-SHA meta files
  frontmatter.py         # parse_frontmatter / first_paragraph helpers used by registry_api

cli/                     # Separate Go module (own go.mod)
  cmd/skill-registry/    # Cobra root + bootstrap/list/get/sync/add/publish commands
  internal/
    agents/              # 53-entry KNOWN_DOT_DIRS catalogue with display names + universal flag
    bootstrap/           # SkillMd renderer + InstallSkillMd + MCP/Codex JSON/TOML snippet builders
    config/              # Go mirror of Python config.py (TOML round-trip)
    registry/            # Go mirror of registry_api.py (gh-api client, atomic Publish, conflict retry)
    scan/                # Dot-folder discovery + frontmatter parsing
    tui/                 # Bubble Tea models: list, multi-select, input, choice

tests/                   # 139 Python tests (pytest)
docs/
  registry.md            # Architecture deep dive
.github/workflows/
  ci.yml                 # Python (lint/format/test) + Go (vet/build/test) matrix
  release.yml            # Source-change auto-release: test, tag, build, GH release, PyPI
```

---

## Architecture

### Three deliverables, one repo

```
[user] → uvx skills-registry init (Python)
            ├─ ensure_authed(gh)
            ├─ gh release download skill-registry (Go binary → ~/.local/bin)
            └─ os.execv → `skill-registry bootstrap`
                            ├─ require `git` on PATH (fail-fast)
                            ├─ scan dot-folders (Go)
                            ├─ prompt name/visibility (Bubble Tea)
                            ├─ gh repo create
                            ├─ PushTreeViaGit: single `git push` of every new skill
                            │   (gh auth setup-git → tempdir clone-or-init → commit → push)
                            ├─ multi-select agent install targets
                            ├─ write skill-registry/SKILL.md to each
                            └─ print MCP JSON snippet
```

Persisting `skill-registry-mcp` for desktop MCP clients (Claude Desktop, Cursor, VS Code/Copilot) is the user's responsibility — `uv tool install skills-registry` (documented in the README) installs both console-script entry points (`skills-registry` and `skill-registry-mcp`) so clients can launch the registry server without depending on the `uvx` cache. `cmd_init` does **not** run this step itself.

### Why a separate Go binary?

The user-facing `building-glamorous-tuis` skill recommends Charmbracelet (Go). Charmbracelet has no first-class Python equivalent. Building the bootstrap UX in Bubble Tea required a Go binary regardless, so `skills-registry init` was reduced to **a thin Python shim that downloads-then-execs**. This keeps the polished TUI logic in one place and lets the MCP server stay in Python (where FastMCP lives).

### Two upload paths: `gh api` for the MCP server, `git push` for bootstrap

Desktop MCP clients (Claude Desktop, Cursor, VS Code/Copilot) spawn the MCP server with a stripped environment:
- `PATH` doesn't include your shell extensions.
- `SSH_AUTH_SOCK` is unset.
- `git config user.email` may be missing.

So the **MCP server** (`registry_api.RegistryClient.publish_skill`) never touches `git`/SSH. Every write goes through the GitHub Git Data API, called via `gh api`. The sequence (mirrored in Go's `registry.Client.Publish`):

```
GET  /repos/{r}/git/ref/heads/{branch}        → parent SHA
GET  /repos/{r}/git/commits/{parent}          → base tree SHA
GET  /repos/{r}/git/trees/{base}?recursive=1  → list stale files under <slug>/
POST /repos/{r}/git/blobs                     → upload each file
POST /repos/{r}/git/trees                     → new tree referencing base + blobs (+ null SHAs for deletions)
POST /repos/{r}/git/commits                   → commit pointing at new tree, parents=[parent]
PATCH /repos/{r}/git/refs/heads/{branch}      → fast-forward ref
```

Conflicts (409/422) trigger up to 3 retries with exponential backoff against the freshly-fetched HEAD. This is fine for `publish_skill` because a single skill is ~1–10 files; well under the secondary-rate-limit threshold.

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
| `registry.Client` | `cli/internal/registry/registry.go` | Go mirror of `RegistryClient`. Same endpoints, same order, same retries. Also exposes `PushTreeViaGit` for the bulk bootstrap path (single `git push` instead of N blob POSTs). |
| `build_server()` | `src/skills_mcp/registry_server.py` | Constructs the FastMCP server. Validates auth + config at boot. |
| `cmd_init` | `src/skills_mcp/init.py` | Thin bootstrap; `os.execv` into Go binary; no TUI. |
| `runBootstrap` | `cli/cmd/skill-registry/bootstrap.go` | Owns the interactive flow (TUI prompts + repo create + agent multi-select). |
| `find_gh` / `FindGH` | `src/skills_mcp/gh.py`, `cli/internal/registry/registry.go` | PATH + fallback lookup (`~/.local/bin`, `/opt/homebrew/bin`, `/usr/local/bin`, `/usr/bin`). |
| `MultiSelectModel` | `cli/internal/tui/multiselect.go` | Fuzzy-searchable multi-select with locked-universal section. |
| `parse_frontmatter` / `first_paragraph` | `src/skills_mcp/frontmatter.py` | Tiny YAML-ish frontmatter parser + description fallback used by `registry_api`. |
| `SkillMd` | `cli/internal/bootstrap/skillmd.go` | Sole source of the generated `skill-registry/SKILL.md` template (CLI-only; written into each agent dot-folder by Go bootstrap). |
| `scan.Discover` | `cli/internal/scan/scan.go` | Local skill discovery + frontmatter parsing. Used by `sync`, `add`, `bootstrap`. |

---

## Testing

- **Python:** focused suite covering `cache`, `config`, `frontmatter`, `gh`, `init`, `registry_api`, and `registry_server`. The `registry_api` suite stubs `gh` with a Python shim that replays scripted JSON responses based on argv substring matches.
- **Go:** Tests for `agents`, `bootstrap`, `config`, `scan`, and `registry` (also uses a `gh` shim invoked via `/bin/sh` → `python3`). Run with `cd cli && go test ./...`.
- Run everything:
  ```bash
  uv run pytest -v --cov=skills_mcp --cov-report=term-missing
  (cd cli && go vet ./... && staticcheck ./... && deadcode -test ./... && go test ./...)
  ```
- **Dead-code detection (Go):** CI runs `staticcheck ./...` (scoped via `cli/staticcheck.conf` to disable the noisy `ST*`/`QF*` style families while keeping every unused-symbol/correctness check) plus `deadcode -test ./...` for reachability-based unused-function analysis. Both must be green to merge. See the **How to Work on This Repo** section below for the pinned install commands.

---

## Known Issues & Improvement Opportunities

### Outstanding

1. **No `remove`/`update` commands.** `Publish` already handles deletions via stale-file detection, but there's no user-facing way to drop a skill from the registry. Easy follow-up.
2. **No multi-registry support.** Config is one-repo. Adding a `[registries]` array + a `connect <owner/repo>` CLI command would let an agent see several registries side-by-side.
3. **Browsing third-party public registries** is not yet a first-class flow. The read tools (`list_skills`, `get_skill`) don't require write access — wiring them to an arbitrary `owner/repo` would be a few lines.
4. **Windows MCP-server-side init path** is best-effort. The Go binary builds for Windows, but `skills-registry init`'s `gh release download` + `chmod` assumes POSIX. PowerShell helpers + `gh.exe` lookup would close this gap.
5. **`build_server()` does no schema validation** of the SKILL.md it serves. A malformed skill makes `list_skills` skip it silently; a verbose-mode error log would help debugging.

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

# Run all tests (Python + Go)
uv run pytest -v --cov=skills_mcp --cov-report=term-missing
(cd cli && go vet ./... && go test ./...)

# Dead-code detection (Go)
(cd cli && staticcheck ./... && deadcode -test ./...)

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
