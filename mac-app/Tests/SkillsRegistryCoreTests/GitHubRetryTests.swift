import XCTest
@testable import SkillsRegistryCore

/// Retry classification for `GitHubAPI.sendRetrying` (core-parity-9).
/// Every response is scripted through a `URLProtocol` stub and every sleep
/// is recorded instead of waited out, so nothing here touches the network
/// or the clock.
final class GitHubRetryTests: XCTestCase {
    override func tearDown() {
        StubRetry.reset()
        super.tearDown()
    }

    private func makeAPI() -> GitHubAPI {
        let cfg = URLSessionConfiguration.ephemeral
        cfg.protocolClasses = [StubRetryProtocol.self]
        return GitHubAPI(token: "t", session: URLSession(configuration: cfg))
    }

    /// Runs `sendRetrying`, recording each requested sleep instead of waiting.
    private func retrying(_ api: GitHubAPI, attempts: Int = 4) async throws -> (Data, HTTPURLResponse) {
        try await api.sendRetrying(api.makeRequest("GET", "repos/o/r"), attempts: attempts) { delay in
            StubRetry.recordSleep(delay)
        }
    }

    func testPermission403FailsFast() async throws {
        StubRetry.script = [.init(status: 403, body: ["message": "Resource not accessible by personal access token"])]
        do {
            _ = try await retrying(makeAPI())
            XCTFail("expected the 403 to surface")
        } catch let e as GitHubError {
            XCTAssertEqual(e.status, 403)
        }
        XCTAssertEqual(StubRetry.calls, 1, "permission 403 must not be retried")
        XCTAssertTrue(StubRetry.sleeps.isEmpty, "no backoff for a permanent failure")
    }

    func testForbiddenWithoutRateLimitSignalFailsFast() async throws {
        // A bare 403 with no message, no Retry-After, and quota remaining is
        // a permission failure, not throttling.
        StubRetry.script = [.init(status: 403, headers: ["x-ratelimit-remaining": "59"], body: [:])]
        do {
            _ = try await retrying(makeAPI())
            XCTFail("expected the 403 to surface")
        } catch let e as GitHubError {
            XCTAssertEqual(e.status, 403)
            XCTAssertFalse(e.isRateLimited)
        }
        XCTAssertEqual(StubRetry.calls, 1)
        XCTAssertTrue(StubRetry.sleeps.isEmpty)
    }

    func testRateLimit403HonorsRetryAfter() async throws {
        StubRetry.script = [
            .init(status: 403, headers: ["Retry-After": "7"],
                  body: ["message": "API rate limit exceeded for user ID 123."]),
            .init(status: 200, body: ["ok": true]),
        ]
        let (_, resp) = try await retrying(makeAPI())
        XCTAssertEqual(StubRetry.calls, 2)
        XCTAssertEqual(StubRetry.sleeps, [7], "must wait exactly what GitHub asked, not exponential backoff")
        XCTAssertEqual(resp.statusCode, 200)
    }

    func testSecondaryRateLimit403BacksOffExponentially() async throws {
        StubRetry.script = [
            .init(status: 403, body: ["message": "You have exceeded a secondary rate limit. Please wait a few minutes."]),
            .init(status: 200, body: ["ok": true]),
        ]
        _ = try await retrying(makeAPI())
        XCTAssertEqual(StubRetry.calls, 2)
        XCTAssertEqual(StubRetry.sleeps, [0.8], "no header → exponential backoff seeded at 0.8s")
    }

    func testExhaustedQuota403Retries() async throws {
        StubRetry.script = [
            .init(status: 403, headers: ["x-ratelimit-remaining": "0"], body: ["message": "Forbidden"]),
            .init(status: 200, body: ["ok": true]),
        ]
        _ = try await retrying(makeAPI())
        XCTAssertEqual(StubRetry.calls, 2)
        XCTAssertEqual(StubRetry.sleeps.count, 1)
    }

    func testRetryAfterIsCapped() async throws {
        StubRetry.script = [
            .init(status: 429, headers: ["Retry-After": "3600"], body: ["message": "too many requests"]),
            .init(status: 200, body: ["ok": true]),
        ]
        _ = try await retrying(makeAPI())
        XCTAssertEqual(StubRetry.sleeps, [GitHubError.maxRetryAfter])
    }

    func testServerErrorsStillBackOff() async throws {
        StubRetry.script = [
            .init(status: 503, body: ["message": "Service Unavailable"]),
            .init(status: 500, body: ["message": "Internal Server Error"]),
            .init(status: 200, body: ["ok": true]),
        ]
        _ = try await retrying(makeAPI())
        XCTAssertEqual(StubRetry.calls, 3)
        XCTAssertEqual(StubRetry.sleeps.count, 2)
        XCTAssertEqual(StubRetry.sleeps[0], 0.8, accuracy: 0.0001)
        XCTAssertEqual(StubRetry.sleeps[1], 1.6, accuracy: 0.0001)
    }

    func testServerErrorsGiveUpAfterAttempts() async throws {
        StubRetry.script = Array(repeating: .init(status: 500, body: ["message": "boom"]), count: 4)
        do {
            _ = try await retrying(makeAPI())
            XCTFail("expected the 500 to surface after exhaustion")
        } catch let e as GitHubError {
            XCTAssertEqual(e.status, 500)
        }
        XCTAssertEqual(StubRetry.calls, 4)
        XCTAssertEqual(StubRetry.sleeps.count, 3)
    }

    func testOtherErrorsNeverRetry() async throws {
        for status in [400, 401, 404, 409, 422] {
            StubRetry.reset()
            StubRetry.script = [.init(status: status, body: ["message": "nope"])]
            do {
                _ = try await retrying(makeAPI())
                XCTFail("expected \(status) to surface")
            } catch let e as GitHubError {
                XCTAssertEqual(e.status, status)
            }
            XCTAssertEqual(StubRetry.calls, 1, "status \(status) must not retry")
            XCTAssertTrue(StubRetry.sleeps.isEmpty, "status \(status) must not sleep")
        }
    }

    func testParseRetryAfterSeconds() {
        XCTAssertEqual(GitHubError.parseRetryAfter("5"), 5)
        XCTAssertEqual(GitHubError.parseRetryAfter("  12  "), 12)
        XCTAssertEqual(GitHubError.parseRetryAfter("0"), 0)
        XCTAssertEqual(GitHubError.parseRetryAfter("3600"), GitHubError.maxRetryAfter)
        XCTAssertEqual(GitHubError.parseRetryAfter("-3"), 0)
        XCTAssertNil(GitHubError.parseRetryAfter("soon"))
        XCTAssertNil(GitHubError.parseRetryAfter(""))
    }

    func testParseRetryAfterHTTPDate() throws {
        let fmt = DateFormatter()
        fmt.locale = Locale(identifier: "en_US_POSIX")
        fmt.timeZone = TimeZone(secondsFromGMT: 0)
        fmt.dateFormat = "EEE, dd MMM yyyy HH:mm:ss zzz"
        let now = Date(timeIntervalSince1970: 1_784_000_000)
        let soon = try XCTUnwrap(GitHubError.parseRetryAfter(
            fmt.string(from: now.addingTimeInterval(30)), now: now))
        XCTAssertEqual(soon, 30, accuracy: 1)
        // A date in the past means "retry now".
        XCTAssertEqual(GitHubError.parseRetryAfter(fmt.string(from: now.addingTimeInterval(-60)), now: now), 0)
        // Far-future dates are capped like large second counts.
        XCTAssertEqual(GitHubError.parseRetryAfter(fmt.string(from: now.addingTimeInterval(3600)), now: now),
                       GitHubError.maxRetryAfter)
    }

    func testSendAttachesHeadersToError() async throws {
        StubRetry.script = [.init(status: 403, headers: ["Retry-After": "9"], body: ["message": "slow down"])]
        let api = makeAPI()
        do {
            _ = try await api.send(api.makeRequest("GET", "repos/o/r"))
            XCTFail("expected the 403 to surface")
        } catch let e as GitHubError {
            XCTAssertEqual(e.retryAfterDelay, 9, "headers must survive send so retry can honor them")
        }
    }
}

// MARK: - stub plumbing

/// Process-global scripted response queue (`URLProtocol` has no instance
/// context). Each request pops the next response; when the script runs out
/// the last response repeats, so exhaustion tests need no padding.
enum StubRetry {
    struct Response {
        var status: Int
        var headers: [String: String] = [:]
        var body: [String: Any] = [:]
    }

    static let lock = NSLock()
    static var script: [Response] = []
    static var calls = 0
    static var sleeps: [TimeInterval] = []

    static func reset() {
        lock.lock(); defer { lock.unlock() }
        script = []; calls = 0; sleeps = []
    }

    static func recordSleep(_ delay: TimeInterval) {
        lock.lock(); defer { lock.unlock() }
        sleeps.append(delay)
    }

    static func next() -> Response {
        lock.lock(); defer { lock.unlock() }
        calls += 1
        if script.count > 1 { return script.removeFirst() }
        return script.first ?? Response(status: 500, body: ["message": "no script"])
    }
}

final class StubRetryProtocol: URLProtocol {
    override class func canInit(with request: URLRequest) -> Bool { true }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }

    override func startLoading() {
        let next = StubRetry.next()
        let payload = (try? JSONSerialization.data(withJSONObject: next.body)) ?? Data()
        var fields = next.headers
        fields["Content-Type"] = "application/json"
        let resp = HTTPURLResponse(url: request.url!, statusCode: next.status,
                                   httpVersion: "HTTP/1.1", headerFields: fields)!
        client?.urlProtocol(self, didReceive: resp, cacheStoragePolicy: .notAllowed)
        client?.urlProtocol(self, didLoad: payload)
        client?.urlProtocolDidFinishLoading(self)
    }

    override func stopLoading() {}
}
