# Getting started

This page covers two paths: running `skills-registry` as a user, and setting up the repository for local development.

## As a user

### Prerequisites

- **GitHub CLI** (`gh`) installed and authenticated. `gh auth status` must succeed before anything else. Install from <https://cli.github.com/>.
- **`git`** on `PATH` — only required the first time, for the bulk push (single-skill `publish` and the MCP server don't need it).
- A terminal with TTY (the interactive wizard / hub use Charmbracelet's alt-screen).

No Python, `uv`, or `pipx` required ahead of time. The Go wizard installs the Python entry point during onboarding.

### Install

```bash
curl -fsSL https://raw.githubusercontent.com/anand-92/skills-registry/main/install.sh | sh
skills-registry
```

`install.sh` detects your OS/arch (`darwin/linux × amd64/arm64`), downloads the matching tarball from the latest GitHub release, and drops the binary at `~/.local/bin/skills-registry`. See [apps/installer](../apps/installer.md) for the environment variables that override behavior (`SKILLS_BIN_DIR`, `SKILLS_REGISTRY_VERSION`, etc.).

Running `skills-registry` with no subcommand routes you based on your state:

- **First time** → onboarding wizard (alt-screen TUI). Scans your dot-folders, creates the GitHub repo, pushes every skill in a single `git push`, lets you multi-select agents to wire up, offers to delete local copies, installs `skills-registry-mcp`, prints the MCP JSON snippet.
- **Returning** → dashboard hub. Cards for Manage / Sync / Add / Publish / Settings.
- **Piped / `--json`** → prints usage text instead of starting a TUI.

### Wire up your MCP client

The wizard prints platform-correct JSON at the end of onboarding. Paste it into:

- **Claude Code / Claude Desktop / Cursor / VS Code** — `mcp.json`:
  ```json
  {
    "mcpServers": {
      "skills-registry": {
        "command": "/Users/you/.local/bin/skills-registry-mcp"
      }
    }
  }
  ```
- **Codex** — `~/.codex/config.toml`:
  ```toml
  [mcp_servers.skills-registry]
  command = "/Users/you/.local/bin/skills-registry-mcp"
  ```

Use the absolute path (the wizard fills it in). Desktop MCP clients spawn the server with a stripped environment, so `PATH` lookups don't work reliably.

### Daily commands

| Goal | Command |
| --- | --- |
| Open the dashboard | `skills-registry` |
| Browse your registry | `skills-registry list` |
| Pull one skill into the current folder | `skills-registry get <slug>` |
| Push local dot-folder skills missing from the registry | `skills-registry sync` |
| Pull from another repo and publish to yours | `skills-registry add <owner/repo>` |
| Publish a single local skill folder | `skills-registry publish <path>` |
| Delete a skill end-to-end (registry + cache + dot-folders) | `skills-registry remove <slug>` |
| Re-run the wizard (idempotent) | `skills-registry bootstrap` |

Every subcommand accepts `--json` for scripted use. See [systems/json-output](../systems/json-output.md).

### Environment variables

| Variable | Default | What it does |
| --- | --- | --- |
| `SKILLS_REGISTRY` | (from config) | Override the registry for one command: `owner/repo` or `owner/repo@branch`. |
| `SKILLS_LOG_LEVEL` | `INFO` | Bump to `DEBUG` for verbose logs. |
| `SKILLS_SKIP_INSTALL` | unset | Skip the auto-install of `skills-registry-mcp`. |
| `SKILLS_REGISTRY_VERSION` | `latest` | Pin `install.sh` to a specific release tag. |
| `SKILLS_BIN_DIR` | `~/.local/bin` | Where `install.sh` drops the binary. |
| `XDG_CONFIG_HOME` / `XDG_CACHE_HOME` | OS default | Where the registry config and skill cache live. |

The registry repo URL is stored in `~/.config/skills-mcp/registry.toml`. See [reference/configuration](../reference/configuration.md).

## As a contributor

### Clone and set up

```bash
git clone https://github.com/anand-92/skills-registry
cd skills-registry          # the directory may be called skillsmcp locally
uv sync --group dev
(cd cli && go mod download)
```

You need Python 3.10+ (3.11+ recommended for `tomllib`), Go 1.24+, and [`uv`](https://github.com/astral-sh/uv) for Python dependency management.

### Install the Go dead-code analyzers (pinned to CI versions)

```bash
go install honnef.co/go/tools/cmd/staticcheck@2025.1.1
go install golang.org/x/tools/cmd/deadcode@v0.45.0
go install github.com/fzipp/gocyclo/cmd/gocyclo@v0.6.0
```

Bump these in lockstep with `.github/workflows/ci.yml`.

### Run all tests

```bash
# Python (139 tests)
uv run pytest -v --cov=skills_mcp --cov-report=term-missing

# Go (build + vet + lint + dead-code + complexity + tests)
(cd cli && go vet ./... && staticcheck ./... && deadcode -test ./... && gocyclo -over 15 -ignore "_test" . && go test ./...)
```

### Lint and format

```bash
uv run ruff check .
uv run ruff format .
(cd cli && gofmt -l .)        # output must be empty
```

CI gates on `ruff check`, `ruff format --check`, `pytest`, `gofmt -l`, `go vet`, `staticcheck`, `deadcode`, `gocyclo`, and `go test`. All must be green to merge.

### Smoke-test the Go binary locally

```bash
(cd cli && go build -o /tmp/skills-registry ./cmd/skills-registry && /tmp/skills-registry --help)
```

### Run the MCP server locally

```bash
SKILLS_REGISTRY=owner/repo uv run python -m skills_mcp.registry_server
```

Or after the wizard has installed it, just `skills-registry-mcp` will work.

### Pre-commit hooks

```bash
uv run pre-commit install
```

The repo ships a minimal `.pre-commit-config.yaml`. Hooks run `ruff check` and `ruff format` on Python files.

## Where to next

- **Build new features** → [how-to-contribute](../how-to-contribute/index.md)
- **Understand the architecture** → [overview/architecture](architecture.md)
- **Modify the registry contract** → [systems/registry-client](../systems/registry-client.md)
- **Add tests** → [how-to-contribute/testing](../how-to-contribute/testing.md)
- **Trace a failure** → [how-to-contribute/debugging](../how-to-contribute/debugging.md)
