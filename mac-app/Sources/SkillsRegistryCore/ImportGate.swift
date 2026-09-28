import Foundation

/// The rules for reviewing one skill before it is imported from an untrusted
/// source. Swift mirror of Go `cli/internal/importgate`, kept apart from the
/// views so the pane, the Add flow, and any later surface reach the same
/// verdict for the same input.
///
/// Two of these are correctness requirements rather than presentation choices:
///
///   - A missing grade renders as `unscored`. The public index leaves a grade
///     empty when it never evaluated the skill, and an empty cell reads as
///     "fine". Unscored means unvetted.
///   - Poor safety blocks. A block is not a refusal to ever import: it means
///     the user has to say so explicitly, with the finding in front of them.
public enum ImportGate {
    /// How an absent grade is rendered. It must never be substituted with a
    /// passing grade or an empty string.
    public static let unscoredLabel = "unscored"

    /// The public index's failing grade.
    public static let levelPoor = "Poor"

    /// The index's middling grade, called out so a badge can tint it.
    public static let levelAverage = "Average"

    /// The index's passing grade.
    public static let levelGood = "Good"

    /// Render one grade, naming an absent grade explicitly.
    public static func label(_ level: String) -> String {
        let trimmed = level.trimmingCharacters(in: .whitespacesAndNewlines)
        return trimmed.isEmpty ? unscoredLabel : trimmed
    }

    /// One-line statement of what the index's grades are worth, shown wherever
    /// they are. Mirrors the CLI's own wording.
    public static let gradeDisclaimer =
        "Grades are the public index's own. \(unscoredLabel) means not graded, not vetted — "
        + "read the skill's source before importing it."

    /// What an untrusted import does by default, stated where the user decides.
    public static let registryOnlyExplanation =
        "Publishes to your registry only. No agent folder is written unless you opt in, "
        + "and nothing under scripts/ is ever run."

    /// The one-line statement of what the local scan is worth. Shown wherever
    /// findings are reported so nobody reads a clean scan as a clean bill of
    /// health. Mirrors Go `importgate.ScanDisclaimer` verbatim.
    public static let scanDisclaimer =
        "The local scan is a regex heuristic, not a guarantee: "
        + "it catches obvious prompt injection, credential exfiltration, and pipe-to-shell lines, "
        + "and nothing subtler. Read the skill's source."
}

/// The public index's grades for one skill. Each is `Good`, `Average`, `Poor`,
/// or empty for "the index never graded this". Mirrors Go
/// `importgate.Scores`.
public struct ImportScores: Sendable, Hashable, Codable {
    public var safety: String
    public var completeness: String
    public var executability: String

    public init(safety: String = "", completeness: String = "", executability: String = "") {
        self.safety = safety
        self.completeness = completeness
        self.executability = executability
    }

    /// Whether the index graded this skill at all.
    public var any: Bool {
        [safety, completeness, executability].contains { ImportGate.label($0) != ImportGate.unscoredLabel }
    }

    /// Whether the index graded safety as `Poor`. An unscored safety grade is
    /// not Poor, and is not a pass either: it is reported as unscored and the
    /// import still needs the user's confirmation.
    public var safetyIsPoor: Bool {
        safety.trimmingCharacters(in: .whitespacesAndNewlines)
            .caseInsensitiveCompare(ImportGate.levelPoor) == .orderedSame
    }

    /// The three grades as (label, value) pairs, always all three and always
    /// naming an absent one. Mirrors Go `Scores.Lines()`.
    public var lines: [(name: String, level: String)] {
        [("safety", ImportGate.label(safety)),
         ("completeness", ImportGate.label(completeness)),
         ("executability", ImportGate.label(executability))]
    }
}

/// Why an import needs explicit consent. The verdict's grade block can arise
/// before a fetch; the scan block only after one, once the caller has the
/// SKILL.md bytes to run `SkillScan` over.
public enum ImportBlockKind: String, Sendable, Codable {
    case poorSafety = "poor_safety"
    case injectionScan = "injection_scan"
}

/// One reason an import is held back.
public struct ImportBlock: Sendable, Hashable, Codable {
    public var kind: ImportBlockKind
    public var reason: String

    public init(kind: ImportBlockKind, reason: String) {
        self.kind = kind
        self.reason = reason
    }
}

/// The verdict for one skill from an untrusted source. Mirrors Go
/// `importgate.Review`.
public struct ImportReview: Sendable, Hashable, Codable {
    public var slug: String
    public var scores: ImportScores
    /// The local scan's hits for this skill, empty when the caller scanned
    /// nothing (or matched nothing). The UI lists these next to the block.
    public var findings: [SkillFinding]
    public var blocks: [ImportBlock]

    public init(slug: String, scores: ImportScores,
                findings: [SkillFinding] = [], blocks: [ImportBlock] = []) {
        self.slug = slug
        self.scores = scores
        self.findings = findings
        self.blocks = blocks
    }

    /// The wire shape matches Go `importgate.Review`, whose findings key is
    /// `scan_findings`.
    enum CodingKeys: String, CodingKey {
        case slug, scores, blocks
        case findings = "scan_findings"
    }

    /// Whether the import needs explicit consent, not that it is forbidden.
    public var blocked: Bool { !blocks.isEmpty }

    /// Each block's prose.
    public var reasons: [String] { blocks.map(\.reason) }

    /// The blocks rendered as one line.
    public var summary: String { reasons.joined(separator: "; ") }

    /// The summary as a display sentence. The stored reasons stay lowercase
    /// (matching Go `importgate.Evaluate`), and the CLI capitalizes only where
    /// it prompts (`confirmUntrusted`), so the app does the same: capitalize
    /// here, at the point of display.
    public var displaySummary: String {
        guard let first = summary.first else { return summary }
        return String(first).uppercased() + summary.dropFirst()
    }

    /// Produce the verdict for one skill. Both inputs are optional: no grades
    /// means the index never saw the skill, and no findings means the
    /// heuristic scan matched nothing (which is not a guarantee of safety).
    /// A caller that has not fetched the skill yet passes no findings, which
    /// is exactly what the Discover confirmation does: it reviews the grades
    /// it has, and the scan block can only join after the fetch.
    public static func evaluate(slug: String, scores: ImportScores,
                                findings: [SkillFinding] = []) -> ImportReview {
        var review = ImportReview(slug: slug, scores: scores, findings: findings)
        if scores.safetyIsPoor {
            review.blocks.append(ImportBlock(
                kind: .poorSafety,
                reason: "the public skill index graded this skill's safety \(ImportGate.levelPoor)"))
        }
        if !findings.isEmpty {
            review.blocks.append(ImportBlock(
                kind: .injectionScan,
                reason: "the local scan of \(Scan.mainFileName) matched "
                    + "\(findings.count) suspicious line(s) (\(SkillScan.summary(of: findings)))"))
        }
        return review
    }
}

/// What one import may write. An untrusted import is registry-only unless the
/// user explicitly opts into the durable agent-folder install, because from
/// then on every agent loads that `SKILL.md` each session with no further
/// prompt.
public struct ImportDecision: Sendable, Equatable {
    /// The row being imported.
    public var url: String
    /// The index's grades for it.
    public var scores: ImportScores
    /// Whether the user opted into the durable agent-folder install.
    public var installIntoAgents: Bool
    /// Whether the user cleared a blocker (the macOS equivalent of
    /// `--allow-unsafe`). Never implied by `installIntoAgents`.
    public var allowUnsafe: Bool

    public init(url: String, scores: ImportScores,
                installIntoAgents: Bool = false, allowUnsafe: Bool = false) {
        self.url = url
        self.scores = scores
        self.installIntoAgents = installIntoAgents
        self.allowUnsafe = allowUnsafe
    }

    /// The review for this decision.
    public var review: ImportReview {
        ImportReview.evaluate(slug: url, scores: scores)
    }

    /// Whether the import may proceed. A blocker needs `allowUnsafe`;
    /// everything else is cleared by the ordinary confirmation.
    public var permitted: Bool { !review.blocked || allowUnsafe }

    /// Whether the durable install may run. Registry-only is the default, and
    /// opting in never follows from clearing a blocker.
    public var installPermitted: Bool { permitted && installIntoAgents }
}

/// The verdict for one Add-from-source invocation: where the source came from
/// and, for an untrusted source, the per-skill review. Swift mirror of Go's
/// `add_gate.go` gate, including the local injection scan over each fetched
/// SKILL.md.
public struct AddGate: Sendable, Equatable {
    /// The source as classified.
    public var assessment: ImportAssessment
    /// The public index's grades for the source folder. Zero when the index
    /// has no row for it, which renders as unscored.
    public var scores: ImportScores
    /// Whether the index actually had a row, so the UI can say "not in the
    /// index" rather than printing three unscored lines with no explanation.
    public var indexed: Bool
    /// The index's category for the source folder, empty when the index has
    /// no row for it or graded it without one. Stamped onto an untrusted
    /// import's copy; an absent category is left absent rather than guessed.
    public var category: String
    /// One entry per skill under review, in the order they were passed. Empty
    /// for a trusted source: the gate does not second-guess a folder the user
    /// already owns.
    public var reviews: [ImportReview]

    public init(assessment: ImportAssessment, scores: ImportScores = ImportScores(),
                indexed: Bool = false, category: String = "",
                reviews: [ImportReview] = []) {
        self.assessment = assessment
        self.scores = scores
        self.indexed = indexed
        self.category = category
        self.reviews = reviews
    }

    /// Classify one Add source and review every discovered skill slug against
    /// the index row (`nil` when the lookup missed or failed, which degrades
    /// to unscored) and the local scan's hits for that skill. A trusted
    /// source is returned unreviewed: it publishes byte-for-byte and never
    /// consults the index.
    public static func build(source: String, owners: [String],
                             slugs: [String], indexed: DiscoverResult? = nil,
                             findings: [String: [SkillFinding]] = [:]) -> AddGate {
        let assessment = ImportTrust.assess(source, owners: owners)
        guard assessment.untrusted else {
            return AddGate(assessment: assessment)
        }
        var gate = AddGate(assessment: assessment)
        if let row = indexed {
            gate.scores = row.scores
            gate.category = row.category
            gate.indexed = true
        }
        // The grades belong to the folder the URL named, so they apply to
        // every skill discovered inside it. For a folder URL that is one
        // skill; for a folder of skills, a Poor grade on the parent holding
        // them back is the safe direction.
        gate.reviews = slugs.map {
            ImportReview.evaluate(slug: $0, scores: gate.scores, findings: findings[$0] ?? [])
        }
        return gate
    }

    /// Whether the gate applies to this source.
    public var untrusted: Bool { assessment.untrusted }

    /// The reviews needing explicit consent.
    public var blocked: [ImportReview] { reviews.filter(\.blocked) }

    /// The review for one slug.
    public func review(slug: String) -> ImportReview? {
        reviews.first { $0.slug == slug }
    }

    /// The scan's hits for one slug, so a caller that only wants the findings
    /// does not have to reach into the review. Mirrors Go `scanFindingsFor`.
    public func scanFindings(slug: String) -> [SkillFinding] {
        review(slug: slug)?.findings ?? []
    }

    /// Whether the selected skills may be published. A blocker needs
    /// `allowUnsafe`; everything else is cleared by the ordinary
    /// confirmation. Refused skills are returned alongside, so the caller can
    /// name them instead of failing the whole run silently.
    public func allowed(slugs: [String], allowUnsafe: Bool) -> (allowed: [String], refused: [ImportReview]) {
        if !untrusted || allowUnsafe {
            return (slugs, [])
        }
        var kept: [String] = []
        var refused: [ImportReview] = []
        for slug in slugs {
            if let r = review(slug: slug), r.blocked {
                refused.append(r)
            } else {
                kept.append(slug)
            }
        }
        return (kept, refused)
    }
}

/// Where an imported skill came from, so a caller can tell "a folder I already
/// own" apart from "a stranger's SKILL.md". Swift mirror of Go
/// `cli/internal/trust`.
public enum ImportOrigin: String, Sendable {
    case localPath = "local_path"
    case ownRepo = "own_repo"
    case publicRepo = "public_repo"
    case remoteGit = "remote_git"
    case discover

    /// Whether an origin must go through the import gate.
    public var untrusted: Bool {
        switch self {
        case .localPath, .ownRepo: return false
        case .publicRepo, .remoteGit, .discover: return true
        }
    }
}

/// The result of classifying one source. Mirrors Go `trust.Assessment`.
public struct ImportAssessment: Sendable, Equatable {
    public var source: String
    public var origin: ImportOrigin
    public var owner: String
    public var reason: String

    public init(source: String, origin: ImportOrigin, owner: String = "", reason: String = "") {
        self.source = source
        self.origin = origin
        self.owner = owner
        self.reason = reason
    }

    public var untrusted: Bool { origin.untrusted }
}

/// Classifies an import source. Offline by construction: it parses the source
/// string and compares owners, so a caller can classify before fetching
/// anything.
public enum ImportTrust {
    /// Classify one source.
    ///
    /// A GitHub source under one of `owners` is the user's own and stays
    /// trusted, whichever URL shape it arrived in. Everything else remote is
    /// untrusted, including a non-GitHub git URL, because nothing in the
    /// string establishes who wrote it. A row picked out of the public index
    /// is untrusted whatever its URL shape, because the user did not choose
    /// the URL.
    public static func assess(_ source: String, owners: [String] = [],
                              fromDiscover: Bool = false) -> ImportAssessment {
        if fromDiscover {
            return ImportAssessment(source: source, origin: .discover,
                                    owner: githubOwner(source) ?? "",
                                    reason: "picked from the public skill index")
        }
        if SourceResolver.isLocalPath(source) {
            return ImportAssessment(source: source, origin: .localPath,
                                    reason: "a local directory on this machine")
        }
        guard let owner = githubOwner(source) else {
            return ImportAssessment(
                source: source, origin: .remoteGit,
                reason: "a third-party git remote; ownership cannot be established from the URL")
        }
        // GitHub logins are case-insensitive, so the comparison is too.
        if owners.contains(where: {
            $0.trimmingCharacters(in: .whitespacesAndNewlines).caseInsensitiveCompare(owner) == .orderedSame
        }) {
            return ImportAssessment(source: source, origin: .ownRepo, owner: owner,
                                    reason: "a GitHub repository owned by \(owner)")
        }
        return ImportAssessment(source: source, origin: .publicRepo, owner: owner,
                                reason: "a public GitHub repository owned by \(owner)")
    }

    /// The owner named by either accepted GitHub shape (`owner/repo`
    /// shorthand, or a github.com URL).
    static func githubOwner(_ source: String) -> String? {
        if let (owner, _) = parseOwnerRepo(source) { return owner }
        return GitHubTarget.parse(source)?.owner
    }

    private static let shorthand = try! NSRegularExpression(
        pattern: #"^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$"#)

    /// Split the `owner/repo` shorthand. Nil for every other shape.
    public static func parseOwnerRepo(_ source: String) -> (owner: String, repo: String)? {
        let s = source.trimmingCharacters(in: .whitespacesAndNewlines)
        guard shorthand.firstMatch(in: s, range: NSRange(s.startIndex..., in: s)) != nil,
              let slash = s.firstIndex(of: "/") else { return nil }
        var repo = String(s[s.index(after: slash)...])
        if repo.hasSuffix(".git") { repo = String(repo.dropLast(4)) }
        return (String(s[..<slash]), repo)
    }
}

/// The provenance keys an untrusted import stamps onto its copy before
/// publishing it, so the file itself records where it came from rather than
/// only the registry commit message. Mirrors Go
/// `cli/cmd/skills-registry/provenance.go`.
public enum ImportProvenance {
    /// Bounds the category taken from the public index. The value is
    /// third-party text written into a file every agent then loads, so a
    /// hostile or broken row cannot turn one frontmatter line into a payload.
    public static let maxCategoryLength = 64

    /// The keys to merge for one imported skill folder. The category is
    /// omitted when the index had no row (or no category for it), because an
    /// invented category is worse than an absent one.
    public static func keys(sourceURL: String, category: String) -> [Frontmatter.Key] {
        var out: [Frontmatter.Key] = []
        let bounded = boundedCategory(category)
        if !bounded.isEmpty {
            out.append(Frontmatter.Key(name: Frontmatter.categoryKey, value: bounded))
        }
        let url = sourceURL.trimmingCharacters(in: .whitespacesAndNewlines)
        if !url.isEmpty {
            out.append(Frontmatter.Key(name: Frontmatter.sourceURLKey, value: url))
        }
        return out
    }

    /// Normalize the index's category to a single short line, clipped to
    /// `maxCategoryLength` Unicode scalars (Go runes), matching Go's clip.
    public static func boundedCategory(_ category: String) -> String {
        let collapsed = category.split(whereSeparator: { $0.isWhitespace || $0.isNewline })
            .joined(separator: " ")
        guard collapsed.unicodeScalars.count > maxCategoryLength else { return collapsed }
        return String(collapsed.unicodeScalars.prefix(maxCategoryLength))
            .trimmingCharacters(in: .whitespacesAndNewlines)
    }

    /// The folder URL to record for one imported skill.
    ///
    /// The URL names the folder, so it ends in the skill's own directory:
    /// `relativeFolder` is the skill folder's slash-separated path under the
    /// fetch root, which keeps a folder-of-skills import honest (each skill
    /// gets its own subfolder URL rather than the parent's). A `/blob/` URL
    /// naming `SKILL.md` resolves to its directory, matching what the fetch
    /// did. A source with no ref is pinned to `HEAD` rather than to a guessed
    /// branch. A non-GitHub remote has no folder-URL form to derive, so the
    /// source string is recorded as given, minus any userinfo.
    public static func sourceURL(for source: String, relativeFolder: String = "") -> String {
        let rel = relativeFolder.trimmingCharacters(in: .whitespacesAndNewlines)
        if var target = GitHubTarget.parse(source) {
            target.path = repoFolderPath(target.path, rel)
            if target.ref.isEmpty { target.ref = defaultRef }
            return target.webURL
        }
        if let (owner, repo) = ImportTrust.parseOwnerRepo(source) {
            return GitHubTarget(owner: owner, repo: repo, ref: defaultRef,
                                path: repoFolderPath("", rel)).webURL
        }
        return redactUserInfo(source.trimmingCharacters(in: .whitespacesAndNewlines))
    }

    /// Strip any `user:password@` userinfo from a source URL, mirroring Go
    /// `redactSourceUserInfo`. The stamped `source_url` is committed to the
    /// registry, so credentials must never survive in it. A string that is not
    /// a URL, or a URL without userinfo, is returned unchanged.
    static func redactUserInfo(_ source: String) -> String {
        guard var components = URLComponents(string: source),
              let scheme = components.scheme, !scheme.isEmpty,
              components.user != nil || components.password != nil
        else { return source }
        components.user = nil
        components.password = nil
        return components.string ?? source
    }

    /// Compose the repository path of a fetched skill folder from the source
    /// URL's own path and the folder's path relative to the fetch root.
    ///
    /// A folder fetch writes the URL's last segment as the top directory under
    /// the fetch root, so a relative path starting with that segment already
    /// covers it. A `/blob/` URL naming `SKILL.md` resolves to the file's
    /// directory, matching what the fetch did.
    static func repoFolderPath(_ urlPath: String, _ relativeFolder: String) -> String {
        var base = urlPath.trimmingCharacters(in: CharacterSet(charactersIn: "/"))
        if (base as NSString).lastPathComponent == Scan.mainFileName {
            base = (base as NSString).deletingLastPathComponent
            if base == "/" || base == "." { base = "" }
        }
        var rel = relativeFolder
        if rel.isEmpty { return base }
        var segments = rel.split(separator: "/").map(String.init)
        if !base.isEmpty, segments.first == (base as NSString).lastPathComponent {
            segments.removeFirst()
        }
        rel = segments.joined(separator: "/")
        if base.isEmpty { return rel }
        if rel.isEmpty { return base }
        return base + "/" + rel
    }

    /// Merge the provenance keys into one skill folder's `SKILL.md`, reporting
    /// whether the file changed. Runs after the gate and before the first
    /// write: the registry must receive the annotated copy.
    @discardableResult
    public static func stamp(folder: String, sourceURL: String, category: String) throws -> Bool {
        let keys = keys(sourceURL: sourceURL, category: category)
        guard !keys.isEmpty else { return false }
        let path = (folder as NSString).appendingPathComponent(Scan.mainFileName)
        let text = try String(contentsOfFile: path, encoding: .utf8)
        guard let merged = Frontmatter.merging(text, keys: keys) else { return false }
        try Data(merged.utf8).write(to: URL(fileURLWithPath: path), options: .atomic)
        return true
    }

    /// Pins a folder URL built for a source that named no ref. GitHub resolves
    /// `HEAD` to the repository's default branch, so the URL stays truthful
    /// without inventing a branch name the import never saw.
    static let defaultRef = "HEAD"
}
