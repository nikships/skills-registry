// Package bootstrap orchestrates the one-time setup flow (gh check, repo
// create, agent multi-select) and the supporting helpers (SKILL.md
// rendering, dot-folder install).
package bootstrap

import "fmt"

// SkillMd returns the body of the generated skill-registry/SKILL.md.
func SkillMd(registryRepo string) string {
	return fmt.Sprintf(skillMdTemplate, registryRepo, registryRepo)
}

const skillMdTemplate = `---
name: skill-registry
description: |
  Broker to your GitHub-hosted personal skill library at %s. Use when the
  user asks for a skill, mentions installing/sharing skills, says 'use the
  X skill', or you need specialized domain instructions not already loaded
  in this session.
---

# Skill Registry

Skills live at https://github.com/%s and can be reached two ways:

1. **MCP (preferred when available).** If this agent's client is wired to
   the hosted MCP server at ` + "`https://mcp.skills-registry.dev/mcp`" + `, you
   already have the ` + "`list_skills`" + ` and ` + "`get_skill`" + ` tools.
   Use them — they're faster and don't require a CLI binary.

2. **CLI (fallback / write-side).** When MCP isn't available, or for write
   operations (publish / sync / remove), shell out to the ` + "`skill-registry`" + `
   binary. Requires the ` + "`gh`" + ` CLI to be authenticated
   (` + "`gh auth status`" + `); all I/O routes through ` + "`gh api`" + ` — no
   ` + "`git`" + ` or SSH needed for day-to-day commands.

**Do not assume any skill is already loaded.** Always discover, fetch,
then read the skill's ` + "`SKILL.md`" + `; it tells you what else to load
and when.

## Install the CLI (one-time, only if the binary isn't on PATH)

` + "```" + `
curl -fsSL https://raw.githubusercontent.com/anand-92/skills-registry/main/install.sh | sh
` + "```" + `

Drops the binary into ` + "`~/.local/bin/skill-registry`" + `. Re-run any time
to upgrade.

## 1. Discover what's available

MCP: call ` + "`list_skills`" + `.

CLI:
` + "```" + `
skill-registry list
` + "```" + `

Match the user's request against descriptions, not just slugs.

## 2. Fetch the skill

MCP: call ` + "`get_skill(slug=\"<slug>\")`" + ` — returns the raw ` + "`SKILL.md`" + ` body.

CLI:
` + "```" + `
skill-registry get <slug> [--dest PATH]
` + "```" + `

The CLI fetches the **entire upstream directory tree** for the skill and
writes it to a local folder. The returned path is a complete, self-contained
skill package — every file and subfolder belonging to the skill is present
on disk.

**After fetching, always inspect the folder contents:**
- Read ` + "`SKILL.md`" + ` at the root first.
- Check for common subfolders: ` + "`references/`" + `, ` + "`scripts/`" + `, ` + "`assets/`" + `, ` + "`resources/`" + `, etc.
- Follow local file references in ` + "`SKILL.md`" + ` by reading the already-fetched
  files at the returned path — do not re-fetch individual referenced files.

By default the CLI writes to ` + "`./.agents/skills/<slug>/`" + ` under the
current working directory (or the path you pass via ` + "`--dest`" + `).

**After reading the skill, offer cleanup.** The fetched skill files are now
in your agent context — the on-disk copy is only needed if you plan to edit
the skill itself or if the user wants to keep it for offline use. Always
inform the user where the skill was installed (the ` + "`path`" + ` returned
by ` + "`get`" + `) and ask:

> "The ` + "`<slug>`" + ` skill has been fetched to ` + "`<path>`" + `. It's
> already loaded into my context, so I don't need the files on disk anymore.
> Would you like me to delete the local copy now?"

If the user says yes, delete the folder at the returned path. Do **not** use
` + "`skill-registry remove`" + ` for this — that command deletes the skill
from the public registry, the cache, and every agent dot-folder. For simple
local cleanup, just remove the specific folder the ` + "`get`" + ` command
returned.

## 3. Publish a new or updated skill (CLI only; hosted MCP is read-only)

- ` + "`skill-registry publish <path>`" + ` — single-skill push from a local folder
- ` + "`skill-registry add <source>`" + ` — pull from a path, ` + "`owner/repo`" + `, or git URL,
  then push selections to the registry
- ` + "`skill-registry sync`" + ` — scan your AI tool dot-folders for skills not yet in
  the registry; multi-select what to push

## 4. Remove a skill (CLI only; hosted MCP is read-only)

` + "```" + `
skill-registry remove <slug>
` + "```" + `

Deletes the slug end-to-end: from the GitHub registry repo (single
atomic commit), the local cache (` + "`~/.cache/skills-mcp/skills/<slug>/`" + `),
and every agent dot-folder copy. Interactive runs prompt for confirmation;
pass ` + "`--yes`" + ` (or ` + "`--json`" + `, which implies it) to skip the prompt.

## 5. Programmatic / scripted use — ` + "`--json`" + `

Every CLI subcommand accepts a persistent ` + "`--json`" + ` flag that
suppresses the TUI and emits a single JSON payload to stdout. Errors land
as ` + "`{\"error\": \"...\"}`" + ` with a non-zero exit code. This is the
entry point when you (the agent) are driving the CLI yourself rather than
letting a human pick from a list.

| Command | Payload shape |
|---|---|
| ` + "`skill-registry list --json`" + ` | ` + "`[{\"slug\": \"...\", \"name\": \"...\", \"description\": \"...\"}, …]`" + ` |
| ` + "`skill-registry get <slug> --json`" + ` | ` + "`{\"slug\": \"...\", \"path\": \"...\"}`" + ` (path is the on-disk dest) |
| ` + "`skill-registry publish <path> --json`" + ` | ` + "`{\"slug\": \"...\", \"sha\": \"...\", \"url\": \"...\"}`" + ` |
| ` + "`skill-registry sync --json`" + ` | ` + "`{\"pushed\": [...slugs], \"skipped\": [...slugs]}`" + ` |
| ` + "`skill-registry remove <slug> --json`" + ` | ` + "`{\"slug\": \"...\", \"repo\": \"...\", \"sha\": \"...\", \"removed_from\": [\"registry\", \"cache\", \"dotfolders\"]}`" + ` |

` + "`--json`" + ` always implies ` + "`--yes`" + ` on destructive commands
(` + "`sync`" + `, ` + "`remove`" + `): JSON callers never get a Bubble Tea
prompt. Combine with ` + "`jq`" + ` to chain calls — e.g.
` + "`skill-registry list --json | jq -r '.[].slug' | xargs -I{} skill-registry get {} --json`" + `.

## Troubleshooting

- ` + "`skill-registry --help`" + ` — full command list and flags
- ` + "`gh auth status`" + ` — confirm GitHub credentials are present
- If ` + "`skill-registry list`" + ` errors, check the config at
  ` + "`~/.config/skills-mcp/registry.toml`" + ` points at the right ` + "`owner/repo`" + `
- If MCP tools (` + "`list_skills`" + ` / ` + "`get_skill`" + `) say "no repo
  linked yet", install the Skills Registry GitHub App on your registry repo
  via the link the server prints, then retry — the webhook auto-links
  within a few seconds.
`
