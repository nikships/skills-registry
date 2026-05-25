# Security

Active contributors: Nik Anand

`skills-registry` deliberately keeps its security surface narrow. There is no embedded HTTP client, no SSH dependency, no token handling in our code. This page captures the threat model, the trust anchors, the hardening that lives in the codebase, and how to report a vulnerability.

## Threat model

`skills-registry` ships two things that touch the network or write to disk:

- The **MCP server** (`src/skills_mcp/registry_server.py`) runs as a long-lived subprocess inside a desktop MCP client (Claude Desktop, Cursor, VS Code/Copilot). It reads from and writes to the user's personal GitHub registry repo on behalf of the model.
- The **Go CLI** (`cli/cmd/skills-registry/`) runs interactively in the user's terminal. The first-run wizard bulk-imports local skills; day-to-day commands publish/list/sync against the same registry repo.

The realistic threats are:

1. A malicious skill payload tries to escape its slug directory and overwrite arbitrary files on the registry (or on disk during cache fetch).
2. An attacker controls a file `publish_skill` is asked to upload, and tries to ship a huge binary or a sensitive byproduct (`.git/`, `__pycache__/`).
3. An attacker controls the registry repo the user has configured and tries to ship a malicious cache payload to be invoked by the model.

Out of scope:

- Risks inherent to the model or MCP client **consuming** a skill. Skills are user-controlled prompts; jailbreaks, prompt injection, and unsafe instructions inside skill content are the user's call. We do not sanitize skill bodies.
- Issues that require the attacker to already have write access to the user's registry repo or local cache. The registry's integrity is anchored on GitHub's auth; we do not re-implement it.

## Trust anchors

Three properties together form the trust boundary. They are non-negotiable; if you find yourself relaxing any of them in a PR, push back.

### 1. `gh` is the only credential anchor

`gh auth status` is the sole authentication check. If it fails, every command in both the Python server and the Go CLI exits before touching the network. See `src/skills_mcp/gh.py:ensure_authed` and `cli/internal/registry/registry.go` for the matching checks.

`find_gh` / `FindGH` walk the process `PATH` plus a curated fallback list (`~/.local/bin`, `/opt/homebrew/bin`, `/usr/local/bin`, `/usr/bin`). This matters in the MCP-server environment: desktop MCP clients spawn the server in a stripped subprocess where the user's shell-extended `PATH` is gone, `SSH_AUTH_SOCK` is unset, and `git config user.email` may be missing. The fallback list keeps `gh` discoverable in that environment.

### 2. The MCP server is gh-only — no SSH, no git, no embedded HTTP

The Python `RegistryClient` (in `src/skills_mcp/registry_api.py`) calls **only** `gh api`. The six-call atomic publish sequence (ref → commit → base tree → blobs → new tree → new commit → ref update) is implemented as a series of `gh api` subprocess invocations. There is no `requests`, no `httpx`, no `paramiko`, no `git` subprocess in the MCP server path. See [systems/registry-client.md](systems/registry-client.md) for the call-by-call breakdown.

### 3. The CLI bootstrap path is git-over-HTTPS, with credentials owned by `gh`

The one place we shell out to `git` is `cli/internal/registry/registry.go:PushTreeViaGit`, used only by the wizard's bulk bootstrap import. It works as follows:

1. `gh auth setup-git --hostname github.com` — idempotent. Writes a credential-helper entry in `~/.gitconfig` pointing at `gh`.
2. `gh api user` → commit author name/email (falls back to `<login>@users.noreply.github.com`).
3. `git init -b main` in a tempdir (or shallow clone of the upstream branch if it exists).
4. Materialize every file. `git add -A`. `git commit`. `git push -u origin main`.
5. `os.RemoveAll` the tempdir on exit.

The token never appears in argv, env, or on disk. Persistent state is limited to the credential-helper entry `gh auth setup-git` writes — which is the standard `gh` workflow, not a `skills-registry`-specific artifact.

The rationale for using git push here (and only here) is the per-file rate limit on `POST /git/blobs`; see [background/design-decisions.md](background/design-decisions.md#why-two-upload-paths-gh-api-vs-git-push).

## Hardening

### Process invocation

- **Python:** every external call is `subprocess.run([...], ...)` with list args. No `shell=True`. No `sh -c`. No string-interpolated commands.
- **Go:** every external call is `exec.CommandContext(ctx, ...)` with list args. No shell interpolation.

If you add a new subprocess invocation, follow the same pattern. Reviewers will catch `shell=True` / `sh -c`.

### Path traversal hardening

The same logic is implemented in both runtimes:

- `_normalize_rel_path` (Python, `src/skills_mcp/registry_api.py`)
- `validateRelPath` (Go, `cli/internal/registry/registry.go`)

Rejected inputs:

- Paths starting with `/` (absolute).
- Paths containing `..` segments after normalization (`..`, `../`, `/../`).
- Backslash-encoded traversals (Python — `..\\path` and friends).
- Windows volume names (`C:`, `D:`, …) when running `PushTreeViaGit` on a non-Windows host.

Both `publish_skill` (Python) and `Client.Publish` (Go) skip dotfiles (`.git`, `.DS_Store`, …) and `__pycache__/` when walking a candidate skill directory. The `PushTreeViaGit` path applies the same traversal rejection before writing any file to disk.

### Per-file size cap

`SKILLS_MAX_FILE_BYTES` (default 2 MiB, see [reference/configuration.md](reference/configuration.md)) protects against accidental uploads of huge binaries — not malicious payloads. The Python publish flow rejects files exceeding the cap; the Go publish flow warns and skips them.

### Cache invalidation

`~/.cache/skills-mcp/skills/<slug>/` is rewritten whenever the registry's `<slug>/` tree SHA changes. The meta file (`<slug>.meta.json`) stores the SHA recorded at last fetch; the next call compares it to the live SHA and wipes the cache directory on mismatch. Force-pushes correctly invalidate. See `src/skills_mcp/cache.py`.

The cache is **not** signature-verified. If an attacker controls the registry repo, they can ship arbitrary content into the cache directory. This is by design — the cache mirrors what GitHub serves, and GitHub's auth is the integrity anchor.

## Scope summary

- **MCP server surface:** three tools (`list_skills`, `get_skill`, `publish_skill`) plus the on-disk cache. Documented in [api/index.md](api/index.md).
- **CLI surface:** seven subcommands (`bootstrap`, `list`, `get`, `sync`, `add`, `publish`, `remove`) plus the wizard/hub TUI launched by bare invocation.

Anything outside those surfaces is not part of `skills-registry`.

## Reporting

Report vulnerabilities via **GitHub Security Advisories**: <https://github.com/anand-92/skills-registry/security/advisories/new>.

Please do not file public issues for security reports.

See also: [systems/registry-client.md](systems/registry-client.md) for the implementation of the atomic publish/delete sequence and the conflict-retry logic.
