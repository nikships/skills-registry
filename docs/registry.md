# Skills Registry architecture

Skills Registry has four user-facing surfaces: release installers and the npm launcher, the Go CLI, the native macOS app, and the static website. Skills remain in a GitHub repository owned by the user; no additional registry service is required.

## Distribution

| Surface | Source | Distribution |
|---|---|---|
| Shell installers | `install.sh`, `install.ps1` | Select and install a GitHub Release binary. |
| npm launcher | `npm/` | `npx skills-registry`; downloads and executes the matching release binary. |
| Go CLI | `cli/` | Release binaries for macOS, Linux, and Windows on amd64 and arm64. |
| macOS app | `mac-app/` | Signed and notarized native SwiftUI app. |
| Website | `website/` | Static Next.js landing site. |

Native macOS CI and Darwin CLI releases use the dedicated Aqua-session self-hosted runner labeled `mac-mini`. Linux, Windows, and untrusted fork jobs remain on GitHub-hosted runners. See [`.github/AGENTS.md`](../.github/AGENTS.md).

## CLI flow

Bare `skills-registry` opens the onboarding wizard when config is absent, the dashboard when config exists, and help for non-interactive or `--json` invocation.

The onboarding wizard has seven steps:

1. Scan known agent skill directories.
2. Choose the GitHub repository name.
3. Choose repository visibility.
4. Create and populate the repository with one authenticated `git push`.
5. Select agents and install their generated gateway skill.
6. Optionally remove redundant local skill copies.
7. Show the resulting registry URL and completion summary.

The headless `skills-registry bootstrap` command performs the same setup and ends by printing the registry URL. Neither flow emits configuration for another protocol or service.

The bulk initial import uses `git push` over HTTPS with credentials configured by `gh auth setup-git`. Day-to-day `publish`, `add`, `sync`, and `remove` operations use the GitHub Git Data API through the authenticated `gh` CLI. Reads use a shallow local mirror when available and fall back to `gh api`.

Every subcommand supports `--json`. The primary commands are `bootstrap`, `list`, `search`, `get`, `sync`, `add`, `publish`, `remove`, and `update`.

## Configuration and cache

Configuration resolution is:

1. `SKILLS_REGISTRY=owner/repo[@branch]` for a process-local override.
2. `~/.config/skills-registry/registry.toml`, or `$XDG_CONFIG_HOME/skills-registry/registry.toml`.

Downloaded skills, metadata, and repository mirrors live under `~/.cache/skills-registry/`, or `$XDG_CACHE_HOME/skills-registry/`.

```toml
[registry]
repo = "alice/skills-registry"
default_branch = "main"
```

## Gateway skill

Bootstrap writes `<agent-dir>/skills/skills-registry/SKILL.md` for each selected agent. This generated gateway is CLI-only: it tells agents to use `skills-registry search`, `get`, and the other JSON-capable commands to discover and manage skills on demand.

The agent catalogue remains centralized in `cli/internal/agents/agents.go`. `.mcpjam/` is retained as the supported MCPJam agent directory; that directory name is agent compatibility, not a service dependency.

## macOS app

The app manages the same registry and configuration as the CLI. It supports GitHub device-flow login, browsing and fuzzy search, publish/remove, bulk import, and CLI installation. Shared slug, frontmatter, fuzzy-scoring, GitHub-write, and gateway-template contracts must remain aligned between Go and Swift.

## Verification

CLI changes should pass `go vet`, `gofmt`, `staticcheck`, `deadcode`, `gocyclo`, build, and tests. macOS changes use Swift build and test jobs. Release automation builds CLI archives, signs and notarizes Darwin binaries, creates a GitHub release, and then publishes the thin npm wrapper when credentials are available.
