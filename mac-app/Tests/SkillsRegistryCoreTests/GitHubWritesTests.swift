import XCTest
@testable import SkillsRegistryCore

/// Empty-repo write tests: the first publish / SKILL.md edit on a freshly
/// created registry (no HEAD yet) must take the initial-commit path instead
/// of surfacing the raw 404 from the ref read, and delete must report
/// slug-not-found. Every response is scripted through a `URLProtocol` stub,
/// so nothing here touches the network.
final class GitHubWritesTests: XCTestCase {
    override func tearDown() {
        StubWrites.reset()
        super.tearDown()
    }

    private func makeAPI() -> GitHubAPI {
        let cfg = URLSessionConfiguration.ephemeral
        cfg.protocolClasses = [StubWritesProtocol.self]
        return GitHubAPI(token: "t", session: URLSession(configuration: cfg))
    }

    private func repo(_ name: String) throws -> RepoRef {
        try XCTUnwrap(RepoRef(fullName: "o/\(name)"))
    }

    /// Script the shared initial-commit tail (blob → tree → commit → ref).
    private func scriptInitialCommit(repoPath: String, commitSHA: String = "commit1") {
        StubWrites.script("POST", "/repos/\(repoPath)/git/blobs", (200, ["sha": "blob1"]))
        StubWrites.script("POST", "/repos/\(repoPath)/git/trees", (200, ["sha": "tree1"]))
        StubWrites.script("POST", "/repos/\(repoPath)/git/commits", (200, ["sha": commitSHA]))
        StubWrites.script("POST", "/repos/\(repoPath)/git/refs", (200, ["sha": commitSHA]))
    }

    func testPublishOnEmptyRepoCreatesRef() async throws {
        let repoPath = "o/empty-publish"
        StubWrites.script("GET", "/repos/\(repoPath)/git/ref/heads/main", (404, ["message": "Not Found"]))
        scriptInitialCommit(repoPath: repoPath)
        let repo = try repo("empty-publish")

        let sha = try await makeAPI().publish(repo, slug: "demo",
            files: ["SKILL.md": Data("# Demo".utf8)], message: "", branch: "main")
        XCTAssertEqual(sha, "commit1")

        // Initial-commit shape: no base_tree, no parents, ref created.
        let trees = try XCTUnwrap(StubWrites.bodies(method: "POST", path: "/repos/\(repoPath)/git/trees").first)
        XCTAssertNil(trees["base_tree"], "initial tree must not carry base_tree: \(trees)")
        let entries = try XCTUnwrap(trees["tree"] as? [[String: Any]])
        XCTAssertEqual(entries.map { $0["path"] as? String }, ["demo/SKILL.md"])
        let commits = try XCTUnwrap(StubWrites.bodies(method: "POST", path: "/repos/\(repoPath)/git/commits").first)
        XCTAssertEqual((commits["parents"] as? [Any])?.count, 0, "initial commit must have no parents")
        XCTAssertEqual(commits["message"] as? String, "publish: demo")
        let refs = try XCTUnwrap(StubWrites.bodies(method: "POST", path: "/repos/\(repoPath)/git/refs").first)
        XCTAssertEqual(refs["ref"] as? String, "refs/heads/main")
        XCTAssertTrue(StubWrites.calls.allSatisfy { $0.method != "PATCH" },
                      "must create the ref, not PATCH it: \(StubWrites.calls)")
    }

    func testPublishOnEmptyRepoRespectsConfiguredBranch() async throws {
        let repoPath = "o/empty-branch"
        StubWrites.script("GET", "/repos/\(repoPath)/git/ref/heads/develop", (404, ["message": "Not Found"]))
        scriptInitialCommit(repoPath: repoPath)
        let repo = try repo("empty-branch")

        _ = try await makeAPI().publish(repo, slug: "demo",
            files: ["SKILL.md": Data("# Demo".utf8)], message: "publish: demo", branch: "develop")

        let refs = try XCTUnwrap(StubWrites.bodies(method: "POST", path: "/repos/\(repoPath)/git/refs").first)
        XCTAssertEqual(refs["ref"] as? String, "refs/heads/develop")
        for call in StubWrites.calls {
            XCTAssertFalse(call.path.contains("heads/main"), "touched main instead of develop: \(call)")
        }
    }

    func testPublishOnExistingRepoFastForwards() async throws {
        let repoPath = "o/existing-publish"
        StubWrites.script("GET", "/repos/\(repoPath)/git/ref/heads/main",
                          (200, ["object": ["sha": "parent1"]]))
        StubWrites.script("GET", "/repos/\(repoPath)/git/commits/parent1",
                          (200, ["sha": "parent1", "tree": ["sha": "base1"]]))
        StubWrites.script("GET", "/repos/\(repoPath)/git/trees/base1",
                          (200, ["sha": "base1", "tree": []]))
        StubWrites.script("POST", "/repos/\(repoPath)/git/blobs", (200, ["sha": "blob1"]))
        StubWrites.script("POST", "/repos/\(repoPath)/git/trees", (200, ["sha": "tree2"]))
        StubWrites.script("POST", "/repos/\(repoPath)/git/commits", (200, ["sha": "commit2"]))
        StubWrites.script("PATCH", "/repos/\(repoPath)/git/refs/heads/main", (200, ["sha": "commit2"]))
        let repo = try repo("existing-publish")

        let sha = try await makeAPI().publish(repo, slug: "demo",
            files: ["SKILL.md": Data("# Demo".utf8)], message: "", branch: "main")
        XCTAssertEqual(sha, "commit2")
        XCTAssertTrue(StubWrites.calls.contains { $0.method == "PATCH" }, "expected a ref fast-forward")
        XCTAssertFalse(StubWrites.calls.contains { $0.method == "POST" && $0.path.hasSuffix("/git/refs") },
                       "must not take the initial-commit path on an existing repo")
    }

    func testUpdateSkillMarkdownOnEmptyRepoCreatesSkill() async throws {
        let repoPath = "o/empty-edit"
        StubWrites.script("GET", "/repos/\(repoPath)/git/ref/heads/main", (404, ["message": "Not Found"]))
        scriptInitialCommit(repoPath: repoPath)
        let repo = try repo("empty-edit")

        let sha = try await makeAPI().updateSkillMarkdown(repo, slug: "demo",
            markdown: "# Demo\n", message: "", branch: "main")
        XCTAssertEqual(sha, "commit1")

        let trees = try XCTUnwrap(StubWrites.bodies(method: "POST", path: "/repos/\(repoPath)/git/trees").first)
        XCTAssertNil(trees["base_tree"], "initial tree must not carry base_tree: \(trees)")
        let entries = try XCTUnwrap(trees["tree"] as? [[String: Any]])
        XCTAssertEqual(entries.map { $0["path"] as? String }, ["demo/SKILL.md"])
        let refs = try XCTUnwrap(StubWrites.bodies(method: "POST", path: "/repos/\(repoPath)/git/refs").first)
        XCTAssertEqual(refs["ref"] as? String, "refs/heads/main")
    }

    func testDeleteOnEmptyRepoThrowsSlugNotFound() async throws {
        let repoPath = "o/empty-delete"
        StubWrites.script("GET", "/repos/\(repoPath)/git/ref/heads/main", (404, ["message": "Not Found"]))
        let repo = try repo("empty-delete")

        do {
            _ = try await makeAPI().delete(repo, slug: "demo", message: "", branch: "main")
            XCTFail("expected slugNotFound on an empty repo")
        } catch let e as WriteError {
            guard case .slugNotFound(let slug) = e else { return XCTFail("got \(e)") }
            XCTAssertEqual(slug, "demo")
        }
        XCTAssertEqual(StubWrites.calls.count, 1, "no write may follow the empty-repo read: \(StubWrites.calls)")
    }

    func testDeleteOnExistingRepoRemovesSubtree() async throws {
        let repoPath = "o/existing-delete"
        StubWrites.script("GET", "/repos/\(repoPath)/git/ref/heads/main",
                          (200, ["object": ["sha": "parent1"]]))
        StubWrites.script("GET", "/repos/\(repoPath)/git/commits/parent1",
                          (200, ["sha": "parent1", "tree": ["sha": "base1"]]))
        StubWrites.script("GET", "/repos/\(repoPath)/git/trees/base1", (200, [
            "sha": "base1",
            "tree": [
                ["path": "demo/SKILL.md", "type": "blob", "sha": "b1"],
                ["path": "other/SKILL.md", "type": "blob", "sha": "b2"],
            ],
        ]))
        StubWrites.script("POST", "/repos/\(repoPath)/git/trees", (200, ["sha": "tree2"]))
        StubWrites.script("POST", "/repos/\(repoPath)/git/commits", (200, ["sha": "commit2"]))
        StubWrites.script("PATCH", "/repos/\(repoPath)/git/refs/heads/main", (200, ["sha": "commit2"]))
        let repo = try repo("existing-delete")

        let sha = try await makeAPI().delete(repo, slug: "demo", message: "", branch: "main")
        XCTAssertEqual(sha, "commit2")
        let trees = try XCTUnwrap(StubWrites.bodies(method: "POST", path: "/repos/\(repoPath)/git/trees").first)
        XCTAssertEqual(trees["base_tree"] as? String, "base1")
        let entries = try XCTUnwrap(trees["tree"] as? [[String: Any]])
        XCTAssertEqual(entries.count, 1)
        XCTAssertEqual(entries[0]["path"] as? String, "demo/SKILL.md")
        XCTAssertTrue(entries[0]["sha"] is NSNull, "deletion entry must carry a null SHA")
    }

    func testBulkPushOnEmptyRepoStillWorks() async throws {
        let repoPath = "o/empty-bulk"
        StubWrites.script("GET", "/repos/\(repoPath)/git/ref/heads/main", (404, ["message": "Not Found"]))
        scriptInitialCommit(repoPath: repoPath)
        let repo = try repo("empty-bulk")

        let sha = try await makeAPI().bulkPush(repo, files: ["demo/SKILL.md": Data("# Demo".utf8)],
                                               message: "import", branch: "main")
        XCTAssertEqual(sha, "commit1")
        let refs = try XCTUnwrap(StubWrites.bodies(method: "POST", path: "/repos/\(repoPath)/git/refs").first)
        XCTAssertEqual(refs["ref"] as? String, "refs/heads/main")
    }
}

// MARK: - stub plumbing

/// Process-global scripted Git Data API. Responses are queued per
/// "METHOD path" (path excludes the query string); every call is recorded
/// with its decoded JSON body. Unscripted calls answer 400 so a missing
/// fixture fails loudly instead of masquerading as an empty repo.
enum StubWrites {
    struct Call {
        var method: String
        var path: String
        var body: [String: Any]?
    }

    static let lock = NSLock()
    static var scripts: [String: [(Int, Any)]] = [:]
    static var calls: [Call] = []

    static func reset() {
        lock.lock(); defer { lock.unlock() }
        scripts = [:]; calls = []
    }

    static func script(_ method: String, _ path: String, _ responses: (Int, Any)...) {
        lock.lock(); defer { lock.unlock() }
        scripts[method + " " + path, default: []].append(contentsOf: responses)
    }

    static func bodies(method: String, path: String) -> [[String: Any]] {
        lock.lock(); defer { lock.unlock() }
        return calls.filter { $0.method == method && $0.path == path }.compactMap { $0.body }
    }

    static func handle(_ request: URLRequest) -> (Int, Any) {
        lock.lock(); defer { lock.unlock() }
        let method = request.httpMethod ?? "GET"
        let path = request.url?.path ?? ""
        // URLSession may hand the protocol a stream instead of the bytes.
        var bodyData = request.httpBody
        if (bodyData == nil || bodyData!.isEmpty), let stream = request.httpBodyStream {
            bodyData = readStream(stream)
        }
        var body: [String: Any]?
        if let data = bodyData, !data.isEmpty {
            body = (try? JSONSerialization.jsonObject(with: data)) as? [String: Any]
        }
        calls.append(Call(method: method, path: path, body: body))
        let key = method + " " + path
        if var queue = scripts[key], !queue.isEmpty {
            let next = queue.removeFirst()
            scripts[key] = queue
            return next
        }
        return (400, ["message": "unstubbed \(key)"])
    }

    private static func readStream(_ stream: InputStream) -> Data {
        stream.open()
        defer { stream.close() }
        var data = Data()
        var buffer = [UInt8](repeating: 0, count: 4096)
        while stream.hasBytesAvailable {
            let n = stream.read(&buffer, maxLength: buffer.count)
            if n <= 0 { break }
            data.append(buffer, count: n)
        }
        return data
    }
}

final class StubWritesProtocol: URLProtocol {
    override class func canInit(with request: URLRequest) -> Bool { true }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }

    override func startLoading() {
        let (status, json) = StubWrites.handle(request)
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
