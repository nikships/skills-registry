# Tooling

Every build, lint, and test tool the repo uses, with pinned versions matching CI. Bump versions in lockstep with `.github/workflows/ci.yml` — both jobs read the same pinned values.

## Quick reference

| Tool | Version | Where pinned | What it does |
| --- | --- | --- | --- |
| `uv` | latest (`astral-sh/setup-uv@v3` enables cache) | CI workflow | Python dep manager, build front-end. |
| `ruff` | `>=0.6` (dev dep), `v0.8.4` in pre-commit | `pyproject.toml`, `.pre-commit-config.yaml` | Python lint + format. |
| `pytest` | `>=7` | `pyproject.toml` | Test runner. |
| `pytest-cov` | latest | `pyproject.toml` | Coverage plugin. |
| `pre-commit` | latest dev | `pyproject.toml` | Hook runner. |
| `pre-commit-hooks` | `v5.0.0` | `.pre-commit-config.yaml` | Trailing whitespace, EOF newline, YAML/TOML/merge-conflict checks. |
| Go | `1.24` | CI workflow (`actions/setup-go@v5`) | Compiler + stdlib test runner. |
| `staticcheck` | `2025.1.1` | CI workflow + `cli/staticcheck.conf` | Go correctness + unused-symbol analyzer. |
| `deadcode` | `v0.45.0` | CI workflow | Go reachability-based dead-code analyzer. |
| `gocyclo` | `v0.6.0` | CI workflow | Go cyclomatic-complexity ceiling. |
| `gofmt` | bundled with Go 1.24 | CI workflow | Go formatter. CI fails if output of `gofmt -l .` is non-empty. |
| `hatchling` + `hatch-vcs` | build deps | `pyproject.toml` | PEP 517 build backend; version derived from `vX.Y.Z` tags. |
| `apple-actions/import-codesign-certs` | `v3` | release workflow | Imports Developer ID Application cert for darwin builds. |
| `xcrun notarytool` | bundled with Xcode | release workflow | Submits the signed binary to Apple for notarization. |
| `pypa/gh-action-pypi-publish` | `release/v1` | release workflow | Uploads the wheel to PyPI. |

## Python tooling

### `uv`

`uv` is the only thing you need to bootstrap Python. CI uses `astral-sh/setup-uv@v3 enable-cache: true`. Local commands:

```bash
uv sync --group dev          # install runtime + dev deps from uv.lock
uv build                     # build wheel + sdist
uv run pytest                # run pytest in the project venv
uv run ruff check .          # lint
uv run ruff format --check . # CI variant; fails on diff
uv tool install skills-registry   # install the published wheel as a tool
```

`[dependency-groups].dev` in `pyproject.toml` lists every dev dep. The only mandatory runtime dep is `fastmcp>=3.1.1,<4`.

### `ruff` config (`ruff.toml`)

`line-length = 100`, `target-version = "py310"`, `[format].indent-style = "tab"`. The whole codebase indents with tabs; editors that auto-convert will trip CI.

Rule sets enabled:

| Set | What it covers |
| --- | --- |
| `E` / `F` | pycodestyle / pyflakes basics. |
| `I` | isort import ordering. |
| `B` | flake8-bugbear. |
| `UP` | pyupgrade. |
| `SIM` | flake8-simplify. |
| `TID` | flake8-tidy-imports. |
| `N` | pep8-naming (snake_case funcs, CapWords classes, UPPER_SNAKE_CASE constants). |
| `C90` | mccabe cyclomatic-complexity, capped at 12. |

`E501` is ignored — the formatter handles wrapping. `extend-ignore-names = ["MCP", "Gh"]` lets domain initialisms live without `N`-rule violations. `tests/**` relaxes `B011`, `N802`, `N803`, `N806` for pytest-specific patterns.

### `pytest`

Run with `uv run pytest`. Test discovery picks up `tests/test_*.py`. Coverage runs come from `--cov=skills_mcp --cov-report=...`. See [testing](testing.md) for the shim pattern.

### Pre-commit hooks

`.pre-commit-config.yaml` ships `pre-commit-hooks v5.0.0` (trailing-whitespace, end-of-file-fixer, check-yaml, check-toml, check-merge-conflict) and `ruff-pre-commit v0.8.4` (`ruff --fix` + `ruff-format`). Install with `uv run pre-commit install`. There's no Go hook — `gofmt -l .` runs in CI.

## Go tooling

### Pinned analyzers

CI installs three Go analyzers at fixed versions. Match them locally:

```bash
go install honnef.co/go/tools/cmd/staticcheck@2025.1.1
go install golang.org/x/tools/cmd/deadcode@v0.45.0
go install github.com/fzipp/gocyclo/cmd/gocyclo@v0.6.0
```

### `staticcheck` config (`cli/staticcheck.conf`)

```toml
checks = ["all", "-ST*", "-QF*"]
```

`all` enables everything; the negations skip `ST*` style checks (e.g. `ST1005` error-message punctuation) and `QF*` quickfix suggestions. What's left covers correctness (`SA*`), unused identifiers (`U*`), and the rest of `all` — "dead-code detection + correctness, no style noise".

### `gofmt`

`(cd cli && gofmt -l .)` must produce empty output. CI fails the job if anything is unformatted. Fix with `(cd cli && gofmt -w .)`.

### `deadcode` and `gocyclo`

`deadcode -test ./...` runs reachability-based unused-function analysis; the `-test` flag adds test files as roots so test-only helpers don't register as dead.

`gocyclo -over 15 -ignore "_test" .` caps cyclomatic complexity at 15 for production code; test files are excluded because table-driven tests naturally inflate the metric. The Python ceiling is 12 (ruff `C90`). Both gates run in `ci.yml` and `release.yml`. **Don't raise them.** Extract helpers.

### `go vet`

Stdlib analyzer for known-bad patterns (printf format mismatches, copied locks, lost cancels). `go vet ./...` from `cli/`.

## Release-pipeline tooling

`.github/workflows/release.yml` orchestrates:

### Python build — `hatchling` + `hatch-vcs`

The wheel uses `hatchling` as the PEP 517 backend. Version is **dynamic** — `hatch-vcs` reads the latest `vX.Y.Z` tag and stamps it onto the wheel (see `[tool.hatch.version] source = "vcs"`). No manual version bumps. The tag job pushes the next tag, then `build-wheel` checks out that tag and runs `uv build`.

### Go build — `goreleaser`-style asset naming

Each `(GOOS, GOARCH)` combo gets one archive named `skills-registry_<os>_<arch>.tar.gz` (or `.zip` on Windows): `darwin_amd64`, `darwin_arm64`, `linux_amd64`, `linux_arm64`, `windows_amd64`. The build is plain `go build -trimpath -ldflags "-s -w -X main.version=${version}"`. There's no `goreleaser` dep; the naming mirrors what `install.sh` substitutes against.

### Codesign + notarize (darwin)

Darwin binaries can't run after download without notarization (`com.apple.provenance` quarantine). The pipeline:

1. `apple-actions/import-codesign-certs@v3` imports the Developer ID Application P12 into the keychain.
2. `codesign --force --options runtime --timestamp --sign "<identity>" skills-registry` — hardened runtime + secure timestamp + Developer ID signature.
3. `codesign --verify --strict --verbose=2 skills-registry`.
4. `ditto -c -k --keepParent skills-registry skills-registry.notarize.zip` (notarytool wants a zip).
5. `xcrun notarytool submit … --wait --output-format json` blocks until Apple accepts or rejects. The job parses `status` from the JSON and fails on non-`Accepted`.

A bare Mach-O can't be stapled, but once the CDHash is in Apple's ticket service macOS performs a one-time online check on first launch.

### PyPI publish

`pypa/gh-action-pypi-publish@release/v1` uploads the wheel + sdist from the `build-wheel` job. The `pypi` GitHub environment scopes `PYPI_API_TOKEN`.

### `install.sh`

POSIX `sh`. Detects OS via `uname -s` (lowercased), arch via `uname -m` (`x86_64` → `amd64`, `aarch64`/`arm64` → `arm64`), downloads the asset, extracts with `tar` (or `unzip` for Windows), drops the binary at `${SKILLS_BIN_DIR:-$HOME/.local/bin}/skills-registry`, chmod 755. Pin a release with `SKILLS_REGISTRY_VERSION=v0.5.1`.

## Cross-references

- [Development workflow](development-workflow.md) — what CI runs, when releases auto-cut, how to dispatch a non-patch bump.
- [Testing](testing.md) — `pytest`, `go test`, the shared shim pattern.
- [Debugging](debugging.md) — `SKILLS_LOG_LEVEL`, inline server runs, wizard-model introspection.
- [Patterns and conventions](patterns-and-conventions.md) — naming, cyclomatic ceilings, the two-language contract.
- [Apps](../apps/index.md), [Systems](../systems/index.md), [Getting started](../overview/getting-started.md), [Deployment](../deployment.md).
