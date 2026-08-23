import Foundation

/// Local cleanup half of `skills-registry remove`, matching the CLI's download
/// cleanup and agent dot-folder sweep.
///
/// The registry delete itself stays in `GitHubWrites.delete`; this enum only
/// sweeps the two local footprints a skill leaves behind: the CLI download
/// cache and any agent dot-folder copy.
public enum LocalRemove {
    /// CLI download cache, honoring XDG_CACHE_HOME.
    public static func cacheRoot() -> String {
        if let base = ProcessInfo.processInfo.environment["XDG_CACHE_HOME"], !base.isEmpty {
            return (base as NSString).appendingPathComponent("skills-registry/skills")
        }
        let home = NSHomeDirectory()
        return (home as NSString).appendingPathComponent(".cache/skills-registry/skills")
    }

    /// Wipe the CLI's per-slug download cache: `<root>/<slug>/` and the sibling
    /// `<root>/<slug>.meta.json`. Returns true if either existed before the
    /// call (so the caller can report "cache cleared").
    @discardableResult
    public static func removeFromCache(slug: String) -> Bool {
        let root = cacheRoot()
        let fm = FileManager.default
        let skillDir = (root as NSString).appendingPathComponent(slug)
        let metaFile = (root as NSString).appendingPathComponent("\(slug).meta.json")
        var removed = false
        if fm.fileExists(atPath: skillDir) {
            if (try? fm.removeItem(atPath: skillDir)) != nil { removed = true }
        }
        if fm.fileExists(atPath: metaFile) {
            if (try? fm.removeItem(atPath: metaFile)) != nil { removed = true }
        }
        return removed
    }

    /// Sweep every known agent dot-folder and remove any direct child whose
    /// name matches the slug under `normalizeForMatch` (lowercase + strip
    /// non-alphanumerics), so separator- or case-only differences
    /// ("agp-9-upgrade" vs "agp_9_upgrade") still match. Returns the absolute
    /// paths actually deleted, sorted. Symlinks are unlinked, not followed
    /// (`removeItem` removes the link itself).
    public static func removeFromDotFolders(slug: String, home: String, cwd: String) -> [String] {
        let fm = FileManager.default
        var deleted: [String] = []
        for target in Agents.all() {
            let dir = target.skillsDir(home: home, cwd: cwd)
            for path in matchSlugChildren(parent: dir, slug: slug, fm: fm) {
                if (try? fm.removeItem(atPath: path)) != nil {
                    deleted.append(path)
                }
            }
        }
        deleted.sort()
        return deleted
    }

    /// Direct children of `parent` whose name matches `slug` under
    /// `normalizeForMatch` (lowercase + strip non-alphanumerics), so a folder
    /// differing from the canonical slug only by separators or case still
    /// matches. Empty when `parent` is absent/unreadable (normal on a fresh
    /// install where most dot-folders don't exist).
    static func matchSlugChildren(parent: String, slug: String, fm: FileManager) -> [String] {
        guard let entries = try? fm.contentsOfDirectory(atPath: parent) else { return [] }
        let want = normalizeForMatch(slug)
        var out: [String] = []
        for name in entries where normalizeForMatch(name) == want {
            out.append((parent as NSString).appendingPathComponent(name))
        }
        return out
    }
}
