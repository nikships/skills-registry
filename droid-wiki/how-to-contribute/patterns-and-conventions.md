# Patterns and conventions

The two-language layout (Python MCP server + Go CLI) only works because both sides agree on a handful of conventions. This page is the short answer for "how do we do things here?".

## The two-language contract

`skills-registry` ships a Python MCP server and a Go CLI that talk to the same GitHub registry. Both speak the same set of endpoints in the same order, with identical retry budgets and the same validation rules. **Keep them in sync.** If you change the contract (`src/skills_mcp/registry_api.py`), update its mirror (`cli/internal/registry/registry.go`) and both test suites in the same PR.

The shared contract surface:

| Concern | Python | Go |
| --- | --- | --- |
| Atomic publish | `RegistryClient.publish_skill` | `Client.Publish` |
| Atomic delete | (no API yet — Go-only) | `Client.Delete` |
| List skills | `RegistryClient.list_skills` | `Client.List` |
| Download skill folder | `RegistryClient.download_skill` | `Client.Get` |
| Tree-SHA cache key | `RegistryClient.get_folder_sha` | `Client.Slugs` (returns names; SHA via `List`) |
| Slugify | `slugify` (in `registry_api.py`) | `Slugify` (in `scan/scan.go`) |
| Path validation | `_normalize_rel_path` | `validateRelPath` |
| Per-file size cap | `SKILLS_MAX_FILE_BYTES` (default 2 MiB) | `maxFileBytes = 2 * 1024 * 1024` |
| Frontmatter parser | `frontmatter.parse_frontmatter` | `registry.parseFlatYAML` + `scan.parseFrontmatter` |
| `gh` lookup | `gh.find_gh` | `registry.FindGH` |

The `skills-registry/SKILL.md` template that's installed into each agent dot-folder is Go-only (lives in `cli/internal/bootstrap/skillmd.go`). There's no Python copy.

## FastMCP server conventions

When adding a tool to `src/skills_mcp/registry_server.py`:

- Construct servers with `FastMCP(name, instructions=..., version=__version__)`. Never bare `FastMCP(name)` — the `instructions` field is what the client uses to teach the agent when to call the server.
- Register every tool via `@server.tool(...)`. Use `name=`, `description=`, `tags=`, and an `annotations={...}` dict carrying the safety hints the client gates on:
  - `readOnlyHint: True` for tools that don't write anything (e.g. `list_skills`).
  - `destructiveHint: True` for tools that mutate state (e.g. `publish_skill`).
  - `openWorldHint: True` for tools that touch the network.
- Use `Args:` docstring sections only when per-parameter descriptions add real value — typically mutually-exclusive parameters like `publish_skill(files=..., local_folder=...)`. Single-arg tools (e.g. `get_skill(slug)`) don't need them.
- Don't pass `title` or `idempotentHint` unless you have a concrete consumer asking for them.

Example from `registry_server.py:_register_tools`:

```python
@server.tool(
    name="get_skill",
    description=("Download a single skill from the registry into a local cache "
                 "folder and return the absolute path. ..."),
    tags={"skills", "registry"},
    annotations={"readOnlyHint": True, "openWorldHint": True},
)
def get_skill(slug: str) -> str:
    ...
```

## GitHub I/O safety

Anything new that talks to GitHub MUST go through `gh api` (or `gh release download` / `gh repo create`). Never assume:

- `git` is on `PATH` (the MCP server runs in a stripped environment).
- `SSH_AUTH_SOCK` is set.
- `user.name` / `user.email` is configured.
- The user's shell `PATH` is inherited.

The one exception is `cli/internal/registry/registry.go:PushTreeViaGit`, the bulk-import path. It uses `git push` because the per-file blob POSTs trip GitHub's secondary rate limit on first-time imports with 100+ files. Auth is wired via `gh auth setup-git` (idempotent — writes `gh` as the HTTPS credential helper to `~/.gitconfig`). This path only runs from the interactive CLI bootstrap; never from the MCP server.

See `cli/internal/registry/registry.go:setupGitAuth` for the integration.

## Path validation

User-supplied paths land in three places: `publish_skill(files=...)`, `publish_skill(local_folder=...)`, and `PushTreeViaGit`. All three apply the same hardening:

- Reject paths starting with `/` (absolute).
- Reject paths containing `..` segments after normalization.
- Reject Windows volume names (`C:`) on `PushTreeViaGit` (`validateRelPath` uses `filepath.VolumeName`).
- Skip dotfiles (`.git`, `.DS_Store`, …) and `__pycache__`.

Python: `_normalize_rel_path` in `src/skills_mcp/registry_server.py`.
Go: `validateRelPath` in `cli/internal/registry/registry.go`.

Both must agree. If one rejects a path, the other must reject it.

## Per-file size cap

`SKILLS_MAX_FILE_BYTES` (default 2 MiB) prevents accidental upload of large binaries. Honor it in any new path that writes blobs.

- Python: env-var-tunable, read at module load (`_MAX_FILE_BYTES` in `registry_server.py`).
- Go: `const maxFileBytes = 2 * 1024 * 1024` in `cli/cmd/skills-registry/publish.go`.

The Python side rejects files that exceed it; the Go publish path logs a warning and skips them. The asymmetry is historical; if you touch either, consider aligning them.

## Naming

Enforced by lint. See `CONTRIBUTING.md` for the full table.

### Python (`src/skills_mcp/`, `tests/`)

ruff's `N` ruleset (`ruff.toml`) enforces:

| Construct | Convention |
| --- | --- |
| Modules / packages | `snake_case` (e.g. `registry_api.py`, `skills_mcp`) |
| Functions, methods, variables | `snake_case` |
| Classes, exceptions, type aliases | `CapWords` (e.g. `RegistryClient`, `GhAuthError`) |
| Constants (module-level) | `UPPER_SNAKE_CASE` (e.g. `SKILLS_MAX_FILE_BYTES`) |
| Private names | leading underscore |

Accepted initialisms in CamelCase boundaries: `MCP`, `Gh` (via `ruff.toml`'s `extend-ignore-names`).

### Go (`cli/`)

`gofmt -l .` + `go vet ./...` gate CI:

| Construct | Convention |
| --- | --- |
| Packages | `lowercase`, single word, no underscores (`registry`, `bootstrap`, `tui`) |
| Files | `lowercase`, underscores allowed (`multiselect.go`, `skillmd.go`) |
| Exported identifiers | `PascalCase` (`RegistryClient`, `PushTreeViaGit`) |
| Unexported identifiers | `camelCase` (`runBootstrap`, `parentSha`) |
| Acronyms | preserve case (`URL`, `SHA`, `MCP`, `ID`) |
| Error variables | `Err`-prefix, `PascalCase` (`ErrNotFound`, `ErrSlugNotFound`) |
| Receivers | 1–2 letter abbreviation (`func (c *Client) Get(...)`, `func (m model) Update(...)`) |

When you introduce a construct the table doesn't cover, expand the linter config and update the table in the same PR. Do not silently introduce a new style.

## Cyclomatic complexity

Both linters cap function complexity:

- **Python** — ruff's `C90` (mccabe) with `max-complexity = 12`. Configured in `ruff.toml`.
- **Go** — `gocyclo -over 15 -ignore "_test"`. Test files are excluded because table-driven tests naturally inflate complexity.

Both ceilings are enforced in `ci.yml` and `release.yml`. **Never raise them casually.** If a new function exceeds the limit, extract helpers.

## Dependencies

Mandatory runtime dependencies are intentionally minimal.

- **Python**: `fastmcp>=3.1.1,<4`. Nothing else.
- **Go**: `cobra` + `bubbletea` + `bubbles` + `lipgloss` + `yaml.v3` (used only for frontmatter parsing in scan; the registry client hand-rolls its own).

Optional dev dependencies (in `[dependency-groups].dev`): `pytest`, `pytest-cov`, `ruff`, `pre-commit`.

Don't add new mandatory deps without justification in the PR description. New optional deps still need justification — every dependency is a future security and maintenance cost.

## Commit messages

Conventional-commit-ish prefixes. Used by the auto-release workflow to compute the next version bump.

- `feat:` — new user-visible feature
- `fix:` — bug fix
- `docs:` — README, examples, contributing notes
- `refactor:` — no behavior change
- `test:` — tests only
- `chore:` — build, deps, tooling
- `ci:` — workflow changes

Example: `fix: ignore SKILL.md files under hidden directories`.

The release auto-tagger always cuts a patch unless dispatched manually with `bump=minor` or `bump=major`. See [deployment](../deployment.md).

## Error handling and surfaces

- **MCP server** — `ConfigError`, `GhNotFoundError`, `GhNotAuthedError` propagate from `build_server()` and exit with codes 2 / 3 / 4 in `main()`. Anything inside a tool propagates as an MCP error response.
- **CLI** — Errors returned from `RunE` print to stderr and exit 1. The hub catches per-action errors as `hubToast{ok: false}` so the user lands back on the dashboard with the failure surfaced.
- **`--json`** — Errors land as `{"error": "..."}` to stdout with a non-zero exit code. Never mix human-readable status lines and JSON payloads in the same invocation.

## Style guidance for prose

When writing this wiki and other docs:

- Reference file paths in backticks on first mention, using the repo-root-relative path so the wiki renders them as clickable source links.
- Use Mermaid for data flows that touch 3+ components. Skip diagrams for things a single sentence can describe.
- Prefer "the code does X" over "X is performed by the code". Active voice, concrete subjects.
- No "serves as", "showcasing", "underscoring", or other AI-tinged filler. State what the code does.
