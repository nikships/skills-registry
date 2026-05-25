# Contributing to skills-registry

Thanks for your interest! `skills-registry` is intentionally small: a GitHub-backed skill registry exposed over MCP, plus the Go CLI and bootstrap that wire it up. We want to keep it that way. Bug fixes, docs, and small focused features are very welcome. For anything larger, **please open an issue first** so we can agree on scope before you write code.

## Ethos

- **Small and focused.** One feature, one PR. If a change adds new mandatory dependencies, a new env var, or new public surface, it needs an issue first.
- **No magic.** A user should be able to read the code in one sitting.
- **Backwards-compatible by default.** Breaking changes to env vars, resource URIs, or CLI flags need a deprecation note.

## Development setup

You need Python 3.10+ and [uv](https://github.com/astral-sh/uv).

```bash
git clone https://github.com/anand-92/skills-registry
cd skills-mcp
uv sync --group dev
```

That installs the package in editable mode along with the dev dependencies.

## Running tests

```bash
uv run pytest
```

Add or update tests for any behavior change. If you fix a bug, add a regression test that fails before your fix.

## Lint and format

We use [ruff](https://github.com/astral-sh/ruff) for both linting and formatting.

```bash
uv run ruff check .
uv run ruff format .
```

CI runs `ruff check` and `pytest`. Both must pass.

The Go CLI is checked with `go vet` and `gofmt -l` (formatting drift fails CI).

```bash
(cd cli && go vet ./... && gofmt -l .)
```

## Naming conventions

We enforce a single, consistent naming style **per language**. These rules
are wired into the linters above; CI will reject violations.

### Python (`src/skills_mcp/`, `tests/`)

Enforced by **ruff's `N` rule set** (pep8-naming) — see `ruff.toml`.

| Construct | Convention | Example |
|---|---|---|
| Modules / packages | `snake_case` | `registry_api.py`, `skills_mcp` |
| Functions, methods, variables | `snake_case` | `def find_gh()`, `parent_sha` |
| Classes, exceptions, type aliases | `CapWords` (PascalCase) | `class RegistryClient`, `class GhAuthError` |
| Constants (module-level) | `UPPER_SNAKE_CASE` | `SKILLS_MAX_FILE_BYTES`, `DEFAULT_BRANCH` |
| Private (module-internal) names | leading underscore | `_normalize_rel_path` |
| Test helpers | follow same rules; pytest fixture functions are `snake_case` | `def tmp_registry(...)` |

Accepted initialisms that may appear in CamelCase boundaries: `MCP`, `Gh`
(declared via `extend-ignore-names` in `ruff.toml`).

### Go (`cli/`)

Go's naming conventions are part of the language culture and are enforced by
`gofmt` + `go vet` (both run in CI) plus code review:

| Construct | Convention | Example |
|---|---|---|
| Packages | `lowercase`, single word, no underscores | `registry`, `bootstrap`, `tui` |
| Files | `lowercase`, underscores allowed | `multiselect.go`, `skillmd.go` |
| Exported identifiers (funcs, types, vars, consts) | `PascalCase` | `RegistryClient`, `PushTreeViaGit` |
| Unexported identifiers | `camelCase` | `runBootstrap`, `parentSha` |
| Acronyms | preserve case (`URL`, `SHA`, `MCP`, `ID`) | `repoURL`, `treeSHA`, `mcpServer` |
| Error variables | `Err`-prefix, `PascalCase` | `ErrNotFound`, `ErrConflict` |
| Receivers | 1–2 letter abbreviation of the type | `func (c *Client) Get(...)`, `func (m model) Update(...)` |
| Constants | Follows visibility rules (PascalCase / camelCase) | DefaultBranch, stateLoading |

Run `gofmt -l .` from `cli/` — output must be empty. Run `go vet ./...`
to catch shadowed identifiers and other naming-adjacent issues.
to catch shadowed identifiers and other naming-adjacent issues.

When you introduce a new construct that doesn't fit the table above, either
extend the linter configuration and update this table in the same PR, or
follow the nearest analogous rule.

### Project brand vs literal identifiers

The project's brand name — PyPI package, GitHub repo, README title, prose
references — is the plural form: **`skills-registry`**. Use that spelling
in any prose that talks about the project as a whole (docs, comments,
website copy, log messages, error text addressed to humans).

The following are spelled singular (`skills-registry`) for historical
reasons; renaming them would break every existing install:

| Token | Where it lives |
|---|---|
| `skills-registry` (Go binary) | `cli/cmd/skills-registry/`, `~/.local/bin/skills-registry`, release tarballs |
| `skills-registry-mcp` | Python console script in `pyproject.toml`, desktop MCP client configs |
| `"skills-registry"` MCP server name | Registered in `registry_server.py:build_server` |
| `[mcp_servers.skills-registry]` / `"skills-registry": {…}` | The config key users paste into Codex / Claude / Cursor / VS Code |
| `<dot-dir>/skills/skills-registry/SKILL.md` | Install path written by Go bootstrap |
| `skills-registry_<os>_<arch>` | Release artifact naming |

Keep these tokens singular whenever you quote them verbatim. New code
or docs must not introduce *new* singular spellings for project-brand
contexts. In one line: **brand = plural; literal token a user types or
pastes = singular**.

## Pre-commit

Install the git hooks so you catch issues before pushing:

```bash
uv run pre-commit install
```

## Commit messages

Use conventional-commit-ish prefixes — they make the changelog readable:

- `fix:` bug fix
- `feat:` new user-visible feature
- `docs:` README, examples, contributing
- `refactor:` no behavior change
- `test:` tests only
- `chore:` build, deps, tooling

Example: `fix: ignore SKILL.md files under hidden directories`.

## Pull request checklist

Before opening the PR, please confirm:

- [ ] Tests pass locally (`uv run pytest`).
- [ ] `uv run ruff check .` is clean.
- [ ] `README.md` is updated if you changed anything user-visible (env vars, CLI, behavior).
- [ ] No new **mandatory** runtime dependencies. Optional ones need justification in the PR description.
- [ ] The PR description explains *why*, not just *what*.

## Reporting bugs

Use the **Bug report** issue template. Please include:

- `skills-registry --version`
- Your MCP client and its version
- OS, Python version (and Go version if the CLI is involved)
- The registry repo (`owner/repo`) if relevant
- A minimal reproduction (commands run, expected vs actual output)

## Security issues

Please do **not** open a public issue. See [SECURITY.md](SECURITY.md).
