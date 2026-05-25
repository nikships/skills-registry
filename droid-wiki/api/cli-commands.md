# CLI commands

Active contributors: Nik Anand

## What this page covers

`skills-registry` (the Go binary) ships seven named subcommands plus a bare-invocation router that lands on the wizard / hub / help dump. Each subcommand is a single file under `cli/cmd/skills-registry/` registered on the root cobra command. They all honor a persistent `--json` flag bound on the root via `jsonout.BindFlag` (see [../systems/json-output.md](../systems/json-output.md)).

This page is the flag-level reference. Cross to [../apps/cli/subcommands.md](../apps/cli/subcommands.md) for the implementation deep dive shared by the standalone subcommands and the hub dispatcher.

## The persistent `--json` flag

Every subcommand checks `jsonout.Enabled()` at the top of its `RunE` and forks into a `run<Foo>JSON` code path: no TUI, no prompts, success emits one line of compact JSON via `jsonout.Print`, failure emits `{"error": "..."}` via `jsonout.PrintError` then `os.Exit(1)`. Empty arrays are always emitted as `[]` (never elided) so `jq 'length'` doesn't have to special-case missing fields.

## `shouldAutoYes` — destructive auto-confirm

Destructive subcommands (`sync`, `add`, `remove`) prompt for confirmation by default. An agent piping commands into the CLI can't render a Bubble Tea prompt without hanging, so `shouldAutoYes()` in `cli/cmd/skills-registry/list.go` resolves the ambiguity:

```go
func shouldAutoYes() bool {
    return jsonout.Enabled() && !isStdinTerminal()
}
```

When `--json` is set AND stdin is not a TTY, every destructive subcommand promotes `--yes` automatically. The condition is the AND, not the OR — interactive `--json` (rare, but possible) still surfaces the prompt; explicit `--yes` always wins. `isStdinTerminal` is a `var`, not a `func`, so tests can stub it.

## `bootstrap`

```
skills-registry bootstrap [--repo OWNER/NAME] [--visibility public|private]
                         [--no-agents] [--yes]
```

| Flag | Type | Purpose |
| --- | --- | --- |
| `--repo` | `string` | Skip the repo-name prompt; expects `owner/name`. |
| `--visibility` | `public\|private` | Skip the visibility prompt. |
| `--no-agents` | `bool` | Don't install `SKILL.md` into any agent dot-folders. |
| `--yes` | `bool` | Accept defaults; useful for scripting. |

Headless onboarding: ensure gh auth + git on PATH → scan dot-folders → create/reuse repo → push via `PushTreeViaGit` → multi-select agents → install `SKILL.md` → offer cleanup → print MCP snippet. The wizard supersedes this interactively. `--json` is not yet structured for `bootstrap`; all other subcommands have explicit JSON contracts.

## `list`

```
skills-registry list [--query SUBSTRING] [--plain] [--json]
```

| Flag | Type | Purpose |
| --- | --- | --- |
| `--query` / `-q` | `string` | Initial filter substring (lower-cased, matched against `slug name description`). |
| `--plain` | `bool` | Print a fixed-width table instead of opening the TUI. |

Default: opens an animated alt-screen Bubble Tea list TUI (`tui.NewList`); pressing enter on a row downloads that skill via the shared `DownloadSkill` core. `--plain` (or no TTY) prints a `SLUG / DESCRIPTION` table; descriptions are clipped to 80 runes — not bytes — so multi-byte UTF-8 stays intact.

```json
[
  {"slug": "auth-skill", "name": "Auth skill", "description": "Validates …"},
  {"slug": "csv-export", "name": "CSV export", "description": "Streams …"}
]
```

Empty registry → `[]`. Errors → `{"error": "..."}` + exit 1.

## `get <slug>`

```
skills-registry get <slug> [--dest PATH] [--json]
```

| Flag | Type | Purpose |
| --- | --- | --- |
| `--dest` | `path` | Override the destination folder (default `./.agents/skills/<canonSlug>`). |

Destination resolution in `resolveDest`: empty `--dest` → `<cwd>/.agents/skills/<canonSlug>`; explicit `--dest` whose basename slugifies to `canonSlug` → used as-is; otherwise treated as a parent dir with `canonSlug` appended. After resolving, the parent is scanned for an existing sibling whose name slugifies to the same canonical form; if found at a different path, that path is reused (prevents the `agp-9-upgrade` vs `agp_9_upgrade` duplicate-folder bug).

```json
{"slug": "auth-skill", "path": "/home/u/proj/.agents/skills/auth-skill"}
```

## `sync`

```
skills-registry sync [--yes] [--all] [--json]
```

| Flag | Type | Purpose |
| --- | --- | --- |
| `--yes` / `-y` | `bool` | Skip the confirmation prompt. |
| `--all` | `bool` | Select every missing skill without prompting. |

Scans known dot-folders for skills whose slug isn't already in the registry. Default: interactive multi-select then a yes/no confirm. `--all` skips the multi-select. `--json` implies `--yes` and publishes every missing slug. Per-skill publishes use the gh-api blob path, not `PushTreeViaGit`.

```json
{"pushed": ["auth-skill", "csv-export"], "skipped": ["already-in-registry"]}
```

Both arrays are always present (possibly empty).

## `add <source>`

```
skills-registry add <source> [--yes] [--all] [--json]
```

| Flag | Type | Purpose |
| --- | --- | --- |
| `--yes` / `-y` | `bool` | Skip the confirmation prompt. |
| `--all` | `bool` | Publish everything in the source without prompting. |

`<source>` accepts a local path, a GitHub shorthand (`owner/repo`, validated by `ghShorthandRe` and cloned via `git clone --depth 1`), or a full git URL. Discovers every `SKILL.md` inside, multi-selects what to publish, pushes to **your** registry. Tempdirs are `os.RemoveAll`-ed on exit. JSON output mirrors `sync`:

```json
{"pushed": ["new-skill"], "skipped": ["dup-of-mine"]}
```

## `publish <path>`

```
skills-registry publish <path> [--name OVERRIDE] [--json]
```

| Flag | Type | Purpose |
| --- | --- | --- |
| `--name` | `string` | Override the skill name (default: read from `SKILL.md` frontmatter, fall back to basename). |

Validates `<path>` is a directory containing `SKILL.md`. Walks the tree; skips hidden entries and `__pycache__`; applies the 2 MiB per-file cap (`maxFileBytes`, matching the Python `SKILLS_MAX_FILE_BYTES` default) with a stderr warning + skip on oversized files. Calls `client.Publish(ctx, slug, files, msg)`.

```json
{"slug": "auth-skill", "sha": "abc123def456…", "url": "https://github.com/owner/repo/tree/abc1234/auth-skill"}
```

`sha` is the full commit hash, not the 7-char short form printed for humans — agents downstream often want the canonical identifier.

## `remove <slug>`

```
skills-registry remove <slug> [--yes] [--json]
```

| Flag | Type | Purpose |
| --- | --- | --- |
| `--yes` / `-y` | `bool` | Skip the confirmation prompt. |

Three-step deletion:

1. **Registry** — `client.Delete(ctx, canonSlug)` atomically drops the `<slug>/` subtree via the Git Data API.
2. **Cache** — `~/.cache/skills-mcp/skills/<slug>/` and the sibling `<slug>.meta.json` are wiped if present.
3. **Dot-folders** — every entry in `agents.All()` is swept; direct children matching the slug literally or via `Slugify` are removed.

Pre-checks the registry via `client.Slugs()` before any prompts — a missing slug exits 1 cleanly without rendering a confirm-then-fail dialog. The interactive confirmation defaults its highlight to **Cancel**.

```json
{"slug": "auth-skill", "repo": "owner/repo", "sha": "abc123def456…", "removed_from": ["registry", "cache", "dotfolders"]}
```

The `removed_from` array uses fixed string constants so consumers can match on stable values. A missing slug returns `{"error": "slug \"foo\" not found in registry owner/repo"}` with exit 1.

## Cross-references

- [../apps/cli/subcommands.md](../apps/cli/subcommands.md) — implementation of `doPublish`, `planSync`, `DownloadSkill`, `runRemove`.
- [../apps/cli/wizard-and-hub.md](../apps/cli/wizard-and-hub.md) — the wizard and hub flows that reuse these handlers.
- [../systems/json-output.md](../systems/json-output.md) — per-payload contracts and version stability rules.
- [../systems/registry-client.md](../systems/registry-client.md) — the `registry.Client` calls every subcommand makes.
- [../primitives/skill.md](../primitives/skill.md) — slugify rules, frontmatter contract.
- [../primitives/registry-config.md](../primitives/registry-config.md) — how `config.Load()` resolves the active registry.
