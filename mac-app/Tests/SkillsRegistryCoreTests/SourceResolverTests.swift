import XCTest
@testable import SkillsRegistryCore

final class SourceResolverTests: XCTestCase {
    // MARK: source classification

    func testIsLocalPath() {
        XCTAssertTrue(SourceResolver.isLocalPath("./skills"))
        XCTAssertTrue(SourceResolver.isLocalPath("../skills"))
        XCTAssertTrue(SourceResolver.isLocalPath("/abs/path"))
        XCTAssertTrue(SourceResolver.isLocalPath("~/skills"))
        XCTAssertFalse(SourceResolver.isLocalPath("owner/repo"))
        XCTAssertFalse(SourceResolver.isLocalPath("https://github.com/o/r.git"))
    }

    // MARK: validateLocalSourcePath (relative-only rules, mirror of Go)

    func testValidLocalRelativePath() throws {
        XCTAssertEqual(try SourceResolver.validateLocalSourcePath("./a/b"), "./a/b")
        XCTAssertEqual(try SourceResolver.validateLocalSourcePath("a/b"), "a/b")
    }

    func testRejectsBackslash() {
        XCTAssertThrowsError(try SourceResolver.validateLocalSourcePath("a\\b"))
        XCTAssertThrowsError(try SourceResolver.validateLocalSourcePath("a%5cb"))
    }

    func testRejectsEncodedSeparator() {
        XCTAssertThrowsError(try SourceResolver.validateLocalSourcePath("a%2fb"))
    }

    func testRejectsTilde() {
        XCTAssertThrowsError(try SourceResolver.validateLocalSourcePath("~/skills"))
    }

    func testRejectsAbsolute() {
        XCTAssertThrowsError(try SourceResolver.validateLocalSourcePath("/etc/passwd"))
        XCTAssertThrowsError(try SourceResolver.validateLocalSourcePath("C:/Windows"))
    }

    func testRejectsTraversal() {
        XCTAssertThrowsError(try SourceResolver.validateLocalSourcePath("../escape"))
        XCTAssertThrowsError(try SourceResolver.validateLocalSourcePath("a/../../escape"))
    }

    // MARK: validateTrustedLocalPath (native-picker path — absolute allowed)

    func testTrustedAllowsAbsolute() throws {
        // NSOpenPanel hands back an absolute path; the strict validator rejects
        // it, but the trusted picker path must accept it as-is.
        XCTAssertThrowsError(try SourceResolver.validateLocalSourcePath("/Users/me/skills"))
        XCTAssertEqual(
            try SourceResolver.validateTrustedLocalPath("/Users/me/skills", cwd: "/tmp"),
            "/Users/me/skills")
    }

    func testTrustedResolvesRelativeAgainstCwd() throws {
        XCTAssertEqual(
            try SourceResolver.validateTrustedLocalPath("skills", cwd: "/tmp/work"),
            "/tmp/work/skills")
    }

    func testTrustedStillRejectsTraversalAndSeparators() {
        XCTAssertThrowsError(try SourceResolver.validateTrustedLocalPath("/a/../../escape", cwd: "/tmp"))
        XCTAssertThrowsError(try SourceResolver.validateTrustedLocalPath("a\\b", cwd: "/tmp"))
        XCTAssertThrowsError(try SourceResolver.validateTrustedLocalPath("a%2fb", cwd: "/tmp"))
    }

    // MARK: clone URL / ref mapping (mirror of Go cloneURLAndRef)

    func testCloneURLAndRefForShorthand() {
        let (url, ref) = SourceResolver.cloneURLAndRef("owner/repo", target: nil)
        XCTAssertEqual(url, "https://github.com/owner/repo.git")
        XCTAssertEqual(ref, "")
    }

    func testCloneURLAndRefPinsBranchForTreeURL() {
        let target = GitHubTarget.parse("https://github.com/owner/repo/tree/dev")
        let (url, ref) = SourceResolver.cloneURLAndRef("https://github.com/owner/repo/tree/dev", target: target)
        XCTAssertEqual(url, "https://github.com/owner/repo.git")
        XCTAssertEqual(ref, "dev")
    }

    func testCloneURLAndRefDropsCommitSHA() {
        // `git clone --branch <sha>` fails, so a SHA ref must not be pinned.
        let sha = String(repeating: "a", count: 40)
        let raw = "https://github.com/owner/repo/tree/\(sha)"
        let (url, ref) = SourceResolver.cloneURLAndRef(raw, target: GitHubTarget.parse(raw))
        XCTAssertEqual(url, "https://github.com/owner/repo.git")
        XCTAssertEqual(ref, "")
    }

    func testCloneURLAndRefPassesForeignURLThrough() {
        let (url, ref) = SourceResolver.cloneURLAndRef("https://gitlab.com/owner/repo.git", target: nil)
        XCTAssertEqual(url, "https://gitlab.com/owner/repo.git")
        XCTAssertEqual(ref, "")
    }

    // MARK: end-to-end resolve (local + shorthand-without-clone)

    func testResolveLocalPathInPlace() async throws {
        let cwd = NSTemporaryDirectory() + "resolve-" + UUID().uuidString
        try FileManager.default.createDirectory(atPath: cwd + "/sub", withIntermediateDirectories: true)
        defer { try? FileManager.default.removeItem(atPath: cwd) }

        let resolved = try await SourceResolver.resolve("./sub", home: cwd, cwd: cwd)
        XCTAssertEqual((resolved.dir as NSString).standardizingPath,
                       ((cwd + "/sub") as NSString).standardizingPath)
    }

    func testResolveLocalNotADirectoryThrows() async {
        let cwd = NSTemporaryDirectory() + "resolve-" + UUID().uuidString
        try? FileManager.default.createDirectory(atPath: cwd, withIntermediateDirectories: true)
        defer { try? FileManager.default.removeItem(atPath: cwd) }
        do {
            _ = try await SourceResolver.resolve("./missing", home: cwd, cwd: cwd)
            XCTFail("expected notADirectory")
        } catch let e as SourceResolver.ResolveError {
            guard case .notADirectory = e else { return XCTFail("got \(e)") }
        } catch {
            XCTFail("unexpected error \(error)")
        }
    }

    func testResolveReportsGitNotFound() async {
        // A non-existent gitPath forces the clone branch to surface gitNotFound
        // without any network access.
        do {
            _ = try await SourceResolver.resolve("owner/repo", home: "/tmp", cwd: "/tmp",
                                                 gitPath: "/nonexistent/git-binary")
            XCTFail("expected clone failure")
        } catch is SourceResolver.ResolveError {
            // cloneFailed (couldn't launch the bogus binary) is acceptable.
        } catch {
            XCTFail("unexpected error \(error)")
        }
    }

    // MARK: folder URLs resolve through the fetcher, never through git

    /// A folder fetcher that materializes scripted files and records the target
    /// it was handed. `gitPath` is pointed at a bogus binary in these tests, so
    /// any fall-through to the clone path fails loudly.
    private final class FakeFetcher: GitHubFolderFetching, @unchecked Sendable {
        let files: [String: String]
        private(set) var received: GitHubTarget?
        var failure: Error?

        init(files: [String: String]) { self.files = files }

        func fetchFolder(_ target: GitHubTarget, into destRoot: String) async throws -> FetchedFolder {
            received = target
            if let failure { throw failure }
            let dir = (destRoot as NSString)
                .appendingPathComponent((target.path as NSString).lastPathComponent)
            let fm = FileManager.default
            for (rel, body) in files {
                let full = (dir as NSString).appendingPathComponent(rel)
                try fm.createDirectory(atPath: (full as NSString).deletingLastPathComponent,
                                       withIntermediateDirectories: true)
                try Data(body.utf8).write(to: URL(fileURLWithPath: full))
            }
            try fm.createDirectory(atPath: dir, withIntermediateDirectories: true)
            return FetchedFolder(dir: dir, target: target, paths: files.keys.sorted())
        }
    }

    func testResolveFetchesTreeFolderURLWithoutCloning() async throws {
        let fetcher = FakeFetcher(files: [
            "SKILL.md": "---\nname: pdf\n---\nBody.",
            "scripts/run.sh": "#!/bin/sh\n",
        ])
        let resolved = try await SourceResolver.resolve(
            "https://github.com/owner/repo/tree/main/skills/pdf",
            home: "/tmp", cwd: "/tmp",
            gitPath: "/nonexistent/git-binary", folderFetcher: fetcher)
        defer { resolved.cleanup() }

        XCTAssertEqual(fetcher.received,
                       GitHubTarget(owner: "owner", repo: "repo", ref: "main", path: "skills/pdf"))
        let discovered = Scan.discover([Scan.Source(path: resolved.dir, label: "test")])
        XCTAssertEqual(discovered.map(\.slug), ["pdf"])
        // Only the requested folder lands in the temp dir.
        let top = try FileManager.default.contentsOfDirectory(atPath: resolved.dir)
        XCTAssertEqual(top, ["pdf"])
    }

    func testResolveFetchesBlobFolderURLWithCommitSHA() async throws {
        let sha = "0123456789abcdef0123456789abcdef01234567"
        let fetcher = FakeFetcher(files: ["SKILL.md": "---\nname: summarize\n---\nBody."])
        let resolved = try await SourceResolver.resolve(
            "https://github.com/openclaw/openclaw/blob/\(sha)/skills/summarize",
            home: "/tmp", cwd: "/tmp",
            gitPath: "/nonexistent/git-binary", folderFetcher: fetcher)
        defer { resolved.cleanup() }

        XCTAssertEqual(fetcher.received, GitHubTarget(owner: "openclaw", repo: "openclaw",
                                                      ref: sha, path: "skills/summarize"))
        XCTAssertEqual(Scan.discover([Scan.Source(path: resolved.dir, label: "t")]).map(\.slug),
                       ["summarize"])
    }

    /// Every URL shape the public index publishes in `skill_url` reaches the
    /// fetcher unchanged — no rewriting between the index row and the fetch,
    /// and never the clone path (`gitPath` is bogus, so a fall-through would
    /// fail loudly).
    func testDiscoverRowURLsReachTheFetcherUnchanged() async throws {
        let sha = "0123456789abcdef0123456789abcdef01234567"
        let cases: [(url: String, want: GitHubTarget)] = [
            ("https://github.com/openclaw/openclaw/blob/\(sha)/skills/pdf",
             GitHubTarget(owner: "openclaw", repo: "openclaw", ref: sha, path: "skills/pdf")),
            ("https://github.com/o/r/blob/main/skills/pdf",
             GitHubTarget(owner: "o", repo: "r", ref: "main", path: "skills/pdf")),
            ("https://github.com/o/r/tree/main/skills/pdf",
             GitHubTarget(owner: "o", repo: "r", ref: "main", path: "skills/pdf")),
            ("https://github.com/o/r/tree/release/2026-01/skills/pdf",
             GitHubTarget(owner: "o", repo: "r", ref: "release", path: "2026-01/skills/pdf")),
        ]
        for c in cases {
            let fetcher = FakeFetcher(files: ["SKILL.md": "---\nname: pdf\n---\nBody."])
            let resolved = try await SourceResolver.resolve(
                c.url, home: "/tmp", cwd: "/tmp",
                gitPath: "/nonexistent/git-binary", folderFetcher: fetcher)
            defer { resolved.cleanup() }
            XCTAssertEqual(fetcher.received, c.want, "resolve(\(c.url))")
            XCTAssertEqual(Scan.discover([Scan.Source(path: resolved.dir, label: "t")]).map(\.slug),
                           ["pdf"], "resolve(\(c.url))")
        }
    }

    func testResolveFolderWithoutSkillFileErrors() async {
        let fetcher = FakeFetcher(files: ["helper.go": "package utils"])
        do {
            _ = try await SourceResolver.resolve(
                "https://github.com/owner/repo/tree/main/src/utils",
                home: "/tmp", cwd: "/tmp",
                gitPath: "/nonexistent/git-binary", folderFetcher: fetcher)
            XCTFail("expected noSkillFile")
        } catch let e as SourceResolver.ResolveError {
            guard case .noSkillFile(let url, let count) = e else { return XCTFail("got \(e)") }
            XCTAssertTrue(url.contains("src/utils"), "message should name the folder: \(url)")
            XCTAssertEqual(count, 1)
        } catch {
            XCTFail("unexpected error \(error)")
        }
    }

    func testResolveFolderURLWithoutFetcherReportsUnavailable() async {
        do {
            _ = try await SourceResolver.resolve(
                "https://github.com/owner/repo/tree/main/skills/pdf",
                home: "/tmp", cwd: "/tmp", gitPath: "/nonexistent/git-binary")
            XCTFail("expected folderFetchUnavailable")
        } catch let e as SourceResolver.ResolveError {
            XCTAssertEqual(e, .folderFetchUnavailable)
        } catch {
            XCTFail("unexpected error \(error)")
        }
    }

    func testResolvePropagatesFetchFailure() async {
        let fetcher = FakeFetcher(files: [:])
        fetcher.failure = SubtreeError.empty("https://github.com/owner/repo/tree/main/skills/pdf")
        do {
            _ = try await SourceResolver.resolve(
                "https://github.com/owner/repo/tree/main/skills/pdf",
                home: "/tmp", cwd: "/tmp",
                gitPath: "/nonexistent/git-binary", folderFetcher: fetcher)
            XCTFail("expected the fetch error to surface")
        } catch let e as SubtreeError {
            XCTAssertEqual(e, .empty("https://github.com/owner/repo/tree/main/skills/pdf"))
        } catch {
            XCTFail("unexpected error \(error)")
        }
    }

    func testRepoLevelURLDoesNotUseTheFetcher() async {
        let fetcher = FakeFetcher(files: ["SKILL.md": "x"])
        for source in ["owner/repo", "https://github.com/owner/repo",
                       "https://github.com/owner/repo/tree/dev",
                       "https://gitlab.com/owner/repo.git"] {
            do {
                _ = try await SourceResolver.resolve(source, home: "/tmp", cwd: "/tmp",
                                                     gitPath: "/nonexistent/git-binary",
                                                     folderFetcher: fetcher)
                XCTFail("expected the clone path to fail for \(source)")
            } catch is SourceResolver.ResolveError {
                XCTAssertNil(fetcher.received, "\(source) must not use the folder fetcher")
            } catch {
                XCTFail("unexpected error \(error)")
            }
        }
    }
}

/// Swift mirror of Go `TestParseGitHubURL`. Go and Swift must accept exactly
/// the same URL shapes, so this table is kept in lockstep with
/// `cli/internal/registry/subtree_test.go`.
final class GitHubTargetTests: XCTestCase {
    func testParseTable() {
        let accepted: [(String, GitHubTarget)] = [
            ("https://github.com/owner/repo/tree/main/skills/pdf",
             GitHubTarget(owner: "owner", repo: "repo", ref: "main", path: "skills/pdf")),
            ("https://github.com/openclaw/openclaw/blob/0123456789abcdef0123456789abcdef01234567/skills/summarize",
             GitHubTarget(owner: "openclaw", repo: "openclaw",
                          ref: "0123456789abcdef0123456789abcdef01234567", path: "skills/summarize")),
            ("https://github.com/o/r/blob/main/skills/foo/SKILL.md",
             GitHubTarget(owner: "o", repo: "r", ref: "main", path: "skills/foo/SKILL.md")),
            ("https://github.com/owner/repo/tree/dev",
             GitHubTarget(owner: "owner", repo: "repo", ref: "dev")),
            ("https://github.com/owner/repo", GitHubTarget(owner: "owner", repo: "repo")),
            ("https://github.com/owner/repo.git/tree/main/x",
             GitHubTarget(owner: "owner", repo: "repo", ref: "main", path: "x")),
            ("https://www.github.com/owner/repo/tree/main/skills/pdf/",
             GitHubTarget(owner: "owner", repo: "repo", ref: "main", path: "skills/pdf")),
            ("https://github.com/owner/repo/tree/main/skills/my%20skill",
             GitHubTarget(owner: "owner", repo: "repo", ref: "main", path: "skills/my skill")),
        ]
        for (raw, want) in accepted {
            XCTAssertEqual(GitHubTarget.parse(raw), want, "parse(\(raw))")
        }

        let rejected = [
            "owner/repo",
            "https://gitlab.com/owner/repo/tree/main/x",
            "git@github.com:owner/repo.git",
            "https://github.com/owner",
            "https://github.com/owner/repo/pull/12",
            "https://github.com/owner/repo/tree",
            "https://github.com/owner/repo/tree/main/..%2f..%2fetc",
            "https://github.com/owner/repo/tree/main/a%2Fb",
        ]
        for raw in rejected {
            XCTAssertNil(GitHubTarget.parse(raw), "parse(\(raw)) should be nil")
        }
    }

    func testAccessors() {
        let folder = GitHubTarget(owner: "o", repo: "r", ref: "main", path: "skills/pdf")
        XCTAssertTrue(folder.isFolder)
        XCTAssertFalse(folder.refIsSHA)
        XCTAssertEqual(folder.cloneURL, "https://github.com/o/r.git")
        XCTAssertEqual(folder.webURL, "https://github.com/o/r/tree/main/skills/pdf")

        XCTAssertTrue(GitHubTarget(owner: "o", repo: "r",
                                  ref: String(repeating: "a", count: 40), path: "x").refIsSHA)

        let repo = GitHubTarget(owner: "o", repo: "r")
        XCTAssertFalse(repo.isFolder)
        XCTAssertEqual(repo.webURL, "https://github.com/o/r")
        XCTAssertEqual(GitHubTarget(owner: "o", repo: "r", ref: "dev").webURL,
                       "https://github.com/o/r/tree/dev")
    }

    func testSplits() {
        let got = GitHubTarget(owner: "o", repo: "r", ref: "release", path: "2026-01/skills/pdf").splits
        XCTAssertEqual(got, [
            GitHubTarget(owner: "o", repo: "r", ref: "release", path: "2026-01/skills/pdf"),
            GitHubTarget(owner: "o", repo: "r", ref: "release/2026-01", path: "skills/pdf"),
            GitHubTarget(owner: "o", repo: "r", ref: "release/2026-01/skills", path: "pdf"),
        ])

        let sha = GitHubTarget(owner: "o", repo: "r", ref: String(repeating: "b", count: 40), path: "a/b/c")
        XCTAssertEqual(sha.splits, [sha], "a full SHA ref is unambiguous")
    }

    func testIsSafeSegment() {
        XCTAssertTrue(GitHubTarget.isSafeSegment("SKILL.md"))
        for bad in ["", ".", "..", "a/b", #"a\b"#] {
            XCTAssertFalse(GitHubTarget.isSafeSegment(bad), "\(bad) should be unsafe")
        }
    }
}
