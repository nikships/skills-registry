# Lore

The `skills-registry` project is three days old at the time of this wiki generation, but the codebase has already lived through a complete architectural pivot. This page walks the timeline.

## Eras

### The skills-mcp era — 2026-05-21

Day 1. The project shipped publicly as `skills-mcp`: a CLI that **consolidated local AI skills** from scattered tool dot-folders (`.claude/skills/`, `.cursor/skills/`, etc.) into a single shared directory, then served that directory through a FastMCP server.

The headline commands were `gather` (walk known dot-folders, copy skills into one place) and `add` (install a skill from a git URL). The MCP server exposed a single tool, `show_skills`, backed by a `SkillsDirectoryProvider` that read the consolidated folder off disk.

About 16 commits landed: initial scaffold, FastMCP v3 migration, `show_skills`, the auto-update-MCP-client-configs step after `gather`, and the `add`-from-git-repo flow.

### The pivot — 2026-05-22 to 2026-05-23

2026-05-22: a single commit, a tolerance fix in `gather` to keep going past a failing per-skill copy. The last meaningful change to the consolidate-local-skills model.

2026-05-23 was the pivot. One commit at the head of the day reads:

> `feat!: GitHub-backed skill registry (init + MCP server + Go CLI)`

The breaking change inverted the project's mental model. Instead of "scan the local machine and consolidate what's already there," the new design says **"your skills live in a GitHub repo you own; the MCP server fetches them on demand."** `gather` and `add` were removed, the local-folder MCP server was deleted, `show_skills` became `list_skills` + `get_skill`, and a new `publish_skill` tool let the LLM write back to the registry.

The same day brought a long string of follow-ups:

- `feat!: rename PyPI package to skills-registry (v0.5.0)` and `feat!: rename GitHub repo to skills-registry; drop stale website/` — the rename plus deletion of the original `website/` and `Web-Prototype.zip` design assets.
- `feat(bootstrap): push initial import via git to avoid GitHub secondary rate limit` — introduced `PushTreeViaGit`. The per-file `POST /git/blobs` path was tripping GitHub's ~80-req/minute limit for users with 30–200 skills; the fix was a single `git push` over HTTPS for the initial import, with credentials wired by `gh auth setup-git`. The MCP server still goes through `gh api` only.
- `feat(cli): F4.1 add remove command + registry.Client.Delete` — atomic slug-level removal via the same Git Data API sequence as `publish`, with null SHAs in the tree.
- `feat(cli): F4.2 wire --json output into every subcommand` — persistent `--json` flag in `cli/internal/jsonout/jsonout.go`.
- `feat(tui): F3.1`/`F3.2`/`F3.3` — alt-screen hub with a responsive card grid, toast feedback loop, settings view, rune- and width-aware list truncation.
- `feat(cli): route bare skills-registry to wizard/hub/help` — `bareRouteDecision` becomes the pure routing function for bare invocation.
- `feat(bootstrap): port _ensure_mcp_entry_point to Go` — MCP-server install (uv → pipx → pip) moved out of Python and into `cli/internal/bootstrap/mcp_install.go`, so first-run users never see Python during onboarding.
- `feat(install): add curl|sh installer for the Go binary` — `install.sh`, the only commit in the entire repo with `droid` as the author.

By end-of-day, 13 PRs (#1 through #12) had been merged and a from-scratch Next.js 16 / React 19 marketing site was live. 82 commits in 24 hours.

### PR polish — 2026-05-24

Today. Four commits, all addressing review feedback from `gemini-code-assist[bot]` and `factory-droid[bot]` on the merged pivot PRs: cancel semantics in the wizard, visibility validation on repo creation, a `dot-folder sweep` test fix, error handling and TUI-safe output polish.

## Longest-standing features

For a 3-day-old codebase, "longest-standing" is shorthand for "survived the pivot." Three patterns have held since 2026-05-23:

- **`gh`-only GitHub I/O for the MCP server.** No SSH, no embedded HTTP client, no direct `git` shell-out. Every request goes through the user's authenticated `gh` CLI, because desktop MCP clients spawn the server in a stripped environment without `SSH_AUTH_SOCK` or full `PATH`. See `background/design-decisions.md`.
- **The slugify rule.** `lower(s) → non-alphanumeric → "_"`, mirrored in Python and Go.
- **The 2 MiB per-file size cap.** `SKILLS_MAX_FILE_BYTES` (default 2,097,152) rejects oversized uploads before they hit the GitHub API. Applied identically by `RegistryClient.publish_skill` and `registry.Client.Publish`.

## Deprecated features

| Feature | Introduced | Removed | Replaced by |
|---|---|---|---|
| `gather` command | 2026-05-21 | 2026-05-23 | Personal registry repo |
| Original `add` (install from git URL) | 2026-05-21 | 2026-05-23 | `publish` (writes to the registry) |
| `show_skills` MCP tool | 2026-05-21 | 2026-05-23 | `list_skills` + `get_skill` |
| `SkillsDirectoryProvider` | 2026-05-21 | 2026-05-23 | `RegistryClient` (gh-api) |
| Local-folder MCP server | 2026-05-21 | 2026-05-23 | `registry_server.py` |
| `Web-Prototype.zip` design assets | 2026-05-21 | 2026-05-23 | Next.js 16 site under `website/` |

## Major rewrites

- **The pivot itself** — `gather`/`add`/`show_skills` → registry model. One day, one breaking commit.
- **PyPI rename** — `skills-mcp` → `skills-registry` at v0.5.0. The Python module path stayed `skills_mcp` to avoid breaking existing installs; the Go binary became `skills-registry` (singular). The mismatch is deliberate.
- **Website rebuild** — original `website/` and `Web-Prototype.zip` deleted, replaced with a Next.js 16 / React 19 / Tailwind 4 single-page site (~631 LOC TS).
- **Bootstrap UX migration** — MCP-entry-point install ported from `init.py:_ensure_mcp_entry_point` to `cli/internal/bootstrap/mcp_install.go:EnsureMCPEntryPoint`. The Python helper is still in-tree but no longer canonical.

## Growth trajectory

- **Day 1 (May 21):** 16 commits — working consolidate-local-skills CLI, FastMCP v3 wired up.
- **Day 2 (May 22):** 1 commit. Effectively a rest day.
- **Day 3 (May 23):** 82 commits, full rewrite. New Go binary (~17,500 LOC), rewritten Python MCP server (~2,500 LOC), installer, website, wizard, hub, `remove`, `--json` plumbing, 13 PRs merged. The repo went from ~3,000 LOC to ~20,000 LOC in 24 hours.
- **Day 4 (May 24, today):** 4 commits of bot-reviewed polish.
