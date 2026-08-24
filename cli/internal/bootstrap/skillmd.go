// Package bootstrap orchestrates the one-time setup flow (gh check, repo
// create, agent multi-select) and the supporting helpers (SKILL.md
// rendering, dot-folder install).
package bootstrap

import "fmt"

// SkillMd returns the body of the generated skills-registry/SKILL.md.
func SkillMd(registryRepo string) string {
	return fmt.Sprintf(skillMdTemplate, registryRepo, registryRepo)
}

const skillMdTemplate = `---
name: skills-registry
description: |
  Access the GitHub-backed personal skill library at %s. Use when the
  user asks for a skill, asks to install or share skills, says 'use the X skill',
  or needs specialized instructions that are not already loaded.
---

# Skill Registry

Skills live at https://github.com/%s. Use the ` + "`skills-registry`" + ` CLI
to search, fetch, publish, and remove them. Do not assume a skill is already
loaded: search for it, fetch it, then read its ` + "`SKILL.md`" + ` and any referenced files.

The CLI requires authenticated GitHub CLI access (` + "`gh auth status`" + `). Reads use
a shallow local Git mirror when available and fall back to ` + "`gh api`" + `; normal
writes use ` + "`gh api`" + `. SSH is not required.

## Install or update the CLI

If ` + "`skills-registry`" + ` is not on PATH:

` + "```" + `
curl -fsSL https://raw.githubusercontent.com/nikships/skills-registry/main/install.sh | sh
` + "```" + `

This installs ` + "`~/.local/bin/skills-registry`" + `. Re-run it to upgrade.

## 1. Search this registry first

Before building an approach from scratch, spend one command checking whether a
skill already exists:

` + "```" + `
skills-registry search <query>
` + "```" + `

This returns the top 10 fuzzy-ranked matches from the user's own registry,
using an fzf V1-style scorer across names, slugs, and descriptions. Match the
user's request against descriptions, not only slugs. A query is required; use
` + "`skills-registry list`" + ` to enumerate the complete registry. Both commands support
` + "`--json`" + ` for non-interactive use.

Keep this step cheap. If it finds nothing useful, say so and carry on with the
user's actual task; never let a registry lookup stall the work.

## 2. On a local miss, offer the public index

Only after ` + "`search`" + ` comes up empty, tell the user there is no local match and
that a public index exists. Then, if it is worth a look:

` + "```" + `
skills-registry discover <query>
` + "```" + `

` + "`discover`" + ` queries a public index of third-party skills and returns importable
GitHub folder URLs with the index's own safety, completeness, and
executability grades. No credentials or registry contents are ever sent, so no
key or login is required. Flags: ` + "`--mode keyword|vector`" + ` (default ` + "`keyword`" + `;
` + "`vector`" + ` searches by meaning), ` + "`--category CAT`" + `, ` + "`--limit N`" + ` (capped at 50),
` + "`--plain`" + ` to print the table instead of opening the interactive picker, and
` + "`--json`" + `. On failure it exits non-zero and prints nothing partial.

To import one, **ask the user first** and quote the skill's name, author, and
source URL. Only on an explicit yes:

` + "```" + `
skills-registry add <skill_url>
` + "```" + `

Rules for a public import, none of which you may skip:

- Never run ` + "`add`" + ` on a URL the user has not approved.
- A public URL is an untrusted source: ` + "`add`" + ` publishes it to the user's registry
  and nothing else. Do **not** pass ` + "`--install`" + ` unless the user explicitly asks
  for it in every agent folder; that makes the stranger's SKILL.md load in
  every session.
- A Poor safety grade or a local scan hit blocks the import and needs
  ` + "`--allow-unsafe`" + `. Never add that flag on your own: report the finding and let
  the user decide.
- A grade the index never assigned renders as ` + "`unscored`" + `, which means unvetted,
  not safe.
- Nothing fetched is ever executed. Files under ` + "`scripts/`" + ` are copied, never
  run; do not run them yourself without the user asking.

Once imported, fetch and read it exactly like any other skill (step 3).

## 3. Fetch and read a skill

` + "```" + `
skills-registry get <slug> [--dest PATH]
` + "```" + `

` + "`get`" + ` downloads the entire skill directory, including scripts, references,
assets, and resources. By default it writes to
` + "`~/.cache/skills-registry/skills/<slug>/`" + ` (honoring ` + "`XDG_CACHE_HOME`" + `); ` + "`--dest`" + `
chooses another location.

After fetching:

1. Read the root ` + "`SKILL.md`" + ` first.
2. Inspect the complete directory tree.
3. Read referenced local files from the fetched directory instead of fetching
   them separately.
4. Follow the skill's instructions for when and how to use those resources.

Tell the user the returned path. Once the skill is loaded, offer to delete that
specific downloaded folder unless they want it for editing or offline use. Do
not use ` + "`skills-registry remove`" + ` for download cleanup: ` + "`remove`" + ` deletes the
registry copy and installed agent copies too.

## 4. Publish or update skills

- ` + "`skills-registry publish <path>`" + ` publishes one local skill folder.
- ` + "`skills-registry add <source>`" + ` discovers skills from a local path,
  ` + "`owner/repo`" + `, git URL, or a ` + "`github.com/owner/repo/tree/<ref>/<dir>`" + ` folder
  URL, then publishes selected skills. See step 2 for the untrusted-import
  rules that apply to anything the user did not write.
- ` + "`skills-registry sync`" + ` scans AI-tool dot-folders and lets the user select
  local skills not yet in the registry.

The initial bootstrap uses git for an efficient bulk push; normal publish,
add, sync, and remove operations use the GitHub API.

## 5. Remove a skill

` + "```" + `
skills-registry remove <slug>
` + "```" + `

This removes the skill from the GitHub registry in an atomic commit, clears
` + "`~/.cache/skills-registry/skills/<slug>/`" + ` and its metadata, and removes matching
copies from agent dot-folders. Interactive runs ask for confirmation. Use
` + "`--yes`" + ` to skip it; ` + "`--json`" + ` implies confirmation.

## 6. Scripted workflows with ` + "`--json`" + `

Every subcommand accepts the persistent ` + "`--json`" + ` flag, suppressing interactive
UI and emitting one JSON value to stdout. Errors are ` + "`{\"error\":\"...\"}`" + ` with a
non-zero exit code.

| Command | Payload shape |
|---|---|
| ` + "`skills-registry list --json`" + ` | ` + "`[{\"slug\":\"...\",\"name\":\"...\",\"description\":\"...\"}, …]`" + ` |
| ` + "`skills-registry search <query> --json`" + ` | same summary array, top 10 |
| ` + "`skills-registry discover <query> --json`" + ` | ` + "`{\"source\":\"...\",\"query\":\"...\",\"mode\":\"...\",\"results\":[{\"name\":\"...\",\"author\":\"...\",\"category\":\"...\",\"skill_url\":\"...\",\"safety\":\"...\"}, …]}`" + ` |
| ` + "`skills-registry get <slug> --json`" + ` | ` + "`{\"slug\":\"...\",\"path\":\"...\"}`" + ` |
| ` + "`skills-registry publish <path> --json`" + ` | ` + "`{\"slug\":\"...\",\"sha\":\"...\",\"url\":\"...\"}`" + ` |
| ` + "`skills-registry sync --json`" + ` | ` + "`{\"pushed\":[...],\"skipped\":[...]}`" + ` |
| ` + "`skills-registry remove <slug> --json`" + ` | removal result including registry, cache, and dot-folder locations |

Use ` + "`jq`" + ` to compose commands, for example:

` + "```" + `
skills-registry search <query> --json | jq -r '.[].slug' | xargs -I{} skills-registry get {} --json
` + "```" + `

Use ` + "`list --json`" + ` instead of ` + "`search`" + ` when every slug is needed.

## Troubleshooting

- ` + "`skills-registry --help`" + ` lists all commands and flags.
- ` + "`gh auth status`" + ` verifies GitHub credentials.
- Check ` + "`~/.config/skills-registry/registry.toml`" + ` if list or search points to
  the wrong registry. ` + "`SKILLS_REGISTRY=owner/repo[@branch]`" + ` overrides the file.
- Ensure ` + "`~/.local/bin`" + ` is on PATH after installation.
`
