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
  Broker to your GitHub-hosted personal skill library at %s via the
  ` + "`skill-registry`" + ` CLI. Use when the user asks for a skill, mentions
  installing/sharing skills, says 'use the X skill', or you need specialized
  domain instructions not already loaded in this session.
---

# Skill Registry (CLI)

Skills live at https://github.com/%s and are fetched on demand by shelling
out to the ` + "`skill-registry`" + ` binary. **Do not assume any skill is already
loaded** — always discover, fetch, then read the skill's ` + "`SKILL.md`" + `; it
tells you what else to load and when.

Requires the ` + "`gh`" + ` CLI to be authenticated (` + "`gh auth status`" + `). All registry
I/O routes through ` + "`gh api`" + `; no ` + "`git`" + ` or SSH is needed.

## Install the CLI

If the ` + "`skill-registry`" + ` binary isn't already on PATH, install it with the
one-line curl|sh installer (POSIX):

` + "```" + `
curl -fsSL https://raw.githubusercontent.com/anand-92/skills-registry/main/install.sh | sh
` + "```" + `

This drops the binary into ` + "`~/.local/bin/skill-registry`" + `. Re-run any time
to upgrade; the installer downloads the matching release for your
OS/arch from GitHub Releases.

## 1. Discover what's available

` + "```" + `
skill-registry list
` + "```" + `

Prints a table of slug, name, and one-line description. Match the user's
request against descriptions, not just slugs.

## 2. Fetch the skill

` + "```" + `
skill-registry get <slug> [--dest PATH]
` + "```" + `

Prints the absolute path to a local folder containing the skill's
` + "`SKILL.md`" + ` plus any supporting files (` + "`scripts/`" + `, ` + "`assets/`" + `, ` + "`resources/`" + `, …).
**Read the ` + "`SKILL.md`" + ` at the root first** — it tells you which supporting
files to load and when. Cached at ` + "`~/.cache/skills-mcp/skills/<slug>/`" + ` and
refreshed automatically when the upstream tree changes.

## 3. Publish a new or updated skill

- ` + "`skill-registry publish <path>`" + ` — single-skill push from a local folder
- ` + "`skill-registry add <source>`" + ` — pull from a path, ` + "`owner/repo`" + `, or git URL,
  then push selections to the registry
- ` + "`skill-registry sync`" + ` — scan your AI tool dot-folders for skills not yet in
  the registry; multi-select what to push

## 4. Remove a skill

` + "```" + `
skill-registry remove <slug>
` + "```" + `

Deletes the slug end-to-end: from the GitHub registry repo (single
atomic commit), the local cache (` + "`~/.cache/skills-mcp/skills/<slug>/`" + `),
and every agent dot-folder copy. Interactive runs prompt for confirmation;
pass ` + "`--yes`" + ` (or ` + "`--json`" + `, which implies it) to skip the prompt.

## 5. Programmatic / scripted use — ` + "`--json`" + `

Every subcommand accepts a persistent ` + "`--json`" + ` flag that suppresses the
TUI and emits a single JSON payload to stdout. Errors land as
` + "`{\"error\": \"...\"}`" + ` with a non-zero exit code. This is the entry point
when you (the agent) are driving the CLI yourself rather than letting a
human pick from a list.

| Command | Payload shape |
|---|---|
| ` + "`skill-registry list --json`" + ` | ` + "`[{\"slug\", \"name\", \"description\"}, …]`" + ` |
| ` + "`skill-registry get <slug> --json`" + ` | ` + "`{\"slug\", \"path\"}`" + ` (path is the on-disk dest) |
| ` + "`skill-registry publish <path> --json`" + ` | ` + "`{\"slug\", \"sha\", \"url\"}`" + ` |
| ` + "`skill-registry sync --json`" + ` | ` + "`{\"pushed\": [...slugs], \"skipped\": [...slugs]}`" + ` |
| ` + "`skill-registry remove <slug> --json`" + ` | ` + "`{\"slug\", \"repo\", \"sha\", \"removed_from\": [\"registry\", \"cache\", \"dotfolders\"]}`" + ` |

` + "`--json`" + ` always implies ` + "`--yes`" + ` on destructive commands (` + "`sync`" + `, ` + "`remove`" + `):
JSON callers never get a Bubble Tea prompt. Combine with ` + "`jq`" + ` to chain
calls — e.g. ` + "`skill-registry list --json | jq -r '.[].slug' | xargs -I{} skill-registry get {} --json`" + `.

## Troubleshooting

- ` + "`skill-registry --help`" + ` — full command list and flags
- ` + "`gh auth status`" + ` — confirm GitHub credentials are present
- If ` + "`skill-registry list`" + ` errors, check the config at
  ` + "`~/.config/skills-mcp/registry.toml`" + ` points at the right ` + "`owner/repo`" + `
`
