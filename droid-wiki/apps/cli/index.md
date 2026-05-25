# CLI

Active contributors: Nik Anand

## What it does

`skills-registry` is the single Go binary installed at `~/.local/bin/skills-registry`. It owns every interactive surface (the onboarding wizard, the dashboard hub) and every headless subcommand (`list`, `get`, `sync`, `add`, `publish`, `remove`, `bootstrap`). A persistent `--json` flag flips every subcommand into machine-readable mode; the bare invocation routes the user to whichever screen is appropriate for their state.

## Layout

The CLI lives in a separate Go module (`cli/go.mod`). The two top-level directories:

- `cli/cmd/skills-registry/` — Cobra entry point, `runRoot`/`runWizard`/`runHub`, one file per subcommand.
- `cli/internal/` — every reusable piece (registry client, Bubble Tea models, config, agents catalogue, dead-folder scan, JSON helpers, MCP installer).

The single entry binary at `cli/cmd/skills-registry/main.go` builds the cobra command tree and dispatches. Subcommands are registered by name; cobra parses arguments and routes to the matching `RunE`. A bare invocation (no subcommand, no `--help`) falls through to `runRoot`.

## Build-time version injection

The `version` variable in `cli/cmd/skills-registry/main.go` defaults to `"dev"` and is overridden at release time via Go's `-ldflags`:

```bash
go build -ldflags "-X main.version=v0.5.1" ./cmd/skills-registry
```

`cobra.Command.Version = version` wires the value into `skills-registry --version`. Local development builds stamp `"dev"`; release tarballs stamp the matching `vX.Y.Z` tag.

## Persistent `--json` flag

`cli/internal/jsonout/jsonout.go` owns the flag. `jsonout.BindFlag(root)` is called once on the root cobra command; cobra propagates persistent flags down to every subcommand at parse time, so each subcommand can read `jsonout.Enabled()` without re-binding.

The contract per subcommand:

1. Check `jsonout.Enabled()` at the top of `RunE`.
2. If `true`, branch into a JSON-only path: no TUI, no prompts. Emit `jsonout.Print(struct{…}{…})` on success; emit `jsonout.PrintError(err)` followed by `os.Exit(1)` on failure.
3. If `false`, run the normal human-facing path.

See [systems/json-output](../../systems/json-output.md) for the per-subcommand payload shapes.

## Bare-command routing

`cli/cmd/skills-registry/main.go:bareRouteDecision` is the pure decision function that picks where a bare `skills-registry` invocation lands. No I/O, no globals — all inputs are arguments so the routing matrix is unit-testable end-to-end.

The four resolutions:

| isTTY | `--json` | config load error | → route | What runs |
| --- | --- | --- | --- | --- |
| any | `true` | any | `bareRouteHelp` | print usage, exit `0` |
| `false` | `false` | any | `bareRouteHelp` | print usage, exit `0` |
| `true` | `false` | `ErrMissing` | `bareRouteWizard` | first-run onboarding wizard |
| `true` | `false` | `nil` | `bareRouteHub` | dashboard hub |
| `true` | `false` | other | `bareRouteError` | surface malformed config |

The contract is "bare invocation always lands somewhere safe". A non-TTY environment can't render Bubble Tea, and `--json` callers have asked for stdout; both short-circuit to help. Otherwise, route based on whether config exists.

## Dependencies

The Go module declares a small, fixed set:

- `github.com/spf13/cobra` — root + subcommand parsing.
- `github.com/charmbracelet/bubbletea` — alt-screen TUI runtime.
- `github.com/charmbracelet/bubbles` — text input, spinner, progress bar widgets.
- `github.com/charmbracelet/lipgloss` — styling and layout.
- `gopkg.in/yaml.v3` — frontmatter parsing only (no PyYAML-shaped runtime dep on the MCP server side).

Anything that talks to GitHub shells out to `gh`. Anything that pushes a bulk tree shells out to `git`. No embedded HTTP client.

## Key source files

| File | Role |
| --- | --- |
| `cli/cmd/skills-registry/main.go` | Cobra root, `version` ldflag injection, `runRoot` + `bareRouteDecision`. |
| `cli/cmd/skills-registry/wizard.go` | `runWizard` — alt-screen onboarding launcher. |
| `cli/cmd/skills-registry/hub.go` | `runHub` — returning-user dashboard launcher. |
| `cli/cmd/skills-registry/bootstrap.go` | Headless `bootstrap` subcommand. |
| `cli/cmd/skills-registry/list.go` `get.go` `sync.go` `add.go` `publish.go` `remove.go` | Per-subcommand handlers, all honor `--json`. |
| `cli/internal/jsonout/jsonout.go` | Persistent `--json` flag + `Print`/`PrintError` helpers. |
| `cli/internal/registry/registry.go` | `registry.Client` — mirror of the Python `RegistryClient`, plus `PushTreeViaGit` and `Delete`. |

## Cross-references

- [apps/cli/wizard-and-hub](wizard-and-hub.md) — the interactive surfaces.
- [apps/cli/subcommands](subcommands.md) — every headless command and its `--json` payload.
- [overview/architecture](../../overview/architecture.md) — how the CLI fits with the MCP server.
- [systems/json-output](../../systems/json-output.md) — the `--json` contract.
- [systems/registry-client](../../systems/registry-client.md) — `registry.Client` deep dive.
- [systems/bootstrap-push](../../systems/bootstrap-push.md) — `PushTreeViaGit` and why it exists.
- [api/cli-commands](../../api/cli-commands.md) — flag-level reference.
