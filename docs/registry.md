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

The returning-user dashboard is a card grid of seven tiles: Manage skills, Sync, Add, Discover, Publish, Purge local, and Settings. Each launches its flow embedded in the one long-lived `HubProgram`, so the terminal never drops back to scrollback between actions, and each flow's ending becomes a toast above the footer. The grid is responsive: four columns at ≥160, three at ≥120, two at ≥80, one below, which keeps seven tiles inside two rows on a wide terminal. `HubModel` measures that threshold against the width the grid is rendered at, not the raw terminal width.

The bulk initial import uses `git push` over HTTPS with credentials configured by `gh auth setup-git`. Day-to-day `publish`, `add`, `sync`, and `remove` operations use the GitHub Git Data API through the authenticated `gh` CLI. Reads use a shallow local mirror when available and fall back to `gh api`.

Every subcommand supports `--json`. The primary commands are `bootstrap`, `list`, `search`, `discover`, `get`, `sync`, `add`, `publish`, `remove`, and `update`.

## Discover

`search` ranks the user's own registry. `discover QUERY` is the outward-facing counterpart: it queries the public SkillNet index and returns importable GitHub URLs.

On a TTY it opens an interactive picker (see [Discover picker](#discover-picker)); `--json`, `--plain`, and a non-TTY stdout print the fixed-width table and download nothing.

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

- `skill_url` is exactly the `/blob/<sha>/<dir>` shape `registry.ParseGitHubURL` accepts, so a result feeds straight into `add` with no rewriting. Such an import is untrusted; see [Import gate](#import-gate).
- The three score fields carry SkillNet's `evaluation.<score>.level` (`Good`, `Average`, or `Poor`) and are **empty when the index has no score**. An absent score means unscored and must render as such (the CLI prints `unscored`); it must never be presented as a pass.
- The index's other fields are dropped, not passed through. Repository star counts in particular are never surfaced or ranked on: they belong to the host repository, not the individual skill.
- Rows are deduplicated on `(name, skill_url)`, first occurrence winning, so the index's own ranking order survives.
- `results` is always a non-nil slice, so it encodes as `[]` rather than `null`.
- Flags are `--mode keyword|vector` (default `keyword`), `--category`, `--limit` (default 10, capped at 50), `--plain`, and the persistent `--json`.

Transport and failure behavior:

- The endpoint is `SKILLS_DISCOVER_URL`, defaulting to `http://api-skillnet.openkg.cn/v1/search`. Tests and the macOS app override it.
- The endpoint is plain **HTTP**: the host serves a certificate that does not match it, so HTTPS cannot be verified and query terms travel in plaintext.
- Because of that, the request attaches no credentials at all — no GitHub token, no `gh` auth header, no cookie, no registry contents. The client builds its own `http.Request` rather than reusing any GitHub transport, and tests assert that no `Authorization`-class header and no token-bearing query parameter is ever sent.
- One 10-second timeout covers DNS through body read, and the response body is size-capped.
- Every failure (unreachable host, timeout, non-2xx, non-JSON body) fails closed: no partial results, exit 1, and `{"error": "..."}` on the `--json` path. The human-readable error states that `skills-registry add <github-url>` still works without the index.

Tests use `httptest` exclusively; no test contacts the live index.

## Discover picker

`tui.NewDiscoverFlow(ctx, tui.DiscoverFlowDeps{…})` is the interactive picker, and it is the entry point the hub's Discover card embeds. It is a flow rather than logic inside the cobra command for exactly that reason: the command hosts it in its own `tea.NewProgram`, and the hub will host the same model.

- `tui.DiscoverRow` is the picker's row type: name, description, author, category, `skill_url`, and the three grades. It deliberately carries **no star count**, so no surface can rank or headline on repository popularity, and the picker never re-sorts — the index's own ranking order stands.
- Rendering reuses the registry list's delegate and `styles.go` tokens through `DiscoverRow.listRow()` (name → title, category → the right-hand column, description → the same two-line budget). There is one row renderer, not two. The preview pane adds author, the three grades, and the `skill_url`.
- Grades render through `importgate.Scores.Lines()`, so an absent grade reads `unscored` here for the same reason it does everywhere else.
- **Ending:** with `WithOnExit` unset the flow returns `tea.Quit` and the caller reads `Toast()`, `Imported()`, and `Err()` off the final model; with it set the flow yields the host's message instead, which is how `HubProgram` will consume it (`flowExitMsg`). `Err()` is non-nil only for a search failure, so a cancelled pick exits 0 while an unreachable index exits 1.
- **Import:** Enter opens a pre-fetch confirmation naming the URL and the grades, then hands the row to `AddFlowModel` via `NewAddFlowFromSource`, which skips the source-input step. The picker therefore adds no trust, grade, or install logic of its own — the row travels the existing gate, registry-only by default. A row the index graded `Poor` for safety gets the cancelling answer as the confirm's default; the gate's own consent step still follows. The embedded add flow reports back through `addFlowExit` (`published` is authoritative, not the toast wording) and control returns to the list, so a user can import several rows in one session.
- The cmd side wires `deps.Add.Gate = importGateHook(cfg, true)`, i.e. the `--from-discover` gate: a picked row is untrusted whatever shape its URL has.
- **States:** a spinner while the HTTP call is in flight, then either the list or `discoverStateError`. A failed search renders the error box and no list at all — "the index is unreachable" and "the index has no match" must never look alike. Esc, `q`, an empty selection, and a declined confirmation all exit having written nothing.

The hub's Discover card hosts that same picker through `WithOnExit`. The subcommand takes its query as an argument and the hub has none, so `discoverHubFlow` (`cli/internal/tui/flow_discover_hub.go`) prompts for one first and constructs the picker only once it is submitted — nothing about the picker changes, and the index is contacted from the picker's own `Init`, never while the dashboard is rendered or idle. `tui.DiscoverHubDeps` carries the query-taking search hook and the same `buildDiscoverAddDeps` set the subcommand uses, so a hub pick travels the identical untrusted gate. `discoverFlowExit` translates the ending into the hub's toast: it reads `Imported()` rather than the picker's caption, because closing the picker overwrites that caption with a neutral "closed" and a session that did import would otherwise report nothing. A successful import refreshes the header's skill count the same way every other writing flow does.

Tests drive the flow with fixture rows and injected fakes (`failIfAddRuns` asserts zero calls on every cancelling path). No test contacts the index or writes to a registry.

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

## Import gate

Publishing a skill into the user's own registry is one revertible commit. Durably installing it into agent dot-folders is categorically different: from then on every agent loads that `SKILL.md` each session with no further prompt. `add` therefore separates the two and decides based on the source's origin.

`cli/internal/trust/` owns the classification. `trust.Assess(source, trust.Options{Owners, FromDiscover})` returns an `Assessment` whose `Origin` is one of `local_path`, `own_repo`, `public_repo`, `remote_git`, or `discover`; `Origin.Untrusted()` is false only for the first two. The package is offline and parses the source string, so a caller can classify before fetching anything. `Owners` is the logins the user controls (`add` passes the configured registry repo's owner) and is compared case-insensitively; an empty list makes every GitHub source third-party, which fails safe. A `discover` pick is untrusted whatever its URL shape, because the user did not choose the URL.

`cli/internal/importgate/` owns the review rules, apart from the cobra command so the CLI, the hub TUI, and the macOS app reach the same verdict:

- `importgate.Label` renders one grade, mapping an absent grade to `unscored`. **This is a correctness requirement.** The public index leaves a grade empty when it never evaluated the skill; an empty cell reads as "fine". `Scores.Lines()` always renders all three grades so a confirmation screen cannot silently omit one.
- `Scores.SafetyIsPoor()` is the blocker predicate. An unscored safety grade is not `Poor`, and is not a pass either: the import is still confirmed by the user.
- `importgate.Evaluate(slug, scores, findings)` returns a `Review` carrying `Blocks` for a `Poor` safety grade (`poor_safety`) and for any local scan hit (`injection_scan`). `Review.Blocked()` means the import needs explicit consent, not that it is forbidden.

`cli/internal/skillscan/` is the local scan: an offline regex pass over `SKILL.md` (frontmatter included, because a `description` is loaded as instructions too) in three categories — `prompt_injection`, `credential_exfiltration`, and `remote_execution`. There is no model, no network call, and no sandbox. Rules requiring a sink (a shell pipe, an HTTP POST) only fire when the sink is on the same line as the secret, which is what keeps documentation-shaped lines such as `curl -H "Authorization: Bearer $API_KEY" …` from warning. Output is bounded (three findings per rule, 24 overall) and input is capped at `MaxScanBytes`, so a hostile file cannot flood the terminal or stall an import. A clean result means "none of these patterns matched", never "safe"; `importgate.ScanDisclaimer` is the one-line statement of that, shown wherever findings are reported.

Behavior for an untrusted source:

| Path | Publish | Durable install | Blocker (`Poor` safety, or a scan hit) |
|---|---|---|---|
| Interactive | after the ordinary confirmation | only after an explicit yes, then the agent picker | extra confirmation whose default answer cancels |
| `--json` / `--yes` | yes | only with `--install` | refused; needs `--allow-unsafe` |

`--yes` deliberately does not clear a blocker: skipping prompts is not consent to import a skill graded unsafe. `--allow-unsafe` clears a blocker and nothing else — it never implies an install. On the `--json` path a refusal is both a `{"error": …}` payload naming `--allow-unsafe` and a non-zero exit, and nothing is published in that run. The payload also carries `source` (`origin`, `untrusted`, `reason`) and `install_skipped` / `install_skipped_reason` so a consumer never has to infer a skipped install from a short `installed` map.

The index lookup (`discover.Client.Lookup`, keyed by `discover.SkillKey` so a revision difference between the pasted URL and the index's row does not matter) is a convenience: a failure or a miss degrades to unscored rather than blocking the import. It also supplies the `category` stamped onto the imported copy; see [Import provenance](#import-provenance).

Trusted sources are unchanged. A local path and a repository under the user's own owner publish and install as before, `publish` and `sync` are untouched (`publish` has never durable-installed and still does not), and a trusted `add` does not consult the public index at all.

Nothing fetched is ever executed. `add` and `discover` copy `scripts/`, `references/`, and `assets/` as bytes; the only processes either spawns are `git` (clone-path sources) and `gh` (API calls). A test asserts that an executable `scripts/run.sh` in a fetched folder never runs.

The hub's Add flow and the Discover picker share the gate through `tui.AddFlowDeps.Gate`, which returns the data-only `tui.ImportGate` (pre-rendered score lines, findings, and block summary) built by `hubGateView`. `importGateHook(cfg, fromDiscover)` builds the hook: the hub passes `false`, the Discover picker passes `true`. Rendering decisions are made once on the cmd side so the surfaces cannot disagree about whether a grade is missing. The hook also receives the resolved directory, because the same call that reviews the fetched files is what then stamps them. The macOS Discover pane is a separate ticket; `trust` and `importgate` are the surfaces it should call.

## Import provenance

An imported skill is a copy. The registry commit records where it came from, but a commit message is not where an agent, a reviewer, or a shallow clone looks. So an **untrusted** import writes two extra frontmatter keys onto the copy before publishing it:

```yaml
---
name: summarize
description: Summarize URLs and PDFs.
category: AIGC
source_url: https://github.com/openclaw/openclaw/tree/<sha>/skills/summarize
---
```

`cli/cmd/skills-registry/provenance.go` owns the stamp; `Frontmatter.merging` in `mac-app/Sources/SkillsRegistryCore/Frontmatter.swift` is the Swift mirror of `mergeFrontmatter`, and the two must stay in step.

Rules:

- **Untrusted only.** `trust.Assess` decides, through the same `gate` the import review uses. A local folder and a repository under the user's own owner publish byte-for-byte as before, and `publish` of a local folder never gains these keys — a folder the user wrote is not an import.
- **The stamp runs after the gate, before the first write.** `skillscan` must read the stranger's file, not one the stamp has already edited; the registry must receive the annotated copy.
- **`source_url` names the folder**, so it ends in the skill's own directory. It is rebuilt from `registry.ParseGitHubURL` plus the skill folder's position under the fetch root, which keeps a folder-of-skills import honest: each skill gets its own subfolder URL rather than the parent's. A `/blob/` URL naming `SKILL.md` resolves to its directory, matching what the fetch did. A source that named no ref (`owner/repo`, a bare repository URL) is pinned to `HEAD` rather than to a guessed branch. A non-GitHub remote has no folder-URL form to derive, so the source string is recorded as given, minus any userinfo.
- **`category` comes from the index row** and is omitted when the index has no row or no category for it: an invented category is worse than an absent one. The value is third-party text written into a file agents load, so it is collapsed to one line and clipped to 64 runes, and `yamlScalar` quotes anything that could smuggle a second key into the block.
- **An existing key is never overwritten** unless its value is empty (`category:`, `category: ""`). An indented `category:` inside a block scalar is that scalar's text, not a top-level key.
- **Unrelated lines do not churn.** The document is edited line by line rather than parsed and re-serialized, so key order, comments, block scalars, and quoting style survive byte-for-byte; a missing key is appended just before the closing `---`. A document with no frontmatter gains a block holding only these keys. A document whose block is never closed is left untouched, because guessing where its metadata ends would risk rewriting the body.

Both parsers already read flat YAML into a map and ignore keys they do not know, so the extra keys break nothing that reads frontmatter. `scan.Skill` now surfaces them as `Category` and `SourceURL`, both empty for a skill that does not carry them — which includes every skill published before the stamp existed. Swift `Frontmatter.parseSummary` still returns name and description with the extra keys present, and a test pins that.

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
