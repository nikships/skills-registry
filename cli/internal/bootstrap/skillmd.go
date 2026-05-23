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
loaded** — always discover, then fetch, then read every file before acting.

Requires the ` + "`gh`" + ` CLI to be authenticated (` + "`gh auth status`" + `). All registry
I/O routes through ` + "`gh api`" + `; no ` + "`git`" + ` or SSH is needed.

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
` + "`SKILL.md`" + ` and every supporting file. **Read every file in that folder**
before acting on the skill. Cached at ` + "`~/.cache/skills-mcp/skills/<slug>/`" + `
and refreshed automatically when the upstream tree changes.

## 3. Publish a new or updated skill

- ` + "`skill-registry publish <path>`" + ` — single-skill push from a local folder
- ` + "`skill-registry add <source>`" + ` — pull from a path, ` + "`owner/repo`" + `, or git URL,
  then push selections to the registry
- ` + "`skill-registry sync`" + ` — scan your AI tool dot-folders for skills not yet in
  the registry; multi-select what to push

## Troubleshooting

- ` + "`skill-registry --help`" + ` — full command list and flags
- ` + "`gh auth status`" + ` — confirm GitHub credentials are present
- If ` + "`skill-registry list`" + ` errors, check the config at
  ` + "`~/.config/skills-mcp/registry.toml`" + ` points at the right ` + "`owner/repo`" + `
`
