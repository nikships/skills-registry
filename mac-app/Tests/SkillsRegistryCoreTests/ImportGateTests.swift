import XCTest
@testable import SkillsRegistryCore

/// Swift mirror of `cli/internal/importgate` and `cli/internal/trust`. The two
/// implementations must reach the same verdict for the same input, so these
/// assertions are kept in lockstep with the Go suites.
final class ImportGateTests: XCTestCase {
    // MARK: - grades

    /// An absent grade renders as `unscored`. This is a correctness
    /// requirement, not a presentation choice: the index leaves a grade empty
    /// when it never evaluated the skill, and an empty cell reads as "fine".
    func testAbsentGradeRendersUnscored() {
        for level in ["", "   ", "\n"] {
            XCTAssertEqual(ImportGate.label(level), ImportGate.unscoredLabel)
        }
        XCTAssertEqual(ImportGate.label("Good"), "Good")
        XCTAssertEqual(ImportGate.label("  Poor  "), "Poor")
    }

    /// All three grades are always rendered, so a confirmation screen cannot
    /// silently omit one.
    func testLinesAlwaysRenderAllThreeGrades() {
        let lines = ImportScores(safety: "Good").lines
        XCTAssertEqual(lines.map(\.name), ["safety", "completeness", "executability"])
        XCTAssertEqual(lines.map(\.level), ["Good", ImportGate.unscoredLabel, ImportGate.unscoredLabel])
    }

    func testAnyReportsWhetherTheIndexGradedAtAll() {
        XCTAssertFalse(ImportScores().any)
        XCTAssertFalse(ImportScores(safety: "  ", completeness: "", executability: " ").any)
        XCTAssertTrue(ImportScores(executability: "Poor").any)
    }

    /// An unscored safety grade is not `Poor`, and is not a pass either.
    func testSafetyIsPoorOnlyForPoor() {
        XCTAssertTrue(ImportScores(safety: "Poor").safetyIsPoor)
        XCTAssertTrue(ImportScores(safety: " poor ").safetyIsPoor, "the grade is compared case-insensitively")
        XCTAssertFalse(ImportScores(safety: "").safetyIsPoor)
        XCTAssertFalse(ImportScores(safety: "Average").safetyIsPoor)
        XCTAssertFalse(ImportScores(safety: "Good").safetyIsPoor)
    }

    // MARK: - review

    func testPoorSafetyBlocks() {
        let review = ImportReview.evaluate(slug: "pdf", scores: ImportScores(safety: "Poor"))
        XCTAssertTrue(review.blocked)
        XCTAssertEqual(review.blocks.map(\.kind), [.poorSafety])
        XCTAssertTrue(review.summary.contains("Poor"), review.summary)
    }

    func testUnscoredAndPassingGradesDoNotBlock() {
        for scores in [ImportScores(), ImportScores(safety: "Good"), ImportScores(safety: "Average")] {
            XCTAssertFalse(ImportReview.evaluate(slug: "pdf", scores: scores).blocked,
                           "\(scores) should not block")
        }
    }

    // MARK: - the decision an import acts on

    /// Registry-only is the default. An untrusted import must not durably
    /// install into agent folders unless the user said so.
    func testRegistryOnlyByDefault() {
        let decision = ImportDecision(url: "https://github.com/o/r/blob/main/skills/pdf",
                                      scores: ImportScores(safety: "Good"))
        XCTAssertFalse(decision.installIntoAgents)
        XCTAssertTrue(decision.permitted)
        XCTAssertFalse(decision.installPermitted, "the durable install is opt-in")
    }

    func testInstallOnlyOnExplicitOptIn() {
        var decision = ImportDecision(url: "u", scores: ImportScores(safety: "Good"))
        decision.installIntoAgents = true
        XCTAssertTrue(decision.installPermitted)
    }

    /// A blocker needs explicit consent, and clearing it never implies an
    /// install (the Go CLI's `--allow-unsafe` does not imply `--install`).
    func testBlockerNeedsConsentAndConsentIsNotAnInstall() {
        var decision = ImportDecision(url: "u", scores: ImportScores(safety: "Poor"))
        XCTAssertFalse(decision.permitted, "Poor safety blocks until acknowledged")
        XCTAssertFalse(decision.installPermitted)

        decision.allowUnsafe = true
        XCTAssertTrue(decision.permitted)
        XCTAssertFalse(decision.installPermitted, "allowUnsafe must never imply an install")

        decision.installIntoAgents = true
        XCTAssertTrue(decision.installPermitted)
    }

    /// Opting into the install does not clear a blocker either: the two
    /// consents are independent in both directions.
    func testInstallOptInDoesNotClearABlocker() {
        var decision = ImportDecision(url: "u", scores: ImportScores(safety: "Poor"))
        decision.installIntoAgents = true
        XCTAssertFalse(decision.permitted)
        XCTAssertFalse(decision.installPermitted)
    }

    // MARK: - trust classification (mirror of Go trust.Assess)

    func testDiscoverPicksAreAlwaysUntrusted() {
        // Even a URL under the user's own owner is untrusted when the user did
        // not choose it.
        let a = ImportTrust.assess("https://github.com/me/repo/blob/main/skills/pdf",
                                   owners: ["me"], fromDiscover: true)
        XCTAssertEqual(a.origin, .discover)
        XCTAssertTrue(a.untrusted)
        XCTAssertEqual(a.owner, "me")
        XCTAssertTrue(a.reason.contains("public skill index"), a.reason)
    }

    func testOwnRepoAndLocalPathAreTrusted() {
        for source in ["./skills", "/abs/skills", "../skills", "~/skills"] {
            let a = ImportTrust.assess(source)
            XCTAssertEqual(a.origin, .localPath, source)
            XCTAssertFalse(a.untrusted, source)
        }
        for source in ["me/repo", "https://github.com/ME/repo",
                       "https://github.com/me/repo/blob/main/skills/pdf"] {
            let a = ImportTrust.assess(source, owners: ["me"])
            XCTAssertEqual(a.origin, .ownRepo, source)
            XCTAssertFalse(a.untrusted, source)
            XCTAssertEqual(a.owner.lowercased(), "me", source)
        }
    }

    func testThirdPartyGitHubAndForeignRemotesAreUntrusted() {
        let pub = ImportTrust.assess("https://github.com/stranger/repo/blob/main/skills/pdf",
                                     owners: ["me"])
        XCTAssertEqual(pub.origin, .publicRepo)
        XCTAssertTrue(pub.untrusted)
        XCTAssertEqual(pub.owner, "stranger")

        for source in ["https://gitlab.com/o/r.git", "git@github.com:o/r.git"] {
            let a = ImportTrust.assess(source, owners: ["me"])
            XCTAssertEqual(a.origin, .remoteGit, source)
            XCTAssertTrue(a.untrusted, source)
        }
    }

    /// An empty owner list makes every GitHub source third-party, which fails
    /// safe.
    func testEmptyOwnerListFailsSafe() {
        let a = ImportTrust.assess("https://github.com/me/repo/blob/main/skills/pdf")
        XCTAssertEqual(a.origin, .publicRepo)
        XCTAssertTrue(a.untrusted)
    }

    func testParseOwnerRepo() {
        XCTAssertEqual(ImportTrust.parseOwnerRepo("owner/repo")?.owner, "owner")
        XCTAssertEqual(ImportTrust.parseOwnerRepo("owner/repo.git")?.repo, "repo")
        for bad in ["./a/b", "https://github.com/o/r", "owner", "a/b/c", "owner/repo/"] {
            XCTAssertNil(ImportTrust.parseOwnerRepo(bad), bad)
        }
    }

    // MARK: - provenance (mirror of Go provenance.go)

    func testProvenanceKeysCarryCategoryAndSourceURL() {
        let keys = ImportProvenance.keys(
            sourceURL: "https://github.com/o/r/tree/abc/skills/pdf", category: "AIGC")
        XCTAssertEqual(keys.map(\.name), [Frontmatter.categoryKey, Frontmatter.sourceURLKey])
        XCTAssertEqual(keys.map(\.value), ["AIGC", "https://github.com/o/r/tree/abc/skills/pdf"])
    }

    /// An invented category is worse than an absent one, so a row with no
    /// category stamps only `source_url`.
    func testProvenanceOmitsAnAbsentCategory() {
        let keys = ImportProvenance.keys(sourceURL: "https://github.com/o/r/tree/abc/x", category: "  ")
        XCTAssertEqual(keys.map(\.name), [Frontmatter.sourceURLKey])
    }

    /// The category is third-party text written into a file agents load, so it
    /// is collapsed to one line and clipped.
    func testCategoryIsBoundedToOneShortLine() {
        XCTAssertEqual(ImportProvenance.boundedCategory("AIGC\nname: hijacked"), "AIGC name: hijacked")
        let long = String(repeating: "x", count: ImportProvenance.maxCategoryLength + 20)
        XCTAssertEqual(ImportProvenance.boundedCategory(long).count, ImportProvenance.maxCategoryLength)
        XCTAssertEqual(ImportProvenance.boundedCategory("  Developer   Tools  "), "Developer Tools")
    }

    /// `source_url` names the skill's own folder. A `/blob/` URL naming
    /// `SKILL.md` resolves to its directory, matching what the fetch did.
    func testSourceURLNamesTheSkillFolder() {
        let cases: [(source: String, rel: String, want: String)] = [
            ("https://github.com/o/r/blob/abc/skills/pdf", "",
             "https://github.com/o/r/tree/abc/skills/pdf"),
            ("https://github.com/o/r/blob/abc/skills/pdf/SKILL.md", "",
             "https://github.com/o/r/tree/abc/skills/pdf"),
            // A folder fetch writes the URL's last segment as the top dir, so a
            // relative path starting with it must not double up.
            ("https://github.com/o/r/tree/main/skills", "skills/pdf",
             "https://github.com/o/r/tree/main/skills/pdf"),
            // A folder of skills: each skill gets its own subfolder URL.
            ("https://github.com/o/r/tree/main/skills", "skills/nested/pdf",
             "https://github.com/o/r/tree/main/skills/nested/pdf"),
            // A source that named no ref is pinned to HEAD, not a guessed branch.
            ("https://github.com/o/r", "pdf", "https://github.com/o/r/tree/HEAD/pdf"),
            ("o/r", "pdf", "https://github.com/o/r/tree/HEAD/pdf"),
        ]
        for c in cases {
            XCTAssertEqual(ImportProvenance.sourceURL(for: c.source, relativeFolder: c.rel), c.want,
                           "sourceURL(\(c.source), \(c.rel))")
        }
    }

    /// A non-GitHub remote has no folder-URL form to derive, so the source is
    /// recorded as given.
    func testSourceURLPassesForeignRemotesThrough() {
        XCTAssertEqual(ImportProvenance.sourceURL(for: "https://gitlab.com/o/r.git"),
                       "https://gitlab.com/o/r.git")
    }

    /// The stamp rewrites the fetched copy in place, so the registry receives
    /// the annotated bytes.
    func testStampWritesBothKeysIntoTheFetchedCopy() throws {
        let dir = NSTemporaryDirectory() + "stamp-" + UUID().uuidString
        try FileManager.default.createDirectory(atPath: dir, withIntermediateDirectories: true)
        defer { try? FileManager.default.removeItem(atPath: dir) }
        let path = (dir as NSString).appendingPathComponent(Scan.mainFileName)
        try Data("---\nname: pdf\ndescription: Fill PDFs.\n---\nBody.\n".utf8)
            .write(to: URL(fileURLWithPath: path))

        let url = "https://github.com/o/r/tree/abc/skills/pdf"
        XCTAssertTrue(try ImportProvenance.stamp(folder: dir, sourceURL: url, category: "AIGC"))

        let written = try String(contentsOfFile: path, encoding: .utf8)
        XCTAssertTrue(written.contains("category: AIGC"), written)
        XCTAssertTrue(written.contains("source_url: \(url)"), written)
        // The body is the upstream skill, unmodified.
        XCTAssertEqual(Frontmatter.body(written), "Body.\n")
        let (name, desc) = Frontmatter.parseSummary(written, slug: "x")
        XCTAssertEqual(name, "pdf")
        XCTAssertEqual(desc, "Fill PDFs.")
    }

    /// An upstream file that already declares a key keeps its own value.
    func testStampKeepsAnUpstreamCategory() throws {
        let dir = NSTemporaryDirectory() + "stamp-" + UUID().uuidString
        try FileManager.default.createDirectory(atPath: dir, withIntermediateDirectories: true)
        defer { try? FileManager.default.removeItem(atPath: dir) }
        let path = (dir as NSString).appendingPathComponent(Scan.mainFileName)
        try Data("---\nname: pdf\ncategory: Upstream Choice\n---\nBody.\n".utf8)
            .write(to: URL(fileURLWithPath: path))

        XCTAssertTrue(try ImportProvenance.stamp(
            folder: dir, sourceURL: "https://github.com/o/r/tree/abc/skills/pdf", category: "AIGC"))
        let written = try String(contentsOfFile: path, encoding: .utf8)
        XCTAssertTrue(written.contains("category: Upstream Choice"), written)
        XCTAssertFalse(written.contains("category: AIGC"), written)
        XCTAssertTrue(written.contains("source_url:"),
                      "source_url is still added alongside the kept category")
    }

    func testStampWithNothingToWriteLeavesTheFileAlone() throws {
        let dir = NSTemporaryDirectory() + "stamp-" + UUID().uuidString
        try FileManager.default.createDirectory(atPath: dir, withIntermediateDirectories: true)
        defer { try? FileManager.default.removeItem(atPath: dir) }
        let path = (dir as NSString).appendingPathComponent(Scan.mainFileName)
        let original = "---\nname: pdf\n---\nBody.\n"
        try Data(original.utf8).write(to: URL(fileURLWithPath: path))

        XCTAssertFalse(try ImportProvenance.stamp(folder: dir, sourceURL: "", category: ""))
        XCTAssertEqual(try String(contentsOfFile: path, encoding: .utf8), original)
    }
}
