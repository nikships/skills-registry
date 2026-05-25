# Design decisions

Active contributors: Nik Anand

The non-obvious choices, grouped by rationale. Each section captures the constraint that forced the decision and the alternatives we rejected. When you find yourself wanting to "simplify" any of these, read the section first.

## Why two upload paths (gh-api vs git push)

The MCP server (`src/skills_mcp/registry_api.py:RegistryClient.publish_skill`) and the single-skill CLI publish (`cli/internal/registry/registry.go:Client.Publish`) both write through the Git Data API as a six-call atomic sequence (ref → commit → base tree → blobs → new tree → new commit → ref update). The wizard's bulk import (`cli/internal/registry/registry.go:Client.PushTreeViaGit`) does **not** — it shells out to `git push` over HTTPS for the whole tree.

The reason: GitHub enforces a secondary rate limit of ~80 POSTs/minute on `git/blobs`. A first-time user typically has 30–200 skills (100–500 files), and the blob path trips the limit and fails halfway through. A single skill is 1–10 files, so `publish_skill` stays well below the ceiling.

We could have made the MCP server use `git push` too — but it cannot, because of the next constraint. See also [../systems/registry-client.md](../systems/registry-client.md).

## Why the MCP server cannot use git push

Desktop MCP clients (Claude Desktop, Cursor, VS Code/Copilot) launch the MCP server in a stripped subprocess environment: `PATH` lacks shell extensions (`git` may not even be on it), `SSH_AUTH_SOCK` is unset, and `git config user.email` may be missing. A `git push` from the server would have to discover `git`, configure an author identity, and arrange credentials from scratch — each a real failure mode.

The `gh api` path inherits the user's authenticated `gh` (via `find_gh` walking `PATH` + the curated fallback dirs `~/.local/bin`, `/opt/homebrew/bin`, `/usr/local/bin`, `/usr/bin`) and side-steps every one of them. See [../security.md](../security.md).

## Why a Go CLI when the server is Python

The interactive surface needed a real TUI: alt-screen wizard, multi-select with fuzzy search and locked entries, dashboard hub with cards and toasts, settings screen. Charmbracelet (Bubble Tea + Lipgloss + bubbles) is the recommended library and it is Go-only. Python has nothing comparable.

Given we needed Go for the TUI, we made the Go binary the user-facing entry point. `install.sh` drops it at `~/.local/bin/skills-registry`. The user never sees Python during onboarding. The Python wheel still ships to PyPI — but only as the host for `skills-registry-mcp`, the FastMCP server. The wizard installs it in the background via `uv tool install` → `pipx install` → `pip install --user`.

## Why no PyYAML

The Python runtime has one mandatory dep (`fastmcp`). The frontmatter we parse is flat key/value with optional block scalars (`>`, `>-`, `|`, `|-`). Lists and nested mappings are dropped on purpose; they would change the contract.

PyYAML would add a second mandatory dep with a C-extension build path, a larger wheel, and a `pip install` that can fail on stripped systems. The hand-rolled parsers (`src/skills_mcp/frontmatter.py:parse_frontmatter` and `cli/internal/registry/registry.go:parseFlatYAML`) are ~80 lines each and cover the shape we serve.

Go does carry `yaml.v3`, but only `cli/internal/scan/scan.go` uses it for the local-skill walk. The registry client itself does not import it, for parity with Python.

## Why `gh` for every GitHub call

We could have used `requests` (Python) or `net/http` (Go) directly with a PAT. We do not. `gh` solves the authentication problem for us: the user has already authenticated. Reusing it means no PAT prompting, no on-disk token storage, no SSH keys, no `git config user.email`.

It also means credentials work in the stripped MCP subprocess environment, because `find_gh` / `FindGH` discover the binary even when the shell-extended `PATH` is gone. Single-binary discovery is the easiest way to ship credentials into the subprocess. The cost is one subprocess invocation per HTTP call — invisible against network latency.

## Why `bareRouteDecision` is a pure function

The bare-command routing matrix (`cli/cmd/skills-registry/main.go:bareRouteDecision`) decides where a user with no subcommand lands. It takes three values (`isTTY`, `--json`, `configLoadErr`) and returns one enum (`bareRouteHelp` / `bareRouteWizard` / `bareRouteHub` / `bareRouteError`). It does no I/O — the caller loaded the config and detected TTY/json mode.

Keeping it pure makes the matrix unit-testable end-to-end. Side effects (rendering the wizard, rendering the hub, printing help) live in the caller. If you sprinkle I/O into the routing function during a feature, push back.

## Why the install cascade is uv → pipx → pip

`cli/internal/bootstrap/mcp_install.go:EnsureMCPEntryPoint` tries three installers in order and stops at the first success:

1. **`uv tool install --force skills-registry`** — fastest; likely already present if the user has run `uvx`.
2. **`pipx install --force skills-registry`** — the next-best persistent installer.
3. **`python3 -m pip install --user --upgrade skills-registry`** — universal fallback; works wherever Python does.

Total failure is non-fatal. The bootstrap continues; the MCP-config snippet just references a binary the user must install themselves. Opt out entirely with `SKILLS_SKIP_INSTALL=1`. The cascade order prefers the better UX when it works and falls back when it does not.

## Known limitations / future work

Tracked but not yet implemented:

- **No `update` command.** Today users `publish` from a folder, which works but does not surface "what changed".
- **No multi-registry support.** Config is one repo. A `[registries]` array + a `connect <owner/repo>` CLI would let an agent see several registries side-by-side.
- **No third-party-registry browsing.** `list_skills` / `get_skill` do not require write access; wiring them to an arbitrary `owner/repo` would be a few lines.
- **No Windows installer.** `install.sh` is POSIX-only. The Go binary builds for `windows/amd64` but Windows users download and unzip it manually. A PowerShell `install.ps1` (with `gh.exe` added to `FindGH`) would close the gap.
- **No schema validation in `build_server()`.** A malformed SKILL.md makes `list_skills` skip it silently.
- **Legacy `skills-registry bootstrap` is dead weight.** Dropping it would let the wheel host only `skills-registry-mcp`.

Cross-links:

- [../security.md](../security.md) for the trust-anchor argument.
- [../deployment.md](../deployment.md) for the release pipeline that ships all of the above.
- [../systems/registry-client.md](../systems/registry-client.md) for the Git Data API call sequence and the conflict-retry logic.
- [../reference/configuration.md](../reference/configuration.md) for the env vars referenced in the install-cascade section (`SKILLS_SKIP_INSTALL`).
- [../reference/dependencies.md](../reference/dependencies.md) for the conscious-minimalism rationale on runtime deps.
