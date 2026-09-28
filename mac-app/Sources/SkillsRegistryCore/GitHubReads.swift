import Foundation

/// A read result paired with GitHub's recursive-tree truncation flag. When
/// a repo exceeds the tree size limit the API sets `truncated: true` and the
/// listing is incomplete, so callers must warn rather than present `value`
/// as complete. Writes take the stricter path and refuse outright — see
/// `WriteError.treeTruncated`.
public struct Truncated<Value: Sendable>: Sendable {
    public var value: Value
    /// True when the tree listing was cut off and `value` may omit entries.
    public var truncated: Bool

    public init(_ value: Value, truncated: Bool) {
        self.value = value
        self.truncated = truncated
    }
}

/// A read that cannot complete because the tree listing was truncated: the
/// target is absent from a partial listing, so "not found" would be a lie.
public enum ReadError: Error, LocalizedError {
    case treeTruncated(String)

    public var errorDescription: String? {
        switch self {
        case .treeTruncated(let slug):
            return "The registry is too large: GitHub truncated the file listing, so \"\(slug)\" may be missing from this partial result. Narrow or split the repo and try again."
        }
    }
}

extension GitHubAPI {
    /// Percent-encode a branch name for use as a single URL path segment
    /// (trees calls) or a `ref` query value (contents calls). Deliberately
    /// strict (alphanumerics only, same rule `GitHubSubtree.contentsEndpoint`
    /// uses for refs) so slashes, spaces, and `#`/`&`/`?` can never split or
    /// truncate the URL: `feature/x` → `feature%2Fx`.
    static func encodedBranch(_ branch: String) -> String {
        branch.addingPercentEncoding(withAllowedCharacters: .alphanumerics) ?? branch
    }

    /// Encode a branch for a `git/ref/heads/<branch>` path, where slashes are
    /// ref separators and must survive: each segment is encoded alone, so
    /// `feature/x` stays `feature/x` while spaces and `#`/`&`/`?` are escaped.
    static func encodedBranchRef(_ branch: String) -> String {
        branch.split(separator: "/", omittingEmptySubsequences: false)
            .map { encodedBranch(String($0)) }
            .joined(separator: "/")
    }

    /// The authenticated user (GET /user).
    public func currentUser() async throws -> Identity {
        let u = try await getDecoded("user", as: GHUser.self)
        return Identity(
            login: u.login,
            name: u.name,
            avatarURL: u.avatar_url.flatMap(URL.init(string:))
        )
    }

    /// Repos accessible to the Skills Registry GitHub App installation(s) for
    /// this user. Drives connect-existing vs. create-new in setup.
    public func skillsRegistryRepos() async throws -> [InstallationRepo] {
        let resp = try await getDecoded("user/installations", as: GHInstallationsResp.self)
        let ours = resp.installations.filter { $0.app_slug == AppConfig.githubAppSlug }
        // Fallback: if slug filtering yields nothing but there's exactly one
        // installation, use it (covers older app-slug mismatches).
        let installs = ours.isEmpty && resp.installations.count == 1 ? resp.installations : ours
        var out: [InstallationRepo] = []
        for inst in installs {
            out.append(contentsOf: try await reposForInstallation(inst.id))
        }
        return out
    }

    private func reposForInstallation(_ id: Int) async throws -> [InstallationRepo] {
        var out: [InstallationRepo] = []
        var page = 1
        while true {
            let resp = try await getDecoded(
                "user/installations/\(id)/repositories?per_page=100&page=\(page)",
                as: GHInstallationReposResp.self)
            out.append(contentsOf: resp.repositories.map {
                InstallationRepo(fullName: $0.full_name,
                                 defaultBranch: $0.default_branch ?? "main",
                                 isPrivate: $0.private ?? false)
            })
            if resp.repositories.count < 100 { break }
            page += 1
        }
        return out
    }

    /// Whether the configured repo is visible to the token. 404 → false.
    public func repoExists(_ repo: RepoRef) async throws -> Bool {
        do {
            _ = try await getDecoded("repos/\(repo.fullName)", as: GHRepo.self)
            return true
        } catch let e as GitHubError where e.isNotFound {
            return false
        }
    }

    /// Default branch for a repo (falls back to "main").
    public func defaultBranch(_ repo: RepoRef) async throws -> String {
        let r = try await getDecoded("repos/\(repo.fullName)", as: GHRepo.self)
        return r.default_branch ?? "main"
    }

    /// Enumerate registry skills with summaries. One recursive tree call to map
    /// slug→treeSHA and slug→SKILL.md blob SHA, then bounded-concurrency blob
    /// fetches. A genuinely empty repo (default branch with no commits yet) →
    /// []; anything else that fails the tree read (bad `@branch`, deleted
    /// repo, …) rethrows so the UI shows a load error. When GitHub truncates
    /// the tree the partial list still returns, flagged, so the UI can warn
    /// instead of silently omitting skills. Sorted by slug.
    public func listSkills(_ repo: RepoRef, branch: String) async throws -> Truncated<[SkillSummary]> {
        let tree: GHTreeResp
        do {
            tree = try await getDecoded("repos/\(repo.fullName)/git/trees/\(Self.encodedBranch(branch))?recursive=1",
                                        as: GHTreeResp.self)
        } catch let e as GitHubError where e.isNotFound || e.isConflict {
            if try await isEmptyRegistry(repo, branch: branch) {
                return Truncated([], truncated: false)  // brand-new / empty repo
            }
            throw e
        }
        let truncated = tree.truncated == true

        var slugTreeSHA: [String: String] = [:]
        var slugBlobSHA: [String: String] = [:]
        for e in tree.tree {
            if e.type == "tree", !e.path.contains("/"), !e.path.hasPrefix(".") {
                slugTreeSHA[e.path] = e.sha
            } else if e.type == "blob" {
                let comps = e.path.split(separator: "/", maxSplits: 1, omittingEmptySubsequences: false)
                if comps.count == 2, comps[1] == "SKILL.md", !comps[0].hasPrefix(".") {
                    slugBlobSHA[String(comps[0])] = e.sha
                }
            }
        }

        let blobSHABySlug = slugBlobSHA
        let treeSHABySlug = slugTreeSHA
        let slugs = blobSHABySlug.keys.sorted()
        let summaries = try await mapConcurrent(slugs, concurrency: 8) { slug -> SkillSummary? in
            guard let blobSHA = blobSHABySlug[slug] else { return nil }
            guard let raw = try await self.blobUTF8(repo, sha: blobSHA) else { return nil }
            let (name, desc) = Frontmatter.parseSummary(raw, slug: slug)
            return SkillSummary(slug: slug, name: name, description: desc,
                                treeSHA: treeSHABySlug[slug] ?? "")
        }
        return Truncated(summaries.compactMap { $0 }.sorted { $0.slug < $1.slug }, truncated: truncated)
    }

    /// True when `repo` exists and `branch` is its default branch with no
    /// commits yet (brand-new repo): the only case where `listSkills` may
    /// truthfully return []. A missing/inaccessible repo, an explicit
    /// `@branch` that doesn't exist, or a default branch whose ref exists but
    /// whose tree read failed all answer false so the caller rethrows.
    func isEmptyRegistry(_ repo: RepoRef, branch: String) async throws -> Bool {
        let info: GHRepo
        do {
            info = try await getDecoded("repos/\(repo.fullName)", as: GHRepo.self)
        } catch {
            return false  // repo missing/inaccessible: surface the tree error
        }
        guard branch == (info.default_branch ?? "main") else { return false }
        do {
            _ = try await getDecoded("repos/\(repo.fullName)/git/ref/heads/\(Self.encodedBranchRef(branch))",
                                     as: GHRefResp.self)
            return false  // ref exists: the tree failure is a real error
        } catch let e as GitHubError where e.isNotFound || e.isConflict {
            return true  // default branch has no commits yet
        }
    }

    /// Fetch a single skill: its SKILL.md body + the relative paths of every
    /// file under `<slug>/`. When the tree is truncated the detail still
    /// returns, flagged, so the UI can warn the file list may be shortened;
    /// when the skill is absent from a truncated listing that is
    /// `.treeTruncated`, not a false "has no SKILL.md".
    public func getSkill(_ repo: RepoRef, slug: String, branch: String) async throws -> SkillDetail {
        let tree = try await getDecoded("repos/\(repo.fullName)/git/trees/\(Self.encodedBranch(branch))?recursive=1",
                                        as: GHTreeResp.self)
        let truncated = tree.truncated == true
        let prefix = "\(slug)/"
        var files: [String] = []
        var skillBlobSHA: String?
        for e in tree.tree where e.type == "blob" && e.path.hasPrefix(prefix) {
            let rel = String(e.path.dropFirst(prefix.count))
            files.append(rel)
            if rel == "SKILL.md" { skillBlobSHA = e.sha }
        }
        files.sort()
        guard let blobSHA = skillBlobSHA, let markdown = try await blobUTF8(repo, sha: blobSHA) else {
            if truncated { throw ReadError.treeTruncated(slug) }
            throw GitHubError(status: 404, message: "Skill \(slug) has no SKILL.md", endpoint: repo.fullName)
        }
        let (name, desc) = Frontmatter.parseSummary(markdown, slug: slug)
        return SkillDetail(slug: slug, name: name, description: desc, markdown: markdown, files: files, truncated: truncated)
    }

    /// Fetch the UTF-8 contents of a single repo-relative file path (e.g.
    /// "<slug>/scripts/run.sh"). Uses the contents API so callers don't need
    /// the blob SHA in hand. Empty string for binary / unreadable content.
    public func fileContent(_ repo: RepoRef, path: String, branch: String) async throws -> String {
        let encoded = path.split(separator: "/", omittingEmptySubsequences: false)
            .map { $0.addingPercentEncoding(withAllowedCharacters: .urlPathAllowed) ?? String($0) }
            .joined(separator: "/")
        let resp = try await getDecoded("repos/\(repo.fullName)/contents/\(encoded)?ref=\(Self.encodedBranch(branch))",
                                        as: GHBlobResp.self)
        guard resp.encoding == "base64" else { return "" }
        let cleaned = resp.content.replacingOccurrences(of: "\n", with: "")
        guard let data = Data(base64Encoded: cleaned),
              let text = String(data: data, encoding: .utf8) else { return "" }
        return text
    }

    /// Fetch every file under `<slug>/` as raw `Data`, keyed by path relative
    /// to the skill folder (e.g. "SKILL.md", "scripts/run.sh"). One recursive
    /// tree call + bounded-concurrency blob fetches. Raw bytes (not UTF-8
    /// decoded) so binaries survive. Feeds `LocalInstall.install` for durable
    /// installs of a registry skill. Throws 404 if the slug has no files —
    /// or `.treeTruncated` when a truncated listing is the reason nothing
    /// was found.
    public func skillFileData(_ repo: RepoRef, slug: String, branch: String) async throws -> Truncated<[String: Data]> {
        let tree = try await getDecoded("repos/\(repo.fullName)/git/trees/\(Self.encodedBranch(branch))?recursive=1",
                                        as: GHTreeResp.self)
        let truncated = tree.truncated == true
        let prefix = "\(slug)/"
        var blobs: [(rel: String, sha: String)] = []
        for e in tree.tree where e.type == "blob" && e.path.hasPrefix(prefix) {
            blobs.append((String(e.path.dropFirst(prefix.count)), e.sha))
        }
        guard !blobs.isEmpty else {
            if truncated { throw ReadError.treeTruncated(slug) }
            throw GitHubError(status: 404, message: "Skill \(slug) has no files", endpoint: repo.fullName)
        }
        let pairs = try await mapConcurrent(blobs, concurrency: 8) { item -> (String, Data) in
            (item.rel, try await self.blobData(repo, sha: item.sha))
        }
        return Truncated(Dictionary(uniqueKeysWithValues: pairs), truncated: truncated)
    }

    // MARK: - blob helpers

    func blobUTF8(_ repo: RepoRef, sha: String) async throws -> String? {
        let blob = try await getDecoded("repos/\(repo.fullName)/git/blobs/\(sha)", as: GHBlobResp.self)
        guard blob.encoding == "base64" else { return nil }
        let cleaned = blob.content.replacingOccurrences(of: "\n", with: "")
        guard let data = Data(base64Encoded: cleaned) else { return nil }
        return String(data: data, encoding: .utf8)
    }

    /// Raw bytes of a blob (base64-decoded). Used by `skillFileData` so binary
    /// supporting files survive the round-trip intact.
    func blobData(_ repo: RepoRef, sha: String) async throws -> Data {
        let blob = try await getDecoded("repos/\(repo.fullName)/git/blobs/\(sha)", as: GHBlobResp.self)
        guard blob.encoding == "base64" else { return Data(blob.content.utf8) }
        let cleaned = blob.content.replacingOccurrences(of: "\n", with: "")
        return Data(base64Encoded: cleaned) ?? Data()
    }
}

/// Run `transform` over `items` with bounded concurrency, preserving input
/// order in the result.
func mapConcurrent<T, R: Sendable>(
    _ items: [T], concurrency: Int, _ transform: @escaping @Sendable (T) async throws -> R
) async throws -> [R] where T: Sendable {
    if items.isEmpty { return [] }
    let limit = max(1, concurrency)
    return try await withThrowingTaskGroup(of: (Int, R).self) { group in
        var results = [R?](repeating: nil, count: items.count)
        var next = 0
        var running = 0
        while next < items.count && running < limit {
            let idx = next; next += 1; running += 1
            group.addTask { (idx, try await transform(items[idx])) }
        }
        while running > 0 {
            let (idx, value) = try await group.next()!
            results[idx] = value
            running -= 1
            if next < items.count {
                let i = next; next += 1; running += 1
                group.addTask { (i, try await transform(items[i])) }
            }
        }
        return results.compactMap { $0 }
    }
}
