# Development workflow

The branch → code → test → PR → merge cycle for `skills-registry`. CI runs the same checks listed here; nothing magic happens server-side.

## Clone and set up

```bash
git clone https://github.com/anand-92/skills-registry
cd skills-registry
uv sync --group dev
(cd cli && go mod download)
```

Prerequisites:

- Python 3.10+ (3.11+ recommended so `tomllib` is available without backports).
- Go 1.24+.
- [`uv`](https://github.com/astral-sh/uv) for Python deps.
- `gh` on `PATH`, authenticated (`gh auth status`) — both for the test suite's manual integration runs and for the install scripts.
- `git` on `PATH` — required by the bulk-import path of the wizard. Optional for everything else.

Install the Go analyzers in the same versions CI uses:

```bash
go install honnef.co/go/tools/cmd/staticcheck@2025.1.1
go install golang.org/x/tools/cmd/deadcode@v0.45.0
go install github.com/fzipp/gocyclo/cmd/gocyclo@v0.6.0
```

Bump these in lockstep with `.github/workflows/ci.yml`.

## Branch

Branch off `main` with a name that reads as a sentence fragment:

```bash
git checkout -b feat/atomic-remove
git checkout -b fix/cache-tree-sha-collision
git checkout -b docs/contributing-onboarding
```

There are no enforced branch-name rules, but the convention matches the conventional-commit prefixes the auto-tagger reads (see below).

## Pre-commit hooks

```bash
uv run pre-commit install
```

The repo ships `.pre-commit-config.yaml` with:

- `trailing-whitespace`, `end-of-file-fixer`, `check-yaml`, `check-toml`, `check-merge-conflict` (from `pre-commit-hooks v5.0.0`).
- `ruff` (with `--fix`) and `ruff-format` (from `ruff-pre-commit v0.8.4`).

There is no Go pre-commit hook — Go formatting is gated by `gofmt -l .` in CI. Run it yourself if you want a fast local check:

```bash
(cd cli && gofmt -l .)        # output must be empty
```

## Code → test loop

Local equivalents of every CI gate:

```bash
# Python: lint + format + tests with coverage
uv run ruff check .
uv run ruff format --check .          # CI uses --check; locally you can format
uv run pytest -v --cov=skills_mcp --cov-report=term-missing

# Go: vet + lint + dead-code + complexity + tests
(cd cli && go vet ./...)
(cd cli && staticcheck ./...)
(cd cli && deadcode -test ./...)
(cd cli && gocyclo -over 15 -ignore "_test" .)
(cd cli && go test ./...)
```

Smoke-test the CLI build:

```bash
(cd cli && go build -o /tmp/skills-registry ./cmd/skills-registry && /tmp/skills-registry --help)
```

Run the MCP server inline against a real registry without installing the wheel:

```bash
SKILLS_REGISTRY=owner/repo uv run python -m skills_mcp.registry_server
```

See [debugging](debugging.md) for more inline-execution recipes.

## Commit prefixes

The release auto-bumper reads conventional-commit-ish prefixes off the latest commits when computing the next semver. Use them.

| Prefix | What it covers | Bump |
| --- | --- | --- |
| `feat:` | New user-visible feature | patch (minor if dispatched manually) |
| `fix:` | Bug fix | patch |
| `docs:` | README, examples, contributing notes | none (doesn't trigger release) |
| `refactor:` | Internal change, no behavior delta | patch |
| `test:` | Tests only | patch (only if `tests/` triggers the path filter) |
| `chore:` | Build, deps, tooling, version bumps | patch |
| `ci:` | Workflow changes | none (doesn't trigger release) |

Add `BREAKING CHANGE:` in the commit body to signal a hand-dispatched major bump. The auto-bumper does not pick this up automatically; see [the release section below](#auto-release-pipeline).

Example commit message:

```
fix: ignore SKILL.md files under hidden directories

scan.Discover was descending into `.git/hooks/` and treating any
SKILL.md it found as a real skill. Add a dotfile check before recurse.
```

## Open the PR

```bash
gh pr create --base main --fill
```

Make sure the PR description explains "why", not just "what". Tag any related issue. CI will run automatically; the merge button stays disabled until both the Python and Go jobs pass.

PR checklist lives on the [section index](index.md#pr-checklist).

## CI gates

`.github/workflows/ci.yml` runs two jobs in parallel on every push and PR:

- **Python** — `ruff check`, `ruff format --check`, `pytest -v --cov=skills_mcp --cov-report=xml`.
- **Go** — `go vet`, `gofmt -l .` (fails if non-empty), `staticcheck ./...`, `deadcode -test ./...`, `gocyclo -over 15 -ignore "_test" .`, `go build ./...`, `go test ./...`.

Both must be green. Coverage XML is generated but not uploaded anywhere yet.

## Auto-release pipeline

`.github/workflows/release.yml` auto-cuts a patch release on every push to `main` that touches `src/`, `cli/`, `tests/`, or `pyproject.toml`. Pushes that only touch `docs/`, `droid-wiki/`, workflow files, or other paths do not trigger a release.

The pipeline:

1. **Test gate** — Same checks as `ci.yml`. Failures abort the release.
2. **Tag** — Compute the next semver from the latest `vX.Y.Z` tag, push a lightweight tag on the triggering commit. CI never commits a version bump back to `main`; the Python wheel version is dynamic via `hatch-vcs`.
3. **Build Python** — `uv build` produces wheel + sdist.
4. **Build Go CLI** — `go build` for darwin/amd64, darwin/arm64, linux/amd64, linux/arm64, windows/amd64. Darwin binaries are codesigned (`apple-actions/import-codesign-certs@v3` + Developer ID Application identity) and notarized (`xcrun notarytool submit --wait`). The result is `.tar.gz` for POSIX platforms and `.zip` for Windows, named `skills-registry_<os>_<arch>.{tar.gz,zip}`.
5. **Release** — `gh release create vX.Y.Z` with all 7 assets attached: wheel, sdist, 4 darwin/linux tarballs, 1 Windows zip.
6. **PyPI publish** — Uploads the wheel to PyPI through the `pypi` environment using `PYPI_API_TOKEN`.

`install.sh` always points at the latest release, so a successful run is immediately the source of truth for new installs.

Force a non-patch bump by hand-dispatching:

```bash
gh workflow run release.yml -f bump=minor
gh workflow run release.yml -f bump=major
```

The dispatch input accepts `patch`, `minor`, or `major`.

## Merge

Squash-and-merge is the default. The commit message that lands on `main` should keep a conventional-commit prefix; the auto-bumper will pick it up.

After merge, watch the Actions tab. If the release run fails (e.g. notarization rejected, PyPI 4xx), the tag is already pushed but the assets aren't published. Re-run the failing job or delete the tag and let the next push retry.

## Cross-references

- [Getting started](../overview/getting-started.md) — full prerequisites and install steps for users vs. contributors.
- [Testing](testing.md) — how to write a new test for either language.
- [Tooling](tooling.md) — pinned versions and config for every linter and analyzer.
- [Systems](../systems/index.md) — the modules a typical change touches.
- [Deployment](../deployment.md) — release pipeline in detail (failure modes, asset layout, codesign + notarize).
