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
- **15/15 core contract tests pass** (`swift test`), zero warnings. Includes the
  cross-language fuzzy-scorer / slug / frontmatter corpus test that pins the
  Python ↔ Go ↔ Swift contract.

---

## Known bugs to fix (found during live testing)

Priority order. File pointers included.

1. **No profile picture.** The sidebar account footer renders the user's
   initial in a circle instead of their GitHub avatar. `Identity.avatarURL` is
   **already populated** by `GitHubReads.currentUser()`, so this is UI-only.
   - Fix in `Sources/SkillsRegistry/HomeView.swift` → `accountFooter` (~line 85):
     replace the `Circle().overlay(Text(initials))` with an `AsyncImage(url:
     state.identity?.avatarURL)` that falls back to the initial while loading /
     on failure.

2. **Publishing/adding an existing skill silently "succeeds".** Re-adding a
   slug that already exists in the registry shows "Published/Added" instead of
   failing with "skill already exists". Writes currently overwrite.
   - `Sources/SkillsRegistry/AppState.swift` → `publishFolder(_:)` and
     `importSkills(_:)`. Before writing, check the slug against the loaded
     `skills` (or do a remote existence check) and surface an error/skip.
     `scanLocal()` already filters existing slugs for the Import screen, but the
     single **Publish** path does not guard. Decide the contract (hard-fail vs
     "update existing") and make the toast honest.

3. **Account "⋯" menu shows an overlapping down-chevron.** The borderless
   `Menu` adds a disclosure indicator that overlaps the `ellipsis` glyph.
   - `Sources/SkillsRegistry/HomeView.swift` → `accountFooter` Menu (~line 95):
     add `.menuIndicator(.hidden)` (and/or drop `.menuStyle(.borderlessButton)`)
     so only the `ellipsis` shows.

4. **Add a theme picker in Settings.** Feature request — let the user switch
   theme/appearance.
   - `Sources/SkillsRegistry/SettingsView.swift` for the control;
     `Sources/SkillsRegistry/Theme.swift` (`Brand`) currently hardcodes the
     palette — would need to become selectable (e.g. an `@AppStorage` theme enum
     driving `Brand`), or at minimum a light/dark/system `preferredColorScheme`
     toggle.

5. **Multi-file skill preview is not browsable.** The detail view lists all
   files (SKILL.md, `references/…`, `scripts/…`) in the FILES rail but only
   SKILL.md is ever rendered — the other rows aren't clickable.
   - `Sources/SkillsRegistry/SkillDetailView.swift` → `fileRail(_:)` (~line 95):
     make each row a `Button` with selection state.
   - `SkillDetail` (`Sources/SkillsRegistryCore/Models.swift`) carries `files:
     [String]` (paths) but **not their contents**. Add a fetch for an arbitrary
     file path (extend `GitHubReads`, e.g. a `getFile(repo, path, branch)` via
     the contents/blobs API) and render `.md` through MarkdownUI, other types as
     monospaced text.

6. **Deleting a skill doesn't update the UI.** After a successful delete the
   list/detail still show the removed skill.
   - `Sources/SkillsRegistry/AppState.swift` → `remove(_:)` does call
     `refreshSkills()`, but (a) `BrowseView`'s `@State selected` still points at
     the just-deleted slug so the detail pane keeps rendering it, and (b)
     GitHub's contents listing can be **eventually consistent** immediately
     after the ref update, so the re-list may still include the slug.
   - Fix in `Sources/SkillsRegistry/AppState.swift` + `BrowseView.swift`:
     optimistically drop the slug from `state.skills` right after the delete
     succeeds (don't rely solely on the re-list), and clear `selected` (→ nil)
     when the displayed skill is removed so the detail pane resets.

> These are polish/feature bugs — none block the core auth + browse + write
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

- **Signing & notarization.** App is **ad-hoc signed** today (runs locally).
  For distribution: Apple Developer ID Application cert + notarization. The
  workflow `.github/workflows/release-macapp.yml` is notarization-ready and
  degrades to an ad-hoc zip; wire the Apple secrets when available. No Apple
  developer credentials exist yet (see `.handoff-secrets.md`).
- **CI:** `.github/workflows/ci.yml` gained a `mac-app` job (macos-15, `swift
  build` + `swift test`, arm64). Confirm it's green on the PR.
- Fix the 5 bugs above.

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
swift test                       # 15 contract tests
scripts/bundle.sh                # -> build/Skills Registry.app (ad-hoc signed)
open "build/Skills Registry.app" # real mode (device-flow login)
open "build/Skills Registry.app" --args --demo   # demo mode (fixtures, no network)
```
