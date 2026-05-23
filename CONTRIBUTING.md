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
