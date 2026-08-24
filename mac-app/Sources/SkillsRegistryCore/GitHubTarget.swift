import Foundation

/// A parsed github.com URL: a repository plus an optional ref and folder path.
/// Swift mirror of Go `registry.GitHubTarget` (`cli/internal/registry/subtree.go`).
/// It covers the three shapes `add` accepts as remote GitHub sources:
///
///     https://github.com/owner/repo                         → ref "", path ""
///     https://github.com/owner/repo/tree/<ref>              → path ""
///     https://github.com/owner/repo/{tree|blob}/<ref>/<dir> → folder target
///
/// `/blob/` and `/tree/` parse identically: the public skill index links skill
/// folders with `/blob/`, and GitHub serves either form for a directory.
public struct GitHubTarget: Equatable, Sendable {
    public var owner: String
    public var repo: String
    public var ref: String
    public var path: String

    public init(owner: String, repo: String, ref: String = "", path: String = "") {
        self.owner = owner
        self.repo = repo
        self.ref = ref
        self.path = path
    }

    /// How many `<ref>/<path>` splits a folder fetch probes when the ref may be
    /// a branch name containing slashes. Matches the Go `maxRefCandidates`.
    static let maxRefCandidates = 4

    /// Parse a github.com repository, tree, or blob URL. Returns nil for any
    /// input that is not a github.com URL naming at least an owner and repo,
    /// and for github.com URLs that are not content links (`/pull/1`,
    /// `/releases`, …) — callers fall back to generic git-URL handling there.
    public static func parse(_ raw: String) -> GitHubTarget? {
        let trimmed = raw.trimmingCharacters(in: .whitespacesAndNewlines)
        guard let comps = URLComponents(string: trimmed),
              let scheme = comps.scheme?.lowercased(), scheme == "http" || scheme == "https",
              let host = comps.host?.lowercased(), host == "github.com" || host == "www.github.com",
              let segs = pathSegments(comps.percentEncodedPath), segs.count >= 2
        else { return nil }

        var repo = segs[1]
        if repo.hasSuffix(".git") { repo = String(repo.dropLast(4)) }
        guard !repo.isEmpty else { return nil }
        var target = GitHubTarget(owner: segs[0], repo: repo)

        let rest = Array(segs.dropFirst(2))
        if rest.isEmpty { return target }
        // Only content links carry a ref. Anything else on github.com (issues,
        // pulls, releases, wiki) is not an importable source.
        guard rest[0] == "tree" || rest[0] == "blob", rest.count >= 2 else { return nil }
        target.ref = rest[1]
        target.path = rest.dropFirst(2).joined(separator: "/")
        return target
    }

    /// Split an escaped URL path into decoded, non-empty segments. Returns nil
    /// when a segment decodes to something that cannot be a repo path
    /// component: a separator, a traversal marker, or an empty string. First
    /// line of defense for remote paths that end up joined onto a local dir.
    static func pathSegments(_ escaped: String) -> [String]? {
        var out: [String] = []
        for raw in escaped.split(separator: "/", omittingEmptySubsequences: true) {
            guard let seg = String(raw).removingPercentEncoding, isSafeSegment(seg) else { return nil }
            out.append(seg)
        }
        return out
    }

    /// Whether a single path component is safe to join onto a local directory.
    static func isSafeSegment(_ seg: String) -> Bool {
        if seg.isEmpty || seg == "." || seg == ".." { return false }
        return !seg.contains("/") && !seg.contains("\\")
    }

    public var fullName: String { "\(owner)/\(repo)" }

    /// HTTPS clone URL for the repository.
    public var cloneURL: String { "https://github.com/\(fullName).git" }

    /// The target rendered back as a github.com link. Used in messages so the
    /// user sees the folder actually fetched, which can differ from what they
    /// pasted when the ref contained slashes.
    public var webURL: String {
        let base = "https://github.com/\(fullName)"
        if ref.isEmpty { return base }
        if path.isEmpty { return base + "/tree/" + ref }
        return base + "/tree/" + ref + "/" + path
    }

    /// Whether the target names a path inside the repository, in which case
    /// only that subtree needs fetching.
    public var isFolder: Bool { !path.isEmpty }

    /// Whether `ref` is a full 40-character commit SHA. Those cannot be passed
    /// to `git clone --branch`, and they make the ref/path split unambiguous.
    public var refIsSHA: Bool {
        ref.count == 40 && ref.allSatisfy(\.isHexDigit)
    }

    /// Candidate ref/path readings of a folder target, most likely first. A
    /// branch name may itself contain slashes (`release/2026-01`) and the URL
    /// gives no way to tell where the ref ends, so a caller that can probe the
    /// API tries each in order. A full commit SHA yields exactly one reading.
    public var splits: [GitHubTarget] {
        if path.isEmpty || refIsSHA { return [self] }
        let segs = path.split(separator: "/").map(String.init)
        var out = [self]
        var i = 0
        while i < segs.count - 1 && out.count < Self.maxRefCandidates {
            out.append(GitHubTarget(
                owner: owner, repo: repo,
                ref: ref + "/" + segs[0...i].joined(separator: "/"),
                path: segs[(i + 1)...].joined(separator: "/")))
            i += 1
        }
        return out
    }
}
