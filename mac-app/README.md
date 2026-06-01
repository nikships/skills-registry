# Skills Registry.app (macOS)

A native, Apple-Silicon SwiftUI app for managing your skills registry end to
end: sign in with GitHub, create or connect a registry repo, browse skills with
rich markdown rendering and fuzzy search, publish a skill from a folder,
**install** a registry skill into your agent folders, **add** skills from an
external source (local path, `owner/repo`, a git URL, or a GitHub
`/tree/<ref>/<path>` link) and publish + install them in one pass, **remove**
one end-to-end (registry + MCP cache + agent folders), bulk-import the skills
already sitting in your local AI-tool folders, and copy the one-click CLI
install + hosted-MCP JSON from a Settings screen.

It is the **third surface** on the same registry the Go CLI and the hosted
Python MCP server already share — same `registry.toml`, same slug derivation,
same fuzzy scorer, same frontmatter parsing.

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
call is short-circuited, so you can drive the whole app offline.

---

## Architecture

Two SwiftPM targets:

| Target | Kind | Job |
|---|---|---|
| `SkillsRegistryCore` | library | Pure-Foundation logic: GitHub REST/auth, registry contracts, scan, CLI install. **No SwiftUI** — fast to compile and the single source of truth the UI drives. Fully unit-tested. |
| `SkillsRegistry` | executable (`@main`) | SwiftUI app: theme, routing, every view, demo mode. Depends on Core + MarkdownUI. Exercised via cua-driver in demo mode. |

```
Sources/SkillsRegistryCore/
  AppConfig.swift       client_id, app slug, hosted MCP URL, install paths, MCP JSON snippet
  Models.swift          SkillSummary, SkillDetail, RepoRef, Identity, InstallationRepo, LocalSkill
  Slug.swift            slugify  ── shared cross-language contract
  FuzzyScore.swift      fzf-V1 scorer ── shared cross-language contract
  Frontmatter.swift     parseSummary/body/flat-YAML ── shared cross-language contract
  RegistryConfig.swift  ~/.config/skills-mcp/registry.toml R/W (XDG-aware, SKILLS_REGISTRY override)
  Keychain.swift        user-to-server token storage
  Agents.swift          56-entry dot-folder catalogue (port of cli/internal/agents)
  Scan.swift            local skill discovery + filesForUpload
  SourceResolver.swift  resolve add source (local/owner-repo/git URL//tree link) → dir (+subpath)
  LocalInstall.swift    write a skill's files into <agent>/skills/<slug>/ (port of install_local.go)
  LocalRemove.swift     wipe MCP cache + sweep agent dot-folders (port of remove.go locals)
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
  HomeView.swift           sidebar (Browse · Add · Import · Settings) + content router + UpdateBanner
  BrowseView.swift         search list + skill rows + detail pane
  SkillDetailView.swift    MarkdownUI render + file rail + actions (Install/GitHub/Copy/Remove)
  AddView.swift            add from source → multi-select → publish + install
  AgentPickerSheet.swift   reusable home-agent multi-select (Install + Add)
  MarkdownTheme.swift      brand-matched MarkdownUI theme
  ImportView.swift         bulk local import checklist
  SettingsView.swift       App + agent-skill + CLI + MCP + registry/account cards
  Demo.swift               demo-mode fixtures
```

### Staying current: app, CLI, and the meta-skill

Three things can fall out of date; the app keeps each one fresh:

- **The app itself** auto-updates via **[Sparkle](https://github.com/sparkle-project/Sparkle)**.
  `UpdaterManager` owns an `SPUStandardUpdaterController`; the feed
  (`SUFeedURL` in `Info.plist`) is `mac-app/appcast.xml` on `main`, and every
  release is EdDSA-signed (`SUPublicEDKey`) before Sparkle will install it. A
  daily background check, a "Check for Updates…" menu item, and an "auto-check"
  toggle in **Settings → App** are the whole surface.
- **The `skills-registry` CLI** is a separate release stream (`v*` tags vs the
  app's `macapp-v*` tags — same repo, so `releases/latest` is ambiguous; see
  `Updates.ReleaseChannel`). On a 6-hour throttle the app checks the CLI
  channel and, if a newer build exists, shows a dismissible Home banner +
  a "update → vX.Y.Z" pill in **Settings → Command-line tool**. One click
  reinstalls the pinned tag.
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

The resulting token can only touch repositories where the App is installed,
which is also exactly what the hosted MCP server needs in order to serve those
skills to coding agents. So "install the app on your registry repo" does double
duty: it grants the desktop app write access **and** lights up the MCP server.

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

### Writes are atomic (Git Data API)

`publish` and `remove` walk the same six-call atomic-commit dance the Go CLI
uses (`ref → commit → recursive tree → blobs → new tree with null-SHA deletes →
commit → patch ref`), retrying up to 3× on 409/422. Bulk import uses a single
commit (`bulkPush`), handling both an empty repo (create the ref) and an
existing branch (base_tree + parent).

### Install · Add · Remove-everywhere (CLI parity)

Three flows mirror the Go CLI's `install` / `add` / `remove`:

- **Install a registry skill locally.** The skill detail pane's **Install**
  button fetches every file under `<slug>/` (`GitHubReads.skillFileData`, raw
  bytes so binaries survive) and writes them into each picked agent's
  `<dot>/skills/<slug>/` (`LocalInstall.install`). The MCP download cache is
  never touched — that's `get`'s job; this is the durable equivalent of the
  CLI's install picker. Re-installing overwrites in place.
- **Add from a source.** The **Add** sidebar section accepts a local path,
  `owner/repo`, a full GitHub/GitLab/`git@…` URL, or a GitHub
  `/tree/<ref>/<subpath>` deep link. `SourceResolver` validates local paths
  (relative-only, same rules as the CLI), shorthand-expands `owner/repo`,
  and shallow-clones remote sources (narrowing to the subpath for `/tree/`
  links). You multi-select discovered skills (dups already in the registry are
  filtered out), then `publishAndInstall` publishes each and installs it into
  the agents you pick.
- **Remove end-to-end.** `remove(_:)` deletes the `<slug>/` subtree from the
  registry, then `LocalRemove` wipes the MCP cache (`<slug>/` +
  `<slug>.meta.json`) and sweeps every agent dot-folder for a literal- or
  slugified-name match. The toast reports `registry · cache · N dot-folders`.

**Home-scoped install (deliberate divergence from the CLI).** `AgentPickerSheet`
only lists the home-based agents (`Agents.all().filter(\.underHome)`) and
pre-checks the ones whose `<dot>` folder already exists. The cwd-based universal
`.agents/skills` target — always-on in the CLI's picker — is intentionally
skipped: a desktop app has no meaningful project working directory (the same
rationale as `MetaSkill.detectedTargets`).

---

## The cross-language contract (READ BEFORE EDITING)

The fuzzy scorer, slug derivation, and frontmatter parsing now have **three**
implementations that must stay in lockstep:

| Concern | Python (hosted MCP) | Go (CLI) | Swift (this app) |
|---|---|---|---|
| Fuzzy scorer | `_fuzzy_score` / `_score_skill` in `infa-not-for-users/skills_mcp/github_api.py` | `fuzzyScore` / `scoreSkill` in `cli/cmd/skills-registry/search.go` | `fuzzyScore` / `scoreAndSort` in `Sources/SkillsRegistryCore/FuzzyScore.swift` |
| Slug | `slugify` in `github_api.py` | `cli/internal/scan` + `registry` | `slugify` in `Slug.swift` |
| Frontmatter | `frontmatter.py` | `cli/internal/scan` | `Frontmatter.swift` |
| Meta-skill `SKILL.md` | — (read-only server) | `SkillMd` in `cli/internal/bootstrap/skillmd.go` | `SkillMdTemplate.swift` |

The Go and Swift `SKILL.md` templates must render **byte-for-byte identical**
output — `SkillMdTemplateTests` pins the rendered length (6428 bytes for
`owner/repo`) and key lines. Edit one, edit the other, refresh the test.

The scorer constants (base 16, boundary 8, camel 7, consecutive 5, case 1, gap
2, field weights name 2 / slug 1 / desc 1, top-N 10) are **duplicated by
design**. A cross-language corpus test pins the contract:

- Python: `test_search_skills_cross_language_corpus`
- Go: `TestScoreAndSortCrossLanguageCorpus`
- Swift: `testCrossLanguageCorpus` in
  `Tests/SkillsRegistryCoreTests/CoreContractTests.swift`

**If you change any of these, update all three implementations and all three
corpus tests in the same PR.** The write surface (publish/delete/bulkPush) is
not shared with Python — the hosted server is read-only.

---

## Testing

```bash
swift test                       # Core contract + cross-language corpus + updates/meta-skill
                                 # + install/remove/source-resolver (62 tests)
```

UI is verified by launching in demo mode and driving it with cua-driver
(macOS Accessibility computer-use). The app exposes stable
`accessibilityIdentifier`s on the key controls (`signInWithGitHub`,
`searchField`, `publishButton`, `importSelected`, `installCLI`, `copyMCP`,
`removeSkill`, `installSkill`, `addSourceField`, `addFetch`, `addSelected`,
`agentPickerConfirm`, `nav-Browse` / `nav-Add` / `nav-Import` / `nav-Settings`)
so an automated driver can find them deterministically.

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
The workflow imports the Developer ID cert, builds + nested-signs the bundle
(including `Sparkle.framework`'s XPC services, `Autoupdate`, and `Updater.app`),
notarizes + staples, EdDSA-signs the zip with `sign_update`, appends an `<item>`
to `mac-app/appcast.xml` on `main`, **creates and pushes the `macapp-v<version>`
tag itself**, and attaches `SkillsRegistry-macos-arm64.zip` (+ `.sha256`) to the
release. The appcast commit isn't in the trigger paths, so it never re-runs the
workflow. Required repo secrets:

| Secret | Purpose |
|---|---|
| `APPLE_DEVELOPER_CERTIFICATE_P12_BASE64` / `APPLE_DEVELOPER_CERTIFICATE_PASSWORD` | base64 **Developer ID Application** `.p12` (cert + private key) and its password |
| `APPLE_DEVELOPER_ID_APPLICATION` | identity name, e.g. `Developer ID Application: … (TEAMID)` |
| `APPLE_ID` / `APPLE_TEAM_ID` / `APPLE_APP_SPECIFIC_PASSWORD` | `notarytool` credentials |
| `SPARKLE_PRIVATE_KEY` | base64 Sparkle EdDSA private key matching `SUPublicEDKey` in `Info.plist` |

> The cert **must** be a *Developer ID Application* certificate — an "Apple
> Development" cert cannot notarize. Generate the Sparkle key pair with
> `generate_keys` (the public key is already in `Info.plist`); export the
> private half with `generate_keys -x` for `SPARKLE_PRIVATE_KEY`.

The app icon is generated on the fly by `scripts/make-icon.sh` (no checked-in
binary asset) — pure `sips` + `iconutil` + a tiny AppKit drawing program.
