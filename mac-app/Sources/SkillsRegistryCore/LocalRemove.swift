import Foundation

/// Local cleanup half of `skills-registry remove`. Port of `removeFromCache` +
/// `removeFromDotFolders` (`cli/cmd/skills-registry/remove.go`) + the cache
/// path resolution in `cli/internal/cache/cache.go`.
///
/// The registry delete itself stays in `GitHubWrites.delete`; this enum only
/// sweeps the two *local* footprints a skill leaves behind: the MCP server's
/// download cache and any agent dot-folder copy.
public enum LocalRemove {
    /// Where the Python MCP server caches downloaded skills. Matches Go
    /// `cache.CacheRoot()` / `cache.py::cache_root`:
    ///   1. `$XDG_CACHE_HOME/skills-mcp/skills` if `XDG_CACHE_HOME` is set,
    ///   2. `~/.cache/skills-mcp/skills` otherwise.
    public static func cacheRoot() -> String {
        if let base = ProcessInfo.processInfo.environment["XDG_CACHE_HOME"], !base.isEmpty {
            return (base as NSString).appendingPathComponent("skills-mcp/skills")
        }
        let home = NSHomeDirectory()
        return (home as NSString).appendingPathComponent(".cache/skills-mcp/skills")
    }

    /// Wipe the MCP server's per-slug cache: `<root>/<slug>/` and the sibling
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
    /// name matches the slug — literally or via `slugify` so hyphenated folder
    /// names ("agp-9-upgrade") match canonical slugs ("agp_9_upgrade").
    /// Returns the absolute paths actually deleted, sorted. Symlinks are
    /// unlinked, not followed (`removeItem` removes the link itself).
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

    /// Direct children of `parent` whose name matches `slug` literally or via
    /// `slugify`. Empty when `parent` is absent/unreadable (normal on a fresh
    /// install where most dot-folders don't exist).
    static func matchSlugChildren(parent: String, slug: String, fm: FileManager) -> [String] {
        guard let entries = try? fm.contentsOfDirectory(atPath: parent) else { return [] }
        var out: [String] = []
        for name in entries where name == slug || slugify(name) == slug {
            out.append((parent as NSString).appendingPathComponent(name))
        }
        return out
    }
}
