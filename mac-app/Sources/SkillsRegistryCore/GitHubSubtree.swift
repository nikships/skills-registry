import Foundation

/// One folder fetched out of a GitHub repository without cloning it.
public struct FetchedFolder: Sendable, Equatable {
    /// Absolute directory the folder's contents were written into. Its
    /// basename is the folder's own name, so skill discovery sees the same
    /// layout a clone would have produced.
    public var dir: String
    /// The ref/path split that actually resolved.
    public var target: GitHubTarget
    /// Folder-relative slash-separated paths written, sorted.
    public var paths: [String]

    public init(dir: String, target: GitHubTarget, paths: [String]) {
        self.dir = dir
        self.target = target
        self.paths = paths
    }
}

/// Downloads a single folder out of any GitHub repository. `SourceResolver`
/// depends on this rather than on `GitHubAPI` so tests can inject a fake with
/// no network access.
public protocol GitHubFolderFetching: Sendable {
    func fetchFolder(_ target: GitHubTarget, into destRoot: String) async throws -> FetchedFolder
}

/// Error from a subtree fetch. Distinct from `GitHubError` so the caller can
/// tell "GitHub said no" from "that folder isn't importable".
public enum SubtreeError: Error, LocalizedError, Equatable {
    case notAFolder(String)
    case notFound(String)
    case empty(String)
    case unsafePath(String)
    case tooManyFiles(String, Int)
    case tooDeep(String, Int)
    case unsupportedEncoding(String)

    public var errorDescription: String? {
        switch self {
        case .notAFolder(let url):
            return "\(url) names no folder inside the repository."
        case .notFound(let url):
            return "Could not find \(url) on GitHub (check the branch, tag, or commit and the folder path)."
        case .empty(let url):
            return "\(url) is empty."
        case .unsafePath(let p):
            return "Refusing unsafe path \(p) from the GitHub API response."
        case .tooManyFiles(let url, let limit):
            return "\(url) holds more than \(limit) files; point the URL at a single skill folder."
        case .tooDeep(let url, let limit):
            return "\(url) nests deeper than \(limit) levels."
        case .unsupportedEncoding(let p):
            return "\(p): unsupported content encoding."
        }
    }
}

/// Contents-API subtree fetch. Swift mirror of Go `registry.Fetcher`
/// (`cli/internal/registry/subtree.go`): walk one folder recursively and
/// materialize it locally, never cloning the parent repository.
extension GitHubAPI: GitHubFolderFetching {
    /// A skill folder is a handful of files; past this the URL points at a
    /// source tree rather than a skill.
    static let maxFolderFiles = 1000
    static let maxFolderDepth = 32

    /// Download `target`'s folder (recursively, including nested `scripts/`,
    /// `references/`, `assets/`, …) into a new directory under `destRoot`
    /// named after the folder itself. A `/blob/` URL pointing at a file
    /// resolves to that file's directory.
    public func fetchFolder(_ target: GitHubTarget, into destRoot: String) async throws -> FetchedFolder {
        guard target.isFolder else { throw SubtreeError.notAFolder(target.webURL) }
        let (resolved, entries) = try await resolveFolder(target)
        let dir = (destRoot as NSString)
            .appendingPathComponent((resolved.path as NSString).lastPathComponent)
        try FileManager.default.createDirectory(atPath: dir, withIntermediateDirectories: true)
        var paths: [String] = []
        try await walk(resolved, rel: "", entries: entries, dir: dir, paths: &paths, depth: 0)
        guard !paths.isEmpty else { throw SubtreeError.empty(resolved.webURL) }
        paths.sort()
        return FetchedFolder(dir: dir, target: resolved, paths: paths)
    }

    /// Find the ref/path split that exists and return its listing. A candidate
    /// whose root listing 404s is a wrong split (or a bad URL); any other
    /// failure surfaces immediately.
    private func resolveFolder(_ target: GitHubTarget) async throws -> (GitHubTarget, [ContentsEntry]) {
        for candidate in target.splits {
            let listing: ContentsListing
            do {
                listing = try await listPath(candidate, repoPath: candidate.path)
            } catch let e as GitHubError where e.isNotFound {
                continue
            }
            switch listing {
            case .directory(let entries):
                return (candidate, entries)
            case .file:
                // A `/blob/` link to a file (the index links SKILL.md itself
                // this way): import the directory holding it.
                let parent = (candidate.path as NSString).deletingLastPathComponent
                guard !parent.isEmpty, parent != "/" else {
                    throw SubtreeError.notAFolder(candidate.webURL)
                }
                var folder = candidate
                folder.path = parent
                guard case .directory(let entries) = try await listPath(folder, repoPath: parent) else {
                    throw SubtreeError.notAFolder(folder.webURL)
                }
                return (folder, entries)
            }
        }
        throw SubtreeError.notFound(target.webURL)
    }

    private func walk(_ t: GitHubTarget, rel: String, entries: [ContentsEntry],
                      dir: String, paths: inout [String], depth: Int) async throws {
        guard depth <= Self.maxFolderDepth else {
            throw SubtreeError.tooDeep(t.webURL, Self.maxFolderDepth)
        }
        let fm = FileManager.default
        for e in entries {
            guard GitHubTarget.isSafeSegment(e.name) else { throw SubtreeError.unsafePath(e.name) }
            let childRel = rel.isEmpty ? e.name : rel + "/" + e.name
            let full = try Self.safeJoin(dir: dir, childRel: childRel)
            switch e.type {
            case "dir":
                let repoPath = t.path + "/" + childRel
                guard case .directory(let sub) = try await listPath(t, repoPath: repoPath) else { continue }
                try fm.createDirectory(atPath: full, withIntermediateDirectories: true)
                try await walk(t, rel: childRel, entries: sub, dir: dir, paths: &paths, depth: depth + 1)
            case "file":
                guard paths.count < Self.maxFolderFiles else {
                    throw SubtreeError.tooManyFiles(t.webURL, Self.maxFolderFiles)
                }
                let data = try await fileBytes(t, entry: e, repoPath: t.path + "/" + childRel)
                try fm.createDirectory(atPath: (full as NSString).deletingLastPathComponent,
                                       withIntermediateDirectories: true)
                try data.write(to: URL(fileURLWithPath: full), options: .atomic)
                paths.append(childRel)
            default:
                break  // "symlink" and "submodule" carry no importable content
            }
        }
    }

    /// Join a folder-relative path onto `dir`, rejecting anything that would
    /// escape it. Each component already passed `isSafeSegment`; the lexical
    /// clean plus prefix check are defense in depth against a hostile
    /// Contents response.
    static func safeJoin(dir: String, childRel: String) throws -> String {
        let clean: String
        do {
            clean = try LocalInstall.validateRelPath(childRel)
        } catch {
            throw SubtreeError.unsafePath(childRel)
        }
        let full = ((dir as NSString).appendingPathComponent(clean) as NSString).standardizingPath
        let root = (dir as NSString).standardizingPath
        guard full == root || full.hasPrefix(root + "/") else {
            throw SubtreeError.unsafePath(childRel)
        }
        return full
    }

    /// A file entry's raw bytes. Files over the Contents API's 1 MB inline
    /// limit come back with no usable body, so those fall back to the blob API.
    private func fileBytes(_ t: GitHubTarget, entry: ContentsEntry, repoPath: String) async throws -> Data {
        let blob = try await getDecoded(Self.contentsEndpoint(t, repoPath: repoPath), as: GHBlobResp.self)
        if blob.encoding == "base64" {
            return Self.decodeBase64(blob.content)
        }
        guard !entry.sha.isEmpty else { throw SubtreeError.unsupportedEncoding(repoPath) }
        let large = try await getDecoded("repos/\(t.fullName)/git/blobs/\(entry.sha)", as: GHBlobResp.self)
        guard large.encoding == "base64" else { throw SubtreeError.unsupportedEncoding(repoPath) }
        return Self.decodeBase64(large.content)
    }

    private func listPath(_ t: GitHubTarget, repoPath: String) async throws -> ContentsListing {
        let (data, _) = try await send(makeRequest("GET", Self.contentsEndpoint(t, repoPath: repoPath)))
        return try ContentsListing(data: data)
    }

    /// Build a Contents endpoint, escaping every path segment and pinning the
    /// ref when the URL carried one, so a moving branch can't mix revisions.
    static func contentsEndpoint(_ t: GitHubTarget, repoPath: String) -> String {
        let escaped = repoPath.split(separator: "/", omittingEmptySubsequences: true)
            .map { String($0).addingPercentEncoding(withAllowedCharacters: .urlPathAllowed) ?? String($0) }
            .joined(separator: "/")
        var endpoint = "repos/\(t.fullName)/contents/\(escaped)"
        if !t.ref.isEmpty {
            let ref = t.ref.addingPercentEncoding(withAllowedCharacters: .alphanumerics) ?? t.ref
            endpoint += "?ref=\(ref)"
        }
        return endpoint
    }

    static func decodeBase64(_ content: String) -> Data {
        Data(base64Encoded: content.replacingOccurrences(of: "\n", with: "")) ?? Data()
    }
}

/// One entry in a Contents directory listing.
struct ContentsEntry: Decodable {
    let name: String
    let type: String  // "file" | "dir" | "symlink" | "submodule"
    let sha: String

    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        name = try c.decode(String.self, forKey: .name)
        type = try c.decode(String.self, forKey: .type)
        sha = try c.decodeIfPresent(String.self, forKey: .sha) ?? ""
    }

    init(name: String, type: String, sha: String = "") {
        self.name = name
        self.type = type
        self.sha = sha
    }

    enum CodingKeys: String, CodingKey { case name, type, sha }
}

/// The Contents API returns an array for a directory and an object for a file,
/// so a response has to be classified before decoding.
enum ContentsListing {
    case directory([ContentsEntry])
    case file

    init(data: Data) throws {
        let first = data.first { !" \t\r\n".utf8.contains($0) }
        if first == UInt8(ascii: "{") {
            self = .file
            return
        }
        self = .directory(try JSONDecoder().decode([ContentsEntry].self, from: data))
    }
}
