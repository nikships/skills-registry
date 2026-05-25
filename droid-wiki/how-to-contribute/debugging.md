# Debugging

A troubleshooting runbook for the two most common failure surfaces — the MCP server boot path and the interactive wizard — plus the inline-execution recipes you'll want when a real client makes the bug hard to reproduce.

## Symptoms → suspects

| Symptom | First place to look | Likely cause |
| --- | --- | --- |
| MCP server exits with code `2` | `~/.config/skills-mcp/registry.toml` | `ConfigError` — no config file, or `repo` field is missing / malformed. Run `skills-registry` (no args) to re-bootstrap, or set `SKILLS_REGISTRY=owner/repo` for a one-shot. |
| MCP server exits with code `3` | `PATH` inside the spawning client | `GhNotFoundError` — `gh` not found by `find_gh`. Desktop clients spawn the server with a stripped `PATH`; install `gh` to `/opt/homebrew/bin`, `/usr/local/bin`, `/usr/bin`, or `~/.local/bin`. |
| MCP server exits with code `4` | `gh auth status` | `GhNotAuthedError` — `gh` is installed but not authed. Run `gh auth login` in a real shell. |
| Wizard fails before the first prompt | `gh auth status`, `git --version` | Missing `gh` or `git`. The wizard `requireGitForBootstrap` step hard-stops if `git` isn't on `PATH`. |
| `publish_skill` keeps returning `HTTP 409` / `422` | Another writer is racing | Retry budget is 3 with exponential backoff. Persistent conflicts mean a second client is publishing the same slug; serialize callers. |
| Cache never invalidates | `~/.cache/skills-mcp/skills/<slug>.meta.json` | Stale `tree_sha` field. Compare against the registry's current `<slug>/` tree SHA from `gh api repos/<repo>/contents/`. Wipe the cache directory to force a refresh. |
| `install.sh` 404s on tarball download | GitHub release assets | Asset pattern is `skills-registry_<os>_<arch>.tar.gz` (or `.zip` for Windows). Mismatched OS/arch detection in `uname` is the usual cause. |
| Wizard hangs on "Installing skills-registry-mcp" | `SKILLS_SKIP_INSTALL=1` not set | The Go binary tries `uv tool install` → `pipx install` → `pip install --user` in order. Long pauses are pip being slow. Set `SKILLS_SKIP_INSTALL=1` to skip and install the entry point yourself. |
| CLI prints help instead of opening the hub | TTY detection | The hub only opens for interactive TTY sessions. Piped invocations (`skills-registry | cat`) or `--json` print help. See `bareRouteDecision` in `cli/cmd/skills-registry/main.go`. |
| `gofmt -l .` fails in CI but passes locally | Editor formatting | Run `(cd cli && gofmt -w .)` to fix. Don't gitignore `.editorconfig`; the repo doesn't ship one. |
| `ruff format --check` fails in CI | Indent style is tabs | `ruff.toml` sets `[format].indent-style = "tab"`. Editors that auto-indent with spaces will trip CI. |

## Verbose logs

```bash
SKILLS_LOG_LEVEL=DEBUG uv run python -m skills_mcp.registry_server
SKILLS_LOG_LEVEL=DEBUG skills-registry-mcp
```

`SKILLS_LOG_LEVEL` is read at server boot by `registry_server.main()` (and again by `__main__.py:main()` for the legacy init path) and fed into `logging.basicConfig`. Valid levels: `DEBUG`, `INFO`, `WARNING`, `ERROR`. Default is `INFO`. Logs land on stderr so they don't tangle with MCP stdio responses.

The Go CLI honours `--json` for machine-readable output but does not have a verbose-log toggle — Bubble Tea owns stdout/stderr during interactive use. For wizard-flow debugging, see [inspecting the wizard model](#inspecting-the-wizard-model) below.

## Where things live on disk

| Path | What's there |
| --- | --- |
| `~/.config/skills-mcp/registry.toml` | Persistent config: `repo = "owner/name"`, optional `branch`. Read by both Python and Go. Overridden per-run with `SKILLS_REGISTRY=owner/repo` (or `owner/repo@branch`). |
| `~/.cache/skills-mcp/skills/<slug>/` | Cached skill folder fetched by `get_skill`. Sibling `<slug>.meta.json` carries the `tree_sha` used for invalidation. |
| `~/.local/bin/skills-registry` | The Go CLI, dropped by `install.sh`. Override with `SKILLS_BIN_DIR=<dir>`. |
| `~/.local/bin/skills-registry-mcp` (or `~/.local/share/uv/tools/skills-registry/bin/skills-registry-mcp`, or `/opt/homebrew/bin/...`, or `/usr/local/bin/...`) | The Python MCP entry point. The wizard probes these in order. |
| `~/.gitconfig` | After the wizard's bootstrap step, contains `[credential "https://github.com"] helper = !gh auth git-credential` written by `gh auth setup-git`. |
| `~/.factory/skills`, `~/.claude/skills`, `~/.cursor/skills`, … | Per-agent dot-folders. The wizard's agent multi-select writes the bootstrap `SKILL.md` into each chosen folder. |

Honor `XDG_CONFIG_HOME` and `XDG_CACHE_HOME` if set; both implementations read them.

## Running the MCP server inline

The fastest way to reproduce a server-side bug is to run it the way a desktop client does — over stdio:

```bash
SKILLS_LOG_LEVEL=DEBUG uv run python -m skills_mcp.registry_server
SKILLS_REGISTRY=owner/repo uv run python -m skills_mcp.registry_server
SKILLS_REGISTRY=owner/repo@dev uv run python -m skills_mcp.registry_server
```

The server prints initialization errors to stderr and exits with codes `2`, `3`, `4`. If it starts cleanly it blocks on stdin; kill with Ctrl-C. To test the installed wheel: `uv tool install --force skills-registry && SKILLS_LOG_LEVEL=DEBUG skills-registry-mcp`. To rule out a stripped-PATH issue, run the server with the exact env the desktop client passes.

## Inspecting the wizard model

The Bubble Tea models expose state via exported accessors so tests and humans can introspect them after the program exits. The useful ones on `WizardModel` (`cli/internal/tui/wizard.go`):

| Accessor | What it tells you |
| --- | --- |
| `Step()` | Current `WizardStep` (auth, scan, repo prompt, push, agents, cleanup, install, done). |
| `Completed()` | Reached Done and pressed enter. |
| `Cancelled()` | Confirmed Esc-cancel or pressed Ctrl-C. |
| `Repo()` | Resolved `owner/name` after create-repo. Empty before. |
| `Pushed()` | Count of skills uploaded by the push step. |
| `AgentsInstalled()` | Number of agent dot-folders that received `SKILL.md`. |
| `MCPInstalled()` | Whether the MCP entry point was found on disk after step 7. |

In tests (`cli/internal/tui/wizard_test.go`), drive the model with `tea.Msg` values and assert on these. In production, `runWizard` reads them after `tea.Program.Run()` returns to decide whether to persist config and print the MCP JSON snippet. `HubModel`, `SettingsModel`, `ChoiceModel`, `InputModel`, and `MultiSelectModel` follow the same pattern.

## Common error patterns

- **"unexpected gh call: …"** — The test shim emitted this. Missing fixture; add `{key, body}` to the queue.
- **"non-fast-forward" on PATCH ref** — Another writer pushed between your `GET ref` and `PATCH ref`. Retries 3× with exponential backoff. Persistent failure means a concurrent publisher; serialize.
- **"SKILL.md missing required field 'name'"** — `frontmatter.parse_frontmatter` silently drops multi-line and nested values. Keep `name:` as a single-line string.
- **"file exceeds SKILLS_MAX_FILE_BYTES"** — Default cap is 2 MiB; override with `SKILLS_MAX_FILE_BYTES=<bytes>`. Python rejects; Go logs a warning and skips.
- **`gh release download` 404 from `install.sh`** — `SKILLS_REGISTRY_VERSION` pinned to a missing tag, or `uname -m` returned something `install.sh` doesn't map.

## Cross-references

- [Getting started](../overview/getting-started.md) — full env-var table and prerequisites.
- [Testing](testing.md) — the `gh` shim pattern you'll want when reproducing a bug as a test.
- [Systems › Caching](../systems/caching.md) — tree-SHA invalidation in detail.
- [Systems](../systems/index.md) — registry client, bootstrap push, cache, MCP-install cascade.
- [Apps](../apps/index.md) — installer, CLI, MCP server.
- [Deployment](../deployment.md) — what runs on release and how to chase a failed release.
