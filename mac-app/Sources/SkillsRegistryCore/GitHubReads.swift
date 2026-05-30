import Foundation

extension GitHubAPI {
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
    /// fetches. Empty/absent repo → []. Sorted by slug.
    public func listSkills(_ repo: RepoRef, branch: String) async throws -> [SkillSummary] {
        let tree: GHTreeResp
        do {
            tree = try await getDecoded("repos/\(repo.fullName)/git/trees/\(branch)?recursive=1",
                                        as: GHTreeResp.self)
        } catch let e as GitHubError where e.isNotFound || e.isConflict {
            return []  // brand-new / empty repo
        }

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
        return summaries.compactMap { $0 }.sorted { $0.slug < $1.slug }
    }

    /// Fetch a single skill: its SKILL.md body + the relative paths of every
    /// file under `<slug>/`.
    public func getSkill(_ repo: RepoRef, slug: String, branch: String) async throws -> SkillDetail {
        let tree = try await getDecoded("repos/\(repo.fullName)/git/trees/\(branch)?recursive=1",
                                        as: GHTreeResp.self)
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
            throw GitHubError(status: 404, message: "Skill \(slug) has no SKILL.md", endpoint: repo.fullName)
        }
        let (name, desc) = Frontmatter.parseSummary(markdown, slug: slug)
        return SkillDetail(slug: slug, name: name, description: desc, markdown: markdown, files: files)
    }

    /// Fetch the UTF-8 contents of a single repo-relative file path (e.g.
    /// "<slug>/scripts/run.sh"). Uses the contents API so callers don't need
    /// the blob SHA in hand. Empty string for binary / unreadable content.
    public func fileContent(_ repo: RepoRef, path: String, branch: String) async throws -> String {
        let encoded = path.split(separator: "/", omittingEmptySubsequences: false)
            .map { $0.addingPercentEncoding(withAllowedCharacters: .urlPathAllowed) ?? String($0) }
            .joined(separator: "/")
        let resp = try await getDecoded("repos/\(repo.fullName)/contents/\(encoded)?ref=\(branch)",
                                        as: GHBlobResp.self)
        guard resp.encoding == "base64" else { return "" }
        let cleaned = resp.content.replacingOccurrences(of: "\n", with: "")
        guard let data = Data(base64Encoded: cleaned),
              let text = String(data: data, encoding: .utf8) else { return "" }
        return text
    }

    // MARK: - blob helpers

    func blobUTF8(_ repo: RepoRef, sha: String) async throws -> String? {
        let blob = try await getDecoded("repos/\(repo.fullName)/git/blobs/\(sha)", as: GHBlobResp.self)
        guard blob.encoding == "base64" else { return nil }
        let cleaned = blob.content.replacingOccurrences(of: "\n", with: "")
        guard let data = Data(base64Encoded: cleaned) else { return nil }
        return String(data: data, encoding: .utf8)
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
