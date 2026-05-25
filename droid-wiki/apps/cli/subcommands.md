# Subcommands

Active contributors: Nik Anand

## What they do

`skills-registry` exposes seven headless subcommands. Each is a single file under `cli/cmd/skills-registry/` registered on the root cobra command. They share one persistent flag (`--json`) and three pieces of shared infrastructure: `config.Load`, `registry.New`, and `walkSkillIntoFiles` / `rekeyBySlug` for file collection.

## The table

| Command | Purpose | Key flags | `--json` payload |
| --- | --- | --- | --- |
| `bootstrap` | Headless onboarding — create repo, push every local skill, install agent docs. Useful in CI; the wizard supersedes it interactively. | `--repo`, `--visibility`, `--no-agents`, `--yes` | (not yet structured; legacy human output) |
| `list` | Browse the registry as a list. With `--plain` or no TTY, prints a fixed-width table. | `--query`/`-q`, `--plain` | `[{slug, name, description}, …]` |
| `get <slug>` | Download one skill into a local folder. Default destination is `./.agents/skills/<slug>/`. | `--dest` | `{slug, path}` |
| `sync` | Push local dot-folder skills missing from the registry. Interactive multi-select unless `--all`. | `--yes`/`-y`, `--all` | `{pushed: [...], skipped: [...]}` |
| `add <source>` | Clone an external source (`./path`, `owner/repo`, or full URL), multi-select skills, publish to your registry. | `--yes`/`-y`, `--all` | `{pushed: [...], skipped: [...]}` |
| `publish <path>` | Upload one local folder as one skill. | `--name` | `{slug, sha, url}` |
| `remove <slug>` | Delete a skill end-to-end: registry, MCP cache, every agent dot-folder. | `--yes`/`-y` | `{slug, removed_from: [...], sha, repo}` |

The `bootstrap` subcommand predates the wizard (F2.x replaced the interactive parts) and is kept around for scripted invocations that don't want a TUI. Its long-form output is still human-readable text.

## The `--json` contract

Every subcommand respects `--json`. The flag is bound once on the root via `jsonout.BindFlag` (see `cli/internal/jsonout/jsonout.go`); each subcommand checks `jsonout.Enabled()` at the top of its `RunE` and branches into a dedicated `run<Foo>JSON` path:

1. No TUI. No Bubble Tea programs. No interactive prompts.
2. Success → `jsonout.Print(struct{…}{…})` writes one line of compact JSON to stdout.
3. Failure → `jsonout.PrintError(err)` writes `{"error": "..."}` to stdout, then `os.Exit(1)`.

Empty arrays are always emitted as `[]` (never elided) so a consumer can `jq 'length'` without special-casing missing fields. See [systems/json-output](../../systems/json-output.md) for the per-payload contracts and version stability rules.

## `shouldAutoYes` — destructive auto-confirm

Destructive commands (`sync`, `add`, `remove`) need a confirmation prompt by default. But an agent driving the CLI with piped stdin can't render a Bubble Tea prompt — it would hang.

`shouldAutoYes()` in `cli/cmd/skills-registry/list.go` resolves the ambiguity:

```go
func shouldAutoYes() bool {
    return jsonout.Enabled() && !isStdinTerminal()
}
```

When `--json` is set AND stdin is not a TTY, every destructive subcommand promotes `--yes` automatically. Callers OR this into their `yes` flag (`yes || shouldAutoYes()`) so explicit `--yes` users keep their existing behavior and interactive `--json` runs (rare, but possible) still see the prompt.

`isStdinTerminal` is a `var`, not a `func` — tests stub it because `go test`'s harness doesn't guarantee a TTY-attached stdin.

## Per-subcommand notes

### `list`

`runListJSON` lists every skill in the registry, filters by `--query` if provided, and emits the array. The plain-text path (`runList` with `--plain` or no TTY) prints a fixed-width table with `SLUG / DESCRIPTION` columns; descriptions are clipped to 80 runes (not bytes) so multi-byte UTF-8 doesn't get cut mid-character. The interactive path opens `tui.NewList` with mouse cell motion enabled.

### `get`

`DownloadSkill` is the shared core (used by `get`, the `list` TUI's enter handler, and the hub's Manage card). Destination resolution: empty `--dest` → `<cwd>/.agents/skills/<canonSlug>`; explicit `--dest` whose basename slugifies to the canon → use as-is; otherwise treat `--dest` as a parent dir and append `<canonSlug>`. The parent dir is then scanned for an existing sibling that slugifies to the same canonical form so the "agp-9-upgrade vs agp_9_upgrade" duplicate-folder case stays consistent.

### `sync` and `add`

Both share `selectSkills*` and `publishSkills` helpers. The shape is the same: discover local skills (or scan a cloned source), `client.Slugs(ctx)` for the remote set, `scan.DedupeAgainst` for the missing partition, multi-select unless `--all`, optional `confirmPush` choice prompt unless `--yes`, then walk + publish one skill at a time. `--json` collapses both into "publish everything missing, no prompts" and emits `{pushed, skipped}`.

`resolveSource` accepts three input forms for `add <source>`: a local path (`./`, `/`, `~`), a GitHub shorthand (`owner/repo`, validated by `ghShorthandRe`), or a full git URL. Shorthand is rewritten to `https://github.com/<source>.git` and cloned shallow into a tempdir; the tempdir is `os.RemoveAll`-ed on exit.

### `publish`

`doPublish` is the shared core. It validates the path is a directory containing `SKILL.md`, parses the frontmatter for the canonical name (override via `--name`), `Slugify`s it, walks the tree, applies the 2 MiB per-file cap (`maxFileBytes`, matching the Python `SKILLS_MAX_FILE_BYTES` default), and calls `client.Publish(ctx, slug, files, msg)`. The JSON shape is `{slug, sha, url}` where `sha` is the full commit hash (not the 7-char short form printed to humans) and `url` is the GitHub tree view.

### `remove`

`runRemove` is the shared core invoked by both the subcommand and the hub's Remove dispatcher. It deletes from three locations in order:

1. **Registry** — `client.Delete(ctx, canonSlug)` atomically drops the `<slug>/` subtree via the Git Data API (null-SHA entries in the new tree).
2. **Cache** — `~/.cache/skills-mcp/skills/<slug>/` and the sibling `<slug>.meta.json` are wiped.
3. **Dot-folders** — every entry in `agents.All()` is swept; any direct child whose name matches the slug literally or via `Slugify` is removed (handles symlinks and real directories alike).

The `--json` payload's `removed_from` array contains string constants (`"registry"`, `"cache"`, `"dotfolders"`) — never free-form text — so consumers can match on stable values.

## Key source files

| File | Role |
| --- | --- |
| `cli/cmd/skills-registry/bootstrap.go` | Headless `bootstrap` flow + shared helpers (`walkSkillIntoFiles`, `locateMCPBinary`, `requireGitForBootstrap`). |
| `cli/cmd/skills-registry/list.go` | `runListJSON` + `runList` + `printPlainList`; also hosts `isTerminal` / `isStdinTerminal` / `shouldAutoYes`. |
| `cli/cmd/skills-registry/get.go` | `runGetJSON` + `runGet` + shared `DownloadSkill` + `resolveDest`. |
| `cli/cmd/skills-registry/sync.go` | `runSyncJSON` + `runSync` + `planSync` + `publishSkills` + `confirmPush` + `promptSync`. |
| `cli/cmd/skills-registry/add.go` | `runAddJSON` + `runAdd` + `resolveSource` + `promptAddSelection`. |
| `cli/cmd/skills-registry/publish.go` | `runPublishJSON` + `runPublish` + shared `doPublish` + `collectFiles`. |
| `cli/cmd/skills-registry/remove.go` | `runRemoveCmd` + `runRemove` (shared with hub) + `removeFromCache` + `removeFromDotFolders`. |
| `cli/internal/jsonout/jsonout.go` | `BindFlag` / `Enabled` / `Print` / `PrintError`. |

## Cross-references

- [apps/cli/index](index.md) — Cobra root, persistent `--json`, `bareRouteDecision`.
- [apps/cli/wizard-and-hub](wizard-and-hub.md) — the interactive flows that reuse these handlers.
- [systems/json-output](../../systems/json-output.md) — per-subcommand JSON contracts.
- [systems/registry-client](../../systems/registry-client.md) — the `registry.Client` calls every subcommand makes.
- [api/cli-commands](../../api/cli-commands.md) — flag-level reference.
