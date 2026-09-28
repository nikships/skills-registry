import Foundation

/// A parsed `major.minor.patch` version. Pre-release / build metadata after the
/// first `-` or `+` is ignored for ordering (good enough for our release tags).
public struct Semver: Comparable, Sendable, Equatable {
    public let major: Int
    public let minor: Int
    public let patch: Int

    public init(major: Int, minor: Int, patch: Int) {
        self.major = major
        self.minor = minor
        self.patch = patch
    }

    /// Parse "1.2.3" / "1.2" / "1" (with optional leading `v` and trailing
    /// `-pre`/`+build`). Returns nil if the leading core isn't numeric.
    public init?(_ raw: String) {
        var s = raw.trimmingCharacters(in: .whitespaces)
        if s.hasPrefix("v") || s.hasPrefix("V") { s.removeFirst() }
        let core = s.prefix { $0 != "-" && $0 != "+" }
        let parts = core.split(separator: ".", omittingEmptySubsequences: false).map(String.init)
        guard let first = parts.first, let ma = Int(first) else { return nil }
        let mi = parts.count > 1 ? Int(parts[1]) : 0
        let pa = parts.count > 2 ? Int(parts[2]) : 0
        guard let mi, let pa else { return nil }
        self.major = ma
        self.minor = mi
        self.patch = pa
    }

    /// Extract the first `digits.digits.digits` token from arbitrary text such
    /// as `skills-registry version v0.5.30`. Returns nil for `dev` / unknown.
    public static func firstIn(_ text: String) -> Semver? {
        guard let r = text.range(of: #"\d+\.\d+\.\d+"#, options: .regularExpression) else { return nil }
        return Semver(String(text[r]))
    }

    public static func < (l: Semver, r: Semver) -> Bool {
        (l.major, l.minor, l.patch) < (r.major, r.minor, r.patch)
    }

    public var string: String { "\(major).\(minor).\(patch)" }
}

/// Which release stream a tag belongs to. The project ships both the Go CLI
/// (`v*` tags) and this macOS app (`macapp-v*` tags) from the **same** repo, so
/// `releases/latest` is ambiguous — always filter by channel.
public enum ReleaseChannel: Sendable {
    case cli
    case macApp

    func matches(tag: String) -> Bool {
        switch self {
        case .macApp:
            return tag.hasPrefix("macapp-v")
        case .cli:
            // `v<digit>...` and explicitly not the macapp stream.
            guard !tag.hasPrefix("macapp-"), tag.hasPrefix("v"), tag.count > 1 else { return false }
            return tag[tag.index(after: tag.startIndex)].isNumber
        }
    }

    func version(from tag: String) -> Semver? {
        switch self {
        case .macApp: return Semver(String(tag.dropFirst("macapp-v".count)))
        case .cli: return Semver(String(tag.dropFirst()))
        }
    }
}

/// One resolved release on a channel.
public struct ReleaseInfo: Sendable, Equatable {
    public var tag: String
    public var version: Semver
    public var htmlURL: URL?

    public init(tag: String, version: Semver, htmlURL: URL? = nil) {
        self.tag = tag
        self.version = version
        self.htmlURL = htmlURL
    }
}

/// Timestamp-TTL throttle for periodic checks (CLI status probes, CLI
/// release lookups). Pure so the app's cached checks stay unit-testable.
public enum CheckThrottle {
    /// Whether enough time has passed since `lastCheck` (0 = never checked).
    public static func shouldCheck(lastCheck: TimeInterval, now: TimeInterval, interval: TimeInterval) -> Bool {
        now - lastCheck >= interval
    }
}

/// Why resolving the CLI install tag failed.
public enum CLIResolveError: Error, Sendable, Equatable {
    /// The CLI channel exists but has no published releases yet.
    case noReleases
}

/// GitHub release discovery, filtered by channel. Unauthenticated REST.
public enum Updates {
    /// Highest-semver non-draft, non-prerelease release on `channel`, or nil if
    /// the repo has none yet.
    public static func latestRelease(
        repo: String,
        channel: ReleaseChannel,
        session: URLSession = .shared
    ) async throws -> ReleaseInfo? {
        var req = URLRequest(url: URL(string: "https://api.github.com/repos/\(repo)/releases?per_page=100")!)
        req.setValue("application/vnd.github+json", forHTTPHeaderField: "Accept")
        req.setValue("skills-registry-mac", forHTTPHeaderField: "User-Agent")
        let (data, resp) = try await session.data(for: req)
        let code = (resp as? HTTPURLResponse)?.statusCode ?? 0
        guard code == 200 else {
            throw GitHubError(status: code, message: "Could not list releases", endpoint: "releases")
        }
        let arr = (try? JSONSerialization.jsonObject(with: data)) as? [[String: Any]] ?? []
        return pickLatest(from: arr, channel: channel)
    }

    /// Which tag a CLI install should download. Never falls back to the
    /// ambiguous `releases/latest` endpoint (the repo ships both `v*` CLI and
    /// `macapp-v*` tags): a failed lookup throws the underlying error so the
    /// UI can surface it, and an empty channel throws `noReleases`.
    public static func installTag(
        repo: String,
        session: URLSession = .shared
    ) async throws -> String {
        guard let latest = try await latestRelease(repo: repo, channel: .cli, session: session) else {
            throw CLIResolveError.noReleases
        }
        return latest.tag
    }

    /// Pure selection over a decoded `/releases` array — unit-testable.
    public static func pickLatest(from releases: [[String: Any]], channel: ReleaseChannel) -> ReleaseInfo? {
        var best: ReleaseInfo?
        for obj in releases {
            if (obj["draft"] as? Bool) == true { continue }
            if (obj["prerelease"] as? Bool) == true { continue }
            guard let tag = obj["tag_name"] as? String,
                  channel.matches(tag: tag),
                  let v = channel.version(from: tag) else { continue }
            let html = (obj["html_url"] as? String).flatMap { URL(string: $0) }
            if best == nil || v > best!.version {
                best = ReleaseInfo(tag: tag, version: v, htmlURL: html)
            }
        }
        return best
    }

    /// Is `available` newer than the `installed` version string? An
    /// unparseable / `dev` installed version is treated as "update available".
    public static func isNewer(installed: String?, than available: Semver) -> Bool {
        guard let installed, let cur = Semver.firstIn(installed) else { return true }
        return available > cur
    }
}
