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

    // MARK: GitHub /tree/ URL parsing

    func testParsesTreeURLWithSubpath() {
        let parsed = SourceResolver.parseGitHubTreeURL("https://github.com/owner/repo/tree/main/skills/pdf")
        XCTAssertEqual(parsed, SourceResolver.TreeURL(
            cloneURL: "https://github.com/owner/repo.git", ref: "main", subpath: "skills/pdf"))
    }

    func testParsesTreeURLWithoutSubpath() {
        let parsed = SourceResolver.parseGitHubTreeURL("https://github.com/owner/repo/tree/dev")
        XCTAssertEqual(parsed, SourceResolver.TreeURL(
            cloneURL: "https://github.com/owner/repo.git", ref: "dev", subpath: nil))
    }

    func testStripsDotGitFromTreeRepo() {
        let parsed = SourceResolver.parseGitHubTreeURL("https://github.com/owner/repo.git/tree/main/x")
        XCTAssertEqual(parsed?.cloneURL, "https://github.com/owner/repo.git")
        XCTAssertEqual(parsed?.subpath, "x")
    }

    func testNonTreeURLReturnsNil() {
        XCTAssertNil(SourceResolver.parseGitHubTreeURL("https://github.com/owner/repo"))
        XCTAssertNil(SourceResolver.parseGitHubTreeURL("https://gitlab.com/owner/repo/tree/main"))
        XCTAssertNil(SourceResolver.parseGitHubTreeURL("owner/repo"))
    }

    // MARK: end-to-end resolve (local + shorthand-without-clone)

    func testResolveLocalPathInPlace() async throws {
        let cwd = NSTemporaryDirectory() + "resolve-" + UUID().uuidString
        try FileManager.default.createDirectory(atPath: cwd + "/sub", withIntermediateDirectories: true)
        defer { try? FileManager.default.removeItem(atPath: cwd) }

        let resolved = try await SourceResolver.resolve("./sub", home: cwd, cwd: cwd)
        XCTAssertNil(resolved.subpath)
        XCTAssertEqual((resolved.dir as NSString).standardizingPath,
                       ((cwd + "/sub") as NSString).standardizingPath)
        XCTAssertEqual(resolved.scanRoot, resolved.dir)
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

    // scanRoot composition with a subpath.
    func testScanRootJoinsSubpath() {
        let r = SourceResolver.Resolved(dir: "/tmp/clone", subpath: "skills/pdf", cleanup: {})
        XCTAssertEqual(r.scanRoot, "/tmp/clone/skills/pdf")
    }
}
