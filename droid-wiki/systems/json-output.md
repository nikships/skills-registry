# JSON output

Active contributors: Nik Anand

## What it does

Every CLI subcommand honors a persistent `--json` flag. When the flag is set, the command suppresses every TUI / interactive surface and emits a single line of JSON to stdout describing its result. Failures emit `{"error": "..."}` and exit non-zero. The mechanism lives in `cli/internal/jsonout/jsonout.go` and is just three functions plus a flag binding.

The point is to make the CLI scriptable. An agent or a shell pipeline can run `skills-registry list --json | jq '.[].slug'` and get a stable, machine-readable result without parsing the human-formatted output.

## The persistent flag pattern

`BindFlag(rootCmd)` attaches `--json` to the root cobra command's `PersistentFlags()`:

```go
func BindFlag(cmd *cobra.Command) {
    cmd.PersistentFlags().BoolVar(&enabled, FlagName, false, flagDescription)
}
```

Cobra propagates persistent flags down to every subcommand at parse time, so a single `BindFlag` call on the root makes `--json` available everywhere. The binding stores the parsed value in a package-level `enabled` bool that subcommands read via `jsonout.Enabled()` — no dependency-injection plumbing, no flag passed through every function signature.

The flag is bound once, in `main.go`, before any subcommand handler runs.

## The helpers

| Function | Purpose |
| --- | --- |
| `BindFlag(cmd *cobra.Command)` | Attach the persistent `--json` flag to a cobra command. Call once on the root. |
| `Enabled() bool` | Reports whether `--json` was passed. Subcommands branch on this early. |
| `Print(v any) error` | Marshals `v` to compact JSON and writes it to stdout followed by a newline. Returns marshal / write errors. |
| `PrintError(err error)` | Writes `{"error": "<err.Error()>"}` to stdout. Never returns an error itself — the caller is already failing. |
| `SwapWriter(io.Writer)` | Replaces the stdout sink for tests. Returns the previous writer so tests can restore it on cleanup. |
| `SetEnabled(bool)` | Test helper to drive `Enabled()` without spinning up a cobra command. |

`SwapWriter` exists so tests can capture emitted JSON in a buffer. We deliberately use a package-level writer (rather than reading `os.Stdout` dynamically inside `Print`) so a test can't accidentally subvert capture by re-pointing `os.Stdout` mid-call.

## The subcommand pattern

Every subcommand follows the same shape:

```go
RunE: func(cmd *cobra.Command, args []string) error {
    if jsonout.Enabled() {
        return runListJSON(cmd.Context(), queryFlag)
    }
    return runList(cmd.Context(), queryFlag, plain)
}
```

The JSON path is its own function (`runListJSON`, `runGetJSON`, `runPublishJSON`, …). It never enters a Bubble Tea program, never prompts for confirmation, and emits exactly one JSON document on success. On failure it calls `jsonout.PrintError(err)` and either returns the error (so cobra exits non-zero) or `os.Exit(1)`s directly.

## Auto-`--yes` for destructive commands

The destructive subcommands (`sync`, `remove`) normally prompt the user before any registry write. In `--json` mode the prompt would hang because Bubble Tea can't render in a piped environment. We solve this with `shouldAutoYes()` in `cli/cmd/skills-registry/list.go`:

```go
func shouldAutoYes() bool {
    return jsonout.Enabled() && !isStdinTerminal()
}
```

The flag-handling lines OR this into the explicit `--yes` flag:

```go
return runRemoveCmd(cmd.Context(), args[0], yes || shouldAutoYes())
```

Two conditions matter:

- **`jsonout.Enabled()`** — the caller asked for JSON, so we can't show a TUI prompt anyway.
- **`!isStdinTerminal()`** — stdin is piped (an agent or a script driving the CLI). If a human is sitting at a terminal that happens to also pass `--json`, we'd rather still ask before destroying anything.

The combination keeps explicit `--yes` users unchanged and adds the auto-confirm only for the pipe-into-CLI case.

## Per-subcommand payload shapes

The exact JSON shape per subcommand is fixed and tested. Field order matches the documented contract so a consumer reading `jq '.slug'` sees a stable layout across releases.

| Subcommand | Payload | Notes |
| --- | --- | --- |
| `list --json` | `[{slug, name, description}, …]` | Empty registry is `[]`, never `null`. |
| `get --json` | `{slug, path}` | `slug` is the canonical (slugified) form; `path` is the absolute on-disk destination. |
| `publish --json` | `{slug, sha, url}` | `sha` is the full 40-char hash, not the 7-char short form printed for humans. `url` is the GitHub tree-view URL. |
| `sync --json` | `{pushed: [slugs], skipped: [slugs]}` | Both arrays always present, possibly empty. No partial-success case — any push failure aborts with `{"error": "..."}`. |
| `add --json` | `{pushed: [slugs], skipped: [slugs]}` | Same shape as `sync`. |
| `remove --json` | `{slug, repo, sha, removed_from: [registry, cache, dotfolders]}` | `removed_from` is the list of locations actually touched (always includes `"registry"`; others appear when present). |

Errors are uniform: `{"error": "<message>"}` to stdout, non-zero exit code. Consumers can branch on `jq -e '.error'` or check the exit code.

## Why agents care

The whole point of `--json` is to make the CLI usable as a building block in a larger automation. A few example pipelines:

```bash
# Download every skill in the registry.
skills-registry list --json | jq -r '.[].slug' | xargs -I{} skills-registry get {} --json

# Push every local dot-folder skill missing from the registry, then capture
# the set of slugs that were actually pushed.
PUSHED=$(skills-registry sync --json | jq -r '.pushed[]')

# Open the GitHub URL after a publish.
URL=$(skills-registry publish ./my-skill --json | jq -r '.url')
open "$URL"

# Remove a slug and capture which locations were cleaned.
skills-registry remove auth_skill --json | jq -r '.removed_from[]'
```

Without `--json`, parsing the human-formatted output is brittle — column widths shift, the unicode `✓` chip might break a downstream `grep`, and any future change to the display format would silently break callers. The JSON payload is the contract; the human output is not.

## Stability guarantees

- Field names are stable. New fields may be added (consumers should ignore unknown keys), but existing fields don't change name or type.
- Empty collections are emitted as `[]`, not omitted or set to `null`.
- The top-level error envelope is `{"error": "..."}`. There is no `{"success": false, "error": ...}` variant.
- Exit codes match the human-mode behavior: 0 on success, non-zero on failure. The presence of `{"error": ...}` on stdout and a non-zero exit code are redundant; consumers can choose either signal.

These are tested in `cli/cmd/skills-registry/json_test.go`. New fields and new commands go through that test file.

## Key source files

| File | Role |
| --- | --- |
| `cli/internal/jsonout/jsonout.go` | The flag binding + helpers. ~100 lines of Go. |
| `cli/cmd/skills-registry/list.go` | Hosts `shouldAutoYes()` and `isStdinTerminal` (the latter is a swappable variable so tests can stub it). |
| `cli/cmd/skills-registry/json_test.go` | The contract tests: per-subcommand payload shapes, error envelope, exit codes. |
| Each `cli/cmd/skills-registry/<cmd>.go` | Per-command handler with the `if jsonout.Enabled() { … }` branch. |

## Cross-links

- [Architecture](../overview/architecture.md) — where the flag fits in the bare-command routing.
- [CLI commands](../api/cli-commands.md) — the per-command flags and payload reference.
- [Registry client](registry-client.md) — what the JSON payloads actually carry under the hood.
