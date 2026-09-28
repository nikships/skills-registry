import XCTest
@testable import SkillsRegistryCore

/// core-parity-7: branch names must be percent-encoded in trees paths and
/// `ref` query values, and `listSkills` must only report [] for a genuinely
/// empty repo. Every response is scripted through a `URLProtocol` stub, so
/// nothing here touches the network.
final class GitHubBranchEncodingTests: XCTestCase {
    private let repo = RepoRef(owner: "o", name: "r")

    override func tearDown() {
        StubBranches.reset()
        super.tearDown()
    }

    private func makeAPI() -> GitHubAPI {
        let cfg = URLSessionConfiguration.ephemeral
        cfg.protocolClasses = [StubBranchesProtocol.self]
        return GitHubAPI(token: "t", session: URLSession(configuration: cfg))
    }

    // MARK: - encoding tables

    func testEncodedBranchTable() {
        let cases: [(branch: String, want: String)] = [
            ("main", "main"),
            ("feature/x", "feature%2Fx"),
            ("release 2026-01", "release%202026%2D01"),
        ]
        for c in cases {
            XCTAssertEqual(GitHubAPI.encodedBranch(c.branch), c.want, "branch: \(c.branch)")
        }
    }

    func testEncodedBranchRefKeepsSlashes() {
        let cases: [(branch: String, want: String)] = [
            ("main", "main"),
            ("feature/x", "feature/x"),
            ("release 2026-01", "release%202026%2D01"),
        ]
        for c in cases {
            XCTAssertEqual(GitHubAPI.encodedBranchRef(c.branch), c.want, "branch: \(c.branch)")
        }
    }

    // MARK: - request URLs escape the branch

    func testListSkillsEncodesSlashBranch() async throws {
        StubBranches.routes = [
            "/repos/o/r/git/trees/feature%2Fx": (200, treeJSON([
                ("pdf", "tree", "t1"), ("pdf/SKILL.md", "blob", "b1"),
            ])),
            "/repos/o/r/git/blobs/b1": (200, blobJSON(
                "---\nname: PDF Tools\ndescription: Work with PDFs.\n---\nBody.")),
        ]
        let got = try await makeAPI().listSkills(repo, branch: "feature/x")
        XCTAssertEqual(got.value.map(\.slug), ["pdf"])
        XCTAssertTrue(StubBranches.calls.contains(where: {
            $0.hasPrefix("/repos/o/r/git/trees/feature%2Fx?")
        }), "trees call must escape the slash: \(StubBranches.calls)")
    }

    func testGetSkillEncodesSlashBranch() async throws {
        StubBranches.routes = [
            "/repos/o/r/git/trees/feature%2Fx": (200, treeJSON([
                ("pdf/SKILL.md", "blob", "b1"),
            ])),
            "/repos/o/r/git/blobs/b1": (200, blobJSON("---\nname: PDF\n---\nBody.")),
        ]
        let got = try await makeAPI().getSkill(repo, slug: "pdf", branch: "feature/x")
        XCTAssertEqual(got.files, ["SKILL.md"])
        XCTAssertTrue(StubBranches.calls[0].hasPrefix("/repos/o/r/git/trees/feature%2Fx?"),
                      "trees call must escape the slash: \(StubBranches.calls)")
    }

    func testSkillFileDataEncodesSlashBranch() async throws {
        StubBranches.routes = [
            "/repos/o/r/git/trees/feature%2Fx": (200, treeJSON([
                ("pdf/SKILL.md", "blob", "b1"),
            ])),
            "/repos/o/r/git/blobs/b1": (200, blobJSON("hello")),
        ]
        let got = try await makeAPI().skillFileData(repo, slug: "pdf", branch: "feature/x")
        XCTAssertEqual(got.value["SKILL.md"], Data("hello".utf8))
        XCTAssertTrue(StubBranches.calls[0].hasPrefix("/repos/o/r/git/trees/feature%2Fx?"),
                      "trees call must escape the slash: \(StubBranches.calls)")
    }

    func testFileContentEncodesRef() async throws {
        StubBranches.routes = [
            "/repos/o/r/contents/pdf/SKILL.md": (200, blobJSON("hello")),
        ]
        let got = try await makeAPI().fileContent(repo, path: "pdf/SKILL.md", branch: "release 2026-01")
        XCTAssertEqual(got, "hello")
        XCTAssertEqual(StubBranches.calls,
                       ["/repos/o/r/contents/pdf/SKILL.md?ref=release%202026%2D01"])
    }

    // MARK: - empty registry vs load failure

    func testListSkillsEmptyRepoReturnsEmpty() async throws {
        StubBranches.routes = [
            "/repos/o/r/git/trees/main": (404, ["message": "Not Found"]),
            "/repos/o/r": (200, repoJSON()),
            "/repos/o/r/git/ref/heads/main": (404, ["message": "Not Found"]),
        ]
        let got = try await makeAPI().listSkills(repo, branch: "main")
        XCTAssertEqual(got.value, [])
    }

    func testListSkillsUnknownBranchThrows() async throws {
        StubBranches.routes = [
            "/repos/o/r/git/trees/feature%2Fx": (404, ["message": "Not Found"]),
            "/repos/o/r": (200, repoJSON()),
        ]
        do {
            _ = try await makeAPI().listSkills(repo, branch: "feature/x")
            XCTFail("expected throw for an @branch that does not exist")
        } catch let e as GitHubError {
            XCTAssertTrue(e.isNotFound)
        }
        XCTAssertFalse(StubBranches.calls.contains(where: { $0.contains("git/ref/heads") }),
                       "no ref probe when the branch isn't the default: \(StubBranches.calls)")
    }

    func testListSkillsMissingRepoThrows() async throws {
        StubBranches.routes = [
            "/repos/o/r/git/trees/main": (404, ["message": "Not Found"]),
            "/repos/o/r": (404, ["message": "Not Found"]),
        ]
        do {
            _ = try await makeAPI().listSkills(repo, branch: "main")
            XCTFail("expected throw for a missing repo")
        } catch let e as GitHubError {
            XCTAssertTrue(e.isNotFound)
        }
    }

    func testListSkillsExistingRefWithFailedTreeThrows() async throws {
        StubBranches.routes = [
            "/repos/o/r/git/trees/main": (422, ["message": "tree failed"]),
            "/repos/o/r": (200, repoJSON()),
            "/repos/o/r/git/ref/heads/main": (200, ["object": ["sha": "c1"]]),
        ]
        do {
            _ = try await makeAPI().listSkills(repo, branch: "main")
            XCTFail("expected throw when the ref exists but the tree read fails")
        } catch let e as GitHubError {
            XCTAssertEqual(e.status, 422)
        }
    }

    func testListSkillsServerErrorRethrowsWithoutProbe() async throws {
        StubBranches.routes = [
            "/repos/o/r/git/trees/main": (500, ["message": "boom"]),
        ]
        do {
            _ = try await makeAPI().listSkills(repo, branch: "main")
            XCTFail("expected the 500 to propagate")
        } catch let e as GitHubError {
            XCTAssertEqual(e.status, 500)
        }
        XCTAssertEqual(StubBranches.calls.count, 1, "no empty-repo probe on non-404: \(StubBranches.calls)")
    }

    // MARK: - fixtures

    private func treeJSON(_ entries: [(path: String, type: String, sha: String)]) -> [String: Any] {
        ["sha": "tree-sha",
         "tree": entries.map { ["path": $0.path, "type": $0.type, "sha": $0.sha] }]
    }

    private func blobJSON(_ text: String) -> [String: Any] {
        ["encoding": "base64", "content": Data(text.utf8).base64EncodedString()]
    }

    private func repoJSON(defaultBranch: String = "main") -> [String: Any] {
        ["name": "r", "full_name": "o/r", "default_branch": defaultBranch]
    }
}

// MARK: - stub plumbing

/// Process-global scripted read API for repo `o/r`, keyed by percent-encoded
/// request path so tests can assert exact branch escaping. Unrouted paths
/// answer 404, like the real API.
enum StubBranches {
    static let lock = NSLock()
    static var calls: [String] = []
    static var routes: [String: (Int, Any)] = [:]

    static func reset() {
        lock.lock(); defer { lock.unlock() }
        calls = []; routes = [:]
    }

    static func handle(path: String, query: String?) -> (Int, Any) {
        lock.lock(); defer { lock.unlock() }
        var recorded = path
        if let query { recorded += "?" + query }
        calls.append(recorded)
        if let r = routes[path] { return r }
        return (404, ["message": "unrouted \(recorded)"])
    }
}

final class StubBranchesProtocol: URLProtocol {
    override class func canInit(with request: URLRequest) -> Bool { true }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }

    override func startLoading() {
        let comps = request.url.flatMap { URLComponents(url: $0, resolvingAgainstBaseURL: false) }
        let (status, json) = StubBranches.handle(path: comps?.percentEncodedPath ?? "",
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
