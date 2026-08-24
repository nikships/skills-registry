import XCTest
@testable import SkillsRegistryCore

/// Swift mirror of the Go `FetchFolder` suite
/// (`cli/internal/registry/subtree_test.go`). Every response is scripted
/// through a `URLProtocol` stub, so nothing here touches the network.
final class GitHubSubtreeTests: XCTestCase {
    override func tearDown() {
        StubContents.reset()
        super.tearDown()
    }

    private func makeAPI() -> GitHubAPI {
        let cfg = URLSessionConfiguration.ephemeral
        cfg.protocolClasses = [StubContentsProtocol.self]
        return GitHubAPI(token: "t", session: URLSession(configuration: cfg))
    }

    /// The shared happy-path fixture: `skills/pdf` holding SKILL.md plus a
    /// nested `scripts/`, next to an unrelated sibling that must never be
    /// fetched.
    private func seedSkillFolder() {
        StubContents.seed(
            dirs: [
                "skills": [.dir("pdf"), .dir("other")],
                "skills/pdf": [.file("SKILL.md", sha: "sha-skill"), .dir("scripts"), .symlink("link")],
                "skills/pdf/scripts": [.file("run.sh", sha: "sha-run")],
                "skills/other": [.file("SKILL.md", sha: "sha-other")],
            ],
            files: [
                "skills/pdf/SKILL.md": "---\nname: pdf\n---\nBody.",
                "skills/pdf/scripts/run.sh": "#!/bin/sh\necho hi\n",
                "skills/other/SKILL.md": "should never be fetched",
            ])
    }

    func testFetchFolderTreeURL() async throws {
        seedSkillFolder()
        let target = try XCTUnwrap(GitHubTarget.parse("https://github.com/o/r/tree/main/skills/pdf"))
        let dest = try tempDir()
        let got = try await makeAPI().fetchFolder(target, into: dest)

        XCTAssertEqual((got.dir as NSString).lastPathComponent, "pdf")
        XCTAssertEqual(got.paths, ["SKILL.md", "scripts/run.sh"])
        XCTAssertEqual(try read(got.dir, "SKILL.md"), "---\nname: pdf\n---\nBody.")
        XCTAssertEqual(try read(got.dir, "scripts/run.sh"), "#!/bin/sh\necho hi\n")
        // Only the requested folder is touched, and every call pins the ref.
        for call in StubContents.calls {
            XCTAssertFalse(call.contains("skills/other"), "fetched outside the target: \(call)")
            XCTAssertTrue(call.contains("ref=main"), "call not ref-pinned: \(call)")
        }
    }

    func testFetchFolderBlobURLWithCommitSHA() async throws {
        seedSkillFolder()
        let sha = "0123456789abcdef0123456789abcdef01234567"
        let target = try XCTUnwrap(GitHubTarget.parse("https://github.com/o/r/blob/\(sha)/skills/pdf"))
        let got = try await makeAPI().fetchFolder(target, into: try tempDir())
        XCTAssertEqual(got.target.ref, sha)
        XCTAssertEqual(got.target.path, "skills/pdf")
        XCTAssertEqual(got.paths, ["SKILL.md", "scripts/run.sh"])
        // A SHA ref is unambiguous, so no alternate ref/path split is probed.
        XCTAssertEqual(StubContents.calls.count, 4, "calls: \(StubContents.calls)")
    }

    func testFetchFolderBlobURLPointingAtFile() async throws {
        // The public index links SKILL.md itself; import the folder holding it.
        seedSkillFolder()
        let target = try XCTUnwrap(GitHubTarget.parse("https://github.com/o/r/blob/main/skills/pdf/SKILL.md"))
        let got = try await makeAPI().fetchFolder(target, into: try tempDir())
        XCTAssertEqual(got.target.path, "skills/pdf")
        XCTAssertEqual((got.dir as NSString).lastPathComponent, "pdf")
        XCTAssertEqual(got.paths, ["SKILL.md", "scripts/run.sh"])
    }

    func testFetchFolderResolvesSlashedBranchName() async throws {
        // `release/2026-01` is a branch, so the first split (ref "release")
        // 404s and the fetcher falls through to the next reading.
        seedSkillFolder()
        let target = GitHubTarget(owner: "o", repo: "r", ref: "release", path: "2026-01/skills/pdf")
        let got = try await makeAPI().fetchFolder(target, into: try tempDir())
        XCTAssertEqual(got.target.ref, "release/2026-01")
        XCTAssertEqual(got.target.path, "skills/pdf")
    }

    func testFetchFolderRejectsTraversalInAPIResponse() async throws {
        for name in ["..", "../escape.md", "../../etc/passwd", "a/b", #"a\b"#] {
            StubContents.seed(
                dirs: ["skills/pdf": [.file(name, sha: "sha-x")]],
                files: ["skills/pdf/" + name: "pwned"])
            let dest = try tempDir()
            let target = GitHubTarget(owner: "o", repo: "r", ref: "main", path: "skills/pdf")
            do {
                _ = try await makeAPI().fetchFolder(target, into: dest)
                XCTFail("accepted unsafe entry \(name)")
            } catch let e as SubtreeError {
                guard case .unsafePath = e else { return XCTFail("got \(e) for \(name)") }
            }
            XCTAssertFalse(FileManager.default.fileExists(
                atPath: (dest as NSString).appendingPathComponent("escape.md")),
                "traversal wrote above the folder dir")
        }
    }

    func testFetchFolderRejectsTraversalInNestedDir() async throws {
        StubContents.seed(
            dirs: [
                "skills/pdf": [.dir("scripts")],
                "skills/pdf/scripts": [.file("../../pwned", sha: "s")],
            ],
            files: [:])
        let target = GitHubTarget(owner: "o", repo: "r", ref: "main", path: "skills/pdf")
        do {
            _ = try await makeAPI().fetchFolder(target, into: try tempDir())
            XCTFail("nested traversal was accepted")
        } catch let e as SubtreeError {
            guard case .unsafePath = e else { return XCTFail("got \(e)") }
        }
    }

    func testFetchFolderEmptyFolderErrors() async throws {
        StubContents.seed(dirs: ["skills/pdf": []], files: [:])
        let target = GitHubTarget(owner: "o", repo: "r", ref: "main", path: "skills/pdf")
        do {
            _ = try await makeAPI().fetchFolder(target, into: try tempDir())
            XCTFail("expected an error for an empty folder")
        } catch let e as SubtreeError {
            XCTAssertEqual(e, .empty("https://github.com/o/r/tree/main/skills/pdf"))
        }
    }

    func testFetchFolderMissingFolderErrorNamesTheURL() async throws {
        StubContents.seed(dirs: [:], files: [:])
        let target = GitHubTarget(owner: "o", repo: "r", ref: "main", path: "skills/nope")
        do {
            _ = try await makeAPI().fetchFolder(target, into: try tempDir())
            XCTFail("expected an error for a missing folder")
        } catch let e as SubtreeError {
            XCTAssertEqual(e, .notFound("https://github.com/o/r/tree/main/skills/nope"))
            let msg = try XCTUnwrap(e.errorDescription)
            XCTAssertTrue(msg.contains("skills/nope"), msg)
            XCTAssertTrue(msg.contains("branch, tag, or commit"), msg)
        }
    }

    func testFetchFolderPropagatesNon404() async throws {
        StubContents.seed(dirs: [:], files: [:])
        StubContents.forcedStatus = 500
        let target = GitHubTarget(owner: "o", repo: "r", ref: "main", path: "skills/pdf")
        do {
            _ = try await makeAPI().fetchFolder(target, into: try tempDir())
            XCTFail("expected the underlying 500 to surface")
        } catch let e as GitHubError {
            XCTAssertEqual(e.status, 500)
        }
    }

    func testFetchFolderRejectsNonFolderTarget() async throws {
        StubContents.seed(dirs: [:], files: [:])
        do {
            _ = try await makeAPI().fetchFolder(GitHubTarget(owner: "o", repo: "r"), into: try tempDir())
            XCTFail("expected an error for a target with no folder path")
        } catch let e as SubtreeError {
            XCTAssertEqual(e, .notAFolder("https://github.com/o/r"))
        }
        XCTAssertTrue(StubContents.calls.isEmpty, "no API call expected: \(StubContents.calls)")
    }

    func testFetchFolderFallsBackToBlobAPIForLargeFile() async throws {
        // Files over the Contents API's inline limit come back with no usable
        // body; the blob API serves them.
        StubContents.seed(
            dirs: ["skills/pdf": [.file("SKILL.md", sha: "sha-skill")]],
            files: [:])
        StubContents.blobs = ["sha-skill": "large body"]
        let target = GitHubTarget(owner: "o", repo: "r", ref: "main", path: "skills/pdf")
        let got = try await makeAPI().fetchFolder(target, into: try tempDir())
        XCTAssertEqual(try read(got.dir, "SKILL.md"), "large body")
    }

    func testContentsEndpointEscapesSegmentsAndPinsRef() {
        let t = GitHubTarget(owner: "o", repo: "r", ref: "release/1 2")
        XCTAssertEqual(GitHubAPI.contentsEndpoint(t, repoPath: "skills/my skill"),
                       "repos/o/r/contents/skills/my%20skill?ref=release%2F1%202")
        XCTAssertEqual(GitHubAPI.contentsEndpoint(GitHubTarget(owner: "o", repo: "r"), repoPath: "a"),
                       "repos/o/r/contents/a")
    }

    func testSafeJoinRejectsEscapes() throws {
        XCTAssertEqual(try GitHubAPI.safeJoin(dir: "/tmp/x", childRel: "a/b"), "/tmp/x/a/b")
        for bad in ["../out", "a/../../out", "/etc/passwd"] {
            XCTAssertThrowsError(try GitHubAPI.safeJoin(dir: "/tmp/x", childRel: bad), bad)
        }
    }

    // MARK: helpers

    private func tempDir() throws -> String {
        let dir = (NSTemporaryDirectory() as NSString)
            .appendingPathComponent("subtree-" + UUID().uuidString)
        try FileManager.default.createDirectory(atPath: dir, withIntermediateDirectories: true)
        addTeardownBlock { try? FileManager.default.removeItem(atPath: dir) }
        return dir
    }

    private func read(_ dir: String, _ rel: String) throws -> String {
        try String(contentsOfFile: (dir as NSString).appendingPathComponent(rel), encoding: .utf8)
    }
}

// MARK: - stub plumbing

/// Process-global scripted Contents API for repo `o/r`. A path that is neither
/// a seeded directory nor a seeded file answers 404, so an unexpected ref/path
/// split behaves exactly like the real API.
enum StubContents {
    struct Entry {
        var name: String
        var type: String
        var sha: String

        static func dir(_ name: String) -> Entry { Entry(name: name, type: "dir", sha: "") }
        static func file(_ name: String, sha: String) -> Entry { Entry(name: name, type: "file", sha: sha) }
        static func symlink(_ name: String) -> Entry { Entry(name: name, type: "symlink", sha: "") }
    }

    static let lock = NSLock()
    static var dirs: [String: [Entry]] = [:]
    static var files: [String: String] = [:]
    static var blobs: [String: String] = [:]
    static var calls: [String] = []
    /// When set, every request answers with this status instead of the script.
    static var forcedStatus: Int?

    static func reset() {
        lock.lock(); defer { lock.unlock() }
        dirs = [:]; files = [:]; blobs = [:]; calls = []; forcedStatus = nil
    }

    static func seed(dirs d: [String: [Entry]], files f: [String: String]) {
        reset()
        lock.lock(); defer { lock.unlock() }
        dirs = d
        files = f
    }

    /// Returns (status, JSON body) for a request path + query.
    static func handle(path: String, query: String?) -> (Int, Any) {
        lock.lock(); defer { lock.unlock() }
        var recorded = path
        if let query { recorded += "?" + query }
        calls.append(recorded)
        if let forcedStatus { return (forcedStatus, ["message": "forced"]) }

        let p = path.hasPrefix("/") ? String(path.dropFirst()) : path
        if let sha = stripPrefix(p, "repos/o/r/git/blobs/") {
            guard let content = blobs[sha] else { return (404, ["message": "no blob"]) }
            return (200, ["encoding": "base64", "content": base64(content)])
        }
        guard let repoPath = stripPrefix(p, "repos/o/r/contents/") else {
            return (404, ["message": "unknown path \(p)"])
        }
        let decoded = repoPath.removingPercentEncoding ?? repoPath
        if let entries = dirs[decoded] {
            return (200, entries.map { ["name": $0.name, "type": $0.type, "sha": $0.sha] })
        }
        if let content = files[decoded] {
            return (200, ["encoding": "base64", "content": base64(content)])
        }
        // A seeded file with no content (large-file case) still exists as an
        // entry in its parent listing; report the empty-content shape.
        if dirs.values.contains(where: { entries in
            entries.contains { $0.type == "file" && decoded.hasSuffix("/" + $0.name) }
        }) {
            return (200, ["encoding": "none", "content": ""])
        }
        return (404, ["message": "not found \(decoded)"])
    }

    private static func stripPrefix(_ s: String, _ prefix: String) -> String? {
        s.hasPrefix(prefix) ? String(s.dropFirst(prefix.count)) : nil
    }

    private static func base64(_ s: String) -> String {
        Data(s.utf8).base64EncodedString()
    }
}

final class StubContentsProtocol: URLProtocol {
    override class func canInit(with request: URLRequest) -> Bool { true }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }

    override func startLoading() {
        let comps = request.url.flatMap { URLComponents(url: $0, resolvingAgainstBaseURL: false) }
        let (status, json) = StubContents.handle(path: comps?.percentEncodedPath ?? "",
                                                query: comps?.percentEncodedQuery)
        let payload = (try? JSONSerialization.data(withJSONObject: json)) ?? Data()
        let resp = HTTPURLResponse(url: request.url!, statusCode: status,
                                   httpVersion: "HTTP/1.1",
                                   headerFields: ["Content-Type": "application/json"])!
        client?.urlProtocol(self, didReceive: resp, cacheStoragePolicy: .notAllowed)
        client?.urlProtocol(self, didLoad: payload)
        client?.urlProtocolDidFinishLoading(self)
    }

    override func stopLoading() {}
}
