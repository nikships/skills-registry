import Foundation

/// Generates the CLI-only gateway skill installed in each agent folder.
public enum SkillMdTemplate {
    public static func skillMd(registryRepo: String) -> String {
        return """
---
name: skills-registry
description: |
  Access the GitHub-backed personal skill library at \(registryRepo). Use when the
  user asks for a skill, asks to install or share skills, says 'use the X skill',
  or needs specialized instructions that are not already loaded.
---

# Skill Registry

Skills live at https://github.com/\(registryRepo). Use the `skills-registry` CLI
to discover, fetch, publish, and remove them. Do not assume a skill is already
loaded: discover it, fetch it, then read its `SKILL.md` and any referenced files.

The CLI requires authenticated GitHub CLI access (`gh auth status`). Reads use
a shallow local Git mirror when available and fall back to `gh api`; normal
writes use `gh api`. SSH is not required.

## Install or update the CLI

If `skills-registry` is not on PATH:

```
curl -fsSL https://raw.githubusercontent.com/nikships/skills-registry/main/install.sh | sh
```

This installs `~/.local/bin/skills-registry`. Re-run it to upgrade.

## 1. Discover skills

Search for the top 10 fuzzy-ranked matches:

```
skills-registry search <query>
```

The search uses an fzf V1-style scorer across names, slugs, and descriptions.
Match the user's request against descriptions, not only slugs. A query is
required; use `skills-registry list` to enumerate the complete registry.
Both commands support `--json` for non-interactive use.

## 2. Fetch and read a skill

```
skills-registry get <slug> [--dest PATH]
```

`get` downloads the entire skill directory, including scripts, references,
assets, and resources. By default it writes to
`~/.cache/skills-registry/skills/<slug>/` (honoring `XDG_CACHE_HOME`); `--dest`
chooses another location.

After fetching:

1. Read the root `SKILL.md` first.
2. Inspect the complete directory tree.
3. Read referenced local files from the fetched directory instead of fetching
   them separately.
4. Follow the skill's instructions for when and how to use those resources.

Tell the user the returned path. Once the skill is loaded, offer to delete that
specific downloaded folder unless they want it for editing or offline use. Do
not use `skills-registry remove` for download cleanup: `remove` deletes the
registry copy and installed agent copies too.

## 3. Publish or update skills

- `skills-registry publish <path>` publishes one local skill folder.
- `skills-registry add <source>` discovers skills from a local path,
  `owner/repo`, git URL, or supported repository URL, then publishes selected
  skills.
- `skills-registry sync` scans AI-tool dot-folders and lets the user select
  local skills not yet in the registry.

The initial bootstrap uses git for an efficient bulk push; normal publish,
add, sync, and remove operations use the GitHub API.

## 4. Remove a skill

```
skills-registry remove <slug>
```

This removes the skill from the GitHub registry in an atomic commit, clears
`~/.cache/skills-registry/skills/<slug>/` and its metadata, and removes matching
copies from agent dot-folders. Interactive runs ask for confirmation. Use
`--yes` to skip it; `--json` implies confirmation.

## 5. Scripted workflows with `--json`

Every subcommand accepts the persistent `--json` flag, suppressing interactive
UI and emitting one JSON value to stdout. Errors are `{"error":"..."}` with a
non-zero exit code.

| Command | Payload shape |
|---|---|
| `skills-registry list --json` | `[{"slug":"...","name":"...","description":"..."}, …]` |
| `skills-registry search <query> --json` | same summary array, top 10 |
| `skills-registry get <slug> --json` | `{"slug":"...","path":"..."}` |
| `skills-registry publish <path> --json` | `{"slug":"...","sha":"...","url":"..."}` |
| `skills-registry sync --json` | `{"pushed":[...],"skipped":[...]}` |
| `skills-registry remove <slug> --json` | removal result including registry, cache, and dot-folder locations |

Use `jq` to compose commands, for example:

```
skills-registry search <query> --json | jq -r '.[].slug' | xargs -I{} skills-registry get {} --json
```

Use `list --json` instead of `search` when every slug is needed.

## Troubleshooting

- `skills-registry --help` lists all commands and flags.
- `gh auth status` verifies GitHub credentials.
- Check `~/.config/skills-registry/registry.toml` if list or search points to
  the wrong registry. `SKILLS_REGISTRY=owner/repo[@branch]` overrides the file.
- Ensure `~/.local/bin` is on PATH after installation.

"""
    }
}
