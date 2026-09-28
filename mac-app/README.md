# Skills Registry.app (macOS)

A native, Apple-Silicon SwiftUI app for managing your skills registry end to
end: sign in with GitHub, create or connect a registry repo, browse skills with
rich markdown rendering and fuzzy search, edit a skill's `SKILL.md` in
place (the editor takes focus on open; Escape or Cancel asks before
discarding unsaved text; switching skills or sections keeps the draft and
restores it on return), publish a skill from a folder,
**install** a registry skill into your agent folders, **discover** third-party
skills in the public index and import one behind the import gate, **add**
skills from an external source (local path, `owner/repo`, a git URL, or a
GitHub `/tree/<ref>/<path>` link) and publish + install them in one pass,
**remove** one end-to-end (registry + local downloads + agent folders),
bulk-import the skills already sitting in your local AI-tool folders, and
install or update the CLI from Settings. To point the app at a different
registry, use **Switch registry…** in the Settings Registry card or the
sidebar account menu — it returns to the create/connect flow without signing
you out, and connecting writes the new `registry.toml` and refreshes skills.

It complements the Go CLI with the same registry format, slug derivation,
fuzzy scorer, and frontmatter parsing.

> **Platform:** macOS 14+ (Sonoma), arm64 only. Swift 6 toolchain, SwiftPM (no
> `.xcodeproj`). One UI dependency: [MarkdownUI](https://github.com/gonzalezreal/swift-markdown-ui).

---

## Quick start

```bash
cd mac-app

# Build + run the unit tests (Core contract + cross-language corpus).
swift test

# Assemble a runnable, ad-hoc-signed bundle → build/Skills Registry.app
bash scripts/bundle.sh             # debug
bash scripts/bundle.sh --release   # optimized

open "build/Skills Registry.app"
```

To explore the full authed UI without GitHub credentials, run in **demo mode**:

```bash
open "build/Skills Registry.app" --args --demo
# or
SKILLS_APP_DEMO=1 open "build/Skills Registry.app"
```

Demo mode injects fixture skills, identity, and detail markdown; every network
call is short-circuited, so you can drive the whole app offline. Demo is
fully side-effect free by construction:

- The Keychain is rerouted to a process-local in-memory dictionary, so Sign
  out in demo can never delete your real saved token.
- Demo uses its own `UserDefaults` suite (`dev.skills-registry.app.demo`), so
  accent/theme, dismissal, and check-timestamp state never leaks between demo
  and real instances.
- CLI and agent-skill installs are simulated with fixture state — no network,
  no `~/.local/bin` or dot-folder writes.
- Every other write path (Import, Add publish, Discover import, publish,
  install, remove) shows an honest `Demo mode: would …` info toast instead of
  silently doing nothing.
- The detail pane's GitHub button is disabled with a tooltip saying why, so
  a demo session cannot open the fixture repository. Copy copies whichever
  file is on screen and names that file in the toast.

`--demo-empty` / `SKILLS_APP_DEMO_EMPTY=1` starts with an empty registry so
the Browse welcome card a new user sees after create/connect is reachable.
Several demo-only drivers make
otherwise-unreachable states reachable: a
Discover query starting with `!` fails the search the way an unreachable index
would (error state, fallback hint, and retry, all offline); the Discover
category field filters the fixtures the way the live index filters server-side;
the Discover pane arrives with a query already run; and the Add pane arrives
with an untrusted fixture source (`AppState.demoAddSource`, resolving to the
Poor-safety fixture row) already fetched, so the gated states are reachable
without typing. That same fixture's `SKILL.md` (`AppState.demoScanHitMarkdown`)
is run through `SkillScan`, so the Add banner lists the hits and Discover's
pdf-scraper import holds on the same acknowledgement after you confirm.
`--demo-scan-sheet` opens that held confirmation immediately (still demo-only;
the findings are the scanner's). Any other source typed into Add classifies
through the real `AddGate.build`, degrading to unscored when no fixture row
matches, and scans clean.

Two more demo-only launch arguments render the login error states with no
Keychain or network touch (for review screenshots): `--demo-auth-expired`
shows the "session expired, sign in again" re-auth prompt, and
`--demo-auth-offline` shows the retryable offline failure with its Retry
button. An expired or revoked token anywhere else in the app routes back to
that same login prompt automatically.

To drive the **Setup screen** (create / connect) without completing real auth,
use the demo-only Setup fixtures:

```bash
open "build/Skills Registry.app" --args --demo-setup
# or: SKILLS_APP_DEMO_SETUP=1 open "build/Skills Registry.app"
```

`--demo-setup` renders Setup with a signed-in fixture identity and two
fixture installations. `--demo-setup-loading` (or
`SKILLS_APP_DEMO_SETUP=loading`) instead holds the installation-list spinner
with an empty list, so the loading state can be inspected. Both are
offline-only fixtures for screenshots and cua-driver runs; production code
never reads them.

call is short-circuited, so you can drive the whole app offline. Sparkle
failures can't be produced offline, so **Settings → App** also shows a
demo-only "Simulate check failure" button that seeds the failure hint + toast
exactly as a real failed check would.

Demo-only failure drivers (for exercising error states without a network):

- **Add:** a source starting with `!` fails the fetch with a canned
  "repository not found" reason (e.g. `!owner/repo`), so the Fetch-failed
  empty state renders its detail line.

`--demo-hover` (or `SKILLS_APP_DEMO_HOVER=1`, only honored together with demo
mode) paints the first Browse row in the hover state. The UI driver cannot
move the OS pointer inside a window, so this is how a screenshot shows that
treatment. It does nothing outside demo mode.
Demo mode also injects install repos, so the Setup create/connect flow is
reachable via **Switch registry…**.
The signed-out card is drivable too: the real GitHub mark and the
permission scope note render in demo without a device flow.

Two extra demo-only launch arguments exist for search screenshots and
automation (both inert unless demo mode is active; production behavior is
unchanged when they are absent):

```bash
open "build/Skills Registry.app" --args --demo --demo-extra-skills 15 --demo-query zzztest
```

- `--demo-extra-skills N` appends N synthetic `zzztest_skill_NN` fixtures so
  Browse can be shown with more matches than the headless top-10 search cap.
- `--demo-query TEXT` presets the Browse search field without keystrokes.

Two extra demo-only launch arguments simulate Browse refresh states (no-ops
outside demo mode), so the loading and failure feedback can be exercised and
screenshotted without a network:

```bash
open "build/Skills Registry.app" --args --demo --demo-refresh-fail  # every refresh fails: stale list + retry banner + error toast
open "build/Skills Registry.app" --args --demo --demo-refresh-slow  # every refresh takes ~3s: spinning refresh button
```

```bash
# Preselect a skill in Browse on launch (screenshot hook; ignored in real mode)
open "build/Skills Registry.app" --args --demo --demo-select react_review
```

Two demo-only fixtures cover the detail-header edge cases: `plain_notes` (no
frontmatter block, so the name falls back to the slug) and `unclosed_draft`
(a `---` block that never closes, so the raw file renders with a header
hint). Pass `--demo-select=<slug>` to land directly on one skill's detail
pane for screenshots:

```bash
open "build/Skills Registry.app" --args --demo --demo-select=plain_notes
```

### Keyboard shortcuts

The **Navigate** menu mirrors every shortcut. Focused text fields show an
accent ring.

| Shortcut | Action |
|---|---|
| `⌘F` | Focus the current section's search / source field (Browse, Discover, Add) |
| `⌘R` | Refresh: reload the Browse list, re-run the Discover search, rescan Import |
| `⌘1`–`⌘5` | Switch to Browse · Discover · Add · Import · Settings |
| `⌘,` | Open Settings |
| `Return` | Confirm: sign in, create/connect, sheet confirm buttons |
| `Esc` | Cancel sheets |

Append `--demo-select <slug>` (demo mode only) to open Browse with that
fixture skill's detail pane preselected — a screenshot helper for states
synthetic clicks can't reach, since browse rows use `onTapGesture`:

```bash
open "build/Skills Registry.app" --args --demo --demo-select brand_voice
```

Demo-only screenshot fixture: `--demo-truncated-list` (alongside `--demo`)
pretends the browse fetch hit GitHub's truncated tree listing, so the
"Results incomplete" banner renders without a huge registry.

---

## Architecture

Two SwiftPM targets:

| Target | Kind | Job |
|---|---|---|
| `SkillsRegistryCore` | library | Pure-Foundation logic: GitHub REST/auth, registry contracts, scan, CLI install. **No SwiftUI** — fast to compile and the single source of truth the UI drives. Fully unit-tested. |
| `SkillsRegistry` | executable (`@main`) | SwiftUI app: theme, routing, every view, demo mode. Depends on Core + MarkdownUI. Exercised via cua-driver in demo mode. |

```
Sources/SkillsRegistryCore/
  AppConfig.swift       GitHub App client_id and slug, project repo, CLI install path
  Models.swift          SkillSummary, SkillDetail, RepoRef, Identity, InstallationRepo, LocalSkill
  PaneState.swift       per-pane navigation snapshots hoisted into AppState (Browse/Discover/Add/Import)
  Slug.swift            slugify  ── shared cross-language contract
  FuzzyScore.swift      fzf-V1 scorer ── shared cross-language contract
  Frontmatter.swift     parseSummary/body/flat-YAML ── shared cross-language contract
  RegistryConfig.swift  ~/.config/skills-registry/registry.toml R/W (XDG-aware, SKILLS_REGISTRY override)
  Keychain.swift        user-to-server token storage
  Agents.swift          56-entry dot-folder catalogue (port of cli/internal/agents)
  Scan.swift            local skill discovery + filesForUpload
  SourceResolver.swift  resolve add source (local/owner-repo/git URL/folder link) → dir
  Discover.swift        public skill-index client ── mirrors cli/internal/discover
  ImportGate.swift      grades, trust origins, write policy, provenance stamp ── mirrors importgate/trust
  SkillScan.swift       heuristic injection scan ── mirrors cli/internal/skillscan
  GitHubTarget.swift    parse github.com repo/tree/blob URLs ── shared cross-language contract
  GitHubSubtree.swift   fetch one folder via the Contents API (port of registry/subtree.go)
  LocalInstall.swift    write a skill's files into <agent>/skills/<slug>/ (port of install_local.go)
  LocalRemove.swift     clear CLI downloads + sweep agent dot-folders
  DeviceFlow.swift      GitHub App Device Flow (browser login, no client secret)
  GitHubAPI.swift       request plumbing + wire models
  GitHubReads.swift     currentUser, installations, listSkills, getSkill, skillFileData
  GitHubWrites.swift    createRepo, publish, delete, bulkPush (atomic Git Data API)
  CLIInstaller.swift    one-click CLI install (mirrors install.sh)
  SkillMdTemplate.swift skills-registry/SKILL.md renderer ── byte-identical to skillmd.go
  MetaSkill.swift       detect / install / refresh the skills-registry meta-skill per agent
  Updates.swift         Semver + release channels (CLI vs macApp) + latest-release lookup
  Subprocess.swift      async Process wrapper

Sources/SkillsRegistry/
  SkillsRegistryApp.swift  @main + RootView router + demo detection + Sparkle wiring
  AppState.swift           @MainActor ObservableObject orchestrating everything
  UpdaterManager.swift     Sparkle SPUStandardUpdaterController wrapper + menu command
  Theme.swift              brand palette + reusable styles
  Components.swift         toast, eyebrow, wordmark, empty state
  UpdateBanner.swift       dismissible CLI-update + meta-skill prompts
  LoginView.swift          sign-in pitch + DeviceCodeSheet
  SetupView.swift          create / connect / install-app
  HomeView.swift           sidebar (Browse · Discover · Add · Import · Settings) + content router + UpdateBanner
  BrowseView.swift         search list + skill rows + detail pane
  SkillDetailView.swift    MarkdownUI render + file rail + actions (Install/GitHub/Copy/Remove)
  DiscoverView.swift       public-index search → grades + source preview → gated import
  AddView.swift            add from source → multi-select → publish + install
  AgentPickerSheet.swift   reusable home-agent multi-select (Install + Add)
  MarkdownTheme.swift      brand-matched MarkdownUI theme
  ImportView.swift         bulk local import checklist
  SettingsView.swift       App + agent-skill + CLI + registry/account cards
  Demo.swift               demo-mode fixtures
```

### Staying current: app, CLI, and the meta-skill

Three things can fall out of date; the app keeps each one fresh:

- **The app itself** auto-updates via **[Sparkle](https://github.com/sparkle-project/Sparkle)**.
  `UpdaterManager` owns an `SPUStandardUpdaterController`; the feed
  (`SUFeedURL` in `Info.plist`) is `mac-app/appcast.xml` on `main`, and every
  release is EdDSA-signed (`SUPublicEDKey`) before Sparkle will install it. A
  daily background check, a "Check for Updates…" menu item, and an "auto-check"
  toggle in **Settings → App** are the whole surface. When a check errors
  (network down, bad feed — not "no update found" or a user-cancelled
  install), the app shows an error toast and a "last check failed" hint in
  **Settings → App** until the next successful check.
- **The `skills-registry` CLI** is a separate release stream (`v*` tags vs the
  app's `macapp-v*` tags — same repo, so `releases/latest` is ambiguous; see
  `Updates.ReleaseChannel`). On a 6-hour throttle the app checks the CLI
  channel and, if a newer build exists, shows a dismissible Home banner +
  a "update → vX.Y.Z" pill in **Settings → Command-line tool**. One click
  reinstalls the pinned tag; if the tag lookup itself fails, the toast says
  so instead of downloading ambiguous `latest`. The local CLI status probes
  (`--version` + login-shell PATH check) are cached for 5 minutes so repeat
  Settings visits don't re-spawn them, and concurrent installs are guarded
  so the banner + Settings can't race each other into a bogus failure.
- **The `skills-registry` meta-skill** (`SKILL.md`) is the gateway that teaches
  each agent how to reach your registry. `MetaSkill` scans every detected
  home-based agent dot-folder, classifies it `missing` / `outdated` / `current`
  against `SkillMdTemplate`, and the Home banner + **Settings → Agent skill**
  card install/refresh it into every agent in one click.

### Auth: GitHub App Device Flow

The app authenticates with the **Skills Registry GitHub App** via the
[Device Flow](https://docs.github.com/en/apps/creating-github-apps/writing-code-for-a-github-app/building-a-github-app-that-responds-to-webhook-events#using-the-device-flow-to-generate-a-user-access-token),
which mints a *user-to-server* token without ever embedding a client secret —
that is what keeps a distributed desktop app self-contained and safe. The
client id (`AppConfig.githubClientID`) is public by design.

The resulting token can only touch repositories where the App is installed.
Installing the App on a registry grants the desktop app the repository access
it needs while preserving GitHub's repository-level permission controls.

The token is stored in the macOS **Keychain**. On 401 the app clears it and
returns to the login screen (no silent secret-bearing refresh).

#### GitHub App settings this app requires

The maintainer must configure the GitHub App once:

- **Enable Device Flow** (App settings → "Enable Device Flow").
- **"Expire user authorization tokens" → OFF.** A distributed app can't hold a
  client secret to perform refreshes, so tokens must be non-expiring.
- **Permissions:**
  - **Contents: Read & write** — list/read/publish/remove skills.
  - **Administration: Read & write** *(optional)* — lets the app create the
    registry repo for the user. Without it, "Create" falls back to opening
    `github.com/new` and the user connects the repo afterward.

### Writes are atomic (Git Data API) — and serialized + HEAD-cached

`publish` and `remove` walk the same six-call atomic-commit dance the Go CLI
uses (`ref → commit → recursive tree → blobs → new tree with null-SHA deletes →
commit → patch ref`), retrying up to 3× on 409/422. Bulk import uses a single
commit (`bulkPush`), handling both an empty repo (create the ref) and an
existing branch (base_tree + parent).

Unlike the CLI (a short-lived process, fresh read per invocation), the app is
long-lived and fires writes back-to-back, so all three writes flow through
`BranchGate` (`GitHubWrites` + `BranchGate.swift`):

- **Per-branch FIFO lock.** Consecutive UI actions (delete, delete, add…)
  queue instead of racing each other into ref conflicts.
- **Cached HEAD.** GitHub's `GET /git/ref` is eventually consistent right
  after a write; re-reading it can return the *previous* HEAD and make the
  next `PATCH refs` (force:false) fail 409/422 — the "registry kept changing
  under us" error — even though the app is the branch's only writer. After
  every successful commit the gate remembers `(commit, tree)` and the next
  write commits straight on top of it, skipping the stale ref read. A genuine
  conflict (someone pushed out-of-band) clears the cache so the retry reads
  fresh.

The UI layer is optimistic to match: `remove` drops the row (and sweeps local
copies) before the network call and restores it on failure; publish/add/import
upsert rows from local frontmatter instead of re-listing the whole tree (which
would be eventually-consistent anyway right after the write).

### Install · Add · Remove-everywhere (CLI parity)

Three flows mirror the Go CLI's `install` / `add` / `remove`:

- **Install a registry skill locally.** The skill detail pane's **Install**
  button fetches every file under `<slug>/` (`GitHubReads.skillFileData`, raw
  bytes so binaries survive) and writes them into each picked agent's
  `<dot>/skills/<slug>/` (`LocalInstall.install`). The CLI download cache is
  never touched — that's `get`'s job; this is the durable equivalent of the
  CLI's install picker. Re-installing overwrites in place.
- **Add from a source.** The **Add** sidebar section accepts a local path,
  `owner/repo`, a full GitHub/GitLab/`git@…` URL, or a GitHub
  `{tree|blob}/<ref>/<dir>` folder link. `SourceResolver` validates local paths
  (relative-only, same rules as the CLI), shorthand-expands `owner/repo`, and
  shallow-clones repo-level remote sources. A folder link is fetched through
  the GitHub Contents API instead (`GitHubSubtree.swift`), so importing one
  skill out of a monorepo never clones the repository. Accepted URL shapes are
  parsed by `GitHubTarget`, kept in lockstep with the CLI's
  `registry.ParseGitHubURL`. A third-party source goes through the same import
  gate as the CLI's `add` (`AddGate` in `ImportGate.swift`, grades via
  `DiscoverClient.lookup`): the results list carries an origin banner with the
  index's three grades, publishing is registry-only unless you pick agent
  folders in the picker (confirming with zero selected reads "Publish"), a
  `Poor` safety grade needs the acknowledgement checkbox before Add proceeds,
  and the published copy is stamped with `source_url` + `category` provenance.
  You multi-select discovered skills (dups already in the registry are filtered
  out), then `publishAndInstall` publishes each and installs it into the agents
  you pick.
- **Discover from the public index.** The **Discover** sidebar section searches
  the public SkillNet index through `DiscoverClient` (the Swift mirror of
  `cli/internal/discover`), so it reads the same JSON contract as
  `skills-registry discover --json` without spawning the CLI. The request is
  built in that client rather than through `GitHubAPI` and carries **no
  credential** — the endpoint is plain HTTP because its certificate does not
  match the host, so only the query terms leave the machine. A search fails
  closed: an unreachable index, a timeout, a 5xx, or a non-JSON body renders an
  inline error and no list, so "unreachable" and "no match" never look alike.
  Importing a row resolves its `skill_url` through the same `SourceResolver`
  fetch path (folder only, no clone), stamps `category` + `source_url` onto the
  copy, and publishes it. The confirmation keeps the durable agent install
  **off by default** (when on, confirming opens the agent picker so the
  install goes only where chosen; picking nothing imports registry-only),
  and a `Poor` safety grade needs a second acknowledgement;
  `ImportGate.swift` owns those rules, mirroring `importgate` + `trust`.
- **Remove end-to-end.** `remove(_:)` deletes the `<slug>/` subtree from the
  registry, then `LocalRemove` clears the CLI download (`<slug>/` +
  `<slug>.meta.json`) and sweeps every agent dot-folder for a literal- or
  slugified-name match. The toast reports `registry · cache · N dot-folders`.
- **Huge registries warn instead of silently omitting.** When GitHub answers
  the recursive tree listing with `truncated: true`, publish/remove refuse
  with a "registry too large" error, Browse shows a "Results incomplete"
  banner over the partial list, and opening or installing a skill toasts that
  its file list may be shortened.

**Install locations.** `AgentPickerSheet` lists the home-based agents plus the
universal `.agents` target. In the macOS app, the latter uses the home directory
as its install base and writes to `~/.agents/skills`. No locations are
pre-selected: existing `<dot>` folders are marked as detected for information,
but every destination must be chosen explicitly. A filter field narrows the
list by display name or dot-folder, and "Select all detected" selects only the
visible rows whose folders exist on disk — tools the user never installed stay
unselected unless picked row by row, so bulk installs never create junk
dot-folders. The CLI's separate picker defaults remain unchanged. The filter
field exposes the `agentPickerFilter` accessibility identifier.

---

## The cross-language contract (READ BEFORE EDITING)

The fuzzy scorer, slug derivation, and frontmatter parsing have **two**
implementations that must stay in lockstep:

| Concern | Go (CLI) | Swift (this app) |
|---|---|---|
| Fuzzy scorer | `fuzzyScore` / `scoreSkill` in `cli/cmd/skills-registry/search.go` | `fuzzyScore` / `scoreAndSort` in `Sources/SkillsRegistryCore/FuzzyScore.swift` |
| Slug | `cli/internal/scan` + `registry` | `slugify` in `Slug.swift` |
| Frontmatter | `cli/internal/scan` | `Frontmatter.swift` |
| Meta-skill `SKILL.md` | CLI gateway template | `SkillMdTemplate.swift` |
| Discover contract | `cli/internal/discover` | `Discover.swift` |
| Import gate + trust + provenance | `cli/internal/importgate`, `cli/internal/trust`, `cmd/skills-registry/provenance.go` | `ImportGate.swift` |

The app's `SkillMdTemplateTests` pins the CLI-only gateway's key workflows,
product-owned paths, and repository interpolation.

The scorer constants (base 16, boundary 8, camel 7, consecutive 5, case 1, gap
2, field weights name 2 / slug 1 / desc 1, top-N 10) are **duplicated by
design**. Both scorers normalize the query and the text to Unicode NFC before
matching, so a precomposed accent and the same accent written with a combining
mark score the same. Indexing is still Unicode scalars in Go and extended
grapheme clusters in Swift, so a sequence NFC cannot compose can still differ.
A cross-language corpus test pins the NFC behavior (é, U+00E9 vs U+0301) plus
the word-boundary, camelCase, consecutive-run, and exact-case bonuses, the gap
penalty (including the floor at 0), name-over-description weighting, slug
tiebreak, the top-10 cutoff, and empty or whitespace queries:

- Go: `TestScoreAndSortCrossLanguageCorpus`
- Swift: `testCrossLanguageCorpus` in
  `Tests/SkillsRegistryCoreTests/CoreContractTests.swift`

**If you change any of these, update the app and CLI implementations and their
corpus tests together.** The two tests run the same cases (same inputs and
expected scores); keep them verbatim.

---

## Accessibility

Browse and Discover rows are buttons. Keyboard, Full Keyboard Access, and
VoiceOver can activate them; VoiceOver hears one combined label (name, slug or
grades, description) plus a selected trait, and the row lifts with an accent
bar on hover or keyboard focus. Icon-only controls have explicit labels
(clear search, refresh, copy, remove, dismiss, account). Toasts and section
changes are announced to VoiceOver.

## Testing

```bash
swift test                       # Core contract + cross-language corpus + updates/meta-skill
                                 # + install/remove/source-resolver + discover/import-gate
                                 # + editor draft store and demo-mode save path
```

UI is verified by launching in demo mode and driving it with cua-driver
(macOS Accessibility computer-use). The app exposes stable
`accessibilityIdentifier`s on the key controls (`signInWithGitHub`,
`searchField`, `publishButton`, `importSelected`, `installCLI`,
`removeSkill`, `installSkill`, `editSkill`, `skillEditor`, `saveSkillEdit`,
`cancelSkillEdit`, `discardSkillEdit`, `keepEditingSkill`, `addSourceField`, `addFetch`, `addSelected`,
`addGateBanner`, `addAllowUnsafe`,
`agentPickerConfirm`, `agentPickerFilter`, `discoverQueryField`,
`discoverCategoryField`, `discoverSearch`, `discoverLimit-10/25/50`,
`discoverRefreshStale`, `discoverImport`,
`discoverInstallToggle`, `discoverAllowUnsafe`, `discoverConfirmImport`,
`copySkillFile`, `openOnGitHub`,
`skillRow-<slug>` / `discoverRow-<name>`,
`nav-Browse` / `nav-Discover` / `nav-Add` / `nav-Import` / `nav-Settings`,
`updaterFailureHint`, `simulateUpdateFailure`, `switchRegistry`)
so an automated driver can find them deterministically. Two demo-only launch
arguments reach states the driver cannot: `--demo-select <slug>` opens that
skill directly, and `--demo-publish <path>` runs the publish flow for one
folder shortly after launch, so the demo publish toast is reachable without
driving the folder picker, which isn't scriptable.

---

## Distribution & notarization

`scripts/bundle.sh` produces `build/Skills Registry.app`. By default it is
**ad-hoc signed** so it launches locally (right-click → Open the first time, or
`xattr -dr com.apple.quarantine` if downloaded).

For a notarized build, supply an Apple **Developer ID Application** identity:

```bash
bash scripts/bundle.sh --release --sign "Developer ID Application: Your Name (TEAMID)"
```

`scripts/bundle.sh --notarize` zips, submits to Apple's notary service, and
staples the ticket (reads `APPLE_ID` / `APPLE_TEAM_ID` /
`APPLE_APP_SPECIFIC_PASSWORD` from the environment).

### CI release (`.github/workflows/release-macapp.yml`)

The workflow **auto-cuts a release on every push to `main` that touches the
macOS app source** (`mac-app/Sources/**`, `mac-app/Resources/**`,
`mac-app/Package.swift`, `mac-app/Package.resolved`, `mac-app/scripts/bundle.sh`)
— the same auto-publish model as the CLI's `release.yml`. The patch version
auto-increments from the latest `macapp-v*` tag; trigger a `workflow_dispatch`
with an explicit `version` to override (or leave it empty to auto-increment).
The workflow imports the Developer ID certificate into an isolated temporary
keychain for CI signing, builds + nested-signs the bundle (including
`Sparkle.framework`'s XPC services, `Autoupdate`, and `Updater.app`),
notarizes + staples, EdDSA-signs the zip with `sign_update`, appends an
`<item>` to `mac-app/appcast.xml` on `main`, **creates and pushes the
`macapp-v<version>` tag itself**, and attaches
`SkillsRegistry-macos-arm64.zip` (+ `.sha256`) to the release. The appcast
commit isn't in the trigger paths, so it never re-runs the workflow. Required
repo secrets:

| Secret | Purpose |
|---|---|
| `APPLE_DEVELOPER_CERTIFICATE_P12_BASE64` / `APPLE_DEVELOPER_CERTIFICATE_PASSWORD` | base64 **Developer ID Application** `.p12` (cert + private key) and its password for the isolated CI keychain |
| `APPLE_DEVELOPER_ID_APPLICATION` | identity name, e.g. `Developer ID Application: … (TEAMID)` |
| `APPLE_ID` / `APPLE_TEAM_ID` / `APPLE_APP_SPECIFIC_PASSWORD` | `notarytool` credentials |
| `SPARKLE_PRIVATE_KEY` | base64 Sparkle EdDSA private key matching `SUPublicEDKey` in `Info.plist` |

> The certificate must be a *Developer ID Application* certificate — an "Apple
> Development" cert cannot notarize. CI imports it only into an isolated
> temporary keychain, never into the normal login-keychain search list. Generate
> the Sparkle key pair with `generate_keys` (the public key is already in
> `Info.plist`); export the private half with `generate_keys -x` for
> `SPARKLE_PRIVATE_KEY`.

The app icon is generated on the fly by `scripts/make-icon.sh` (no checked-in
binary asset) — pure `sips` + `iconutil` + a tiny AppKit drawing program.
