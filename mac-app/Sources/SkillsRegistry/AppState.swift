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

    /// Remove a skill end-to-end, mirroring `skills-registry remove`: delete
    /// the `<slug>/` subtree from the registry, then sweep the two local
    /// footprints (MCP download cache + every agent dot-folder copy).
    func remove(_ slug: String) async {
        guard let api, let repo else { return }
        do {
            _ = try await api.delete(repo, slug: slug, message: "remove: \(slug)", branch: branch)
            // Drop it locally right away — GitHub's tree listing is eventually
            // consistent just after the ref update, so the re-list below can
            // still return the slug. Re-apply the removal afterward to be sure.
            skills.removeAll { $0.slug == slug }

            // Local cleanup: MCP cache + agent dot-folders (matches remove.go).
            let home = FileManager.default.homeDirectoryForCurrentUser.path
            let cwd = FileManager.default.currentDirectoryPath
            let cacheCleared = LocalRemove.removeFromCache(slug: slug)
            let dotFolders = LocalRemove.removeFromDotFolders(slug: slug, home: home, cwd: cwd)

            showToast(removeSummary(slug: slug, cacheCleared: cacheCleared, dotFolders: dotFolders.count), .ok)
            await refreshSkills()
            skills.removeAll { $0.slug == slug }
            refreshMetaSkillStatus()
        } catch {
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
    /// `<dot>/skills/<slug>/`. The MCP download cache is never touched (that's
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
    /// `/tree/` URL), discover its skills, and filter out slugs already in the
    /// registry (dup-safe like `importSkills`). The resolved temp clone is kept
    /// alive until the next `resolveAndScan` or a `publishAndInstall` call so
    /// the discovered `folder` paths stay readable for upload.
    /// Returns the discovered (dup-filtered) skills, or `nil` if the source
    /// couldn't be resolved/scanned — letting the caller distinguish a fetch
    /// failure from a genuinely empty result. `trustedLocalDir` relaxes the
    /// relative-only path guard for directories chosen via the native picker.
    func resolveAndScan(_ source: String, trustedLocalDir: Bool = false) async -> [LocalSkill]? {
        if isDemo { return Self.demoLocal }
        addCleanup?()
        addCleanup = nil
        let home = FileManager.default.homeDirectoryForCurrentUser.path
        let cwd = FileManager.default.currentDirectoryPath
        do {
            let resolved = try await SourceResolver.resolve(
                source, home: home, cwd: cwd, allowAbsoluteLocal: trustedLocalDir)
            addCleanup = resolved.cleanup
            let discovered = Scan.discover([Scan.Source(path: resolved.scanRoot, label: source)])
            // Normalize both sides so a local "simplify_swarm" dedupes against a
            // registry "simplify-swarm" (mirrors Go scan.DedupeAgainst).
            let existing = Set(skills.map { normalizeForMatch($0.slug) })
            let fresh = discovered.filter {
                !existing.contains(normalizeForMatch($0.slug)) && $0.slug != MetaSkill.slug
            }
            if discovered.isEmpty {
                showToast("No SKILL.md files found under \(source).", .info)
            }
            return fresh
        } catch {
            addCleanup = nil
            showToast("Couldn't fetch source: \(error.localizedDescription)", .error)
            return nil
        }
    }

    /// Publish each selected skill to the registry, then durably install it
    /// into the chosen agents. Dup-safe: slugs already in the registry are
    /// skipped. Cleans up the resolved temp clone when done.
    func publishAndInstall(_ locals: [LocalSkill], targets: [AgentTarget],
                           progress: @escaping @Sendable (Int, Int) -> Void) async {
        guard let api, let repo else { return }
        defer { addCleanup?(); addCleanup = nil }
        // Normalize both sides so separator/case-only variants dedupe against
        // an existing registry slug (mirrors Go scan.DedupeAgainst).
        let existing = Set(skills.map { normalizeForMatch($0.slug) })
        let fresh = locals.filter { !existing.contains(normalizeForMatch($0.slug)) }
        let skipped = locals.count - fresh.count
        guard !fresh.isEmpty else {
            showToast(skipped > 0 ? "All selected skills already exist in the registry." : "Nothing to add.", .info)
            return
        }
        let home = FileManager.default.homeDirectoryForCurrentUser.path
        let total = fresh.count
        var done = 0
        do {
            for sk in fresh {
                let rel = stripSlugPrefix(try Scan.filesForUpload(slug: sk.slug, folder: sk.folder), slug: sk.slug)
                _ = try await api.publish(repo, slug: sk.slug, files: rel,
                                          message: "add: \(sk.slug)", branch: branch)
                if !targets.isEmpty {
                    _ = try LocalInstall.install(slug: sk.slug, files: rel, targets: targets, home: home, cwd: home)
                }
                done += 1
                progress(done, total)
            }
            let base = targets.isEmpty
                ? "Added \(fresh.count) skill\(fresh.count == 1 ? "" : "s")"
                : "Added + installed \(fresh.count) skill\(fresh.count == 1 ? "" : "s")"
            showToast(skipped > 0 ? "\(base); skipped \(skipped) already in registry" : base, .ok)
            await refreshSkills()
            refreshMetaSkillStatus()
        } catch {
            showToast("Add failed: \(error.localizedDescription)", .error)
        }
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
        let (name, _) = Frontmatter.parseSummary(text, slug: folderName)
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
            showToast("Published \(slug)", .ok)
            await refreshSkills()
        } catch {
            showToast("Publish failed: \(error.localizedDescription)", .error)
        }
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
        let existing = Set(skills.map(\.slug))
        return all.filter { !existing.contains($0.slug) && $0.slug != "skills-registry" }
    }

    func importSkills(_ locals: [LocalSkill], progress: @escaping @Sendable (Int, Int) -> Void) async {
        guard let api, let repo else { return }
        // Defensive: never overwrite a slug already in the registry. The Import
        // screen pre-filters these, but guard the write path directly too.
        let existing = Set(skills.map(\.slug))
        let fresh = locals.filter { !existing.contains($0.slug) }
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
            let base = "Imported \(fresh.count) skill(s)"
            showToast(skipped > 0 ? "\(base); skipped \(skipped) already in registry" : base, .ok)
            await refreshSkills()
        } catch {
            showToast("Import failed: \(error.localizedDescription)", .error)
        }
    }

    // MARK: - CLI

    func installCLI() async {
        do {
            // Pin the resolved CLI tag so we never pull the CLI asset from a
            // `macapp-v*` release (the project ships both streams from one repo;
            // GitHub's `releases/latest` is ambiguous across them).
            let resolved = try? await Updates.latestRelease(repo: AppConfig.projectRepo, channel: .cli)
            let version = (resolved ?? nil)?.tag ?? "latest"
            _ = try await CLIInstaller.install(version: version)
            cliInstalled = true
            cliVersion = await CLIInstaller.installedVersion()
            cliUpdate = nil
            showToast("CLI installed to ~/.local/bin/skills-registry", .ok)
        } catch {
            showToast("CLI install failed: \(error.localizedDescription)", .error)
        }
    }

    func refreshCLIStatus() async {
        cliInstalled = CLIInstaller.isInstalled()
        cliVersion = await CLIInstaller.installedVersion()
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

enum Clipboard {
    static func copy(_ s: String) {
        NSPasteboard.general.clearContents()
        NSPasteboard.general.setString(s, forType: .string)
    }
}
