# Fun facts

A handful of details about `skills-registry` that don't fit anywhere else, but are worth knowing.

## The 24-hour pivot

On 2026-05-23, the project did a complete architectural redesign in a single day. **82 of the repo's 103 lifetime commits landed in that 24-hour window.**

That day produced:

- The breaking commit `feat!: GitHub-backed skill registry (init + MCP server + Go CLI)`.
- The removal of `gather`, `add`, `show_skills`, and the local-folder MCP server.
- A new Go binary at ~17,500 LOC of production + test code.
- A rewritten Python FastMCP server.
- `PushTreeViaGit` (the bulk-import workaround for GitHub's secondary rate limit).
- A `remove` subcommand with atomic Git-Data-API deletion.
- Persistent `--json` output across every subcommand.
- An 8-step Bubble Tea wizard and an alt-screen dashboard hub.
- A from-scratch Next.js 16 marketing site.
- 13 PRs (#1–#12) merged.

Day 1 had 16 commits. Day 2 had 1. Day 4 had 4. The pivot day is an outlier visible from orbit.

## The AI-authored installer

The `install.sh` script — the only thing a new user actually runs (`curl https://…/install.sh | sh`) — is the **only commit in the entire git history not authored by a human**. The commit reads:

> `feat(install): add curl|sh installer for the Go binary`
> Author: `droid`

The user-facing entry point of the whole project was AI-authored. Everything below the installer (the Go binary, the Python MCP server, the wizard) was authored by Nik Anand with varying levels of AI review assistance, but the script that delivers the binary to the user came out of a droid session.

This is the floor on AI authorship in the codebase, not the ceiling. Inline tools (Copilot tab-complete, Cursor edits, IDE autocomplete) leave no trace in `git log`, so the actual AI contribution rate is higher than any author-line analysis can show.

## Three names for one project

The brand-binary-module split is deliberate and a little unhinged:

| Where it appears | What it's called |
|---|---|
| The brand / PyPI package | `skills-registry` (plural) |
| The Go binary | `skills-registry` (singular) |
| The local working directory | `skillsmcp` (no hyphen) |
| The Python module path | `skills_mcp` (underscore) |
| The GitHub repo's old name | `skills-mcp` (the URL still resolves) |

Why? Because renaming any of them would break someone:

- The Python module path is stamped into every existing user's `mcp.json` and shell PATH. Renaming `skills_mcp` to `skills_registry` breaks every install in the wild.
- The Go binary name `skills-registry` is hard-coded into `install.sh` and every PATH lookup. Re-pluralizing it breaks every install.
- The local directory `skillsmcp` is just where the original developer cloned it on day 1 and nobody renamed it.

The brand stayed plural because "skills-registry" reads more naturally than "skills-registry-registry" in a sentence. The binary stayed singular because shells like singular nouns for verbs (`skills-registry list` reads like `git clone`).

The result: every cross-reference in the docs, the README, the wiki, and the source tree has to pick the right spelling for the right context, and there are four valid ones.

## Zero TODO comments

A repo-wide grep for `TODO`, `FIXME`, and `HACK` in production code returns **zero matches**. After 103 commits, a complete pivot, an 8-step wizard, a Git-Data-API client with retries, and an installer — no one left a note saying "come back to this."

There are no `// XXX` markers either. No `eslint-disable`. No `# noqa`. No `//nolint`. The CI gates (ruff with `C90` ≤ 12, gocyclo ≤ 15, staticcheck, deadcode) are all green on `main`.

For a codebase that was rebuilt from scratch in 24 hours, the absence of follow-up debt is the most surprising number on the project.

## The largest file is a state machine

`cli/internal/tui/wizard.go` clocks in at **1,518 lines** — the biggest source file in the repo. It implements the first-run onboarding wizard as a single Bubble Tea model with eight discrete steps:

1. Auth check (`gh` + `git`)
2. Scan local dot-folders for existing skills
3. Prompt for repo name + visibility
4. `gh repo create` → `PushTreeViaGit`
5. Multi-select which AI agents get a `SKILL.md` stub
6. Optionally delete the local dot-folder copies that were just pushed
7. Install the MCP server entry point (uv → pipx → pip)
8. Print the MCP JSON snippet and exit

Each step is a substate inside the same `Update` switch. The companion `cli/internal/tui/wizard_steps.go` (785 LOC) extracts the per-step handlers, and `cli/internal/tui/wizard_test.go` (924 LOC) verifies every transition.

The largest non-wizard, non-test file is `cli/internal/registry/registry.go` at 1,289 lines — the registry client itself. The TUI weighs more than the network layer.
