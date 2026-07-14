import XCTest
@testable import SkillsRegistryCore

final class BranchGateTests: XCTestCase {
    func testHeadCacheSetAndClear() async {
        let gate = BranchGate()
        let key = "o/r#main"
        let none = await gate.head(key)
        XCTAssertNil(none)

        await gate.setHead(key, commit: "c1", tree: "t1")
        let cached = await gate.head(key)
        XCTAssertEqual(cached?.commit, "c1")
        XCTAssertEqual(cached?.tree, "t1")

        await gate.clearHead(key)
        let cleared = await gate.head(key)
        XCTAssertNil(cleared)
    }

    func testKeyIsPerRepoAndBranch() {
        let repo = RepoRef(owner: "octo", name: "skills")
        XCTAssertEqual(BranchGate.key(repo, "main"), "octo/skills#main")
        XCTAssertNotEqual(BranchGate.key(repo, "main"), BranchGate.key(repo, "dev"))
    }

    func testWithLockSerializesFIFO() async throws {
        let gate = BranchGate()
        let order = Order()

        // Enqueue 5 ops that each suspend mid-flight; without the lock they'd
        // interleave. With it, start/end pairs must be strictly sequential.
        try await withThrowingTaskGroup(of: Void.self) { group in
            for i in 0..<5 {
                group.addTask {
                    _ = try await gate.withLock("k") {
                        await order.append("start\(i)")
                        try await Task.sleep(nanoseconds: 10_000_000)
                        await order.append("end\(i)")
                        return i
                    }
                }
                // Give each enqueue a moment so FIFO order is deterministic.
                try await Task.sleep(nanoseconds: 2_000_000)
            }
            try await group.waitForAll()
        }

        let events = await order.events
        XCTAssertEqual(events.count, 10)
        // Every "start" must be immediately followed by its own "end" — no overlap.
        for i in stride(from: 0, to: events.count, by: 2) {
            let start = events[i], end = events[i + 1]
            XCTAssertTrue(start.hasPrefix("start"))
            XCTAssertEqual("end" + start.dropFirst("start".count), end)
        }
    }

    func testThrowingOpDoesNotWedgeQueue() async throws {
        let gate = BranchGate()
        struct Boom: Error {}

        do {
            _ = try await gate.withLock("k") { () -> Int in throw Boom() }
            XCTFail("expected throw")
        } catch {}

        // The chain must still make progress after a failure.
        let got = try await gate.withLock("k") { 42 }
        XCTAssertEqual(got, 42)
    }

    func testIndependentKeysDoNotBlockEachOther() async throws {
        let gate = BranchGate()
        async let slow: Int = gate.withLock("a") {
            try await Task.sleep(nanoseconds: 50_000_000)
            return 1
        }
        let start = Date()
        let fast = try await gate.withLock("b") { 2 }
        XCTAssertEqual(fast, 2)
        XCTAssertLessThan(Date().timeIntervalSince(start), 0.05)
        _ = try await slow
    }
}

private actor Order {
    var events: [String] = []
    func append(_ e: String) { events.append(e) }
}

// MARK: - cached-HEAD reuse against a stubbed GitHub

/// Scripted in-memory GitHub Git Data API: enough of refs/commits/trees/blobs
/// for `publish` and `delete` to run end-to-end, counting ref reads so we can
/// assert consecutive writes skip the network HEAD read.
final class CachedHeadTests: XCTestCase {
    override func tearDown() {
        StubGitHub.reset()
        super.tearDown()
    }

    private func makeAPI() -> GitHubAPI {
        let cfg = URLSessionConfiguration.ephemeral
        cfg.protocolClasses = [StubURLProtocol.self]
        return GitHubAPI(token: "t", session: URLSession(configuration: cfg))
    }

    func testConsecutiveWritesReuseCachedHead() async throws {
        // Unique repo per test — BranchGate.shared is process-global.
        let repo = RepoRef(owner: "u", name: "reg-\(UUID().uuidString.prefix(8))")
        StubGitHub.seed(repo: repo, branch: "main",
                        files: ["alpha/SKILL.md": "hi", "beta/SKILL.md": "yo"])
        let api = makeAPI()

        _ = try await api.delete(repo, slug: "alpha", message: "remove: alpha", branch: "main")
        let refReadsAfterFirst = StubGitHub.refReads
        XCTAssertEqual(refReadsAfterFirst, 1, "first write cold-reads HEAD once")

        _ = try await api.delete(repo, slug: "beta", message: "remove: beta", branch: "main")
        XCTAssertEqual(StubGitHub.refReads, refReadsAfterFirst,
                       "second write must reuse the cached HEAD — no ref re-read")

        _ = try await api.publish(repo, slug: "gamma", files: ["SKILL.md": Data("g".utf8)],
                                  message: "add: gamma", branch: "main")
        XCTAssertEqual(StubGitHub.refReads, refReadsAfterFirst,
                       "publish after delete must also reuse the cached HEAD")

        // The stub's ref must have advanced 3 commits past the seed.
        XCTAssertEqual(StubGitHub.commitCount, 3)
    }

    func testConflictClearsCacheAndRecovers() async throws {
        let repo = RepoRef(owner: "u", name: "reg-\(UUID().uuidString.prefix(8))")
        StubGitHub.seed(repo: repo, branch: "main", files: ["alpha/SKILL.md": "hi"])
        let api = makeAPI()

        _ = try await api.publish(repo, slug: "one", files: ["SKILL.md": Data("1".utf8)],
                                  message: "add: one", branch: "main")
        XCTAssertEqual(StubGitHub.refReads, 1)

        // Simulate an out-of-band push: move the ref under the cached HEAD.
        StubGitHub.moveRefOutOfBand()

        // Next write commits against the stale cached HEAD → PATCH 422 →
        // retryOnConflict clears the cache, re-reads, and succeeds.
        _ = try await api.publish(repo, slug: "two", files: ["SKILL.md": Data("2".utf8)],
                                  message: "add: two", branch: "main")
        XCTAssertEqual(StubGitHub.refReads, 2, "conflict retry must re-read HEAD fresh")
    }
}

// MARK: - stub plumbing

/// Process-global scripted GitHub state (URLProtocol has no instance context).
enum StubGitHub {
    struct Tree { var files: [String: String] }  // path → blob SHA

    static var repoPath = ""
    static var branch = "main"
    static var headCommit = ""
    static var commitTree: [String: String] = [:]   // commit SHA → tree SHA
    static var trees: [String: Tree] = [:]          // tree SHA → contents
    static var commitParent: [String: String] = [:]  // commit SHA → parent SHA
    static var refReads = 0
    static var commitCount = 0
    private static var serial = 0

    static let lock = NSLock()

    static func reset() {
        lock.lock(); defer { lock.unlock() }
        repoPath = ""; branch = "main"; headCommit = ""
        commitTree = [:]; trees = [:]; commitParent = [:]
        refReads = 0; commitCount = 0; serial = 0
    }

    static func nextSHA(_ kind: String) -> String {
        serial += 1
        return "\(kind)\(serial)"
    }

    static func seed(repo: RepoRef, branch b: String, files: [String: String]) {
        reset()
        lock.lock(); defer { lock.unlock() }
        repoPath = repo.fullName
        branch = b
        var t = Tree(files: [:])
        for (path, _) in files { t.files[path] = nextSHA("blob") }
        let treeSHA = nextSHA("tree")
        trees[treeSHA] = t
        headCommit = nextSHA("commit")
        commitTree[headCommit] = treeSHA
    }

    /// Move HEAD to a fresh empty-delta commit, as if pushed from elsewhere.
    static func moveRefOutOfBand() {
        lock.lock(); defer { lock.unlock() }
        let oldTree = commitTree[headCommit]!
        let newTree = nextSHA("tree")
        trees[newTree] = trees[oldTree]
        headCommit = nextSHA("commit")
        commitTree[headCommit] = newTree
    }

    // Request router. Returns (status, JSON object).
    static func handle(method: String, path: String, body: [String: Any]?) -> (Int, [String: Any]) {
        lock.lock(); defer { lock.unlock() }
        let p = path.hasPrefix("/") ? String(path.dropFirst()) : path
        let prefix = "repos/\(repoPath)/git/"
        guard p.hasPrefix(prefix) else { return (404, ["message": "unknown repo \(p)"]) }
        let rest = String(p.dropFirst(prefix.count))

        switch (method, rest) {
        case ("GET", "ref/heads/\(branch)"):
            refReads += 1
            return (200, ["object": ["sha": headCommit]])
        case ("GET", let r) where r.hasPrefix("commits/"):
            let sha = String(r.dropFirst("commits/".count))
            guard let tree = commitTree[sha] else { return (404, ["message": "no commit"]) }
            return (200, ["sha": sha, "tree": ["sha": tree]])
        case ("GET", let r) where r.hasPrefix("trees/"):
            let sha = String(r.dropFirst("trees/".count)).components(separatedBy: "?")[0]
            guard let t = trees[sha] else { return (404, ["message": "no tree"]) }
            let entries: [[String: Any]] = t.files.map {
                ["path": $0.key, "type": "blob", "sha": $0.value]
            }
            return (200, ["sha": sha, "tree": entries])
        case ("POST", "blobs"):
            return (201, ["sha": nextSHA("blob")])
        case ("POST", "trees"):
            guard let base = body?["base_tree"] as? String,
                  var t = trees[base],
                  let entries = body?["tree"] as? [[String: Any]] else {
                return (422, ["message": "bad tree"])
            }
            for e in entries {
                guard let path = e["path"] as? String else { continue }
                if let sha = e["sha"] as? String {
                    t.files[path] = sha
                } else {
                    t.files.removeValue(forKey: path)  // null SHA → delete
                }
            }
            let sha = nextSHA("tree")
            trees[sha] = t
            return (201, ["sha": sha])
        case ("POST", "commits"):
            guard let tree = body?["tree"] as? String,
                  let parents = body?["parents"] as? [String] else {
                return (422, ["message": "bad commit"])
            }
            let sha = nextSHA("commit")
            commitTree[sha] = tree
            // Record the parent for the PATCH fast-forward check.
            commitParent[sha] = parents.first ?? ""
            return (201, ["sha": sha])
        case ("PATCH", "refs/heads/\(branch)"):
            guard let sha = body?["sha"] as? String else { return (422, ["message": "no sha"]) }
            // force:false semantics — reject non-fast-forward.
            guard commitParent[sha] == headCommit else {
                return (422, ["message": "Update is not a fast forward"])
            }
            headCommit = sha
            commitCount += 1
            return (200, ["object": ["sha": sha]])
        default:
            return (404, ["message": "unhandled \(method) \(rest)"])
        }
    }
}

final class StubURLProtocol: URLProtocol {
    override class func canInit(with request: URLRequest) -> Bool { true }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }

    override func startLoading() {
        let method = request.httpMethod ?? "GET"
        let path = request.url?.path ?? ""
        var body: [String: Any]?
        if let stream = request.httpBodyStream {
            stream.open()
            var data = Data()
            let bufSize = 4096
            let buf = UnsafeMutablePointer<UInt8>.allocate(capacity: bufSize)
            defer { buf.deallocate(); stream.close() }
            while stream.hasBytesAvailable {
                let n = stream.read(buf, maxLength: bufSize)
                if n <= 0 { break }
                data.append(buf, count: n)
            }
            body = (try? JSONSerialization.jsonObject(with: data)) as? [String: Any]
        } else if let data = request.httpBody {
            body = (try? JSONSerialization.jsonObject(with: data)) as? [String: Any]
        }

        let (status, json) = StubGitHub.handle(method: method, path: path, body: body)
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
