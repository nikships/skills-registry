# Apps

Active contributors: Nik Anand

## What's here

`skills-registry` ships three first-party deliverables out of one repository, plus a marketing site that mirrors the README. Each piece has its own scope and its own deep-dive page.

| App | Language | Distribution | What it owns |
| --- | --- | --- | --- |
| [Installer (`install.sh`)](installer.md) | POSIX `sh` | `curl … \| sh` from the GitHub raw URL | OS/arch detection, tarball download, drop the binary into `~/.local/bin` |
| [CLI (`skills-registry`)](cli/index.md) | Go 1.24+ | GitHub Releases tarballs (one per OS/arch) | Cobra root, persistent `--json`, bare-command routing into wizard/hub/help, every subcommand |
| [MCP server (`skills-registry-mcp`)](mcp-server.md) | Python 3.10+ | PyPI (`uv tool install` / `pipx install` / `pip install --user`) | FastMCP stdio server with `list_skills`, `get_skill`, `publish_skill` |

The installer is the only one-shot `curl … | sh` surface. It drops the Go binary, then the Go wizard installs the Python MCP server in the background — the user never sees Python during onboarding.

## Auxiliary

| Site | Stack | Where |
| --- | --- | --- |
| [Marketing site](website.md) | Next.js 16 + React 19 + Tailwind 4 (Bun) | `website/`, deployed to Firebase Hosting |

The site is auxiliary. Architectural docs live in this wiki and in `docs/registry.md`; the website is a single-page mirror of the README pitch.

## How the pieces talk

- The installer downloads the Go binary and exits. It does not invoke the Python wheel.
- The Go CLI is the only interactive surface. The wizard installs `skills-registry-mcp` via `uv` → `pipx` → `pip` after the registry has been bootstrapped on GitHub.
- The MCP server runs as a stdio subprocess spawned by desktop clients (Claude Desktop, Cursor, VS Code/Copilot). It validates auth + config at boot and never invokes `git` or `ssh`.
- Both the Go CLI and the Python server speak to GitHub through the user's authenticated `gh` CLI. The CLI bootstrap uses a single `git push` for the bulk import; everything else routes through `gh api`. See [systems/registry-client](../systems/registry-client.md) and [systems/bootstrap-push](../systems/bootstrap-push.md).

## Cross-references

- [overview/architecture](../overview/architecture.md) — how the three deliverables fit together.
- [overview/getting-started](../overview/getting-started.md) — install + daily use.
- [systems/json-output](../systems/json-output.md) — the persistent `--json` contract honored by every CLI subcommand.
- [api/cli-commands](../api/cli-commands.md) — every CLI flag and JSON payload shape.
- [api/mcp-tools](../api/mcp-tools.md) — the three MCP tools and their annotations.
