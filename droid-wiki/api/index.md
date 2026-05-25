# API

`skills-registry` exposes two user-facing surfaces. The MCP server is what an autonomous coding agent talks to; the CLI is what a human types in a terminal. Both wrap the same `registry.Client` contract against the same GitHub registry repo, and both honor the same config file at `~/.config/skills-mcp/registry.toml`.

## The two surfaces

The MCP server is a FastMCP stdio process spawned by desktop clients (Claude Desktop, Cursor, VS Code/Copilot). It registers three tools — `list_skills`, `get_skill`, `publish_skill` — and proxies every call through `gh api` so that no `git` binary or SSH agent is required at runtime. See [../apps/mcp-server.md](../apps/mcp-server.md) for the boot-time validation, exit codes, and tool registration mechanics.

The CLI is the Go binary dropped on disk by `install.sh`. A bare `skills-registry` invocation routes to the first-run wizard or the dashboard hub based on whether config exists; named subcommands give scripted callers the same operations the hub exposes. Every subcommand honors a persistent `--json` flag for machine-readable output. See [../apps/cli/subcommands.md](../apps/cli/subcommands.md) for the implementation deep dive.

## Pages in this section

| Page | What it covers |
| --- | --- |
| [MCP tools](mcp-tools.md) | The three FastMCP tools: signatures, argument schemas, return shapes, side effects, annotation profiles, and the cache flow for `get_skill`. |
| [CLI commands](cli-commands.md) | The seven subcommands: usage lines, flag tables, `--json` payload shapes, behaviour summaries, and the `shouldAutoYes()` rule that controls destructive-action auto-confirmation. |

## Quick map

```
MCP agent  ─────►  list_skills        ─┐
                   get_skill           │
                   publish_skill       │
                                       ├──►  RegistryClient (Python)
Terminal   ─────►  skills-registry list ─┤            │
                   skills-registry get   │            ▼
                   skills-registry sync  │      gh api → GitHub
                   skills-registry add   │
                   skills-registry pub   │
                   skills-registry rm    │
                   skills-registry boot ─┘
```

The two surfaces are intentionally symmetric: anything you can do interactively from the CLI, an agent can do from the MCP server (modulo `add` and `sync`, which are CLI-only because they walk the local filesystem). Read names and slug rules are identical on both sides — see [../primitives/skill.md](../primitives/skill.md) for the slugify contract every surface shares.

## What's not here

- **The bootstrap push path.** `PushTreeViaGit` is a CLI-only fast path for first-time bulk imports; it isn't exposed as an MCP tool. See [../systems/bootstrap-push.md](../systems/bootstrap-push.md).
- **The dashboard hub and onboarding wizard.** These are CLI flows that compose the same handlers the subcommands use. See [../apps/cli/wizard-and-hub.md](../apps/cli/wizard-and-hub.md).
- **Config resolution.** The `SKILLS_REGISTRY` env var and the TOML file format are documented under [../reference/configuration.md](../reference/configuration.md); the in-memory shape is documented in [../primitives/registry-config.md](../primitives/registry-config.md).
