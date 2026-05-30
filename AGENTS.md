`skills-registry` is **two coordinated deliverables** shipped from a single repo:

| Piece | Language | Distribution | Job |
|---|---|---|---|
| `skills-registry` (Go) | Go 1.24+ | GitHub Releases tarballs, installed by `install.sh` (`curl … \| sh`) | Charmbracelet TUI + headless commands. Bare invocation routes to wizard / hub / help. Subcommands: `bootstrap`, `list`, `search`, `get`, `sync`, `add`, `publish`, `remove`, `update`. All subcommands honor a persistent `--json` flag. |
| `skills-registry-mcp` (Python, hosted) | Python 3.10+ (FastMCP) | Docker image on Railway, served at `https://mcp.skills-registry.dev/mcp` | Streamable HTTP MCP server with **2 read-only tools** (`search_skills`, `get_skill`). All writes (`publish` / `sync` / `remove`) go through the Go CLI — the hosted server never mutates the user's repo. OAuth + GitHub App on first connect. Users never install this. |

- **Build (Python, maintainer-only):** `hatchling` (PEP 517) with a static `version = "0.0.0+server"` in `pyproject.toml`. The server is never published to PyPI, never tagged, and Railway redeploys on every push to `main`, so there's no semver to derive. The wheel exists only to provide the `skills-registry-mcp` entry point inside the Docker image.
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
  - **Installer (`install.sh`):** the only one-shot `curl … | sh` surface. POSIX `sh`, detects OS/arch, downloads the matching tarball, drops the binary into `~/.local/bin/skills-registry`. Never touches Python.

---

## Repository Layout

```text
install.sh               # POSIX `curl | sh` installer — the user-facing entry point.
                         # Downloads the matching skills-registry tarball from GitHub Releases.

infa-not-for-users/      # Maintainer-only. Hosted MCP server source + Docker/Railway config.
  skills_mcp/            # Python package (no `src/` layout — packages = ["skills_mcp"] in pyproject.toml)
    __init__.py          # __version__ resolved from installed package metadata
    remote_server.py     # `skills-registry-mcp` — FastMCP build_server() + main(); registers search_skills + get_skill, wires middleware stack + mask_error_details
    middleware.py        # Production middleware stack: ErrorHandling → RateLimiting (per-user `sub` token bucket) → StructuredLogging
    github_api.py        # Token-based GitHub REST helpers: list_skill_folders, get_skill_md, repo_has_skills. Fan-out capped at _FANOUT_CONCURRENCY (8) via asyncio.Semaphore
    github_app.py        # GitHubAppClient: JWT minting, installation lookup, installation-token issuance with in-process TTL cache + asyncio.Lock, retry
    linking.py           # LinkStore + LinkedRepo: {github_user → owner/repo} persistence on the Railway volume. DeliveryStore: webhook replay protection keyed on X-GitHub-Delivery
    setup_routes.py      # /setup/install + post-install landing routes (GitHub App install handoff)
    webhooks.py          # /webhooks/github handler: parses `installation` events and writes to LinkStore. Dedupes deliveries via DeliveryStore
    frontmatter.py       # parse_frontmatter / first_paragraph helpers used by github_api
  tests/                 # pytest suite (~102 tests) covering frontmatter, github_api, github_app, linking, middleware, rate_limiting, remote_server, setup_routes, webhooks
  pyproject.toml         # hatchling + static version + fastmcp + uvicorn + httpx + PyJWT + cryptography + py-key-value-aio + starlette
  Dockerfile             # uv → build wheel → install entry point → run skills-registry-mcp
  railway.json           # Railway service definition (volume mount at /data/oauth)
  .env.example           # OAuth + GitHub App env var template (FASTMCP_*, GITHUB_APP_*, JWT_SIGNING_KEY, STORAGE_ENCRYPTION_KEY)
  README.md              # Deployment + env-var notes (maintainer-facing)

cli/                     # Separate Go module (own go.mod) — the user-facing binary.
  cmd/skills-registry/
    main.go              # Cobra root + bare-command routing (wizard / hub / help)
    wizard.go            # First-run onboarding wizard (Bubble Tea alt-screen, 8 steps)
    hub.go               # Returning-user dashboard hub (alt-screen card grid)
    bootstrap.go         # Legacy headless `bootstrap` subcommand (still useful for scripting)
    list.go / search.go / get.go / sync.go / add.go / publish.go / remove.go / update.go   # Per-subcommand handlers
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
                ├─ download skills-registry_<os>_<arch>.tar.gz from GitHub Releases
                └─ drop binary into ~/.local/bin/skills-registry

[user] → skills-registry
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
                  Manage / Sync / Add / Publish / Purge / Settings
                  Each card launches the same code path the standalone subcommand
                  uses; the result is captured as a toast and seeded into the
                  next hub frame. Quit = q / esc / ctrl+c.

[MCP client] → https://mcp.skills-registry.dev/mcp (Streamable HTTP)
            ├─ OAuth handshake on first connect (browser pop-up → GitHub)
            ├─ Server resolves {github_user → owner/repo} from its repo-link table
            │     (table populated by Skills Registry GitHub App `installation` webhook)
            └─ search_skills / get_skill → GitHub REST contents API
                  via installation-scoped GitHub App token (read-only)
```

**MCP wire-up is a static URL.** `cli/internal/bootstrap/install.go` exposes `HostedMCPURL = "https://mcp.skills-registry.dev/mcp"` and `MCPJSONSnippet()` (no arguments — no binary path to compute). The wizard's step 7 (`WizardStepMCPConnect`) and the headless `bootstrap` subcommand both print this snippet. **The CLI never installs, boots, or proxies an MCP server.** Codex remains unsupported because the hosted server speaks Streamable HTTP and Codex's TOML config only accepts stdio (`command = "..."`); the wizard prints a one-line caveat instead of a Codex snippet.

### Bare-command routing (hub / wizard / help)

`cli/cmd/skills-registry/main.go:bareRouteDecision` is the single decision point for bare `skills-registry`. Pure (no I/O), so the routing matrix is unit-testable end-to-end. The four resolutions:

| isTTY | --json | config | → route | what fires |
|---|---|---|---|---|
| any | `true` | any | `bareRouteHelp` | print usage; exit 0 |
| `false` | `false` | any | `bareRouteHelp` | print usage; exit 0 |
| `true` | `false` | `ErrMissing` | `bareRouteWizard` | first-run onboarding wizard |
| `true` | `false` | nil | `bareRouteHub` | dashboard hub |
| `true` | `false` | other err | `bareRouteError` | surface the malformed-config error |

Bare invocation should always land somewhere safe: non-TTY → no Bubble Tea (can't render); `--json` → no Bubble Tea (caller asked for stdout); otherwise route by config presence.

### The dashboard hub

`cli/cmd/skills-registry/hub.go:runHub` launches a single alt-screen Bubble Tea program once config exists. The heavy lifting lives in `cli/internal/tui/hub_program.go:HubProgram`, which embeds the dashboard (`HubModel`) and every action flow inside one long-lived model:

1. Loads the registry config (fail-fast on read error).
2. Builds `tui.HubProgram` with the repo, the six card grid actions, and a closure that lazily lists the registry to populate the skill count.
3. Runs the Bubble Tea program with `tea.WithAltScreen()`.
4. `HubProgram` catches `hubLaunchMsg`, swaps in the matching embedded flow (`ListModel` / `SyncFlowModel` / `AddFlowModel` / `PublishFlowModel` / `PurgeFlowModel` / `SettingsModel`), and returns to the dashboard when the flow emits `flowExitMsg`.
5. On exit, `HubProgram` seeds the toast into the hub model and re-runs the count loader so the user sees "✓ added from owner/repo" / "✗ remove: slug not found" / "✓ purged 12 local skill folder(s)" / etc.

Per-action errors land as red toasts; the user sees them on the next frame and can retry. The hub terminates on `Quit()` (q / esc / ctrl+c) or empty selection.

### Two upload paths in the CLI: `gh api` for day-to-day writes, `git push` for bootstrap

The **hosted MCP server is read-only.** It runs in a Docker container with no shell state — no `gh`, no `git`, no SSH, no `user.email` — and its only credential is an installation-scoped GitHub App token fetched per request. `search_skills` and `get_skill` are served via the GitHub Contents API; the server never invokes anything that could mutate the user's repo.

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

Force-pushes and any subtree change invalidate correctly. The hosted MCP does not cache `SKILL.md` *content* between requests — every `get_skill` reads through to GitHub. It does, however, cache **installation tokens** in-process (see `GitHubAppClient`): tokens are good for ~1 hour, and re-minting one per request was previously paying a JWT sign + `POST /access_tokens` round-trip every tool call. Cache hit returns the existing token until 60 s before its `expires_at`; cache miss / near-expiry holds an `asyncio.Lock` so two concurrent first-time requests don't both burn a mint.

### Production safeguards

Every MCP request to the hosted server flows through three middleware in this order (see `infa-not-for-users/skills_mcp/middleware.py:build_middleware_stack`):

1. **`ErrorHandlingMiddleware`** (outermost). Converts uncaught exceptions into MCP error responses, with `include_traceback=False` and `transform_errors=True`. Pairs with `mask_error_details=True` on the `FastMCP` constructor so raw `GitHubAppError("status=404 …")` text never bleeds into LLM-visible payloads. `ToolError` remains the escape hatch when you *do* want a specific message to reach the client.
2. **`RateLimitingMiddleware`**. Token-bucket per authenticated GitHub user: **5 req/s sustained, 15-request burst**. `client_id_from_token` keys on the OAuth `sub` claim (`get_access_token().claims["sub"]`) so two users behind a shared NAT don't share a bucket and a malformed token falls back to a single `"anonymous"` bucket rather than gifting itself a fresh one. **Constants are hardcoded; tuning them is a code-review-gated change, not a Railway env flip.** Reasoning lives in `middleware.py`: read tools fan out to GitHub, so the MCP request rate is a leverage multiplier — 5 RPS sustained × the `search_skills` fan-out is already close to GitHub's per-installation REST budget, and 15-burst covers the typical Claude/Cursor session opening.
3. **`StructuredLoggingMiddleware`** (innermost). Emits JSON per accepted request: client id, method, duration. Honors `SKILLS_LOG_LEVEL`. This is what makes the rate limit tunable — without it we can't see who's getting throttled or why.

Two additional safeguards run outside the middleware chain:

- **GitHub fan-out cap.** `list_skill_folders` and `repo_has_skills` would otherwise `asyncio.gather` over every top-level folder. A user with hundreds of skills could trip GitHub's secondary rate limit (≈80 RPS per source IP) all by themselves, locking their *own* installation out for a few minutes. `_FANOUT_CONCURRENCY = 8` enforces a per-call `asyncio.Semaphore`.
- **Webhook delivery-ID dedupe.** `WebhookHandler` reads `X-GitHub-Delivery` after signature verification, checks `DeliveryStore.seen(...)`, and short-circuits with `{"deduped": <id>}` on a hit. Bad payloads and ignored events are marked seen too (GitHub stops re-sending broken events; legit retries no-op). Transient `GitHubAppError`s are *not* marked seen so the next retry actually runs. TTL is 25 h to cover GitHub's 24 h redelivery window with margin.

**Single-instance assumption.** Both `FileTreeStore` (OAuth + linking + `webhook_deliveries`) and the installation-token cache assume one Railway container. Horizontal scale requires swapping `FileTreeStore` for a shared backend (e.g. Redis via `py-key-value-aio`) **and** moving the token cache out of process in the same change — they're correctness-coupled. Until then, scale vertically.

### Single source of truth for agent dot-folders

`cli/internal/agents/agents.go` holds the canonical 56-entry list of known AI tool dot-folders, each with a display name and a `Universal`/`UnderHome` flag. The Python side doesn't need this list — the only consumer (the legacy `gather` command, removed in 0.3.0) is gone. Go-only.

---

## Key Symbols

| Symbol | File | Role |
|---|---|---|
| `build_server()` | `infa-not-for-users/skills_mcp/remote_server.py` | Constructs the FastMCP server, validates settings at boot, registers the two read-only tools (`search_skills`, `get_skill`), and wires the production middleware stack + `mask_error_details=True`. Returns `(FastMCP, LinkStore, GitHubAppClient)`. |
| `build_middleware_stack` / `client_id_from_token` | `infa-not-for-users/skills_mcp/middleware.py` | Returns the ordered list `[ErrorHandlingMiddleware, RateLimitingMiddleware, StructuredLoggingMiddleware]`. The rate limiter keys on the OAuth `sub` claim (5 RPS sustained, 15-request burst, per user — hardcoded). |
| `list_skill_folders` / `get_skill_md` / `repo_has_skills` | `infa-not-for-users/skills_mcp/github_api.py` | Token-based GitHub REST helpers used by the hosted server's read tools. No `gh` binary, no `git`. SKILL.md fan-out bounded by `_FANOUT_CONCURRENCY = 8` via `asyncio.Semaphore`. |
| `GitHubAppClient` | `infa-not-for-users/skills_mcp/github_app.py` | Mints the JWT, looks up the installation for a given user, and exchanges JWT → installation access token. Caches installation tokens in-process per `installation_id` (refresh 60s before `expires_at`) under an `asyncio.Lock` so concurrent first-time mints fan into one HTTP call. Owns the retry policy. |
| `LinkStore` / `LinkedRepo` / `DeliveryStore` | `infa-not-for-users/skills_mcp/linking.py` | Persists `{github_user → owner/repo}` on the Railway-backed volume. `DeliveryStore` records seen `X-GitHub-Delivery` IDs for 25 hours so webhook replays (legitimate or hostile) are no-ops instead of state mutations. |
| `parse_frontmatter` / `first_paragraph` | `infa-not-for-users/skills_mcp/frontmatter.py` | YAML-ish frontmatter parser + description fallback used by `github_api` to render `search_skills` rows. |
| `registry.Client` | `cli/internal/registry/registry.go` | The Go-side GitHub Git Data API client: `Publish`, `Delete`, `PushTreeViaGit`, list/get mirror operations. All CLI writes flow through here. |
| `validateRelPath` | `cli/internal/registry/registry.go` | Path-traversal guard for repo-relative paths. Rejects `..`, absolute paths, and empty strings. Applied to every file before blob upload or `git add`. |
| `bareRouteDecision` | `cli/cmd/skills-registry/main.go` | Pure routing function for `skills-registry` with no subcommand: returns `bareRouteHelp` / `bareRouteWizard` / `bareRouteHub` / `bareRouteError`. |
| `runWizard` | `cli/cmd/skills-registry/wizard.go` | First-run alt-screen Bubble Tea wizard. 8 steps, owns scan/repo-create/push/agent-install/cleanup/MCP-snippet/done. |
| `runHub` | `cli/cmd/skills-registry/hub.go` | Returning-user dashboard: loads config, launches `tui.HubProgram` which embeds `HubModel` and every action flow. |
| `runBootstrap` | `cli/cmd/skills-registry/bootstrap.go` | Headless / scripted bootstrap (legacy flow, still useful for CI). |
| `HostedMCPURL` / `MCPJSONSnippet` | `cli/internal/bootstrap/install.go` | The Streamable-HTTP URL the wizard prints (`https://mcp.skills-registry.dev/mcp`) and the JSON formatter for `mcp.json`. No binary lookup; no install path. |
| `WizardStepMCPConnect` / `startMCPConnect` | `cli/internal/tui/wizard.go`, `cli/internal/tui/wizard_steps.go` | Step 7 of the wizard — purely informational. Synchronous snapshot of `MCPJSONSnippet()`, no goroutine. |
| `jsonout.BindFlag` / `Enabled` / `Print` / `PrintError` | `cli/internal/jsonout/jsonout.go` | Persistent `--json` flag plumbing. Every subcommand checks `Enabled()` and branches into a JSON-only code path. |
| `Client.Delete` | `cli/internal/registry/registry.go` | Atomic `<slug>/` removal via the Git Data API. Mirrors `Publish` but builds a tree with null-SHA entries. Used by `remove` (and the hub's Remove card). |
| `FindGH` | `cli/internal/registry/registry.go` | PATH + fallback lookup for the `gh` CLI. CLI-side only — the hosted server doesn't shell out to anything. |
| `MultiSelectModel` | `cli/internal/tui/multiselect.go` | Fuzzy-searchable multi-select with locked-universal section. |
| `SkillMd` | `cli/internal/bootstrap/skillmd.go` | Sole source of the generated `skills-registry/SKILL.md` template (CLI-only; written into each agent dot-folder by Go bootstrap). Documents both the hosted MCP (preferred for reads) and the CLI (writes + fallback reads). |
| `scan.Discover` | `cli/internal/scan/scan.go` | Local skill discovery + frontmatter parsing. Used by `sync`, `add`, `bootstrap`, and the Purge hub action. |
| `PurgeFlowModel` / `PurgeFlowDeps` | `cli/internal/tui/flow_purge.go` | The "Purge local" hub flow: scan → confirm → `os.RemoveAll` per discovered skill folder. Deletes only local copies; the registry repo is never touched. |
| `purgeLocalSkills` / `pathUnderAnyRoot` / `filterMetaSkill` | `cli/cmd/skills-registry/hub_flow_deps.go` | Safety-checked delete helper for the Purge flow. Cross-checks every candidate folder against the `scan.DiscoverSources` allow-list before calling `os.RemoveAll` so a tampered `scan.Skill` value can't redirect deletes at an arbitrary path. `filterMetaSkill` (used by the Discover side) and an `isMetaSkill` guard inside the delete loop together preserve the bootstrapped `skills-registry/` meta-skill folder — Purge would otherwise wipe the agent's gateway back into the registry, matching the same carve-out `scan.EntriesForCleanup` makes for the wizard's post-publish cleanup. |
| `skillDelegate` / `wrapToLines` / `clampPreviewDesc` | `cli/internal/tui/listmodel.go` | Bubbles list delegate for the `list` TUI. `Height(): 3` so every row is `title + 2 wrapped description lines` (issue #28 — single-line descriptions silently `…`-truncated past ~70 cells). `wrapToLines` is the rune-/width-aware soft-wrap helper feeding the delegate; `clampPreviewDesc` ellipsizes inside the preview pane's description block so the gradient / meta / hint footer never disappears when the description overruns the panel. |
| `performUpdate` / `latestReleaseTag` / `downloadUpdateAsset` | `cli/cmd/skills-registry/update.go` | Self-updater. Resolves the latest tag via GitHub's REST API, downloads the matching release tarball straight from `github.com/<owner>/<repo>/releases/...` (no `gh` dependency — mirrors `install.sh`), extracts, and `os.Rename`s the binary in place. `--version`, `--bin`, `--force`, `--dry-run` flags. `runAutoUpdate` opportunistically invokes it before the hub when `SKILLS_REGISTRY_AUTO_UPDATE=1`. |

---

## Testing

- **Python (hosted server):** 102 tests covering `frontmatter`, `github_api` (incl. fan-out concurrency assertion), `github_app` (incl. installation-token cache + lock), `linking`, `middleware`, `rate_limiting` (per-user isolation + burst behavior against the real FastMCP limiter), `remote_server` (incl. middleware stack order + `mask_error_details`), `setup_routes`, `webhooks` (incl. delivery-ID dedupe). GitHub REST calls are stubbed with `httpx.MockTransport`; OAuth + GitHub App flows use scripted JWT fixtures. Run from `infa-not-for-users/`:
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

1. **No `update --diff` / skill-content `update`.** `remove` shipped in F4.1 and a binary-level self-update shipped alongside `update.go` (mirrors `install.sh`); an *in-place skill update* command that surfaces "what changed" diffs in registry skills is still missing — today users `publish` from a folder, which works but is opaque.
2. **No multi-registry support.** Config is one-repo. A `[registries]` array + `connect <owner/repo>` would let an agent see several side-by-side.
3. **Browsing third-party public registries** isn't a first-class flow. The read tools (`search_skills`, `get_skill`) don't require write access — wiring them to an arbitrary `owner/repo` would be a few lines.
4. **`get_skill_md` does no schema validation** of the SKILL.md it serves. Malformed skills are silently skipped by `search_skills`; a verbose-mode error log in the hosted server would help diagnose user reports.
5. **No server-side cache.** Every `get_skill` reads through to GitHub. A short-TTL in-process cache keyed on tree SHA would cut latency for hot slugs.

---

## Security Notes

- **Hosted MCP server (Python):** runs in a Docker container with no `gh`, no `git`, no SSH, no user shell state. All GitHub I/O uses installation-scoped GitHub App tokens fetched (and cached) via `GitHubAppClient`. OAuth state, the repo-link table, and the webhook delivery-ID dedupe collection live on a Railway-backed volume at `/data/oauth/`. The server is **read-only** — it never mutates the user's repo, so it has no need for write-path safety checks. Per-user rate limiting, internal-error masking, webhook replay protection, and GitHub fan-out capping are all in place; see "Production safeguards" above.
- **CLI writes (Go):** `registry.Client.Publish` and `registry.Client.Delete` shell out to `gh api` (no token in argv/env/disk — `gh` resolves credentials from its own store). `registry.Client.PushTreeViaGit` shells out to `git` over HTTPS with credentials resolved by `gh auth setup-git`. The bootstrap tempdir is `os.RemoveAll`'d on exit.
- `subprocess.run()` (Python) and `exec.CommandContext()` (Go) are used with list args; no `shell=True` / `sh -c`.
- **Path-traversal guard (Go):** `validateRelPath` rejects `..`, `../`, `/../`, absolute paths, and empty strings; it normalizes separators and re-checks via `filepath.Clean`. Applied to every file before blob upload (`Publish`) and before `git add` (`PushTreeViaGit`).
- `Client.Publish` skips dotfiles (`.git`, `.DS_Store`, …) and `__pycache__` directories so accidental upload of editor or build artifacts can't slip through.
- A per-file size cap (`SKILLS_MAX_FILE_BYTES`, default 2 MiB) prevents accidental upload of huge binaries.

---

When making changes:
- **FastMCP server conventions.** Construct servers with `FastMCP(name, instructions=..., version=__version__)`. Register every tool through `@server.tool(...)` with an `annotations={...}` dict carrying client-gating safety hints — `readOnlyHint` / `destructiveHint` / `openWorldHint`. Use `Args:` docstring sections only when per-parameter descriptions add real value (e.g. mutually-exclusive params); single-arg tools don't need them. Don't pass `title` or `idempotentHint` without a concrete consumer.
- **Naming conventions are enforced by lint.** Authoritative table in `CONTRIBUTING.md` ("Naming conventions"). Summary:
  - **Python:** `snake_case` for functions/vars/modules, `CapWords` for classes, `UPPER_SNAKE_CASE` for module constants, leading underscore for private. Enforced by ruff's `N` rule set (`infa-not-for-users/ruff.toml`).
  - **Go (`cli/`):** packages short, lowercase, no underscores; exported `PascalCase`, unexported `camelCase`; acronyms keep case (`URL`, `SHA`, `MCP`, `ID`); receivers 1–2 letter abbreviations. Enforced by `gofmt -l` + `go vet` (both gate CI) plus code review.
  When you add a construct existing rules don't cover, expand both the linter config and the `CONTRIBUTING.md` table in the same PR — do not silently introduce a new style.
- **Keep Python, Go, and Swift in sync on the shared contract.** Three surfaces now read the registry: the hosted server (Python), the CLI (Go), and the macOS app (Swift, in `mac-app/`). If you change skill-folder discovery, slug derivation (`infa-not-for-users/skills_mcp/github_api.py:slugify` ↔ `cli/internal/scan` / `cli/internal/registry` ↔ `mac-app/Sources/SkillsRegistryCore/Slug.swift`), frontmatter parsing (`frontmatter.py` ↔ `cli/internal/scan` ↔ `mac-app/Sources/SkillsRegistryCore/Frontmatter.swift`), **or the fuzzy scorer** (`_fuzzy_score` / `_score_skill` in `github_api.py` ↔ `fuzzyScore` / `scoreSkill` in `cli/cmd/skills-registry/search.go` ↔ `fuzzyScore` / `scoreAndSort` in `mac-app/Sources/SkillsRegistryCore/FuzzyScore.swift`), update all three implementations and all three test suites in the same PR. The scorer constants (`_BASE_MATCH_SCORE`, `_BOUNDARY_BONUS`, `_CAMEL_BONUS`, `_CONSECUTIVE_BONUS`, `_CASE_BONUS`, `_GAP_PENALTY`, `_FIELD_WEIGHTS`, `_SEARCH_TOP_N`) are duplicated by design — cross-language corpus tests (`test_search_skills_cross_language_corpus` / `TestScoreAndSortCrossLanguageCorpus` / Swift `testCrossLanguageCorpus` in `mac-app/Tests/SkillsRegistryCoreTests/CoreContractTests.swift`) pin the contract. The single-skill write surface (`registry.Client.Publish` / `Delete`) has a Swift mirror in `mac-app/Sources/SkillsRegistryCore/GitHubWrites.swift`; the bulk `PushTreeViaGit` (git push) is Go-only — the macOS app uses its own atomic Git Data API `bulkPush` for local import. The hosted server (Python) stays read-only and has no write mirror. The `skills-registry/SKILL.md` meta-skill template has two implementations that MUST stay byte-for-byte identical: `cli/internal/bootstrap/skillmd.go:SkillMd` (Go) ↔ `mac-app/Sources/SkillsRegistryCore/SkillMdTemplate.swift` (Swift) — both render the gateway skill agents read to reach the registry. The Swift `SkillMdTemplateTests` pins the contract by asserting the rendered byte length and key lines; if you edit one template, edit the other in the same PR and refresh the test. The Python server never renders it.
- **Do not reintroduce the local MCP-install path.** The hosted MCP is the only MCP server users connect to. The CLI must never shell out to `uv tool install` / `pipx install` / `pip install` for any MCP-related purpose. The wizard's step 7 is a static snippet; nothing more.
- Do not add new mandatory runtime dependencies without justification. The hosted server has one (`fastmcp`); the Go side has cobra + bubbletea/bubbles/lipgloss + yaml.v3.
- Update `README.md` and `docs/registry.md` for any user-visible change.
- Add or update tests for any behavior change. Untested behavior is undefined.
- Use conventional-commit prefixes (`feat:`, `fix:`, `docs:`, `refactor:`, `test:`, `chore:`).
- **Hosted server safety:** any new server code that talks to GitHub MUST route through `GitHubAppClient` so it gets an installation-scoped token (with the right retry policy) for the requesting user. Never assume `git`, `ssh`, `gh`, or `user.name`/`user.email` are configured — the container has none of them. The server stays read-only; if a feature genuinely needs to write, route it through the CLI instead.

