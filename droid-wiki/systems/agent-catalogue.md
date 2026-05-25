# Agent catalogue

Active contributors: Nik Anand

## What it does

The agent catalogue is the single source of truth for every AI tool dot-folder we recognise. It's a 56-entry table of `Target` records living in `cli/internal/agents/agents.go`, each describing one tool's dot-folder convention: where it lives on disk, what we call it in the multi-select TUI, and whether it counts as a universal (project-local, applies to every agent) target. The list seeds three things in the CLI:

- `scan.DiscoverSources` uses it to find local skills during `sync` and the wizard's scan step.
- `bootstrap.InstallSkillMd` writes the generated `skills-registry/SKILL.md` into every selected target's `skills/` folder.
- The wizard's agent multi-select renders one row per target, with universals locked on at the top.

The Python side does not carry this list. It used to, when the legacy `gather` command consolidated local skills, but that command was removed in 0.3.0; the catalogue is Go-only now.

## The `Target` struct

```go
type Target struct {
    DotDir    string // e.g. ".claude" (relative under $HOME or .)
    Display   string // shown in the TUI, e.g. "Claude Code"
    Universal bool   // true if selected by default and can't be toggled off
    UnderHome bool   // true if the install path lives under $HOME (vs cwd)
}
```

Four fields, no methods of substance besides `SkillsDir(home, cwd)`. The fields:

- **`DotDir`** is the folder name as it appears on disk. We always store it with a leading dot (`.claude`, `.factory`, `.agents`) so it stays in sync with the conventions each tool uses in its docs.
- **`Display`** is the human label we show in the multi-select. Sourced from each tool's documentation or brand guidelines — for example `.copilot` displays as `GitHub Copilot`, `.roo` displays as `Roo`, `.roocode` displays as `Roo Code`.
- **`Universal`** marks the entries that aren't tied to one specific tool. Today the only universal is `.agents/` (a project-local folder that many tools — including Claude Code, Codex, and others — pick up by convention).
- **`UnderHome`** distinguishes home-anchored folders (`~/.claude/skills`) from project-local folders (`./.agents/skills`).

## `SkillsDir(home, cwd)`

This is the only method on `Target`:

```go
func (t Target) SkillsDir(home, cwd string) string {
    if t.UnderHome {
        return home + "/" + t.DotDir + "/skills"
    }
    return cwd + "/" + t.DotDir + "/skills"
}
```

It takes the user's home directory and the current working directory as explicit arguments, so callers (and tests) don't have to fight `os.UserHomeDir()` and `os.Getwd()` for control over the resolved paths. The returned string is always `<base>/<dotdir>/skills` — the `skills/` subfolder is the consistent convention every AI tool we list has settled on. A `Target{DotDir: ".claude", UnderHome: true}` with `home="/Users/alice"` resolves to `/Users/alice/.claude/skills`. A `Target{DotDir: ".agents", UnderHome: false}` with `cwd="/repo/foo"` resolves to `/repo/foo/.agents/skills`.

## The `Universal` flag

Universals are project-local folders that aren't tied to one specific tool. Today there is exactly one universal target: `.agents/` under the working directory. Many tools — Claude Code, Codex CLI, others — read `./.agents/skills` as a convention, in addition to (or instead of) their own dot-folder. Writing `skills-registry/SKILL.md` there once means every tool that respects the convention picks it up.

The flag has two consequences:

- **In `All()`**, universals sort first (alphabetically among themselves, then alphabetically among non-universals).
- **In the multi-select TUI** (`MultiSelectModel` in `cli/internal/tui/multiselect.go`), universals are rendered in a locked-on section at the top. The user can't toggle them off — they're always installed. The intent is to keep the project-local marker in place even if the user de-selects every other tool.

If we ever add a second universal (say a hypothetical `.mcp/skills/`), it joins the locked section without code changes — the rendering loop reads `Universal` directly.

## `All()` and ordering

```go
func All() []Target {
    out := make([]Target, 0, len(known))
    out = append(out, known...)
    sort.SliceStable(out, func(i, j int) bool {
        if out[i].Universal != out[j].Universal {
            return out[i].Universal
        }
        return out[i].Display < out[j].Display
    })
    return out
}
```

Universals first, then everything else sorted by display name. `sort.SliceStable` preserves the source order within ties (no ties happen today because no two entries share a display name, but the stability is a free hedge against future duplicates).

`All()` returns a fresh slice each call — the underlying `known` slice is never exposed. Callers can sort or filter the returned slice without affecting any other caller.

## Where the catalogue is consumed

| Caller | What it does |
| --- | --- |
| `scan.DiscoverSources(home, cwd, extra, dotDirs)` | Receives a `[]string` of dot-dirs derived from `agents.All()` and probes each `<home>/<dot>/skills` plus `<cwd>/<dot>/skills` for existence. Skills found in those folders are the candidates for `sync`. |
| `bootstrap.InstallSkillMd(targets, body, home, cwd)` | Writes the generated `skills-registry/SKILL.md` body into `target.SkillsDir(home, cwd) + "/skills-registry"` for each selected target. |
| `tui.NewMultiSelect(...)` (wizard step 5) | Renders one row per `Target`, with universals locked on. |
| `runRemove → removeFromDotFoldersAt` | When deleting a slug, iterates `agents.All()` and tries to remove the slug subfolder from every target's `SkillsDir`. |

## Adding a new entry

The list is a Go literal. Adding a new tool means appending one line to `known` in `cli/internal/agents/agents.go`:

```go
{DotDir: ".newtool", Display: "New Tool", UnderHome: true},
```

The next CI run picks it up; no other code changes are needed. Tests in `cli/internal/agents/agents_test.go` lock the table against duplicate dot-dirs (because two entries sharing a `DotDir` would collide on disk) and verify `All()` returns universals first.

## Why not Python parity

The MCP server is the wrong place for this list. The server never reads local dot-folders — its tools all operate against the GitHub registry. The catalogue's three consumers (`scan`, `bootstrap`, the wizard's multi-select) all live in the Go binary. Carrying a duplicate in Python would create a synchronisation hazard for no real benefit.

If a future CLI flow ever needs to call this list from Python (it shouldn't), the right move is to expose it as a config file the Go binary writes once at install time, not to copy-paste the table.

## Key source files

| File | Role |
| --- | --- |
| `cli/internal/agents/agents.go` | The catalogue + `Target` struct + `All()` + `SkillsDir`. |
| `cli/internal/agents/agents_test.go` | Lock tests: no duplicate dot-dirs, universals come first, expected count. |
| `cli/internal/scan/scan.go` | `DiscoverSources` — the catalogue's largest consumer. |
| `cli/internal/bootstrap/install.go` | `InstallSkillMd` writes the registry pointer into each target. |
| `cli/internal/tui/multiselect.go` | The TUI that renders one row per target. |

## Cross-links

- [Bootstrap push](bootstrap-push.md) — uses the catalogue to materialize local skills for the bulk push.
- [Architecture](../overview/architecture.md) — where the catalogue sits in the wizard flow.
- [CLI commands](../api/cli-commands.md) — the subcommands that read the catalogue.
- [Skill primitive](../primitives/skill.md) — the `SKILL.md` shape that ends up in each `target.SkillsDir`.
