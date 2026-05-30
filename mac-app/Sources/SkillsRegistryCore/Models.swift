import Foundation

/// One row in a registry listing. Mirrors `registry.Summary` (Go) /
/// `SkillSummary` (Python): slug, name, description.
public struct SkillSummary: Identifiable, Hashable, Sendable {
    public var slug: String
    public var name: String
    public var description: String
    /// Git tree SHA of the `<slug>/` folder at fetch time (for cache busting).
    public var treeSHA: String

    public var id: String { slug }

    public init(slug: String, name: String, description: String, treeSHA: String = "") {
        self.slug = slug
        self.name = name
        self.description = description
        self.treeSHA = treeSHA
    }
}

/// A fully-fetched skill: its SKILL.md body plus the relative paths of every
/// supporting file in the `<slug>/` subtree.
public struct SkillDetail: Sendable {
    public var slug: String
    public var name: String
    public var description: String
    public var markdown: String
    /// Repo-relative file paths under `<slug>/`, e.g. ["SKILL.md", "scripts/run.sh"].
    public var files: [String]

    public init(slug: String, name: String, description: String, markdown: String, files: [String]) {
        self.slug = slug
        self.name = name
        self.description = description
        self.markdown = markdown
        self.files = files
    }
}

/// A repository reference, "owner/repo".
public struct RepoRef: Hashable, Sendable {
    public var owner: String
    public var name: String

    public init(owner: String, name: String) {
        self.owner = owner
        self.name = name
    }

    /// Parse "owner/repo"; nil if malformed.
    public init?(fullName: String) {
        let parts = fullName.split(separator: "/", omittingEmptySubsequences: false)
        guard parts.count == 2, !parts[0].isEmpty, !parts[1].isEmpty else { return nil }
        self.owner = String(parts[0])
        self.name = String(parts[1])
    }

    public var fullName: String { "\(owner)/\(name)" }
    public var htmlURL: URL { URL(string: "https://github.com/\(fullName)")! }
}

/// The authenticated GitHub user.
public struct Identity: Sendable, Equatable {
    public var login: String
    public var name: String?
    public var avatarURL: URL?

    public init(login: String, name: String? = nil, avatarURL: URL? = nil) {
        self.login = login
        self.name = name
        self.avatarURL = avatarURL
    }

    public var displayName: String {
        if let name, !name.isEmpty { return name }
        return login
    }
}

/// A repo the GitHub App installation can access.
public struct InstallationRepo: Hashable, Sendable {
    public var fullName: String
    public var defaultBranch: String
    public var isPrivate: Bool

    public init(fullName: String, defaultBranch: String, isPrivate: Bool) {
        self.fullName = fullName
        self.defaultBranch = defaultBranch
        self.isPrivate = isPrivate
    }
}

/// A skill discovered in a local AI-tool dot-folder (for bulk import).
public struct LocalSkill: Identifiable, Hashable, Sendable {
    public var slug: String
    public var name: String
    public var description: String
    /// Absolute path to the folder containing SKILL.md.
    public var folder: String
    /// Human label of the source dir, e.g. "~/.claude/skills".
    public var source: String

    public var id: String { folder }

    public init(slug: String, name: String, description: String, folder: String, source: String) {
        self.slug = slug
        self.name = name
        self.description = description
        self.folder = folder
        self.source = source
    }
}
