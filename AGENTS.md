# Agent Notes — skills-mcp

This file is a living guide for AI agents and new contributors. It captures the architecture, patterns, known issues, and improvement opportunities identified during codebase reviews.

---

## Project Overview

`skills-mcp` is a tiny [FastMCP](https://github.com/jlowin/fastmcp) server that exposes a directory of Markdown `SKILL.md` files as MCP resources and a single `show_skills` discovery tool. It ships with two CLI subcommands — `gather` (consolidate scattered skills) and `add` (install from a git repo or local path).

- **Language:** Python 3.10+
- **Build:** `hatchling` (PEP 517)
- **Package manager:** `uv`
- **Test runner:** `pytest` with `pytest-cov`
- **Lint/Format:** `ruff`
- **Entry point:** `skills-mcp = skills_mcp.__main__:main`

---

## Repository Layout

```
src/skills_mcp/
  __init__.py      # Package metadata (__version__ = "0.2.0")
  __main__.py      # Core server, CLI orchestration, Skill class, discovery, frontmatter parsing
  gather.py        # `skills-mcp gather` — scan dot-folders, dedupe, copy, auto-config clients
  add.py           # `skills-mcp add` — resolve git/local sources, clone, filter, install
tests/
  conftest.py      # Shared `make_skill` fixture
  test_*.py        # 140 tests covering CLI, discovery, frontmatter, slugification, gather, add, skills tool
```

---

## Architecture

### Dual-Mode Design
The same entry point acts as both a CLI utility and an MCP server over stdio:
- No subcommand / `serve` → run `build_server().run()` (stdio MCP transport)
- `list` → print discovered skills and exit
- `gather` → consolidate skills from known AI-tool dot-folders
- `add` → install skills from a git repo or local path

### Subcommand Registration Pattern
`gather.py` and `add.py` each export a `register_subparser()` function that is imported **lazily** inside `main()`. This avoids loading heavy modules when they are not needed, but it also creates a mild circular-import workaround: both submodules import `Skill` and `discover_skills` from `.__main__` at runtime.

### Plan-Then-Execute (`gather.py`)
The `gather` command is designed around pure planning:
1. `find_source_dirs()` → discover known AI-tool dot-folders
2. `build_plan()` → compute a `Plan` dataclass (entries + conflicts) without any I/O
3. `print_plan()` → show the plan to the user
4. `execute_plan()` → perform copies/symlinks
5. `delete_sources()` → optional cleanup
6. `_auto_configure_clients()` → update known MCP client configs (JSON for Claude/Cursor/VS Code, TOML for Codex)

### Skill Identity
All skills are normalized to a filesystem-safe slug via `_slug()` (lowercase, non-alphanumeric → `_`). Duplicate slugs are deduped with a "first wins" strategy; a warning is logged.

### FastMCP Integration
- `FastMCP(server_name, instructions=...)` initializes the server
- `SkillsDirectoryProvider(roots, main_file_name, supporting_files="resources", reload)` wires the skills folder as MCP resources
- A single `show_skills` tool is registered for discovery (per-skill tools were intentionally removed to avoid system-prompt bloat)

---

## Key Classes & Functions

| Symbol | File | Role |
|--------|------|------|
| `Skill` | `__main__.py` | Value object: reads `SKILL.md`, parses frontmatter, derives `name`, `slug`, `description`. Uses `__slots__`. |
| `discover_skills()` | `__main__.py` | Recursively finds `SKILL.md` under roots, dedupes by slug. |
| `_parse_frontmatter()` | `__main__.py` | Hand-rolled YAML-ish frontmatter parser (avoids PyYAML dep). Flat `key: value` only. |
| `_first_paragraph()` | `__main__.py` | Extracts first prose paragraph (≤240 chars) for description fallback. |
| `build_server()` | `__main__.py` | Composes `FastMCP` + `SkillsDirectoryProvider` + `show_skills` tool. |
| `build_plan()` | `gather.py` | Content-aware dedupe via SHA-256; conflict strategies: `skip`, `newest`, `rename`. |
| `cmd_add()` | `add.py` | Resolves GitHub shorthand → full URL, clones, filters by skill name/slug, copies. |

---

## Testing

- **140 tests**, all passing. ~80% coverage overall.
- Coverage gaps: `add.py` subprocess/prompt paths (77%), `gather.py` auto-config/prompt paths (75%), `__main__.py` error branches (95%).
- Fixtures: `make_skill` (creates fake `SKILL.md` with optional frontmatter), `subparsers` (for argparse subparser tests).
- Run: `uv run pytest -v --cov=skills_mcp --cov-report=term-missing`

---

## Known Issues & Improvement Opportunities

### High Impact

1. **Domain objects live in `__main__.py`** — `Skill`, `discover_skills`, `_slug`, `_parse_frontmatter` are imported by `gather.py` and `add.py` from the CLI entry point. This forces lazy imports and creates a circular dependency risk. **Fix:** Extract a `skills_mcp.core` or `skills_mcp.models` module.

2. **`show_skills` tool output is too minimal** — The README claims it returns "name, description, slug, and file path", but the implementation only emits slug names and the root path. The MCP client (LLM) gets very little metadata to decide which skill to load. **Fix:** Enrich the response with a markdown table of `name`, `description`, and `skill://<slug>/SKILL.md` URIs.

3. **`SKILLS_RELOAD=true` does not refresh `show_skills`** — `discover_skills()` is called once at server construction. Even with `reload=True` on `SkillsDirectoryProvider`, the cached `skills` list in `_register_show_skills_tool` stays stale. **Fix:** Re-discover inside the tool or share reload-aware state.

### Medium Impact

4. **Mixed UI & business logic** — `print()` calls are embedded deep in `cmd_gather()`, `cmd_add()`, and `_cmd_list()`. Makes unit testing noisy (requires `capsys`). **Fix:** Return data structures from command handlers and let a thin presentation layer do the printing.

5. **`SystemExit` raised from utility functions** — `_parse_roots()`, `_parse_bool()` raise `SystemExit` instead of typed exceptions. Prevents callers from deciding how to handle errors. **Fix:** Raise typed exceptions and convert to exit codes only at the `main()` boundary.

6. **`Skill.__init__` reads disk immediately** — Constructor I/O makes it harder to unit-test with mocked files. **Fix:** Provide `@classmethod Skill.from_path(...)` or accept pre-read text.

7. **`build_server()` discovers skills twice** — Once for `show_skills`, once inside `SkillsDirectoryProvider`. **Fix:** Share a single discovery result.

### Low Impact

8. **Frontmatter parser is ad-hoc** — No support for multi-line values, lists, or nested YAML. Documented as "YAML-ish". This is fine for the current scope but may confuse users expecting real YAML.

9. **Hard-coded client config paths** — `_auto_configure_clients()` encodes per-platform, per-client file paths. Adds maintenance burden as clients evolve.

10. **No `remove` / `uninstall` / `update` commands** — `gather` can copy and optionally delete sources, but there is no way to remove a single skill or pull upstream changes for git-installed skills.

---

## CI / CD

- `.github/workflows/ci.yml` — lint (`ruff check`), format (`ruff format --check`), test (`pytest + coverage XML`). Runs on `ubuntu-latest` only.
- `.github/workflows/release.yml` — builds and publishes to PyPI on `release: published`. Does **not** run tests before publishing (relies on CI having passed on `main`).
- **Gaps:** No Python version matrix (only tests one version), no OS matrix, no Dependabot, no issue templates (referenced in `CONTRIBUTING.md` but missing), coverage XML generated but not uploaded.

---

## Security Notes

- No secrets in source. `SECURITY.md` explicitly warns users not to place secrets inside `SKILLS_ROOT`.
- `subprocess.run()` is used with a list (no `shell=True`).
- Temp directories from `git clone` are cleaned up in `finally` blocks.
- `_parse_roots()` validates paths are directories. `gather.py` refuses a destination that lives inside a source to prevent recursive clobbering.
- Frontmatter parser avoids PyYAML, eliminating YAML deserialization attack surface.

---

## How to Work on This Repo

```bash
# Setup
uv sync --group dev

# Run tests
uv run pytest -v --cov=skills_mcp --cov-report=term-missing

# Lint & format
uv run ruff check .
uv run ruff format .

# Install pre-commit hooks
uv run pre-commit install
```

When making changes:
- Keep the project small and focused. One feature, one PR.
- Do not add new mandatory runtime dependencies without justification.
- Update `README.md` if you change anything user-visible.
- Add or update tests for any behavior change.
- Use conventional-commit-ish prefixes (`feat:`, `fix:`, `docs:`, `refactor:`, `test:`, `chore:`).
