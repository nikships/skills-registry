# Repository guidance

`skills-registry` has four user-facing surfaces:

| Surface | Distribution | Role |
|---|---|---|
| Installers and npm wrapper | `install.sh`, `install.ps1`, and `npx skills-registry` | Download and launch the matching release binary. |
| Go CLI | GitHub Release binaries | Charmbracelet TUI and headless registry commands. |
| macOS app | Signed and notarized SwiftUI app | Native GUI for the same registry and configuration. |
| Website | Static Next.js site | Project landing page and product documentation. |

## Layout and contracts

- `cli/` is the Go module. Bare invocation opens the seven-step onboarding wizard when configuration is missing and the dashboard otherwise. Headless commands are `bootstrap`, `list`, `search`, `get`, `sync`, `add`, `publish`, `remove`, and `update`; all honor `--json`.
- The wizard scans local skills, chooses a repository and visibility, performs the bulk push, installs the gateway skill, optionally cleans up local copies, and finishes with the registry URL. `bootstrap` likewise ends with the registry URL and does not print client configuration.
- The generated `skills-registry/SKILL.md` gateway is CLI-only: agents use `skills-registry search`, `get`, and other commands rather than a separate service.
- Configuration lives at `~/.config/skills-registry/registry.toml` (or `$XDG_CONFIG_HOME/skills-registry/registry.toml`). Cache and mirrors live under `~/.cache/skills-registry/` (or `$XDG_CACHE_HOME/skills-registry/`).
- `mac-app/` is the Swift package and app. Keep shared slug, frontmatter, fuzzy-search, GitHub-write, configuration, and gateway-template behavior aligned with the CLI.
- `npm/` is a thin launcher; it must not commit or bundle release binaries.
- `website/` is the static project site.
- `.mcpjam/` is a supported agent directory. Keep MCPJam agent discovery support even though the project does not ship an MCP service.

## Build and test

From `cli/` run:

```bash
go vet ./...
gofmt -l .
staticcheck ./...
deadcode -test ./...
gocyclo -over 15 -ignore "_test" .
go test ./...
```

Go naming follows standard conventions: short lowercase packages, `PascalCase` exports, `camelCase` unexported names, and preserved initialisms such as `URL`, `SHA`, and `ID`.

Native macOS jobs and Darwin release builds use the dedicated Aqua-session self-hosted runner labeled `mac-mini`; Linux, Windows, and untrusted fork jobs remain on GitHub-hosted runners. Follow `.github/AGENTS.md` for runner and signing operations.

When changing user-visible behavior, update `README.md` and `docs/registry.md`. Add or update focused tests, avoid new mandatory runtime dependencies without justification, and use conventional-commit prefixes (`feat:`, `fix:`, `docs:`, `refactor:`, `test:`, `chore:`).
