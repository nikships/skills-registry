# Configuration

Active contributors: Nik Anand

Every knob `skills-registry` reads, where it reads it from, and the order of precedence. The Python MCP server and the Go CLI share the same config file but layer environment overrides on top.

## Config file

The single config file pins which GitHub repo backs the registry.

**Location:**

- `$XDG_CONFIG_HOME/skills-mcp/registry.toml` when `XDG_CONFIG_HOME` is set.
- `~/.config/skills-mcp/registry.toml` otherwise.

**Format:**

```toml
[registry]
repo = "owner/name"
default_branch = "main"
```

Both keys are required. `default_branch` defaults to `main` when the wizard writes the file, but the loader does not synthesize one — a malformed file is surfaced as `ErrMissing`-adjacent so the bare-command router can land the user on the right screen (see `cli/cmd/skills-registry/main.go:bareRouteDecision`).

The reader is `src/skills_mcp/config.py:load_config` on the Python side and `cli/internal/config/config.go` on the Go side. The two are kept in sync; if you change one, change the other in the same PR.

## Cache layout

`~/.cache/skills-mcp/` (or `$XDG_CACHE_HOME/skills-mcp/`) is the only on-disk state besides the config:

```text
~/.cache/skills-mcp/
└── skills/
    ├── <slug>/                 # downloaded skill contents
    └── <slug>.meta.json        # {"tree_sha": "..."} for cache invalidation
```

The MCP server invalidates `<slug>/` by comparing the live registry tree SHA to the saved `tree_sha`. Force-pushes and any subtree change correctly invalidate. See `src/skills_mcp/cache.py` and `cli/internal/cache/cache.go`.

## Environment variables

| Variable | Default | What it does | Used by |
| --- | --- | --- | --- |
| `SKILLS_REGISTRY` | (from config) | Override the registry for one command. Accepts `owner/repo` or `owner/repo@branch`. Takes precedence over `registry.toml`. | MCP server, CLI |
| `SKILLS_LOG_LEVEL` | `INFO` | Log verbosity for the MCP server. Set to `DEBUG` for verbose output. | MCP server |
| `SKILLS_SKIP_INSTALL` | unset | Set to `1` to skip auto-install of `skills-registry-mcp` in the wizard. | Go bootstrap |
| `SKILLS_REGISTRY_VERSION` | `latest` | Pin `install.sh` to a specific release tag (e.g. `v0.5.1`). | `install.sh` |
| `SKILLS_REGISTRY_REPO` | `anand-92/skills-registry` | Override the GitHub repo `install.sh` fetches the binary from. | `install.sh` |
| `SKILLS_BIN_DIR` | `~/.local/bin` | Where `install.sh` drops the binary. | `install.sh` |
| `SKILLS_REGISTRY_OS` | from `uname -s` | Override OS detection in the installer. | `install.sh` |
| `SKILLS_REGISTRY_ARCH` | from `uname -m` | Override arch detection in the installer. | `install.sh` |
| `SKILLS_REGISTRY_URL` | computed | Override the full tarball URL the installer downloads. | `install.sh` |
| `SKILLS_REGISTRY_TARBALL` | unset | Use a local tarball file instead of downloading. Useful for offline installs. | `install.sh` |
| `SKILLS_REGISTRY_DRY_RUN` | unset | If non-empty, print the resolved URL/dest and exit without writing anything. | `install.sh` |
| `SKILLS_MAX_FILE_BYTES` | `2097152` (2 MiB) | Per-file size cap on publish. Hard reject in Python; warning + skip in Go. | MCP server, Go publish |
| `XDG_CONFIG_HOME` | `~/.config` | Base directory for the config file. | All |
| `XDG_CACHE_HOME` | `~/.cache` | Base directory for the cache. | MCP server, Go cache |
| `GH_BIN` | `gh` from PATH | Override the `gh` binary path. Tests pin this to a stub. | Go registry client |

## Resolution order

The MCP server and the CLI both walk the same resolution chain when answering "which registry are we talking to?":

1. **`SKILLS_REGISTRY` env var.** If set, it wins. Parsed as `owner/repo` or `owner/repo@branch`. No fallback to the file.
2. **`registry.toml`.** Read from `$XDG_CONFIG_HOME/skills-mcp/registry.toml` (falling back to `~/.config/...`). Both `[registry].repo` and `[registry].default_branch` are required.
3. **`ErrMissing`.** Neither source produced a usable repo. The MCP server raises and the CLI either launches the wizard (bare invocation, TTY, no `--json`) or prints a help dump (non-TTY or `--json`).

`SKILLS_REGISTRY=owner/repo@feature-branch` is the standard way to point a single command at a fork or a branch without touching the user's config. Useful for reviewers checking a publish PR.

## XDG honor

Both `XDG_CONFIG_HOME` and `XDG_CACHE_HOME` are respected. On macOS, where Apple does not ship an XDG default, the loader falls back to the conventional `~/.config` and `~/.cache` paths. There is no Windows-specific path handling — the Go binary builds for `windows/amd64` but the install path is still rough; see the Windows installer gap noted in `/Users/dks0662779/skillsmcp/CLAUDE.md`.

## `gh` discovery

`gh` is the single credential anchor. Both runtimes locate it via:

1. `GH_BIN` env var if set.
2. `gh` on the process `PATH`.
3. A curated fallback list: `~/.local/bin`, `/opt/homebrew/bin`, `/usr/local/bin`, `/usr/bin`.

The fallback list matters in the MCP-server environment: desktop MCP clients (Claude Desktop, Cursor, VS Code/Copilot) launch the server in a stripped subprocess where the user's shell-extended PATH is gone. See `src/skills_mcp/gh.py:find_gh` and `cli/internal/registry/registry.go:FindGH`.

## Per-file size cap details

`SKILLS_MAX_FILE_BYTES` applies to publish only (the MCP server's `publish_skill` tool and the CLI's `publish` / wizard bootstrap path). Read paths (`list_skills`, `get_skill`, `sync`) have no equivalent ceiling — anything already in the registry is fetched in full.

- **Python (`src/skills_mcp/registry_api.py`):** files exceeding the cap raise a `ValueError`. The tool call fails.
- **Go (`cli/internal/registry/registry.go`):** files exceeding the cap log a warning and are skipped. The rest of the skill publishes.

The default of 2 MiB is a sanity bound against accidental binary uploads, not a security control.
