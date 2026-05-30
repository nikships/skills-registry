import Foundation

/// Local skill discovery across known AI-tool dot-folders. Port of
/// `cli/internal/scan/scan.go`. Used by the bulk-import flow.
public enum Scan {
    public static let mainFileName = "SKILL.md"

    public struct Source: Hashable, Sendable {
        public var path: String   // absolute
        public var label: String  // e.g. "~/.claude/skills"
    }

    /// Every known skill-bearing directory under $HOME (and cwd, if different).
    public static func discoverSources(home: String, cwd: String, dotDirs: [String]) -> [Source] {
        var seen = Set<String>()
        var sources: [Source] = []
        let fm = FileManager.default

        var bases: [(root: String, prefix: String)] = [(home, "~")]
        if cwd != home { bases.append((cwd, ".")) }

        for base in bases {
            for dot in dotDirs {
                let p = (base.root as NSString).appendingPathComponent("\(dot)/skills")
                var isDir: ObjCBool = false
                guard fm.fileExists(atPath: p, isDirectory: &isDir), isDir.boolValue else { continue }
                let abs = (p as NSString).standardizingPath
                if seen.contains(abs) { continue }
                seen.insert(abs)
                sources.append(Source(path: abs, label: "\(base.prefix)/\(dot)/skills"))
            }
        }
        return sources
    }

    /// Walk each source and return every discovered skill (slug-deduped,
    /// first source wins; sorted by slug).
    public static func discover(_ sources: [Source]) -> [LocalSkill] {
        var out: [LocalSkill] = []
        var seen = Set<String>()
        for src in sources {
            for mainPath in findMainFiles(src.path) {
                guard let skill = load(source: src, mainPath: mainPath) else { continue }
                if seen.contains(skill.slug) { continue }
                seen.insert(skill.slug)
                out.append(skill)
            }
        }
        out.sort { $0.slug < $1.slug }
        return out
    }

    static func findMainFiles(_ root: String) -> [String] {
        var out: [String] = []
        let fm = FileManager.default
        guard let en = fm.enumerator(at: URL(fileURLWithPath: root),
                                     includingPropertiesForKeys: [.isRegularFileKey],
                                     options: [.skipsHiddenFiles]) else { return [] }
        for case let url as URL in en {
            if url.lastPathComponent == mainFileName {
                out.append(url.path)
            }
        }
        out.sort()
        return out
    }

    static func load(source: Source, mainPath: String) -> LocalSkill? {
        let folder = (mainPath as NSString).deletingLastPathComponent
        guard let text = try? String(contentsOfFile: mainPath, encoding: .utf8) else { return nil }
        let folderName = (folder as NSString).lastPathComponent
        var (name, desc) = Frontmatter.parseSummary(text, slug: folderName)
        if name == folderName {
            // parseSummary fell back to the folder name; keep it as the name.
        }
        if name.isEmpty { name = folderName }
        if desc.isEmpty { desc = "Skill: \(name)" }
        return LocalSkill(
            slug: slugify(name),
            name: name,
            description: desc,
            folder: folder,
            source: source.label
        )
    }

    /// Read every file under a skill folder (skipping hidden + __pycache__),
    /// returning repo-relative paths (prefixed with `<slug>/`) → bytes.
    /// Mirrors `walkSkillIntoFiles` in bootstrap.go.
    public static func filesForUpload(slug: String, folder: String) throws -> [String: Data] {
        var out: [String: Data] = [:]
        let fm = FileManager.default
        let root = URL(fileURLWithPath: folder)
        guard let en = fm.enumerator(at: root, includingPropertiesForKeys: [.isRegularFileKey, .isDirectoryKey]) else {
            return out
        }
        for case let url as URL in en {
            let name = url.lastPathComponent
            if name.hasPrefix(".") || name == "__pycache__" {
                if (try? url.resourceValues(forKeys: [.isDirectoryKey]).isDirectory) == true {
                    en.skipDescendants()
                }
                continue
            }
            let isDir = (try? url.resourceValues(forKeys: [.isDirectoryKey]).isDirectory) ?? false
            if isDir { continue }
            let rel = relativePath(of: url.path, under: folder)
            let data = try Data(contentsOf: url)
            out["\(slug)/\(rel)"] = data
        }
        return out
    }

    private static func relativePath(of path: String, under base: String) -> String {
        var b = base
        if !b.hasSuffix("/") { b += "/" }
        if path.hasPrefix(b) { return String(path.dropFirst(b.count)) }
        return (path as NSString).lastPathComponent
    }
}
