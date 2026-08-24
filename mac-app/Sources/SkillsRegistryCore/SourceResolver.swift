import Foundation

/// Resolve an `add` source string into a local directory to scan. Port of
/// `cli/cmd/skills-registry/add.go:resolveSource` + `validateLocalSourcePath`,
/// extended for the `npx skills add` source formats:
///
/// - local relative path (`./`, `../`, `~`, or absolute `/…`) → validate
///   (relative-only, same rules as the Go CLI) and use in place.
/// - `owner/repo` → `https://github.com/owner/repo.git`, shallow clone.
/// - GitHub `{tree|blob}/<ref>/<dir>` → fetch only that folder through the
///   GitHub Contents API. The parent repository is never cloned, so importing
///   one skill out of a monorepo costs a handful of API calls.
/// - GitHub repo or `/tree/<branch>` link → shallow clone, branch pinned.
/// - GitLab / `git@…` / any other git URL → clone as-is.
///
/// Accepted URL shapes are kept identical to the Go CLI's
/// `registry.ParseGitHubURL`.
public enum SourceResolver {
    public struct Resolved: Sendable {
        /// Absolute directory `Scan.discover` should walk: the local path, the
        /// clone root, or the temp dir holding a fetched folder.
        public var dir: String
        /// Best-effort temp-dir cleanup; no-op for local sources.
        public var cleanup: @Sendable () -> Void

        public init(dir: String, cleanup: @escaping @Sendable () -> Void) {
            self.dir = dir
            self.cleanup = cleanup
        }
    }

    public enum ResolveError: Error, LocalizedError, Equatable {
        case invalidLocalPath(String)
        case notADirectory(String)
        case gitNotFound
        case cloneFailed(String)
        case noSkillFile(String, Int)
        case folderFetchUnavailable

        public var errorDescription: String? {
            switch self {
            case .invalidLocalPath(let why): return "Invalid source path: \(why)"
            case .notADirectory(let s): return "Not a directory: \(s)"
            case .gitNotFound:
                return "git was not found. Install the Xcode Command Line Tools (xcode-select --install) or Homebrew git."
            case .cloneFailed(let out): return "git clone failed: \(out)"
            case .noSkillFile(let url, let count):
                return "\(url) has no \(Scan.mainFileName) (found \(count) file(s)); "
                    + "point the URL at a skill folder or its parent."
            case .folderFetchUnavailable:
                return "Sign in to GitHub to import a folder URL."
            }
        }
    }

    private static let ghShorthand = try! NSRegularExpression(pattern: #"^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$"#)
    private static let windowsDrive = try! NSRegularExpression(pattern: #"^[A-Za-z]:"#)

    /// Resolve `source`. Fetches or clones remote sources into a temp dir
    /// (caller must invoke `cleanup` when done). `gitPath` overrides git
    /// discovery (tests). `folderFetcher` supplies the GitHub Contents-API
    /// client used for folder URLs; without one, a folder URL cannot be
    /// imported. `allowAbsoluteLocal` relaxes the relative-only local-path
    /// guard for directories the user explicitly chose via the native file
    /// picker (`NSOpenPanel` always returns an absolute path); typed input
    /// keeps the strict, Go-parity `validateLocalSourcePath` rules.
    public static func resolve(_ source: String, home: String, cwd: String,
                               gitPath: String? = nil,
                               folderFetcher: GitHubFolderFetching? = nil,
                               allowAbsoluteLocal: Bool = false) async throws -> Resolved {
        if isLocalPath(source) {
            return try resolveLocal(source, cwd: cwd, allowAbsoluteLocal: allowAbsoluteLocal)
        }
        let target = GitHubTarget.parse(source)
        if let target, target.isFolder {
            guard let folderFetcher else { throw ResolveError.folderFetchUnavailable }
            return try await fetchFolder(target, using: folderFetcher)
        }
        let (url, ref) = cloneURLAndRef(source, target: target)
        return try await clone(url: url, ref: ref, gitPath: gitPath)
    }

    private static func resolveLocal(_ source: String, cwd: String,
                                     allowAbsoluteLocal: Bool) throws -> Resolved {
        let abs = allowAbsoluteLocal
            ? try validateTrustedLocalPath(source, cwd: cwd)
            : absolutePath(try validateLocalSourcePath(source), cwd: cwd)
        var isDir: ObjCBool = false
        guard FileManager.default.fileExists(atPath: abs, isDirectory: &isDir), isDir.boolValue else {
            throw ResolveError.notADirectory(source)
        }
        return Resolved(dir: abs, cleanup: {})
    }

    /// Fetch only the target folder through the Contents API and return the
    /// temp dir holding it, so the caller's discover → select → publish
    /// pipeline runs unchanged against a folder that was never cloned.
    private static func fetchFolder(_ target: GitHubTarget,
                                    using fetcher: GitHubFolderFetching) async throws -> Resolved {
        let tmp = try makeTempDir()
        let cleanup: @Sendable () -> Void = { try? FileManager.default.removeItem(atPath: tmp) }
        let folder: FetchedFolder
        do {
            folder = try await fetcher.fetchFolder(target, into: tmp)
        } catch {
            cleanup()
            throw error
        }
        guard folder.paths.contains(where: { ($0 as NSString).lastPathComponent == Scan.mainFileName }) else {
            cleanup()
            throw ResolveError.noSkillFile(folder.target.webURL, folder.paths.count)
        }
        return Resolved(dir: tmp, cleanup: cleanup)
    }

    /// Map a non-folder source to the URL git should clone and the branch to
    /// pin, if any. Mirrors Go `cloneURLAndRef`: `owner/repo` shorthand expands
    /// to a GitHub HTTPS remote; a github.com repo or `/tree/<branch>` link
    /// cannot be handed to git verbatim, so it becomes the clone URL plus its
    /// branch; anything else (GitLab, `git@…`) is cloned as-is. A full commit
    /// SHA is dropped because `git clone --branch <sha>` fails.
    static func cloneURLAndRef(_ source: String, target: GitHubTarget?) -> (url: String, ref: String) {
        if matches(ghShorthand, source) { return ("https://github.com/\(source).git", "") }
        guard let target else { return (source, "") }
        return (target.cloneURL, target.refIsSHA ? "" : target.ref)
    }

    private static func clone(url: String, ref: String, gitPath: String?) async throws -> Resolved {
        let git = try gitPath ?? resolveGitPath()
        let tmp = try makeTempDir()
        let cleanup: @Sendable () -> Void = { try? FileManager.default.removeItem(atPath: tmp) }

        var args = ["clone", "--depth", "1", "--single-branch"]
        if !ref.isEmpty { args += ["--branch", ref] }
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
        return Resolved(dir: tmp, cleanup: cleanup)
    }

    private static func makeTempDir() throws -> String {
        let tmp = (NSTemporaryDirectory() as NSString)
            .appendingPathComponent("skills-registry-add-\(UUID().uuidString)")
        try FileManager.default.createDirectory(atPath: tmp, withIntermediateDirectories: true)
        return tmp
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
