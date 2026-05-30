# Skills Registry.app — Handoff

Native macOS app (SwiftUI, Apple-Silicon only) for the `skills-registry`
ecosystem. Users install fresh and manage everything from the app: GitHub
login, connect/create a registry repo, browse skills with rich markdown,
fuzzy search, publish, remove, bulk-import local skills, and a Settings
screen with 1-click CLI install + copyable hosted-MCP JSON.

See `mac-app/README.md` for build/run instructions and architecture. This
doc captures **status, what's verified, what isn't, and the known bugs** for
the next agent.

> **Credentials / GitHub App config** live in `mac-app/.handoff-secrets.md`
> (gitignored). Read that first — it has the App ID, client ID, installation
> ID, registry repo, and how to get a working token.

---

## Status: working & verified end-to-end

The hard part — **real GitHub auth** — is done and tested live:

- **GitHub App configured** (Device Flow on, user-token expiration opted out →
  non-expiring tokens, Contents + Administration = Read & write, installation
  permission upgrade approved). Details in `.handoff-secrets.md`.
- **Device-flow login through the actual app** ✅ — clicked "Sign in with
  GitHub", the app requested a device code, opened the browser, the code was
  authorized, the app polled, received a **non-expiring `ghu_` token**, stored
  it in the Keychain, fetched the GitHub profile, and transitioned to Browse.
- **Live registry browse** ✅ — loaded **`anand-92/my-skills` (108 real
  skills)**, header/identity/branch all correct.
- Earlier (demo mode) verified visually: Browse list/detail, fuzzy search,
  markdown rendering, Import, Settings (real CLI v0.5.30 detected), inter-screen
  animations.
- **32/32 core contract tests pass** (`swift test`), zero warnings. Includes the
  cross-language fuzzy-scorer / slug / frontmatter corpus test that pins the
  Python ↔ Go ↔ Swift contract, plus the new updates / meta-skill / SKILL.md
  golden suites.

---

## Auto-update, CLI update prompt & meta-skill install (NEW)

Three "keep current" surfaces were added (build + tests green; live UI
re-verify pending):

- **App self-update via Sparkle.** `Package.swift` pulls Sparkle 2.x;
  `UpdaterManager` (`Sources/SkillsRegistry/UpdaterManager.swift`) wraps
  `SPUStandardUpdaterController`. `Info.plist` has `SUFeedURL`
  (`mac-app/appcast.xml` on `main`), `SUPublicEDKey`, daily check interval.
  Surface: "Check for Updates…" app-menu item + **Settings → App** card
  (version, manual check, auto-check toggle). Native Sparkle UI handles the
  download/install prompt.
- **CLI update prompt.** `Updates.swift` (Core) models the two release channels
  (`v*` = CLI, `macapp-v*` = app — same repo) + semver. `AppState.checkCLIUpdate`
  polls the CLI channel on a 6h throttle; when newer, a dismissible Home banner
  (`UpdateBanner.swift`) + a "update → vX.Y.Z" pill in **Settings → Command-line
  tool** appear. `installCLI()` now **pins the resolved CLI tag** (fixes the
  ambiguous `releases/latest` across the two streams; the buggy
  `CLIInstaller.latestTag()` was removed).
- **Meta-skill install/refresh.** `MetaSkill.swift` (Core) detects home-based
  agent dot-folders, classifies the `skills-registry/SKILL.md` as
  missing/outdated/current against `SkillMdTemplate.swift` (a **byte-identical**
  Swift mirror of the Go `skillmd.go`), and installs/refreshes into all detected
  agents in one click. Surface: Home banner + **Settings → Agent skill** card.

**Cross-language note:** `SkillMdTemplate.swift` must stay byte-for-byte
identical to `cli/internal/bootstrap/skillmd.go` — `SkillMdTemplateTests` pins
the rendered length (6428 bytes for `owner/repo`).

---

## Known bugs — all 6 FIXED (build + 15/15 tests green; live UI re-verify pending)

Priority order. Each fix below is implemented and compiles clean; they're
UI/app-layer only and don't touch the cross-language contract. Live (running
app) re-verification of the visual results is still worth a pass.

1. ✅ **Profile picture.** `accountFooter` now renders an `AsyncImage(url:
   state.identity?.avatarURL)` (clipped to a circle, `.scaledToFill`) that falls
   back to the initial while loading / on failure.
   `Sources/SkillsRegistry/HomeView.swift` → `avatar` / `avatarFallback`.

2. ✅ **Existing-slug writes no longer silently overwrite.** `publishFolder(_:)`
   hard-fails with `"Skill <slug> already exists in the registry. Remove it
   first to republish."` when the slug is already loaded; `importSkills(_:)`
   filters out existing slugs defensively and the toast is honest about how many
   were imported vs. skipped. `Sources/SkillsRegistry/AppState.swift`.

3. ✅ **Account "⋯" menu chevron.** Added `.menuIndicator(.hidden)` to the
   account `Menu` so only the `ellipsis` shows.
   `Sources/SkillsRegistry/HomeView.swift` → `accountFooter`.

4. ✅ **Theme picker (accent colors).** Settings now has an "Appearance" card
   with five accent swatches (pink/blue/green/amber/violet). Surfaces stay dark
   (a true light theme would clash with the hardcoded near-black palette).
   - `Theme.swift`: `AccentTheme` enum, `AppTheme.current` holder, `Brand.accent`
     / `Brand.accentSoft` are now computed, and `@MainActor ThemeManager:
     ObservableObject` persists the choice to `UserDefaults`.
   - `MarkdownTheme.swift`: `.brand` is now a computed `static var` so markdown
     link/code accents track the theme.
   - `SkillsRegistryApp.swift`: `ThemeManager` injected as a `@StateObject`;
     `RootView` keys its phase content with `.id(theme.accent)` so the palette
     repaints on change without re-running `bootstrap`.
   - `SettingsView.swift`: `appearanceCard` + `swatch(_:)`.

5. ✅ **Multi-file skill preview is browsable.** FILES-rail rows are now
   `Button`s with selection highlight; SKILL.md renders as before, other `.md`
   files via MarkdownUI, everything else as monospaced text.
   - `Sources/SkillsRegistryCore/GitHubReads.swift`: new
     `fileContent(_ repo:path:branch:)` (contents API, base64-decoded).
   - `AppState.fetchFile(slug:path:)` (demo-aware) + `Demo.demoFile(...)`.
   - `SkillDetailView.swift`: `selectedFile` + aux loading/text/error state,
     `fileViewer(_:)`, `fileRow(_:)`, `selectFile(_:)`.

6. ✅ **Delete updates the UI.** `remove(_:)` optimistically drops the slug from
   `state.skills` before *and* after `refreshSkills()` (defeating GitHub's
   eventual-consistency re-list); `BrowseView` clears `selected` via
   `.onChange(of: state.skills)` when the displayed skill is gone.
   `Sources/SkillsRegistry/AppState.swift` + `BrowseView.swift`.

> These were polish/feature bugs — none block the core auth + browse + write
> paths, which are verified.

---

## Not yet tested (live)

- **SetupView** (create/connect/install-app screen). On this machine the shared
  `registry.toml` already pointed at `anand-92/my-skills`, so the app resolved
  straight to Browse and the setup screen wasn't exercised. It reuses verified
  components, but the create-repo / connect / "install app on a repo" branches
  are unproven live.
- **`createRegistry` (repo creation via the App's Administration perm).** Admin
  R/W is now granted, but creating a brand-new repo from the app wasn't run.
- **Publish / remove / bulk-import against the live repo end-to-end.** The auth
  + read path is fully verified; a smoke-test publish was set up but the final
  write was completed/handled outside the automated run. Re-verify publish,
  remove, and bulk import write correctly (and that bug #2 above is the only
  surprise).
- **`logout`** and re-login loop.
- **Toast auto-dismiss timing** under real (non-demo) conditions.
- **CLI install download** path on a machine without the CLI (here v0.5.30 was
  already installed and detected).

---

## Remaining work to ship

- **Signing & notarization.** `scripts/bundle.sh --release --sign "<id>"` does
  full nested signing (Sparkle XPC services / Autoupdate / Updater.app +
  hardened runtime); `--notarize` zips, submits to `notarytool`, and staples.
  `.github/workflows/release-macapp.yml` does all of this on a `macapp-v*` tag,
  EdDSA-signs the zip, and appends to `mac-app/appcast.xml`. **Remaining:** set
  the repo secrets — most are known, but the **Developer ID Application** p12
  still needs exporting from the login keychain (the previously-pasted p12 was
  an "Apple Development" cert, which can't notarize). Exact commands + the
  Sparkle key location are in `.handoff-secrets.md`.
- **CI:** `.github/workflows/ci.yml` gained a `mac-app` job (macos-15, `swift
  build` + `swift test`, arm64). Confirm it's green on the PR.

---

## Cross-language contract (do not break)

Per `AGENTS.md`/`CLAUDE.md`, the macOS app is the **third** implementation of a
shared contract alongside Python (hosted MCP) and Go (CLI). If you touch slug
derivation, frontmatter parsing, or the fuzzy scorer in
`Sources/SkillsRegistryCore/{Slug,Frontmatter,FuzzyScore}.swift`, you must keep
all three languages in lockstep and update the corpus tests
(`Tests/SkillsRegistryCoreTests/CoreContractTests.swift:testCrossLanguageCorpus`
↔ the Python/Go corpus tests) in the same change. The bug fixes above are all
UI/app-layer and do **not** touch the contract.

---

## Build & run (quick)

```bash
cd mac-app
swift build                      # core + app
swift test                       # 32 contract tests
scripts/bundle.sh                # -> build/Skills Registry.app (ad-hoc signed)
open "build/Skills Registry.app" # real mode (device-flow login)
open "build/Skills Registry.app" --args --demo   # demo mode (fixtures, no network)
```
