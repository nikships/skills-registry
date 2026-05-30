import Foundation

/// Progress callback for long uploads: (filesDone, filesTotal).
public typealias UploadProgress = @Sendable (Int, Int) -> Void

actor Counter {
    private var n = 0
    func inc() -> Int { n += 1; return n }
}

public enum WriteError: Error, LocalizedError {
    case slugNotFound(String)
    case conflictAfterRetries(String)
    case adminPermissionMissing
    case nothingToUpload

    public var errorDescription: String? {
        switch self {
        case .slugNotFound(let s): return "Skill \"\(s)\" not found in the registry."
        case .conflictAfterRetries(let s): return "Could not \(s): the registry kept changing under us. Try again."
        case .adminPermissionMissing:
            return "The Skills Registry app can't create repos (Administration permission not granted). Create it on github.com instead."
        case .nothingToUpload: return "Nothing to upload."
        }
    }
}

extension GitHubAPI {
    /// Create a new repo on the authenticated user's account. Requires the App
    /// to hold Administration:write; a 403 surfaces as `.adminPermissionMissing`
    /// so the UI can offer a github.com fallback.
    public func createRepo(name: String, isPrivate: Bool, description: String) async throws -> RepoRef {
        do {
            let resp = try await postDecoded("user/repos", json: [
                "name": name,
                "private": isPrivate,
                "description": description,
                "auto_init": false,
            ], as: GHRepo.self)
            guard let ref = RepoRef(fullName: resp.full_name) else {
                throw GitHubError(status: 0, message: "Bad repo name from GitHub", endpoint: "user/repos")
            }
            return ref
        } catch let e as GitHubError where e.isForbidden {
            throw WriteError.adminPermissionMissing
        }
    }

    /// Atomically replace `<slug>/` with `files` (paths relative to the skill
    /// folder, e.g. "SKILL.md"). Returns the new commit SHA. Port of
    /// `registry.Client.Publish`.
    @discardableResult
    public func publish(_ repo: RepoRef, slug: String, files: [String: Data],
                        message: String, branch: String) async throws -> String {
        let msg = message.isEmpty ? "publish: \(slug)" : message
        return try await retryOnConflict("publish \(slug)") {
            try await self.publishOnce(repo, slug: slug, files: files, message: msg, branch: branch)
        }
    }

    private func publishOnce(_ repo: RepoRef, slug: String, files: [String: Data],
                            message: String, branch: String) async throws -> String {
        let (parentSHA, baseTreeSHA) = try await headTree(repo, branch: branch)
        let previous = try await listTreePaths(repo, rootSHA: baseTreeSHA, subPath: slug)

        var normalized: [String: Data] = [:]
        var incoming = Set<String>()
        for (rel, content) in files {
            let r = rel.replacingOccurrences(of: "\\", with: "/").trimmingPrefixSlash()
            normalized[r] = content
            incoming.insert(r)
        }
        let blobs = try await uploadBlobs(repo, normalized, progress: nil)

        var entries: [[String: Any]] = []
        for (rel, sha) in blobs {
            entries.append(["path": "\(slug)/\(rel)", "mode": "100644", "type": "blob", "sha": sha])
        }
        for stale in previous.subtracting(incoming).sorted() {
            entries.append(["path": "\(slug)/\(stale)", "mode": "100644", "type": "blob", "sha": NSNull()])
        }
        return try await commitTree(repo, baseTreeSHA: baseTreeSHA, parentSHA: parentSHA,
                                    entries: entries, message: message, branch: branch)
    }

    /// Atomically remove the entire `<slug>/` subtree. Port of `registry.Client.Delete`.
    @discardableResult
    public func delete(_ repo: RepoRef, slug: String, message: String, branch: String) async throws -> String {
        let msg = message.isEmpty ? "remove: \(slug)" : message
        return try await retryOnConflict("delete \(slug)") {
            let (parentSHA, baseTreeSHA) = try await self.headTree(repo, branch: branch)
            let previous = try await self.listTreePaths(repo, rootSHA: baseTreeSHA, subPath: slug)
            if previous.isEmpty { throw WriteError.slugNotFound(slug) }
            let entries: [[String: Any]] = previous.sorted().map {
                ["path": "\(slug)/\($0)", "mode": "100644", "type": "blob", "sha": NSNull()]
            }
            return try await self.commitTree(repo, baseTreeSHA: baseTreeSHA, parentSHA: parentSHA,
                                             entries: entries, message: msg, branch: branch)
        }
    }

    /// Bulk-import many files in a single commit (used by local import). Handles
    /// both an existing branch (base_tree + parent) and a brand-new empty repo
    /// (no base_tree, no parents, create the ref). Additions/overwrites only.
    @discardableResult
    public func bulkPush(_ repo: RepoRef, files: [String: Data], message: String,
                        branch: String, progress: UploadProgress? = nil) async throws -> String {
        if files.isEmpty { throw WriteError.nothingToUpload }
        let blobs = try await uploadBlobs(repo, files, progress: progress)
        let entries: [[String: Any]] = blobs.map {
            ["path": $0.key, "mode": "100644", "type": "blob", "sha": $0.value]
        }

        if let head = try await headTreeIfExists(repo, branch: branch) {
            return try await commitTree(repo, baseTreeSHA: head.treeSHA, parentSHA: head.commitSHA,
                                        entries: entries, message: message, branch: branch)
        }
        // Empty repo: tree with no base, commit with no parents, then create ref.
        let tree = try await postDecoded("repos/\(repo.fullName)/git/trees",
                                         json: ["tree": entries], as: GHShaResp.self)
        let commit = try await postDecoded("repos/\(repo.fullName)/git/commits",
                                           json: ["message": message, "tree": tree.sha, "parents": []],
                                           as: GHShaResp.self)
        _ = try await postDecoded("repos/\(repo.fullName)/git/refs",
                                  json: ["ref": "refs/heads/\(branch)", "sha": commit.sha],
                                  as: GHShaResp.self)
        return commit.sha
    }

    // MARK: - shared Git Data API primitives

    private func retryOnConflict(_ label: String, _ op: @escaping @Sendable () async throws -> String) async throws -> String {
        let maxRetries = 3
        var lastError: Error?
        for attempt in 0..<maxRetries {
            do {
                return try await op()
            } catch let e as GitHubError where e.isConflict {
                lastError = e
                try await Task.sleep(nanoseconds: UInt64(pow(2.0, Double(attempt)) * 0.5 * 1_000_000_000))
            }
        }
        _ = lastError
        throw WriteError.conflictAfterRetries(label)
    }

    /// (parentCommitSHA, baseTreeSHA) for branch HEAD.
    private func headTree(_ repo: RepoRef, branch: String) async throws -> (String, String) {
        let ref = try await getDecoded("repos/\(repo.fullName)/git/ref/heads/\(branch)", as: GHRefResp.self)
        let commit = try await getDecoded("repos/\(repo.fullName)/git/commits/\(ref.object.sha)", as: GHCommitResp.self)
        return (ref.object.sha, commit.tree?.sha ?? "")
    }

    private func headTreeIfExists(_ repo: RepoRef, branch: String) async throws -> (commitSHA: String, treeSHA: String)? {
        do {
            let (c, t) = try await headTree(repo, branch: branch)
            return (c, t)
        } catch let e as GitHubError where e.isNotFound || e.isConflict {
            return nil
        }
    }

    private func commitTree(_ repo: RepoRef, baseTreeSHA: String, parentSHA: String,
                            entries: [[String: Any]], message: String, branch: String) async throws -> String {
        let tree = try await postDecoded("repos/\(repo.fullName)/git/trees",
                                         json: ["base_tree": baseTreeSHA, "tree": entries], as: GHShaResp.self)
        let commit = try await postDecoded("repos/\(repo.fullName)/git/commits",
                                           json: ["message": message, "tree": tree.sha, "parents": [parentSHA]],
                                           as: GHShaResp.self)
        let body = try JSONSerialization.data(withJSONObject: ["sha": commit.sha, "force": false])
        _ = try await sendRetrying(makeRequest("PATCH", "repos/\(repo.fullName)/git/refs/heads/\(branch)", body: body))
        return commit.sha
    }

    private func listTreePaths(_ repo: RepoRef, rootSHA: String, subPath: String) async throws -> Set<String> {
        do {
            let resp = try await getDecoded("repos/\(repo.fullName)/git/trees/\(rootSHA)?recursive=1", as: GHTreeResp.self)
            let prefix = subPath + "/"
            var out = Set<String>()
            for e in resp.tree where e.type == "blob" && e.path.hasPrefix(prefix) {
                out.insert(String(e.path.dropFirst(prefix.count)))
            }
            return out
        } catch let e as GitHubError where e.isNotFound {
            return []
        }
    }

    func uploadBlobs(_ repo: RepoRef, _ files: [String: Data], progress: UploadProgress?) async throws -> [String: String] {
        if files.isEmpty { return [:] }
        let keys = Array(files.keys)
        let total = keys.count
        let counter = Counter()
        let pairs = try await mapConcurrent(keys, concurrency: 8) { key -> (String, String) in
            let sha = try await self.uploadBlob(repo, data: files[key]!)
            let done = await counter.inc()
            progress?(done, total)
            return (key, sha)
        }
        return Dictionary(uniqueKeysWithValues: pairs)
    }

    private func uploadBlob(_ repo: RepoRef, data: Data) async throws -> String {
        let resp = try await postDecoded("repos/\(repo.fullName)/git/blobs",
                                         json: ["content": data.base64EncodedString(), "encoding": "base64"],
                                         as: GHShaResp.self)
        return resp.sha
    }
}

extension String {
    func trimmingPrefixSlash() -> String {
        var s = self
        while s.hasPrefix("/") { s.removeFirst() }
        return s
    }
}
