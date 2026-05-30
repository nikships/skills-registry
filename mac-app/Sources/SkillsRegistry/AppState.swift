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

    let isDemo: Bool
    private var token: String?
    private var api: GitHubAPI?
    private var authTask: Task<Void, Never>?
    private let flow = DeviceFlow()

    init(demo: Bool = false) {
        self.isDemo = demo
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

    func remove(_ slug: String) async {
        guard let api, let repo else { return }
        do {
            _ = try await api.delete(repo, slug: slug, message: "remove: \(slug)", branch: branch)
            showToast("Removed \(slug)", .ok)
            await refreshSkills()
        } catch {
            showToast("Remove failed: \(error.localizedDescription)", .error)
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
        do {
            let files = try Scan.filesForUpload(slug: slug, folder: folder)
            // filesForUpload prefixes with "<slug>/"; publish wants paths
            // relative to the skill folder, so strip the prefix.
            var rel: [String: Data] = [:]
            let prefix = slug + "/"
            for (k, v) in files { rel[String(k.dropFirst(prefix.count))] = v }
            _ = try await api.publish(repo, slug: slug, files: rel, message: "publish: \(slug)", branch: branch)
            showToast("Published \(slug)", .ok)
            await refreshSkills()
        } catch {
            showToast("Publish failed: \(error.localizedDescription)", .error)
        }
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
        do {
            var files: [String: Data] = [:]
            for sk in locals {
                let f = try Scan.filesForUpload(slug: sk.slug, folder: sk.folder)
                files.merge(f) { a, _ in a }
            }
            guard !files.isEmpty else { showToast("Nothing to import.", .info); return }
            _ = try await api.bulkPush(repo, files: files,
                                       message: "import: \(locals.count) skill(s)",
                                       branch: branch, progress: progress)
            showToast("Imported \(locals.count) skill(s)", .ok)
            await refreshSkills()
        } catch {
            showToast("Import failed: \(error.localizedDescription)", .error)
        }
    }

    // MARK: - CLI

    func installCLI() async {
        do {
            _ = try await CLIInstaller.install()
            cliInstalled = true
            cliVersion = await CLIInstaller.installedVersion()
            showToast("CLI installed to ~/.local/bin/skills-registry", .ok)
        } catch {
            showToast("CLI install failed: \(error.localizedDescription)", .error)
        }
    }

    func refreshCLIStatus() async {
        cliInstalled = CLIInstaller.isInstalled()
        cliVersion = await CLIInstaller.installedVersion()
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
