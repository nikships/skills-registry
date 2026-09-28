import SwiftUI
import AppKit
import SkillsRegistryCore

@MainActor
final class AppState: ObservableObject {
    enum Phase: Equatable { case loading, signedOut, setup, ready }

    @Published var phase: Phase = .loading
    @Published var identity: Identity?
    @Published var repo: RepoRef?
    @Published var branch: String = "main"

    @Published var skills: [SkillSummary] = []
    @Published var skillsLoading = false
    @Published var skillsError: String?

    @Published var installRepos: [InstallationRepo] = []
    @Published var setupLoading = false

    // Device-flow sheet state.
    @Published var deviceCode: DeviceCode?
    @Published var authInProgress = false
    @Published var authError: String?

    @Published var toast: ToastItem?
    @Published var cliInstalled = false
    @Published var cliVersion: String?
    // Defaults to true until the async shell probe completes. A Finder-
    // launched app cannot know its shell PATH from ProcessInfo alone.
    @Published var cliInstallDirOnPath = true

    // Update / meta-skill prompts surfaced in the Home banner + Settings.
    @Published var cliUpdate: ReleaseInfo?
    @Published var metaSkill = MetaSkill.Status()
    @Published var dismissedKeys: Set<String> = []

    let isDemo: Bool
    private var token: String?
    private var api: GitHubAPI?
    private var authTask: Task<Void, Never>?
    private let flow = DeviceFlow()

    /// Best-effort cleanup for the temp clone created by `resolveAndScan`. Held
    /// until `publishAndInstall` finishes (the discovered skill folders point
    /// into the clone) or a new resolve supersedes it.
    private var addCleanup: (@Sendable () -> Void)?

    /// The import gate for the current Add fetch: the source's classification
    /// plus the index row it degraded to when untrusted. Published so AddView
    /// can render the origin banner, and read back by `publishAndInstall` to
    /// enforce the verdict. Nil until the first fetch.
    @Published var addGate: AddGate?
    /// The trimmed source the current `addGate` was built from. Stamped onto
    /// untrusted imports as their provenance.
    private var addSource = ""
    /// The resolved directory the current Add fetch scans, so
    /// `publishAndInstall` can derive each skill's own `source_url`.
    private var addResolveDir = ""

    /// An import the post-fetch scan held back. Published so DiscoverView can
    /// offer the acknowledgement that clears it. Nil unless the scan matched
    /// and the confirmation did not already allow it.
    @Published var scanBlockedImport: ScanBlockedImport?

    private let defaults = UserDefaults.standard
    private let dismissKey = "dismissedUpdatePrompts"
    private let lastCLICheckKey = "lastCLIUpdateCheck"
    private let cliCheckInterval: TimeInterval = 6 * 3600

    init(demo: Bool = false) {
        self.isDemo = demo
        dismissedKeys = Set(defaults.stringArray(forKey: dismissKey) ?? [])
    }

    // MARK: - lifecycle

    func bootstrap() async {
        if isDemo { startDemo(); return }
        cliInstalled = CLIInstaller.isInstalled()
        guard let saved = Keychain.get() else { phase = .signedOut; return }
        token = saved
        let client = GitHubAPI(token: saved)
        do {
            let me = try await client.currentUser()
            api = client
            identity = me
            await resolveAfterAuth()
        } catch let e as GitHubError where e.isUnauthorized {
            Keychain.delete()
            token = nil
            phase = .signedOut
        } catch {
            // Network hiccup — let them retry from signed-out rather than wedge.
            phase = .signedOut
            authError = error.localizedDescription
        }
    }

    /// After we have a valid token + identity, decide setup vs ready.
    private func resolveAfterAuth() async {
        if let cfg = RegistryConfig.loadOptional(), let ref = cfg.ref, let client = api {
            do {
                if try await client.repoExists(ref) {
                    repo = ref
                    branch = cfg.defaultBranch
                    phase = .ready
                    await refreshSkills()
                    return
                }
            } catch { /* fall through to setup */ }
        }
        phase = .setup
        await loadInstallations()
    }

    // MARK: - auth

    func beginLogin() {
        authError = nil
        authInProgress = true
        authTask?.cancel()
        authTask = Task { await self.runDeviceFlow() }
    }

    func cancelLogin() {
        authTask?.cancel()
        authTask = nil
        authInProgress = false
        deviceCode = nil
    }

    private func runDeviceFlow() async {
        do {
            let code = try await flow.requestCode()
            deviceCode = code
            // Pre-copy the code and open the browser for a frictionless hand-off.
            Clipboard.copy(code.userCode)
            NSWorkspace.shared.open(code.verificationURI)
            let result = try await flow.pollForToken(code)
            try Task.checkCancellation()
            await finishAuth(token: result.accessToken)
        } catch is CancellationError {
            // user cancelled — already reset
        } catch {
            authError = error.localizedDescription
            authInProgress = false
            deviceCode = nil
        }
    }

    private func finishAuth(token newToken: String) async {
        let client = GitHubAPI(token: newToken)
        do {
            let me = try await client.currentUser()
            Keychain.set(newToken)
            token = newToken
            api = client
            identity = me
            authInProgress = false
            deviceCode = nil
            await resolveAfterAuth()
        } catch {
            authError = "Signed in, but couldn't read your GitHub profile: \(error.localizedDescription)"
            authInProgress = false
            deviceCode = nil
        }
    }

    func logout() {
        Keychain.delete()
        token = nil
        api = nil
        identity = nil
        repo = nil
        skills = []
        installRepos = []
        phase = .signedOut
    }

    // MARK: - setup

    func loadInstallations() async {
        guard let api else { return }
        setupLoading = true
        defer { setupLoading = false }
        do {
            installRepos = try await api.skillsRegistryRepos().sorted { $0.fullName < $1.fullName }
        } catch {
            installRepos = []
            showToast("Couldn't list installed repos: \(error.localizedDescription)", .error)
        }
    }

    func connect(_ repoRef: RepoRef, branch defaultBranch: String) async {
        guard let api else { return }
        do {
            let exists = try await api.repoExists(repoRef)
            guard exists else {
                showToast("Can't access \(repoRef.fullName). Install the app on it first.", .error)
                return
            }
            let resolved = (try? await api.defaultBranch(repoRef)) ?? defaultBranch
            try RegistryConfig(repo: repoRef.fullName, defaultBranch: resolved).save()
            repo = repoRef
            branch = resolved
            phase = .ready
            await refreshSkills()
        } catch {
            showToast("Connect failed: \(error.localizedDescription)", .error)
        }
    }

    func createRegistry(name: String, isPrivate: Bool) async {
        guard let api else { return }
        setupLoading = true
        defer { setupLoading = false }
        do {
            let ref = try await api.createRepo(
                name: name, isPrivate: isPrivate,
                description: "Personal skill registry — managed via Skills Registry.app")
            try RegistryConfig(repo: ref.fullName, defaultBranch: "main").save()
            repo = ref
            branch = "main"
            phase = .ready
            await refreshSkills()
            showToast("Created \(ref.fullName)", .ok)
        } catch WriteError.adminPermissionMissing {
            showToast("App can't create repos. Create it on github.com, then connect it here.", .info)
            NSWorkspace.shared.open(URL(string: "https://github.com/new")!)
        } catch {
            showToast("Create failed: \(error.localizedDescription)", .error)
        }
    }

    // MARK: - skills

    func refreshSkills() async {
        guard let api, let repo else { return }
        skillsLoading = true
        skillsError = nil
        defer { skillsLoading = false }
        do {
            skills = try await api.listSkills(repo, branch: branch)
        } catch {
            skillsError = error.localizedDescription
        }
    }

    func fetchDetail(_ slug: String) async throws -> SkillDetail {
        if isDemo { return Self.demoDetail(slug) }
        guard let api, let repo else { throw GitHubError(status: 0, message: "Not ready", endpoint: "") }
        return try await api.getSkill(repo, slug: slug, branch: branch)
    }

    /// Contents of a single supporting file (path relative to `<slug>/`).
    func fetchFile(slug: String, path: String) async throws -> String {
        if isDemo { return Self.demoFile(slug: slug, path: path) }
        guard let api, let repo else { throw GitHubError(status: 0, message: "Not ready", endpoint: "") }
        return try await api.fileContent(repo, path: "\(slug)/\(path)", branch: branch)
    }

    /// Commit an edited SKILL.md without replacing the skill's supporting
    /// files, then immediately refresh its summary in the browse list.
    func saveSkillMarkdown(_ slug: String, markdown: String) async throws -> SkillSummary {
        let (name, description) = Frontmatter.parseSummary(markdown, slug: slug)
        let summary = SkillSummary(slug: slug, name: name, description: description)
        if !isDemo {
            guard let api, let repo else {
                throw GitHubError(status: 0, message: "Not ready", endpoint: "")
            }
            _ = try await api.updateSkillMarkdown(
                repo, slug: slug, markdown: markdown,
                message: "edit: \(slug)", branch: branch)
        }
        upsertSkill(summary)
        showToast("Saved \(slug)", .ok)
        return summary
    }

    /// Remove a skill end-to-end, mirroring `skills-registry remove`: delete
    /// the `<slug>/` subtree from the registry, then sweep the two local
    /// footprints (CLI download cache + every agent dot-folder copy).
    ///
    /// Optimistic: the row disappears from the UI immediately; the registry
    /// delete runs behind it (serialized by `BranchGate`). On failure the row
    /// is restored. No post-op re-list — the local mutation is authoritative,
    /// and GitHub's tree listing is eventually consistent right after a write
    /// anyway (a re-list could resurrect the slug).
    func remove(_ slug: String) async {
        guard let api, let repo else { return }
        let removed = skills.filter { $0.slug == slug }
        skills.removeAll { $0.slug == slug }

        // Local cleanup first — instant, and independent of the network.
        let home = FileManager.default.homeDirectoryForCurrentUser.path
        let cwd = FileManager.default.currentDirectoryPath
        let cacheCleared = LocalRemove.removeFromCache(slug: slug)
        let dotFolders = LocalRemove.removeFromDotFolders(slug: slug, home: home, cwd: cwd)
        showToast(removeSummary(slug: slug, cacheCleared: cacheCleared, dotFolders: dotFolders.count), .ok)
        refreshMetaSkillStatus()

        do {
            _ = try await api.delete(repo, slug: slug, message: "remove: \(slug)", branch: branch)
        } catch {
            // Roll back: the registry still has it, so the UI must too.
            skills = (skills + removed).sorted { $0.slug < $1.slug }
            showToast("Remove failed: \(error.localizedDescription)", .error)
        }
    }

    /// Compress the removal report into a one-line toast:
    /// "slug · registry · cache · 2 dot-folders" (mirrors removeSummaryLine).
    private func removeSummary(slug: String, cacheCleared: Bool, dotFolders: Int) -> String {
        var parts = ["registry"]
        if cacheCleared { parts.append("cache") }
        if dotFolders > 0 { parts.append("\(dotFolders) dot-folder\(dotFolders == 1 ? "" : "s")") }
        return "Removed \(slug) · " + parts.joined(separator: " · ")
    }

    // MARK: - install registry skill locally

    /// Durably install a registry skill into the selected agent dot-folders:
    /// fetch every file under `<slug>/` and write it into each target's
    /// `<dot>/skills/<slug>/`. The CLI download cache is never touched (that's
    /// `get`'s job) — this is the durable equivalent of the CLI's install
    /// picker.
    func installRegistrySkill(_ slug: String, targets: [AgentTarget]) async {
        guard let api, let repo else { return }
        guard !targets.isEmpty else { showToast("Pick at least one agent to install into.", .info); return }
        do {
            let files = try await api.skillFileData(repo, slug: slug, branch: branch)
            let home = FileManager.default.homeDirectoryForCurrentUser.path
            let written = try LocalInstall.install(slug: slug, files: files, targets: targets, home: home, cwd: home)
            showToast("Installed \(slug) into \(written.count) agent\(written.count == 1 ? "" : "s")", .ok)
            refreshMetaSkillStatus()
        } catch {
            showToast("Install failed: \(error.localizedDescription)", .error)
        }
    }

    // MARK: - add (resolve external source → publish + install)

    /// Resolve an `add` source (local path / owner-repo / git URL / GitHub
    /// `{tree|blob}` folder URL), discover its skills, and filter out slugs
    /// already in the registry (dup-safe like `importSkills`). A folder URL is
    /// fetched through the Contents API, so a monorepo link never clones the
    /// repository. The resolved temp dir is kept alive until the next
    /// `resolveAndScan` or a `publishAndInstall` call so the discovered
    /// `folder` paths stay readable for upload.
    /// Returns the discovered (dup-filtered) skills, or `nil` if the source
    /// couldn't be resolved/scanned — letting the caller distinguish a fetch
    /// failure from a genuinely empty result. `trustedLocalDir` relaxes the
    /// relative-only path guard for directories chosen via the native picker.
    func resolveAndScan(_ source: String, trustedLocalDir: Bool = false) async -> [LocalSkill]? {
        if isDemo { return demoResolveAndScan(source) }
        addCleanup?()
        addCleanup = nil
        addGate = nil
        addSource = ""
        addResolveDir = ""
        let src = source.trimmingCharacters(in: .whitespacesAndNewlines)
        let home = FileManager.default.homeDirectoryForCurrentUser.path
        let cwd = FileManager.default.currentDirectoryPath
        do {
            let resolved = try await SourceResolver.resolve(
                src, home: home, cwd: cwd, folderFetcher: api,
                allowAbsoluteLocal: trustedLocalDir)
            addCleanup = resolved.cleanup
            let discovered = Scan.discover([Scan.Source(path: resolved.dir, label: src)])
            // Normalize both sides so a local "simplify_swarm" dedupes against a
            // registry "simplify-swarm" (mirrors Go scan.DedupeAgainst).
            let existing = Set(skills.map { normalizeForMatch($0.slug) })
            let fresh = discovered.filter {
                !existing.contains(normalizeForMatch($0.slug)) && $0.slug != MetaSkill.slug
            }
            addSource = src
            addResolveDir = resolved.dir
            addGate = try await gateForAdd(source: src, skills: fresh)
            if discovered.isEmpty {
                showToast("No SKILL.md files found under \(src).", .info)
            }
            return fresh
        } catch {
            addCleanup = nil
            addGate = nil
            addSource = ""
            addResolveDir = ""
            showToast("Couldn't fetch source: \(error.localizedDescription)", .error)
            return nil
        }
    }

    /// Demo-mode seam: install the verdict `demoResolveAndScan` computed,
    /// keeping the stored source/dir/gate trio consistent. Production fetches
    /// set the same trio in `resolveAndScan`.
    func setAddDemoState(source: String, gate: AddGate) {
        addCleanup?()
        addCleanup = nil
        addSource = source
        addResolveDir = ""
        addGate = gate
    }

    /// Build the Add gate for one source: classify it against the registry
    /// owner's login, and for an untrusted source look up the index row whose
    /// grades the results banner shows and scan each fetched SKILL.md. A
    /// lookup miss or failure degrades to unscored rather than blocking the
    /// fetch — the index is a convenience, and unscored already needs the
    /// user's confirmation. The scan reads the upstream files before any
    /// provenance stamp rewrites them. A scan that cannot read the file fails
    /// the fetch: an unreviewed skill must not be offered for import.
    private func gateForAdd(source: String, skills: [LocalSkill]) async throws -> AddGate {
        let owners = repo.map { [$0.owner] } ?? []
        guard ImportTrust.assess(source, owners: owners).untrusted else {
            return AddGate.build(source: source, owners: owners, slugs: [])
        }
        let row = try? await DiscoverClient().lookup(source)
        var findings: [String: [SkillFinding]] = [:]
        for sk in skills {
            findings[sk.slug] = try SkillScan.scanSkill(folder: sk.folder)
        }
        return AddGate.build(source: source, owners: owners,
                             slugs: skills.map(\.slug), indexed: row, findings: findings)
    }

    /// Publish each selected skill to the registry, then durably install it
    /// into the chosen agents. Dup-safe: slugs already in the registry are
    /// skipped. Cleans up the resolved temp clone when done.
    ///
    /// The Add gate is enforced, not just displayed: an untrusted source
    /// stamps `source_url`/`category` provenance onto its copy, refused
    /// (blocked and unacknowledged) skills are left unpublished, and an empty
    /// target list publishes registry-only. `allowUnsafe` is the user's
    /// acknowledgement of a blocker (a Poor safety grade or a local scan
    /// hit), never implied by picking install targets.
    func publishAndInstall(_ locals: [LocalSkill], targets: [AgentTarget],
                           allowUnsafe: Bool = false,
                           progress: @escaping @Sendable (Int, Int) -> Void) async {
        guard let api, let repo else { return }
        defer {
            addCleanup?()
            addCleanup = nil
            addGate = nil
            addSource = ""
            addResolveDir = ""
        }
        // Normalize both sides so separator/case-only variants dedupe against
        // an existing registry slug (mirrors Go scan.DedupeAgainst).
        let existing = Set(skills.map { normalizeForMatch($0.slug) })
        let fresh = locals.filter { !existing.contains(normalizeForMatch($0.slug)) }
        let skipped = locals.count - fresh.count
        guard !fresh.isEmpty else {
            showToast(skipped > 0 ? "All selected skills already exist in the registry." : "Nothing to add.", .info)
            return
        }
        let gate = addGate
        let (gated, refused) = gate?.allowed(slugs: fresh.map(\.slug), allowUnsafe: allowUnsafe)
            ?? (fresh.map(\.slug), [])
        guard !gated.isEmpty else {
            let why = refused.map { "\($0.slug): \($0.summary)" }.joined(separator: "; ")
            showToast("Refused \(refused.count) skill\(refused.count == 1 ? "" : "s") — \(why).", .error)
            return
        }
        let publishable = fresh.filter { gated.contains($0.slug) }
        if gate?.untrusted == true {
            do {
                try stampAddProvenance(publishable, gate: gate)
            } catch {
                showToast("Couldn't stamp import provenance: \(error.localizedDescription)", .error)
                return
            }
        }
        let home = FileManager.default.homeDirectoryForCurrentUser.path
        let total = publishable.count
        var done = 0
        do {
            for sk in publishable {
                let rel = stripSlugPrefix(try Scan.filesForUpload(slug: sk.slug, folder: sk.folder), slug: sk.slug)
                _ = try await api.publish(repo, slug: sk.slug, files: rel,
                                          message: "add: \(sk.slug)", branch: branch)
                // Reflect the successful publish immediately — no re-list.
                upsertSkill(SkillSummary(slug: sk.slug, name: sk.name, description: sk.description))
                if !targets.isEmpty {
                    _ = try LocalInstall.install(slug: sk.slug, files: rel, targets: targets, home: home, cwd: home)
                }
                done += 1
                progress(done, total)
            }
            let base = targets.isEmpty
                ? "Added \(publishable.count) skill\(publishable.count == 1 ? "" : "s")"
                : "Added + installed \(publishable.count) skill\(publishable.count == 1 ? "" : "s")"
            var notes: [String] = []
            if skipped > 0 { notes.append("skipped \(skipped) already in registry") }
            if !refused.isEmpty {
                notes.append("refused \(refused.count) blocked (acknowledge to import)")
            }
            showToast(notes.isEmpty ? base : "\(base); " + notes.joined(separator: "; "), .ok)
            refreshMetaSkillStatus()
        } catch {
            showToast("Add failed: \(error.localizedDescription)", .error)
        }
    }

    /// Stamp `source_url`/`category` provenance onto each skill fetched from
    /// an untrusted Add source, before the first write. Each skill gets its
    /// own subfolder URL via the shared `relativeFolder(_:under:)` helper,
    /// exactly like the Discover import path.
    private func stampAddProvenance(_ skills: [LocalSkill], gate: AddGate?) throws {
        guard let gate, !addSource.isEmpty else { return }
        for sk in skills {
            try ImportProvenance.stamp(
                folder: sk.folder,
                sourceURL: ImportProvenance.sourceURL(
                    for: addSource,
                    relativeFolder: Self.relativeFolder(sk.folder, under: addResolveDir)),
                category: gate.category)
        }
    }

    // MARK: - discover (public index → untrusted import)

    /// Search the public skill index. Nothing is written and no credential is
    /// attached; the client builds its own request rather than reusing the
    /// GitHub transport (see `DiscoverClient`).
    func discoverSearch(_ query: DiscoverQuery) async throws -> DiscoverResponse {
        if isDemo { return try Self.demoDiscoverResponse(query) }
        return try await DiscoverClient().search(query)
    }

    /// Import one row picked out of the public index.
    ///
    /// Untrusted by construction: the row's folder URL is fetched through the
    /// Contents API (never a clone), scanned for injection shapes, stamped
    /// with `category` + `source_url`, and published to the user's registry.
    /// `targets` is empty unless the user explicitly opted into the durable
    /// agent-folder install, so the default really is registry-only.
    /// `allowUnsafe` is the confirmation's acknowledgement of a blocker.
    ///
    /// The scan runs after the fetch because there is nothing to scan before
    /// it. A hit holds the import even when `allowUnsafe` already cleared a
    /// grade block: that consent was given before the findings existed.
    /// `scanAcknowledged` is the second consent, given with the findings in
    /// front of the user. Nothing is written until both are clear. Returns
    /// whether anything was published.
    @discardableResult
    func importDiscovered(_ result: DiscoverResult, targets: [AgentTarget],
                          allowUnsafe: Bool = false,
                          scanAcknowledged: Bool = false) async -> Bool {
        if isDemo {
            return demoImportDiscovered(result, targets: targets,
                                        allowUnsafe: allowUnsafe,
                                        scanAcknowledged: scanAcknowledged)
        }
        guard let api, let repo else { return false }
        let home = FileManager.default.homeDirectoryForCurrentUser.path
        let cwd = FileManager.default.currentDirectoryPath
        do {
            let resolved = try await SourceResolver.resolve(
                result.skillURL, home: home, cwd: cwd, folderFetcher: api)
            defer { resolved.cleanup() }

            let discovered = Scan.discover([Scan.Source(path: resolved.dir, label: result.skillURL)])
            guard !discovered.isEmpty else {
                showToast("No SKILL.md found at \(result.skillURL).", .error)
                return false
            }
            let existing = Set(skills.map { normalizeForMatch($0.slug) })
            let fresh = discovered.filter { !existing.contains(normalizeForMatch($0.slug)) }
            guard !fresh.isEmpty else {
                showToast("\(discovered[0].slug) is already in your registry.", .info)
                return false
            }
            if !scanAcknowledged {
                let held = try scanHitRefusal(result: result, skills: fresh)
                if let held {
                    scanBlockedImport = ScanBlockedImport(result: result, targets: targets, refusal: held)
                    return false
                }
            }
            scanBlockedImport = nil
            if !allowUnsafe, result.scores.safetyIsPoor {
                showToast("Refused: the public skill index graded this skill's safety Poor.", .error)
                return false
            }
            for sk in fresh {
                try ImportProvenance.stamp(
                    folder: sk.folder,
                    sourceURL: ImportProvenance.sourceURL(
                        for: result.skillURL,
                        relativeFolder: Self.relativeFolder(sk.folder, under: resolved.dir)),
                    category: result.category)
            }
            try await publishDiscovered(fresh, api: api, repo: repo, targets: targets, home: home)
            refreshMetaSkillStatus()
            return true
        } catch {
            showToast("Import failed: \(error.localizedDescription)", .error)
            return false
        }
    }

    /// Scan each fetched skill. A read error fails the import: an unreviewed
    /// file must not be published. Returns one review carrying every scan hit
    /// (slug-prefixed when more than one skill matched), or nil when the
    /// heuristic matched nothing. A Poor grade alone does not hold here; the
    /// confirmation sheet already required that consent.
    private func scanHitRefusal(result: DiscoverResult, skills: [LocalSkill]) throws -> ImportReview? {
        var rows: [(slug: String, findings: [SkillFinding])] = []
        for sk in skills {
            let findings = try SkillScan.scanSkill(folder: sk.folder)
            if !findings.isEmpty { rows.append((sk.slug, findings)) }
        }
        guard !rows.isEmpty else { return nil }
        if rows.count == 1 {
            return ImportReview.evaluate(slug: rows[0].slug, scores: result.scores,
                                         findings: rows[0].findings)
        }
        let combined = rows.flatMap { row in
            row.findings.map {
                SkillFinding(category: $0.category, rule: $0.rule, line: $0.line,
                             excerpt: "\(row.slug): \($0.excerpt)")
            }
        }
        return ImportReview.evaluate(slug: rows[0].slug, scores: result.scores, findings: combined)
    }

    /// Publish every fetched skill and, only when `targets` is non-empty,
    /// durably install it. Split out so `importDiscovered` stays readable.
    private func publishDiscovered(_ fresh: [LocalSkill], api: GitHubAPI, repo: RepoRef,
                                   targets: [AgentTarget], home: String) async throws {
        for sk in fresh {
            let rel = stripSlugPrefix(try Scan.filesForUpload(slug: sk.slug, folder: sk.folder),
                                      slug: sk.slug)
            _ = try await api.publish(repo, slug: sk.slug, files: rel,
                                      message: "add: \(sk.slug)", branch: branch)
            upsertSkill(SkillSummary(slug: sk.slug, name: sk.name, description: sk.description))
            if !targets.isEmpty {
                _ = try LocalInstall.install(slug: sk.slug, files: rel, targets: targets,
                                             home: home, cwd: home)
            }
        }
        let names = fresh.map(\.slug).joined(separator: ", ")
        showToast(targets.isEmpty
                  ? "Imported \(names) into your registry"
                  : "Imported \(names) and installed it into \(targets.count) agent\(targets.count == 1 ? "" : "s")",
                  .ok)
    }

    /// A fetched skill folder's slash-separated path under the fetch root, so
    /// each skill in a folder-of-skills import records its own `source_url`.
    private static func relativeFolder(_ folder: String, under root: String) -> String {
        let base = (root as NSString).standardizingPath
        let path = (folder as NSString).standardizingPath
        guard path != base, path.hasPrefix(base + "/") else { return "" }
        return String(path.dropFirst(base.count + 1))
    }

    /// Publish a skill from a local folder containing SKILL.md.
    func publishFolder(_ url: URL) async {
        guard let api, let repo else { return }
        let folder = url.path
        let main = (folder as NSString).appendingPathComponent("SKILL.md")
        guard FileManager.default.fileExists(atPath: main) else {
            showToast("No SKILL.md found in that folder.", .error)
            return
        }
        let text = (try? String(contentsOfFile: main, encoding: .utf8)) ?? ""
        let folderName = (folder as NSString).lastPathComponent
        let (name, desc) = Frontmatter.parseSummary(text, slug: folderName)
        let slug = slugify(name.isEmpty ? folderName : name)
        // Normalize both sides so a separator/case-only variant already in the
        // registry is detected (mirrors Go scan.DedupeAgainst).
        let want = normalizeForMatch(slug)
        guard !skills.contains(where: { normalizeForMatch($0.slug) == want }) else {
            showToast("Skill \(slug) already exists in the registry. Remove it first to republish.", .error)
            return
        }
        do {
            let rel = stripSlugPrefix(try Scan.filesForUpload(slug: slug, folder: folder), slug: slug)
            _ = try await api.publish(repo, slug: slug, files: rel, message: "publish: \(slug)", branch: branch)
            // Reflect the successful publish immediately — no re-list (which is
            // eventually consistent right after a write anyway).
            upsertSkill(SkillSummary(slug: slug, name: name.isEmpty ? slug : name, description: desc))
            showToast("Published \(slug)", .ok)
        } catch {
            showToast("Publish failed: \(error.localizedDescription)", .error)
        }
    }

    /// Insert or replace a summary row in the sorted local list.
    private func upsertSkill(_ summary: SkillSummary) {
        skills.removeAll { $0.slug == summary.slug }
        skills = (skills + [summary]).sorted { $0.slug < $1.slug }
    }

    /// `Scan.filesForUpload` prefixes every path with "<slug>/"; both publish
    /// and install want paths relative to the skill folder, so strip it.
    private func stripSlugPrefix(_ files: [String: Data], slug: String) -> [String: Data] {
        let prefix = slug + "/"
        var rel: [String: Data] = [:]
        for (k, v) in files { rel[String(k.dropFirst(prefix.count))] = v }
        return rel
    }

    // MARK: - local import

    func scanLocal() -> [LocalSkill] {
        if isDemo { return Self.demoLocal }
        let home = FileManager.default.homeDirectoryForCurrentUser.path
        let cwd = FileManager.default.currentDirectoryPath
        let sources = Scan.discoverSources(home: home, cwd: cwd, dotDirs: Agents.dotDirs())
        let all = Scan.discover(sources)
        // Normalized comparison (mirrors Go scan.DedupeAgainst): a local
        // "simplify_swarm" dedupes against registry "simplify-swarm".
        return Scan.dedupeAgainst(all, remoteSlugs: skills.map(\.slug))
            .filter { $0.slug != "skills-registry" }
    }

    func importSkills(_ locals: [LocalSkill], progress: @escaping @Sendable (Int, Int) -> Void) async {
        guard let api, let repo else { return }
        // Defensive: never overwrite a slug already in the registry. The Import
        // screen pre-filters these, but guard the write path directly too.
        // Normalized comparison (mirrors Go scan.DedupeAgainst).
        let fresh = Scan.dedupeAgainst(locals, remoteSlugs: skills.map(\.slug))
        let skipped = locals.count - fresh.count
        guard !fresh.isEmpty else {
            showToast(skipped > 0 ? "All selected skills already exist in the registry." : "Nothing to import.", .info)
            return
        }
        do {
            var files: [String: Data] = [:]
            for sk in fresh {
                let f = try Scan.filesForUpload(slug: sk.slug, folder: sk.folder)
                files.merge(f) { a, _ in a }
            }
            guard !files.isEmpty else { showToast("Nothing to import.", .info); return }
            _ = try await api.bulkPush(repo, files: files,
                                       message: "import: \(fresh.count) skill(s)",
                                       branch: branch, progress: progress)
            // Reflect the successful import immediately — no re-list.
            for sk in fresh {
                upsertSkill(SkillSummary(slug: sk.slug, name: sk.name, description: sk.description))
            }
            let base = "Imported \(fresh.count) skill(s)"
            showToast(skipped > 0 ? "\(base); skipped \(skipped) already in registry" : base, .ok)
        } catch {
            showToast("Import failed: \(error.localizedDescription)", .error)
        }
    }

    // MARK: - CLI

    func installCLI() async {
        // Pin the resolved CLI tag so we never pull the CLI asset from a
        // `macapp-v*` release (the project ships both streams from one repo;
        // GitHub's `releases/latest` is ambiguous across them). Resolution
        // failures surface as their own error — never a "latest" fallback,
        // which would 404 whenever the newest release is an app release.
        let tag: String
        do {
            guard let resolved = try await Updates.latestRelease(repo: AppConfig.projectRepo, channel: .cli) else {
                showToast("No published CLI release found — try again later", .error)
                return
            }
            tag = resolved.tag
        } catch {
            showToast("Couldn't reach GitHub to resolve the latest CLI release", .error)
            return
        }
        do {
            _ = try await CLIInstaller.install(version: tag)
            cliInstalled = true
            cliVersion = await CLIInstaller.installedVersion()
            cliInstallDirOnPath = await CLIInstaller.shellInstallDirOnPath()
            cliUpdate = nil
            showToast("CLI installed to ~/.local/bin/skills-registry", .ok)
        } catch {
            showToast("CLI install failed: \(error.localizedDescription)", .error)
        }
    }

    func refreshCLIStatus() async {
        cliInstalled = CLIInstaller.isInstalled()
        cliVersion = await CLIInstaller.installedVersion()
        cliInstallDirOnPath = cliInstalled
            ? await CLIInstaller.shellInstallDirOnPath()
            : true
    }

    // MARK: - update / meta-skill prompts

    /// Run on entering the ready phase. Recomputes the (local) meta-skill
    /// status every call and checks the CLI release channel on a 6h throttle.
    /// Sparkle owns the app's own self-update, so it isn't checked here.
    func checkForUpdates() async {
        if isDemo { return }
        refreshMetaSkillStatus()
        await refreshCLIStatus()
        await checkCLIUpdate()
    }

    /// Local-only: classify the meta-skill across detected agent dot-folders.
    func refreshMetaSkillStatus() {
        guard let repo else { metaSkill = MetaSkill.Status(); return }
        let home = FileManager.default.homeDirectoryForCurrentUser.path
        metaSkill = MetaSkill.status(home: home, registryRepo: repo.fullName)
    }

    private func checkCLIUpdate() async {
        guard cliInstalled else { cliUpdate = nil; return }
        let now = Date().timeIntervalSince1970
        // Throttle the network call; keep showing an already-found update.
        if cliUpdate == nil, now - defaults.double(forKey: lastCLICheckKey) < cliCheckInterval {
            return
        }
        guard let latest = (try? await Updates.latestRelease(
            repo: AppConfig.projectRepo, channel: .cli)) ?? nil else { return }
        defaults.set(now, forKey: lastCLICheckKey)
        cliUpdate = Updates.isNewer(installed: cliVersion, than: latest.version) ? latest : nil
    }

    /// Install / refresh the `skills-registry` meta-skill into every detected
    /// agent (one click; only writes the missing/outdated ones).
    func installMetaSkill() async {
        guard let repo else { return }
        let home = FileManager.default.homeDirectoryForCurrentUser.path
        do {
            let n = try MetaSkill.install(home: home, registryRepo: repo.fullName)
            refreshMetaSkillStatus()
            if n > 0 {
                showToast("Installed skills-registry skill in \(n) agent\(n == 1 ? "" : "s")", .ok)
            } else {
                showToast("skills-registry skill already up to date", .info)
            }
        } catch {
            showToast("Couldn't install skill: \(error.localizedDescription)", .error)
        }
    }

    // MARK: - prompt dismissal (persisted)

    func dismiss(_ key: String) {
        dismissedKeys.insert(key)
        defaults.set(Array(dismissedKeys), forKey: dismissKey)
    }

    func isDismissed(_ key: String) -> Bool { dismissedKeys.contains(key) }

    /// Stable key for the current CLI update prompt (re-pesters on a new tag).
    var cliUpdateKey: String? { cliUpdate.map { "cli:\($0.version.string)" } }

    /// Stable key for the meta-skill prompt (re-pesters when the situation
    /// changes — a new agent appears, or a refresh is needed).
    var metaSkillKey: String {
        let missing = metaSkill.targets.filter { $0.state == .missing }.count
        let outdated = metaSkill.targets.filter { $0.state == .outdated }.count
        return "skill:\(repo?.fullName ?? "-"):\(missing)-\(outdated)"
    }

    // MARK: - toast

    func showToast(_ message: String, _ kind: ToastItem.Kind) {
        let item = ToastItem(message: message, kind: kind)
        toast = item
        Task {
            try? await Task.sleep(nanoseconds: 3_500_000_000)
            if self.toast?.id == item.id { self.toast = nil }
        }
    }
}

struct ToastItem: Identifiable, Equatable {
    enum Kind { case ok, error, info }
    let id = UUID()
    let message: String
    let kind: Kind
}

/// An import the post-fetch scan held back: the row, the install targets the
/// confirmation chose, and the review (with findings) that needs
/// acknowledging. Re-running the import with `allowUnsafe` clears it.
struct ScanBlockedImport: Identifiable, Equatable {
    let id = UUID()
    let result: DiscoverResult
    let targets: [AgentTarget]
    let refusal: ImportReview
}

enum Clipboard {
    static func copy(_ s: String) {
        NSPasteboard.general.clearContents()
        NSPasteboard.general.setString(s, forType: .string)
    }
}
