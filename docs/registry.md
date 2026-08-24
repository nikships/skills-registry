# Skills Registry architecture

Skills Registry has four user-facing surfaces: release installers and the npm launcher, the Go CLI, the native macOS app, and the static website. Skills remain in a GitHub repository owned by the user; no additional registry service is required.

## Distribution

| Surface | Source | Distribution |
|---|---|---|
| Shell installers | `install.sh`, `install.ps1` | Select and install a GitHub Release binary. |
| npm launcher | `npm/` | `npx skills-registry`; downloads and executes the matching release binary. |
| Go CLI | `cli/` | Release binaries for macOS, Linux, and Windows on amd64 and arm64. |
| macOS app | `mac-app/` | Signed and notarized native SwiftUI app. |
| Website | `website/` | Static Next.js landing site. |

Native macOS CI and Darwin CLI releases use the dedicated Aqua-session self-hosted runner labeled `mac-mini`. Linux, Windows, and untrusted fork jobs remain on GitHub-hosted runners. See [`.github/AGENTS.md`](../.github/AGENTS.md).

## CLI flow

Bare `skills-registry` opens the onboarding wizard when config is absent, the dashboard when config exists, and help for non-interactive or `--json` invocation.

The onboarding wizard has seven steps:

1. Scan known agent skill directories.
2. Choose the GitHub repository name.
3. Choose repository visibility.
4. Create and populate the repository with one authenticated `git push`.
5. Select agents and install their generated gateway skill.
6. Optionally remove redundant local skill copies.
7. Show the resulting registry URL and completion summary.

The headless `skills-registry bootstrap` command performs the same setup and ends by printing the registry URL. Neither flow emits configuration for another protocol or service.

The bulk initial import uses `git push` over HTTPS with credentials configured by `gh auth setup-git`. Day-to-day `publish`, `add`, `sync`, and `remove` operations use the GitHub Git Data API through the authenticated `gh` CLI. Reads use a shallow local mirror when available and fall back to `gh api`.

Every subcommand supports `--json`. The primary commands are `bootstrap`, `list`, `search`, `discover`, `get`, `sync`, `add`, `publish`, `remove`, and `update`.

## Discover

`search` ranks the user's own registry. `discover QUERY` is the outward-facing counterpart: it queries the public SkillNet index and returns importable GitHub URLs. It is headless, has no TUI, and downloads nothing.

The client lives in `cli/internal/discover/`, deliberately separate from the cobra command so the TUI, the hub, and the macOS app can import it. `discover.Client.Search` is the only entry point; `discover.Response` is the published payload:

```json
{
  "source": "skillnet",
  "query": "pdf",
  "mode": "keyword",
  "results": [
    {
      "name": "summarize",
      "description": "…",
      "author": "openclaw",
      "category": "AIGC",
      "skill_url": "https://github.com/openclaw/openclaw/blob/<sha>/skills/summarize",
      "safety": "Good",
      "completeness": "Good",
      "executability": "Good"
    }
  ]
}
```

Contract rules:

- `skill_url` is exactly the `/blob/<sha>/<dir>` shape `registry.ParseGitHubURL` accepts, so a result feeds straight into `add` with no rewriting.
- The three score fields carry SkillNet's `evaluation.<score>.level` (`Good`, `Average`, or `Poor`) and are **empty when the index has no score**. An absent score means unscored and must render as such (the CLI prints `unscored`); it must never be presented as a pass.
- The index's other fields are dropped, not passed through. Repository star counts in particular are never surfaced or ranked on: they belong to the host repository, not the individual skill.
- Rows are deduplicated on `(name, skill_url)`, first occurrence winning, so the index's own ranking order survives.
- `results` is always a non-nil slice, so it encodes as `[]` rather than `null`.
- Flags are `--mode keyword|vector` (default `keyword`), `--category`, `--limit` (default 10, capped at 50), and the persistent `--json`.

Transport and failure behavior:

- The endpoint is `SKILLS_DISCOVER_URL`, defaulting to `http://api-skillnet.openkg.cn/v1/search`. Tests and the macOS app override it.
- The endpoint is plain **HTTP**: the host serves a certificate that does not match it, so HTTPS cannot be verified and query terms travel in plaintext.
- Because of that, the request attaches no credentials at all — no GitHub token, no `gh` auth header, no cookie, no registry contents. The client builds its own `http.Request` rather than reusing any GitHub transport, and tests assert that no `Authorization`-class header and no token-bearing query parameter is ever sent.
- One 10-second timeout covers DNS through body read, and the response body is size-capped.
- Every failure (unreachable host, timeout, non-2xx, non-JSON body) fails closed: no partial results, exit 1, and `{"error": "..."}` on the `--json` path. The human-readable error states that `skills-registry add <github-url>` still works without the index.

Tests use `httptest` exclusively; no test contacts the live index.

## Add sources

`add` accepts a local directory, `owner/repo` shorthand, any git URL, and GitHub folder URLs in both the `/tree/` and `/blob/` forms:

| Source | Resolution |
|---|---|
| `./path` | Used in place. |
| `owner/repo` | `git clone --depth=1 --single-branch https://github.com/owner/repo.git` |
| `https://github.com/owner/repo` | Shallow clone of the default branch. |
| `https://github.com/owner/repo/tree/<branch>` | Shallow clone with `--branch <branch>`. |
| `https://github.com/owner/repo/{tree,blob}/<ref>/<dir>` | Recursive GitHub Contents API fetch of `<dir>` only, no clone. |
| Any other git URL | Cloned as-is. |

For a folder URL, `<ref>` may be a branch (including one containing slashes), a tag, or a full commit SHA; every Contents request is pinned to it so a moving branch cannot mix revisions. A branch name with slashes is disambiguated by probing successive `<ref>/<path>` splits, most-likely first, and treating a 404 as the wrong split. A `/blob/` link naming a file resolves to that file's directory, because the public skill index links `SKILL.md` itself. Paths from the API response are rejected unless every component is a safe single segment and the joined path stays inside the fetch directory, so a hostile response cannot write outside it. A folder with no `SKILL.md`, an empty folder, and a missing ref or path each fail with a message naming the resolved URL.

The parser and fetch live in `cli/internal/registry/subtree.go` (`registry.ParseGitHubURL`, `registry.Fetcher`), next to the other GitHub helpers. The macOS app mirrors both in `mac-app/Sources/SkillsRegistryCore/GitHubTarget.swift` and `GitHubSubtree.swift`; the two implementations must accept exactly the same URL shapes, and their table tests are kept in lockstep.

## Configuration and cache

Configuration resolution is:

1. `SKILLS_REGISTRY=owner/repo[@branch]` for a process-local override.
2. `~/.config/skills-registry/registry.toml`, or `$XDG_CONFIG_HOME/skills-registry/registry.toml`.

Downloaded skills, metadata, and repository mirrors live under `~/.cache/skills-registry/`, or `$XDG_CACHE_HOME/skills-registry/`.

```toml
[registry]
repo = "alice/skills-registry"
default_branch = "main"
```

## Gateway skill

Bootstrap writes `<agent-dir>/skills/skills-registry/SKILL.md` for each selected agent. This generated gateway is CLI-only: it tells agents to use `skills-registry search`, `get`, and the other JSON-capable commands to discover and manage skills on demand.

The agent catalogue remains centralized in `cli/internal/agents/agents.go`. `.mcpjam/` is retained as the supported MCPJam agent directory; that directory name is agent compatibility, not a service dependency.

## macOS app

The app manages the same registry and configuration as the CLI. It supports GitHub device-flow login, browsing and fuzzy search, publish/remove, bulk import, and CLI installation. Shared slug, frontmatter, fuzzy-scoring, GitHub-write, add-source URL parsing, and gateway-template contracts must remain aligned between Go and Swift. The app's Add field takes the same folder URLs as the CLI and likewise fetches only that folder.

## Verification

CLI changes should pass `go vet`, `gofmt`, `staticcheck`, `deadcode`, `gocyclo`, build, and tests. macOS changes use Swift build and test jobs. Release automation builds CLI archives, signs and notarizes Darwin binaries, creates a GitHub release, and then publishes the thin npm wrapper when credentials are available.
