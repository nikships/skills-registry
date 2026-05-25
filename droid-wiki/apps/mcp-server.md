# MCP server

Active contributors: Nik Anand

## What it does

`skills-registry-mcp` is a FastMCP stdio server that exposes the user's GitHub-backed registry as three MCP tools. Desktop clients (Claude Desktop, Cursor, VS Code/Copilot) spawn it as a subprocess; the agent calls `list_skills`, `get_skill`, or `publish_skill` and the server proxies through to GitHub via the user's authenticated `gh` CLI.

The entire server is one Python file: `src/skills_mcp/registry_server.py`. The Git Data API client it delegates to lives in `src/skills_mcp/registry_api.py`. There is no embedded HTTP client, no `git` shell-out, and no SSH dependency — desktop MCP clients spawn the subprocess with a stripped environment, so every assumption baked in has to survive without `PATH` extensions or `SSH_AUTH_SOCK`.

## `build_server()` — boot-time validation

`build_server()` runs three checks before returning the `FastMCP` instance:

1. `load_config()` reads `~/.config/skills-mcp/registry.toml` (or `$SKILLS_REGISTRY` if set). Missing or malformed config raises `ConfigError`.
2. `ensure_authed()` locates `gh` via `find_gh()` (PATH + curated fallback dirs) and runs `gh auth status`. Missing binary raises `GhNotFoundError`; unauthenticated session raises `GhNotAuthedError`.
3. `RegistryClient(repo=…, default_branch=…)` is constructed and its `gh` attribute is set to the already-resolved path so later calls skip re-lookup.

The `FastMCP` constructor itself receives `name="skills-registry"`, an `instructions` blurb pointing at the configured repo, and `version=__version__` (resolved from installed package metadata via `importlib.metadata`).

Boot-time failures are surfaced to the desktop client as exit codes — never as silent retries. See [exit codes](#exit-codes) below.

## Tool registration

Each tool is registered through the `@server.tool(...)` decorator inside `_register_tools(server, client, repo)`. Three tools, three annotation profiles:

| Tool | `readOnlyHint` | `destructiveHint` | `openWorldHint` | Returns |
| --- | --- | --- | --- | --- |
| `list_skills` | ✓ | — | ✓ | Markdown table (slug / name / description). |
| `get_skill` | ✓ | — | ✓ | Absolute path on disk to the cached folder. |
| `publish_skill` | — | ✓ | ✓ | `"Published \`<slug>\` to <repo>@<sha7>. View: …"`. |

The annotations are surfaced to MCP clients so they can gate destructive operations behind explicit user approval. `openWorldHint=True` on all three signals that the tool touches the network.

### `list_skills`

Lists every skill via `client.list_skills()` and formats a markdown table. Empty registries return `"No skills found in <repo>."`. Pipe characters and newlines in descriptions are escaped (`|` → `\|`, `\n` → space) so the rendered table stays well-formed.

### `get_skill`

Implements a tree-SHA-aware cache:

1. `client.get_folder_sha(slug)` returns the current SHA of the `<slug>/` subtree, or `None` if missing.
2. `cache.lookup(slug)` returns the cached path + recorded SHA, or `None` on miss.
3. If both SHAs match → return the cached path immediately (cache hit).
4. Otherwise wipe the cached dir via `cache.reserve(slug)`, call `client.download_skill(slug, dest)`, then `cache.commit(slug, current_sha)` to record the new SHA in `<slug>.meta.json`.

Force-pushes invalidate correctly because the SHA changes. See [systems/registry-client](../systems/registry-client.md) and `src/skills_mcp/cache.py`.

### `publish_skill`

Accepts `name` (required) plus exactly one of `files` (a dict of relative paths → text content) or `local_folder` (an absolute path containing `SKILL.md`). Passing both or neither raises `ValueError`.

The control flow:

1. `slugify(name)` produces the canonical slug.
2. If `files` was passed, every key is run through `_normalize_rel_path` and every value is encoded to bytes via `_encode_text` (rejects non-strings with a `TypeError`).
3. If `local_folder` was passed, `_collect_local_folder(folder)` walks the tree, skipping hidden entries (any path component starting with `.`) and `__pycache__`.
4. `SKILL.md` must be present at the root of the payload, or the call raises `ValueError`.
5. `_validate_size(payload)` rejects any file larger than `_MAX_FILE_BYTES`.
6. `client.publish_skill(slug, payload)` runs the six-call Git Data API sequence (GET ref → GET commit → GET trees → POST blobs → POST trees → POST commits → PATCH ref) with retries on 409/422.

## Path-traversal hardening

`_normalize_rel_path(raw)`:

```python
rel = raw.replace(os.sep, "/")
while rel.startswith("./"):
    rel = rel[2:]
rel = rel.lstrip("/")
if ".." in rel.split("/"):
    raise ValueError(f"Refusing path with '..' segments: {raw!r}")
return rel
```

Three layers of defense:

1. `os.sep → /` so backslash-encoded traversals on Windows can't sneak through.
2. Leading `./` stripped iteratively so `././../etc` normalizes correctly.
3. Leading `/` stripped so absolute-path injection (`"/etc/passwd"`) becomes `"etc/passwd"`.
4. After splitting on `/`, any segment equal to `..` is rejected outright.

`_collect_local_folder` additionally skips any path with a dotfile component anywhere in the tree, so `.git/HEAD` and `.DS_Store` never reach the registry. The same hardening is mirrored on the Go side in `cli/internal/registry/registry.go`.

## File-size cap

`_MAX_FILE_BYTES` is `2 * 1024 * 1024` (2 MiB) by default. Override with the `SKILLS_MAX_FILE_BYTES` environment variable:

```python
_MAX_FILE_BYTES = int(os.environ.get("SKILLS_MAX_FILE_BYTES", str(2 * 1024 * 1024)))
```

`_validate_size(payload)` checks every file before the publish call goes out and raises `ValueError` listing the offending file's byte count and the cap. The Go CLI's `publish.go:collectFiles` enforces the same 2 MiB ceiling.

## Logging

`main()` configures the root logger from `$SKILLS_LOG_LEVEL` (default `INFO`), writing to stderr in a `%(asctime)s %(levelname)s %(name)s: %(message)s` format. The server's own logger is `skills_mcp.registry_server`; the API client logs under `skills_mcp.registry_api`. Stderr is the only safe sink because stdio is the MCP transport.

## Exit codes

`main()` translates boot failures into a small, stable set of process exit codes so desktop clients can show targeted error UI:

| Code | Triggered by | Meaning |
| --- | --- | --- |
| `0` | clean stdio close | normal exit |
| `2` | `ConfigError` | `~/.config/skills-mcp/registry.toml` is missing, malformed, or has no `repo` |
| `3` | `GhNotFoundError` | `gh` not found on `PATH` or in the curated fallback dirs |
| `4` | `GhNotAuthedError` | `gh` is present but `gh auth status` failed |

Once `server.run()` is reached, the FastMCP runtime owns the lifecycle and any tool-call error is returned to the client as an MCP error response rather than a process exit.

## Key source files

| File | Role |
| --- | --- |
| `src/skills_mcp/registry_server.py` | `build_server()`, tool registration, path-traversal hardening, `main()` with exit codes. |
| `src/skills_mcp/registry_api.py` | `RegistryClient` — gh-api wrapper, atomic Git Data API publish/delete, retry on 409/422. |
| `src/skills_mcp/gh.py` | `find_gh()`, `ensure_authed()`, `gh_api()` — every GitHub call goes through these. |
| `src/skills_mcp/config.py` | TOML config read/save; honors `$SKILLS_REGISTRY` override. |
| `src/skills_mcp/cache.py` | `lookup()` / `reserve()` / `commit()` with `<slug>.meta.json` tree-SHA storage. |
| `src/skills_mcp/frontmatter.py` | `parse_frontmatter` + `first_paragraph` — YAML-ish parser, no PyYAML. |
| `src/skills_mcp/init.py` | Legacy `skills-registry bootstrap` console script (still in wheel for back-compat). |

## Cross-references

- [overview/architecture](../overview/architecture.md) — the two upload paths and why they differ.
- [overview/getting-started](../overview/getting-started.md) — how to wire the server into Claude / Cursor / Codex / VS Code.
- [systems/registry-client](../systems/registry-client.md) — `RegistryClient` deep dive (Python and Go).
- [api/mcp-tools](../api/mcp-tools.md) — full tool signatures, argument schemas, and return shapes.
