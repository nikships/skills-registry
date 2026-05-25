# Dependencies

Active contributors: Nik Anand

Every third-party library `skills-registry` pulls in, grouped by deliverable. The project is intentionally minimal — the Python MCP server has exactly one mandatory runtime dependency, and the Go CLI sticks to Charmbracelet plus cobra plus yaml.

## Python runtime

Declared in `/Users/dks0662779/skillsmcp/pyproject.toml` under `[project].dependencies`.

| Package | Version | Purpose |
| --- | --- | --- |
| `fastmcp` | `>=3.1.1,<4` | FastMCP server library. Provides `FastMCP`, `@tool` decorator, stdio transport. |

That is the entire runtime surface. The server shells out to `gh` for all network I/O, so there is no HTTP client, no SSH library, no git library, no YAML parser in the runtime dep set. Adding a second mandatory dep needs an explicit justification in the PR description.

## Python dev tooling

Declared in `[dependency-groups].dev`:

| Package | Purpose |
| --- | --- |
| `pytest>=7` | Test runner. |
| `pytest-cov` | Coverage reporting (XML + terminal). |
| `ruff>=0.6` | Lint + format. Configured in `/Users/dks0662779/skillsmcp/ruff.toml`. |
| `pre-commit` | Git hook orchestration. `uv run pre-commit install` wires it up. |

## Python build backend

Declared in `[build-system]`:

| Package | Purpose |
| --- | --- |
| `hatchling` | PEP 517 build backend. |
| `hatch-vcs` | Reads the package version from `vX.Y.Z` git tags. The version is dynamic; there is no hand-edited `__version__` literal. |

## Go runtime

Declared in `/Users/dks0662779/skillsmcp/cli/go.mod`.

| Module | Version | Purpose |
| --- | --- | --- |
| `github.com/charmbracelet/bubbletea` | `v1.3.10` | TUI runtime. The wizard, hub, multi-select, input, and choice models all run on top of it. |
| `github.com/charmbracelet/bubbles` | `v0.21.0` | Stock components (text input, viewport, progress, list). |
| `github.com/charmbracelet/lipgloss` | `v1.1.0` | Styling primitives — colors, borders, padding. Used everywhere a TUI surface needs visual structure. |
| `github.com/spf13/cobra` | `v1.10.1` | Subcommand dispatcher. Powers `bootstrap`, `list`, `get`, `sync`, `add`, `publish`, `remove`. Also owns the persistent `--json` flag. |
| `gopkg.in/yaml.v3` | `v3.0.1` | Real YAML parsing for `cli/internal/scan/scan.go`'s local-skill walk. The registry client deliberately does **not** use it (see below). |

Indirect modules (go-runewidth, go-isatty, fuzzy, termenv, ansi, cellbuf, colorprofile, …) come along for the Charmbracelet ride; nothing in the project imports them directly.

## Go dev / CI tooling

Pinned in `/Users/dks0662779/skillsmcp/.github/workflows/ci.yml`. The pinned versions are also documented in `/Users/dks0662779/skillsmcp/CLAUDE.md` under "How to Work on This Repo" so local installs stay in lockstep with CI:

| Tool | Version | Purpose |
| --- | --- | --- |
| `staticcheck` | `2025.1.1` | Unused-symbol + correctness checks. Style families (`ST*`, `QF*`) are disabled via `cli/staticcheck.conf`. |
| `deadcode` | `v0.45.0` | Reachability-based dead-function analysis. Runs with `-test` so test-only helpers are not flagged. |
| `gocyclo` | `v0.6.0` | Cyclomatic-complexity ceiling. CI enforces `-over 15 -ignore "_test"`. Test files are excluded because table-driven tests naturally inflate the metric. |

Bump these in lockstep with the workflow when there's a reason to.

## Website

Lives in `/Users/dks0662779/skillsmcp/website/`. Declared in `website/package.json`.

| Package | Version | Purpose |
| --- | --- | --- |
| `next` | `16.2.6` | React framework. **Note: Next.js 16 introduces breaking changes from 15.** Anyone touching the website needs to read the Next 16 upgrade notes before assuming pre-16 patterns work. |
| `react` | `19.2.4` | React runtime. |
| `react-dom` | `19.2.4` | React DOM. |
| `tailwindcss` | `^4` | Tailwind v4. The `@tailwindcss/postcss` plugin is the new wiring; the old `tailwind.config.js` flow is gone. |
| `@tailwindcss/postcss` | `^4` | Tailwind 4's PostCSS plugin. |
| `typescript` | `^5` (dev) | Type checker. |
| `@types/node` | `^20` (dev) | Node typings. |
| `@types/react` / `@types/react-dom` | `^19` (dev) | React typings. |
| `eslint` | `^9` (dev) | Linter. |
| `eslint-config-next` | `16.2.6` (dev) | Next.js ESLint preset. |

The website uses **Bun** as the package manager (`bun.lock` is the lockfile, not `package-lock.json`). It deploys to Firebase. The site is a docs / marketing surface, not part of the runtime path for `skills-registry` or `skills-registry-mcp` — both ship through GitHub Releases and PyPI respectively.

## Why the runtime surface is intentionally small

The MCP server runs as a long-lived subprocess inside other people's tooling (Claude Desktop, Cursor, VS Code/Copilot). Every new mandatory dep is a new chance for an install failure inside a stripped subprocess environment with no shell PATH. Keeping the runtime dep set at one (`fastmcp`) is a deliberate stance:

- All GitHub I/O goes through `gh api` (subprocess), not an HTTP client.
- All filesystem traversal uses `pathlib` + `os`, not a third-party walker.
- Frontmatter parsing is hand-rolled in `src/skills_mcp/frontmatter.py`. The flat key/value plus block-scalar (`>`, `>-`, `|`, `|-`) shape we care about does not justify pulling in PyYAML. Lists and nested mappings are silently dropped — fine for the current scope.
- No retry/backoff library; the Git Data API retry is a literal `for _ in range(3)` with exponential sleep.

The Go side is similarly minimal: cobra for subcommand dispatch, Bubble Tea for the TUI, yaml.v3 for local-skill scanning. The registry client itself avoids yaml.v3 and reuses the hand-rolled `parseFlatYAML` for parity with the Python frontmatter parser.

See [../background/design-decisions.md](../background/design-decisions.md) for the longer-form rationale on individual decisions (why no PyYAML, why two upload paths, why a Go CLI when the server is Python).
