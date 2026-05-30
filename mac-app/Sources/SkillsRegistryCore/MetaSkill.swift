import Foundation

/// Detect, install, and refresh the `skills-registry` meta-skill — the
/// generated `SKILL.md` that teaches an agent how to reach the registry. The
/// CLI's wizard writes it (`cli/internal/bootstrap/install.go:InstallSkillMd`);
/// the macOS app does the same so users who set up via the app still get the
/// gateway skill in every agent they have installed.
///
/// "Detected agents" are the home-based dot-folders whose base dir (e.g.
/// `~/.claude`) exists. The cwd-based universal `.agents` target is skipped —
/// a desktop app has no meaningful project working directory.
public enum MetaSkill {
    /// Slug of the meta-skill (matches the folder name the CLI installs into).
    public static let slug = "skills-registry"

    /// Install state of the meta-skill in one agent.
    public enum State: Sendable, Equatable {
        case missing   // no SKILL.md at the expected path
        case outdated  // present but content differs from the current template
        case current   // present and byte-identical to the current template
    }

    /// Per-agent status row.
    public struct TargetStatus: Identifiable, Hashable, Sendable {
        public var target: AgentTarget
        public var state: State
        public var path: String
        public var id: String { target.dotDir }
    }

    /// Aggregate status across every detected agent.
    public struct Status: Sendable, Equatable {
        public var targets: [TargetStatus]

        public init(targets: [TargetStatus] = []) { self.targets = targets }

        public var detectedCount: Int { targets.count }
        public var installedCount: Int { targets.filter { $0.state != .missing }.count }
        public var anyMissing: Bool { targets.contains { $0.state == .missing } }
        public var anyOutdated: Bool { targets.contains { $0.state == .outdated } }
        /// True when at least one detected agent needs a fresh install or refresh.
        public var needsAction: Bool { anyMissing || anyOutdated }
    }

    /// Home-based agent targets whose base dot-folder exists on disk.
    public static func detectedTargets(home: String) -> [AgentTarget] {
        let fm = FileManager.default
        return Agents.all().filter { t in
            guard t.underHome else { return false }
            let base = (home as NSString).appendingPathComponent(t.dotDir)
            var isDir: ObjCBool = false
            return fm.fileExists(atPath: base, isDirectory: &isDir) && isDir.boolValue
        }
    }

    /// Absolute `<dot>/skills/skills-registry/SKILL.md` path for a target.
    public static func skillPath(for target: AgentTarget, home: String) -> String {
        let dir = target.skillsDir(home: home, cwd: home)
        return (dir as NSString).appendingPathComponent("\(slug)/SKILL.md")
    }

    /// Classify the meta-skill across every detected agent.
    public static func status(home: String, registryRepo: String) -> Status {
        let expected = SkillMdTemplate.skillMd(registryRepo: registryRepo)
        let rows = detectedTargets(home: home).map { t -> TargetStatus in
            let path = skillPath(for: t, home: home)
            let state: State
            if let existing = try? String(contentsOfFile: path, encoding: .utf8) {
                state = existing == expected ? .current : .outdated
            } else {
                state = .missing
            }
            return TargetStatus(target: t, state: state, path: path)
        }
        return Status(targets: rows)
    }

    /// Write the current template into every detected agent that is missing or
    /// outdated (leaves `.current` ones untouched). Returns files written.
    @discardableResult
    public static func install(home: String, registryRepo: String) throws -> Int {
        let expected = SkillMdTemplate.skillMd(registryRepo: registryRepo)
        let fm = FileManager.default
        var written = 0
        for row in status(home: home, registryRepo: registryRepo).targets where row.state != .current {
            let dir = (row.path as NSString).deletingLastPathComponent
            try fm.createDirectory(atPath: dir, withIntermediateDirectories: true)
            try expected.write(toFile: row.path, atomically: true, encoding: .utf8)
            written += 1
        }
        return written
    }
}
