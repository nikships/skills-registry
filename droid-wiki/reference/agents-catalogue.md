# Agents catalogue

Active contributors: Nik Anand

The 56-entry catalogue of known AI tool dot-folders the wizard's multi-select offers when copying `skills-registry/SKILL.md` into your environment. Source of truth: `cli/internal/agents/agents.go`.

## Schema

Each entry has four fields:

- **DotDir** — the literal directory name on disk (e.g. `.claude`).
- **Display** — the human label shown in the multi-select.
- **Universal** — when `true`, the entry is locked-on at the top of the list and cannot be unchecked. Only `.agents/` carries this flag; it is the convention shared across multiple agents for project-local skill discovery.
- **UnderHome** — when `true`, the directory is resolved relative to the user's home (`~/.claude`). When `false`, it is resolved relative to the current project working directory (used by `.agents/` for project-local skills).
- **Default-checked** — pre-ticked when the multi-select opens. Default-checked entries: Claude Code, Factory, Cursor, Codex CLI. Everything else starts unchecked.

`Universal` status applies only to `.agents/`. Calling another entry "universal" in conversation is fine — what matters in code is whether the entry has `Universal=true`, which forces the multi-select to lock it.

## The 56 entries

| DotDir | Display | Universal | UnderHome | Default-checked |
| --- | --- | --- | --- | --- |
| `.agents` | Universal (.agents/skills) | yes | no | yes (locked) |
| `.claude` | Claude Code | no | yes | yes |
| `.claude-code` | Claude Code legacy | no | yes | no |
| `.factory` | Factory | no | yes | yes |
| `.codex` | Codex CLI | no | yes | yes |
| `.cursor` | Cursor | no | yes | yes |
| `.junie` | Junie | no | yes | no |
| `.aider` | Aider | no | yes | no |
| `.continue` | Continue | no | yes | no |
| `.windsurf` | Windsurf | no | yes | no |
| `.codeium` | Codeium | no | yes | no |
| `.zed` | Zed | no | yes | no |
| `.anthropic` | Anthropic | no | yes | no |
| `.openai` | OpenAI | no | yes | no |
| `.cline` | Cline | no | yes | no |
| `.roo` | Roo | no | yes | no |
| `.roocode` | Roo Code | no | yes | no |
| `.gemini` | Gemini | no | yes | no |
| `.antigravity` | Antigravity | no | yes | no |
| `.aider-desk` | Aider Desk | no | yes | no |
| `.augment` | Augment | no | yes | no |
| `.bob` | Bob | no | yes | no |
| `.codeartsdoer` | CodeArts Doer | no | yes | no |
| `.codebuddy` | CodeBuddy | no | yes | no |
| `.codemaker` | CodeMaker | no | yes | no |
| `.codestudio` | Code Studio | no | yes | no |
| `.commandcode` | Command Code | no | yes | no |
| `.copilot` | GitHub Copilot | no | yes | no |
| `.cortex` | Cortex | no | yes | no |
| `.crush` | Crush | no | yes | no |
| `.deepagents` | DeepAgents | no | yes | no |
| `.devin` | Devin | no | yes | no |
| `.firebender` | Firebender | no | yes | no |
| `.forge` | Forge | no | yes | no |
| `.goose` | Goose | no | yes | no |
| `.iflow` | iFlow | no | yes | no |
| `.kilocode` | Kilo Code | no | yes | no |
| `.kiro` | Kiro | no | yes | no |
| `.kode` | Kode | no | yes | no |
| `.mcpjam` | MCPJam | no | yes | no |
| `.mux` | Mux | no | yes | no |
| `.opencode` | OpenCode | no | yes | no |
| `.openhands` | OpenHands | no | yes | no |
| `.pi` | Pi | no | yes | no |
| `.qoder` | Qoder | no | yes | no |
| `.qwen` | Qwen Code | no | yes | no |
| `.rovodev` | Rovo Dev | no | yes | no |
| `.tabnine` | Tabnine | no | yes | no |
| `.trae` | Trae | no | yes | no |
| `.trae-cn` | Trae CN | no | yes | no |
| `.vibe` | Vibe | no | yes | no |
| `.zencoder` | Zencoder | no | yes | no |
| `.neovate` | Neovate | no | yes | no |
| `.pochi` | Pochi | no | yes | no |
| `.adal` | Adal | no | yes | no |
| `.snowflake` | Snowflake | no | yes | no |

55 home-directory agents plus the one project-local universal entry = 56 total.

## How the catalogue is consumed

Three call sites in the Go binary read this list:

- **`cli/cmd/skills-registry/wizard.go`** — step 5 of the first-run wizard. Discovers which of these directories exist locally, opens the multi-select pre-populated with that subset, and writes `skills-registry/SKILL.md` (from `cli/internal/bootstrap/skillmd.go`) into each picked dot-folder.
- **`cli/cmd/skills-registry/hub.go`** — the Add card on the dashboard hub. Same flow as the wizard's step 5, run on demand.
- **`cli/internal/scan/scan.go`** — `scan.Discover` walks the catalogue when looking for existing local skills to import. The bootstrap path uses this to seed the initial registry repo.

The Python side does not need the catalogue. The legacy `gather` command was the only Python consumer and was removed in 0.3.0.

## Adding a new agent

Edit `cli/internal/agents/agents.go`. New entries default to `Universal=false`, `UnderHome=true`. Only flip `Universal=true` if the new directory is genuinely a multi-agent convention; today `.agents/` is the only such case. The multi-select preserves catalogue order, so add new entries at the bottom unless there is a reason to interleave them.

Tests in `cli/internal/agents/agents_test.go` enforce uniqueness of `DotDir` and `Display`. If you add a duplicate the build will fail.
