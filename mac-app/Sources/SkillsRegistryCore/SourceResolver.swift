import Foundation

/// Resolve an `add` source string into a local directory to scan. Port of
/// `cli/cmd/skills-registry/add.go:resolveSource` + `validateLocalSourcePath`,
/// extended for the `npx skills add` source formats:
///
/// - local relative path (`./`, `../`, `~`, or absolute `/…`) → validate
///   (relative-only, same rules as the Go CLI) and use in place.
/// - `owner/repo` → `https://github.com/owner/repo.git`, shallow clone.
/// - GitHub `/tree/<ref>/<subpath>` → clone the repo at `<ref>`, narrow
///   discovery to `<subpath>`.
/// - GitLab / `git@…` / any other git URL → clone as-is.
public enum SourceResolver {
    public struct Resolved: Sendable {
        /// Absolute directory the clone / local path resolved to.
        public var dir: String
        /// Optional subpath within `dir` to narrow discovery to (set for
        /// GitHub `/tree/<ref>/<subpath>` URLs).
        public var subpath: String?
        /// Best-effort temp-dir cleanup; no-op for local sources.
        public var cleanup: @Sendable () -> Void

        /// The directory `Scan.discover` should actually walk.
        public var scanRoot: String {
            guard let sub = subpath, !sub.isEmpty else { return dir }
            return (dir as NSString).appendingPathComponent(sub)
        }
    }

    public enum ResolveError: Error, LocalizedError, Equatable {
        case invalidLocalPath(String)
        case notADirectory(String)
        case gitNotFound
        case cloneFailed(String)

        public var errorDescription: String? {
            switch self {
            case .invalidLocalPath(let why): return "Invalid source path: \(why)"
            case .notADirectory(let s): return "Not a directory: \(s)"
            case .gitNotFound:
                return "git was not found. Install the Xcode Command Line Tools (xcode-select --install) or Homebrew git."
            case .cloneFailed(let out): return "git clone failed: \(out)"
            }
        }
    }

    private static let ghShorthand = try! NSRegularExpression(pattern: #"^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$"#)
    private static let windowsDrive = try! NSRegularExpression(pattern: #"^[A-Za-z]:"#)

    /// Resolve `source`. Clones remote sources into a temp dir (caller must
    /// invoke `cleanup` when done). `gitPath` overrides git discovery (tests).
    /// `allowAbsoluteLocal` relaxes the relative-only local-path guard for
    /// directories the user explicitly chose via the native file picker
    /// (`NSOpenPanel` always returns an absolute path); typed input keeps the
    /// strict, Go-parity `validateLocalSourcePath` rules.
    public static func resolve(_ source: String, home: String, cwd: String,
                               gitPath: String? = nil,
                               allowAbsoluteLocal: Bool = false) async throws -> Resolved {
        if isLocalPath(source) {
            let abs: String
            if allowAbsoluteLocal {
                abs = try validateTrustedLocalPath(source, cwd: cwd)
            } else {
                abs = absolutePath(try validateLocalSourcePath(source), cwd: cwd)
            }
            var isDir: ObjCBool = false
            guard FileManager.default.fileExists(atPath: abs, isDirectory: &isDir), isDir.boolValue else {
                throw ResolveError.notADirectory(source)
            }
            return Resolved(dir: abs, subpath: nil, cleanup: {})
        }

        var url = source
        var ref: String?
        var subpath: String?
        if let tree = parseGitHubTreeURL(source) {
            url = tree.cloneURL
            ref = tree.ref
            subpath = tree.subpath
        } else if matches(ghShorthand, source) {
            url = "https://github.com/\(source).git"
        }

        let git = try gitPath ?? resolveGitPath()
        let tmp = (NSTemporaryDirectory() as NSString)
            .appendingPathComponent("skills-registry-add-\(UUID().uuidString)")
        try FileManager.default.createDirectory(atPath: tmp, withIntermediateDirectories: true)
        let cleanup: @Sendable () -> Void = { try? FileManager.default.removeItem(atPath: tmp) }

        var args = ["clone", "--depth", "1", "--single-branch"]
        if let ref, !ref.isEmpty { args += ["--branch", ref] }
        args += [url, tmp]

        let result: Subprocess.Result
        do {
            result = try await Subprocess.run(git, args)
        } catch {
            cleanup()
            throw ResolveError.cloneFailed(error.localizedDescription)
        }
        guard result.exitCode == 0 else {
            cleanup()
            let msg = (result.stderr.isEmpty ? result.stdout : result.stderr)
                .trimmingCharacters(in: .whitespacesAndNewlines)
            throw ResolveError.cloneFailed(msg)
        }
        return Resolved(dir: tmp, subpath: subpath, cleanup: cleanup)
    }

    // MARK: - source classification

    static func isLocalPath(_ source: String) -> Bool {
        source.hasPrefix("./") || source.hasPrefix("/")
            || source.hasPrefix("../") || source.hasPrefix("~")
    }

    /// Relative-only local-path validation. Swift mirror of Go
    /// `validateLocalSourcePath`: rejects backslashes, encoded separators,
    /// tilde, absolute paths, and any `..` traversal segment. Returns the
    /// validated (still-relative) path.
    static func validateLocalSourcePath(_ source: String) throws -> String {
        let path = try decodeAndRejectSeparators(source)
        if path.hasPrefix("~") {
            throw ResolveError.invalidLocalPath("tilde expansion is not allowed")
        }
        if path.hasPrefix("/") || matches(windowsDrive, path) {
            throw ResolveError.invalidLocalPath("absolute paths are not allowed")
        }
        try rejectTraversal(path)
        return path
    }

    /// Validation for a local directory the user picked via the native file
    /// picker (`NSOpenPanel`). Unlike `validateLocalSourcePath` this permits the
    /// absolute path the picker hands back, since it isn't untrusted text input
    /// — but it still rejects backslashes, encoded separators, and any `..`
    /// traversal. Returns the absolute path (relative input is resolved against
    /// `cwd`).
    static func validateTrustedLocalPath(_ source: String, cwd: String) throws -> String {
        let path = try decodeAndRejectSeparators(source)
        try rejectTraversal(path)
        return absolutePath(path, cwd: cwd)
    }

    /// Percent-decode `source`, then reject backslashes and encoded separators
    /// (`%5c` / `%2f`). Shared first step of both local-path validators; the
    /// decoded path is returned for the caller's remaining checks.
    private static func decodeAndRejectSeparators(_ source: String) throws -> String {
        guard let path = source.removingPercentEncoding else {
            throw ResolveError.invalidLocalPath("invalid source path encoding")
        }
        let lower = source.lowercased()
        if path.contains("\\") || lower.contains("%5c") {
            throw ResolveError.invalidLocalPath("backslashes are not allowed")
        }
        if lower.contains("%2f") {
            throw ResolveError.invalidLocalPath("encoded separators are not allowed")
        }
        return path
    }

    /// Reject any `..` path segment.
    private static func rejectTraversal(_ path: String) throws {
        for segment in path.split(separator: "/", omittingEmptySubsequences: false) where segment == ".." {
            throw ResolveError.invalidLocalPath("traversal is not allowed")
        }
    }

    struct TreeURL: Equatable {
        var cloneURL: String
        var ref: String
        var subpath: String?
    }

    /// Parse a GitHub `/tree/<ref>/<subpath>` URL into a clone URL + ref +
    /// subpath. Returns nil for any URL that isn't a github.com tree link.
    /// `<ref>` is taken as the first path segment after `/tree/`; the rest is
    /// the subpath (may be empty → whole repo).
    static func parseGitHubTreeURL(_ source: String) -> TreeURL? {
        guard let comps = URLComponents(string: source),
              let host = comps.host, host == "github.com" || host == "www.github.com"
        else { return nil }
        let parts = comps.path.split(separator: "/", omittingEmptySubsequences: true).map(String.init)
        // owner / repo / "tree" / ref / subpath...
        guard parts.count >= 4, parts[2] == "tree" else { return nil }
        let owner = parts[0]
        var repo = parts[1]
        if repo.hasSuffix(".git") { repo = String(repo.dropLast(4)) }
        let ref = parts[3]
        let subParts = parts.dropFirst(4)
        let subpath = subParts.isEmpty ? nil : subParts.joined(separator: "/")
        return TreeURL(cloneURL: "https://github.com/\(owner)/\(repo).git", ref: ref, subpath: subpath)
    }

    // MARK: - git discovery

    /// Locate a usable `git`: the standard macOS paths first, then `$PATH`.
    static func resolveGitPath() throws -> String {
        let fm = FileManager.default
        for candidate in ["/usr/bin/git", "/opt/homebrew/bin/git", "/usr/local/bin/git"]
        where fm.isExecutableFile(atPath: candidate) {
            return candidate
        }
        if let pathEnv = ProcessInfo.processInfo.environment["PATH"] {
            for dir in pathEnv.split(separator: ":") {
                let p = (String(dir) as NSString).appendingPathComponent("git")
                if fm.isExecutableFile(atPath: p) { return p }
            }
        }
        throw ResolveError.gitNotFound
    }

    // MARK: - helpers

    private static func absolutePath(_ rel: String, cwd: String) -> String {
        if rel.hasPrefix("/") { return rel }
        return ((cwd as NSString).appendingPathComponent(rel) as NSString).standardizingPath
    }

    private static func matches(_ re: NSRegularExpression, _ s: String) -> Bool {
        re.firstMatch(in: s, range: NSRange(s.startIndex..., in: s)) != nil
    }
}
