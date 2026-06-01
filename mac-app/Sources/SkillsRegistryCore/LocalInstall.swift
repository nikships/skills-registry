import Foundation

/// Durable local install of a skill into selected agent dot-folders. Port of
/// the copy half of `cli/cmd/skills-registry/install_local.go`
/// (`installSkillIntoTargets` + `copyTree` + `copyFileForInstall`).
///
/// Unlike `get`, this never touches the global MCP cache: it writes the
/// skill's files straight into each target's `<dot>/skills/<slug>/`. The Go
/// CLI fetches into a scratch dir and copies; the app already has the bytes in
/// hand (from `GitHubReads.skillFileData` or `Scan.filesForUpload`), so it
/// writes them directly.
public enum LocalInstall {
    public enum InstallError: Error, LocalizedError, Equatable {
        case noTargets
        case invalidPath(String)
        case symlinkRejected(String)

        public var errorDescription: String? {
            switch self {
            case .noTargets:
                return "Install requires at least one target."
            case .invalidPath(let p):
                return "Refusing to write unsafe path: \(p)"
            case .symlinkRejected(let p):
                return "Refusing to write through a symlink: \(p)"
            }
        }
    }

    /// Write `files` (keyed by path relative to the skill folder, e.g.
    /// "SKILL.md" / "scripts/run.sh") into `<target.skillsDir>/<slug>/<rel>`
    /// for every target. Existing files are overwritten so a re-install
    /// refreshes the local copy. Returns the absolute `<slug>` directory
    /// written for each target (one per target), sorted.
    @discardableResult
    public static func install(slug: String, files: [String: Data],
                               targets: [AgentTarget], home: String, cwd: String) throws -> [String] {
        guard !targets.isEmpty else { throw InstallError.noTargets }
        let canon = slugify(slug)

        // Validate every relative path up front so a bad entry fails before we
        // write anything anywhere.
        var normalized: [(rel: String, data: Data)] = []
        for (rel, data) in files {
            normalized.append((try validateRelPath(rel), data))
        }

        let fm = FileManager.default
        var written: [String] = []
        for t in targets {
            let dest = (t.skillsDir(home: home, cwd: cwd) as NSString).appendingPathComponent(canon)
            try writeTree(into: dest, files: normalized, fm: fm)
            written.append(dest)
        }
        written.sort()
        return written
    }

    private static func writeTree(into dest: String, files: [(rel: String, data: Data)], fm: FileManager) throws {
        try ensureNotSymlink(dest, fm: fm)
        try fm.createDirectory(atPath: dest, withIntermediateDirectories: true)
        for (rel, data) in files {
            let full = (dest as NSString).appendingPathComponent(rel)
            let dir = (full as NSString).deletingLastPathComponent
            try ensureNotSymlink(dir, fm: fm)
            try fm.createDirectory(atPath: dir, withIntermediateDirectories: true)
            try ensureNotSymlink(full, fm: fm)
            // Overwrite existing content — re-installing a skill refreshes the
            // local copy rather than leaving the previous version lingering.
            try data.write(to: URL(fileURLWithPath: full), options: .atomic)
        }
    }

    /// Reject a path that already exists as a symlink — we never follow or
    /// write through one (mirrors `copyTree`'s `os.ModeSymlink` refusal).
    /// Uses `lstat` semantics (`attributesOfItem` does not traverse the final
    /// component), so a dangling or directory symlink is still caught.
    private static func ensureNotSymlink(_ path: String, fm: FileManager) throws {
        guard let type = (try? fm.attributesOfItem(atPath: path))?[.type] as? FileAttributeType else {
            return  // doesn't exist yet — nothing to follow
        }
        if type == .typeSymbolicLink {
            throw InstallError.symlinkRejected(path)
        }
    }

    /// Path-traversal guard. Swift mirror of Go `registry.validateRelPath`:
    /// rejects empty, absolute, and any `..` segment after lexical cleaning;
    /// normalizes backslashes to forward slashes first. Returns the cleaned
    /// forward-slash path.
    static func validateRelPath(_ rel: String) throws -> String {
        let slashed = rel.replacingOccurrences(of: "\\", with: "/")
        if slashed.isEmpty { throw InstallError.invalidPath(rel) }
        if slashed.hasPrefix("/") { throw InstallError.invalidPath(rel) }
        let clean = cleanSlashPath(slashed)
        if clean == ".." || clean.hasPrefix("../") || clean.contains("/../") || clean.hasPrefix("/") {
            throw InstallError.invalidPath(rel)
        }
        return clean
    }

    /// Lexical path cleaner mirroring Go's `filepath.Clean` for forward-slash
    /// paths. No filesystem access, no symlink resolution, no tilde expansion —
    /// just `.` / `..` collapsing so the traversal check above is reliable.
    static func cleanSlashPath(_ path: String) -> String {
        let isAbs = path.hasPrefix("/")
        var out: [String] = []
        for seg in path.split(separator: "/", omittingEmptySubsequences: true) {
            let s = String(seg)
            if s == "." { continue }
            if s == ".." {
                if let last = out.last, last != ".." {
                    out.removeLast()
                } else if !isAbs {
                    out.append("..")
                }
                continue
            }
            out.append(s)
        }
        var result = out.joined(separator: "/")
        if isAbs { result = "/" + result }
        if result.isEmpty { result = "." }
        return result
    }
}
