# MCP entry-point install

Active contributors: Nik Anand

## What it does

The Go wizard's job is to leave the user with a working `skills-registry-mcp` binary on disk, so desktop MCP clients (Claude Desktop, Cursor, VS Code/Copilot, Codex) can launch it from the stripped subprocess environment they spawn it in. `EnsureMCPEntryPoint(ctx)` in `cli/internal/bootstrap/mcp_install.go` is the cascade that puts it there. It tries three installers in order — `uv tool install` → `pipx install` → `python3 -m pip install --user` — and stops at the first one that succeeds. Failure prints a manual-install hint and never aborts the wizard, because a missing MCP entry point doesn't break anything else in the bootstrap.

## Why an installer cascade

The MCP entry point is a Python console script provided by the `skills-registry` PyPI wheel. We have to land it on disk somewhere a desktop MCP client can find it. The three installers cover the realistic surface:

- **`uv tool install`** — the recommended option. `uv` is fast, isolates the install in `~/.local/share/uv/tools/skills-registry/`, and symlinks the binary into `~/.local/bin/skills-registry-mcp`. Users on the modern Python toolchain already have it.
- **`pipx install`** — the older equivalent. Same isolation behavior, different binary. Common on systems that pre-date `uv`'s adoption.
- **`python3 -m pip install --user`** — the universal fallback. Always available on any system with Python on PATH. Slower and less isolated, but it works.

We try them in that order because `uv` is the modern default, `pipx` is the established alternative, and pip-user is the lowest common denominator. The first attempt whose process exits 0 AND lands a binary in one of the curated fallback dirs wins.

## Skip switch: `SKILLS_SKIP_INSTALL`

Setting `SKILLS_SKIP_INSTALL=1` short-circuits `Ensure` before any work happens. This is the escape hatch for users who manage Python installations through some unusual route (NixOS, vendored CI containers, immutable systems, etc.) and don't want the wizard touching anything. The wizard still prints the MCP wire-up snippet at the end — the user is responsible for getting the binary on disk themselves.

`MCPInstaller.SkipInstall` is a `func() bool` field so tests can drive it without setting environment variables.

## The `entryPointPresent` probe

Before running any installer, `Ensure` checks whether the binary already exists in one of the curated fallback dirs:

```go
FallbackDirs: []string{
    filepath.Join(home, ".local", "bin"),
    filepath.Join(home, ".local", "share", "uv", "tools", "skills-registry", "bin"),
    "/opt/homebrew/bin",
    "/usr/local/bin",
},
```

If the binary is already there, we return immediately. This is the no-op case for repeat-wizard runs and for users who installed the entry point manually before the wizard ran.

The fallback dir list **must mirror `locateMCPBinary`** in `cli/cmd/skills-registry/bootstrap.go`, the function that resolves the absolute path embedded in the MCP wire-up snippet printed at the end of the wizard. If the install probe and the snippet probe drift, we could install into `~/.local/bin` and print a snippet pointing at `/opt/homebrew/bin`. Keeping the two lists identical (and tested together) prevents that bug.

On Windows the probe also checks for `skills-registry-mcp.exe`. The `MCPInstaller.GOOS` field is overridable so tests can pretend to be on Windows on a Linux runner.

## The `MCPInstaller` struct

```go
type MCPInstaller struct {
    FallbackDirs []string
    LookPath     func(name string) (string, error)
    RunInstaller func(ctx context.Context, argv []string) (int, error)
    SkipInstall  func() bool
    Stderr       io.Writer
    GOOS         string
}
```

Every dependency on the host environment is a field. Defaults wire to real system calls; tests substitute fakes to drive the flow without touching the filesystem or PATH. The pattern is "no globals, no init", and the package-level `EnsureMCPEntryPoint` is just `defaultMCPInstaller().Ensure(ctx)`.

The fields:

- **`FallbackDirs`** — the curated install / probe locations (see above).
- **`LookPath`** — defaults to `exec.LookPath`. Tests stub which installers count as "available" by returning a not-found error for the named binary.
- **`RunInstaller`** — defaults to `runInstallerCommand`, which exec's argv and returns the exit code. Tests replace it with a fake that records arguments and returns scripted exit codes.
- **`SkipInstall`** — defaults to reading `SKILLS_SKIP_INSTALL`. Tests can force-skip or force-run.
- **`Stderr`** — defaults to `io.Discard` for production (the wizard owns the user-facing output), tests substitute a buffer to assert hint messages.
- **`GOOS`** — defaults to `runtime.GOOS`, used to decide whether to look for `.exe` suffixes.

## Candidate selection

```go
func (m *MCPInstaller) candidates() []installerAttempt {
    out := make([]installerAttempt, 0, 3)
    if m.lookPath("uv") {
        out = append(out, installerAttempt{
            Label: "uv tool install",
            Argv:  []string{"uv", "tool", "install", "--force", PYPIDist},
        })
    }
    if m.lookPath("pipx") {
        out = append(out, installerAttempt{
            Label: "pipx install",
            Argv:  []string{"pipx", "install", "--force", PYPIDist},
        })
    }
    out = append(out, installerAttempt{
        Label: "pip install --user",
        Argv:  []string{m.pythonExe(), "-m", "pip", "install", "--user", "--upgrade", PYPIDist},
    })
    return out
}
```

`uv` and `pipx` are skipped if their binary isn't on PATH. Pip-user is always appended last so the function always has at least one attempt. `pythonExe()` prefers `python3` and falls back to `python` so the produced argv runs on systems where only the unversioned name exists.

The `--force` flag on `uv` and `pipx` makes the install idempotent — re-running the wizard on a system that already has an old version of the binary upgrades it cleanly instead of erroring out.

## The success criterion

```go
if rc == 0 && m.entryPointPresent() {
    fmt.Fprintf(m.Stderr, "  ✓ installed via `%s`\n", attempt.Label)
    return
}
```

An installer counts as successful when **both** conditions hold:

- The process exits with code 0 (no error from the installer).
- A binary lands in one of the fallback dirs.

We need both checks because some installers report success even when the binary ends up in an unexpected location (e.g. a non-standard prefix from `~/.pyenv`). The second check is what guarantees the wire-up snippet's path is real.

If `rc == 0` but `entryPointPresent()` returns false, we treat the attempt as a failure and move on to the next candidate. If every candidate fails (exit non-zero or binary not found), we fall through to `printManualHint`.

## The manual-install hint

Total failure writes a four-line block to stderr:

```
! Could not auto-install `skills-registry-mcp`. Continuing — the Go bootstrap
  will still run, but the MCP snippet it prints will refer to a binary
  that does not yet exist. Install it manually with one of:
    uv tool install skills-registry
    pipx install skills-registry
    python -m pip install --user skills-registry
```

The hint matches the cascade so the user can pick whichever installer they actually have. The wizard still finishes — the user gets a registry repo and the agent dot-folders populated, just not the MCP binary. They can install it manually later and the snippet's path will start working.

## Python legacy: `_ensure_mcp_entry_point` in `init.py`

The Go cascade replaced an older Python implementation in `src/skills_mcp/init.py:_ensure_mcp_entry_point`. The Python function is still present in the wheel because some users discovered the project before the `curl | sh` flow existed and run `uvx skills-registry bootstrap` — that path still works and still installs the MCP entry point. The Python implementation is otherwise dead weight for the canonical Go-first install flow.

The Python and Go implementations behave identically on the cascade order, the fallback dirs, and the success criterion. If we ever rev one, we have to rev the other (or, as the project's known-issues list flags, remove the Python implementation entirely and drop the `skills-registry bootstrap` console script).

## Key source files

| File | Role |
| --- | --- |
| `cli/internal/bootstrap/mcp_install.go` | The Go cascade: `EnsureMCPEntryPoint`, `MCPInstaller`, `entryPointPresent`, `candidates`, `printManualHint`. |
| `cli/cmd/skills-registry/bootstrap.go` | `locateMCPBinary` — must keep the same fallback-dir list. |
| `src/skills_mcp/init.py` | Legacy Python implementation. Still functional, no longer canonical. |
| `cli/cmd/skills-registry/wizard.go` | Calls `EnsureMCPEntryPoint(ctx)` at step 7. |

## Cross-links

- [Architecture](../overview/architecture.md) — where the install sits in the wizard's 8-step flow.
- [Agent catalogue](agent-catalogue.md) — the multi-select that runs the step before this one.
- [MCP server](../apps/mcp-server.md) — the binary this install puts on disk.
- [Getting started](../overview/getting-started.md) — the user-facing description of what `curl | sh` does.
