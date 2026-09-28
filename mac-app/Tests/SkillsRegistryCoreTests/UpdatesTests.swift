import XCTest
@testable import SkillsRegistryCore

final class SemverTests: XCTestCase {
    func testParse() {
        XCTAssertEqual(Semver("1.2.3"), Semver(major: 1, minor: 2, patch: 3))
        XCTAssertEqual(Semver("v0.6.0"), Semver(major: 0, minor: 6, patch: 0))
        XCTAssertEqual(Semver("1.2"), Semver(major: 1, minor: 2, patch: 0))
        XCTAssertEqual(Semver("2"), Semver(major: 2, minor: 0, patch: 0))
        XCTAssertEqual(Semver("1.2.3-rc1"), Semver(major: 1, minor: 2, patch: 3))
        XCTAssertNil(Semver("dev"))
        XCTAssertNil(Semver(""))
    }

    func testOrdering() {
        XCTAssertLessThan(Semver("0.5.30")!, Semver("0.6.0")!)
        XCTAssertLessThan(Semver("1.0.0")!, Semver("1.0.1")!)
        XCTAssertGreaterThan(Semver("2.0.0")!, Semver("1.9.9")!)
    }

    func testFirstIn() {
        XCTAssertEqual(Semver.firstIn("skills-registry version v0.5.30"),
                       Semver(major: 0, minor: 5, patch: 30))
        XCTAssertEqual(Semver.firstIn("v1.2.3"), Semver(major: 1, minor: 2, patch: 3))
        XCTAssertNil(Semver.firstIn("dev"))
    }
}

final class ReleaseChannelTests: XCTestCase {
    func testCLIMatching() {
        XCTAssertTrue(ReleaseChannel.cli.matches(tag: "v0.6.0"))
        XCTAssertFalse(ReleaseChannel.cli.matches(tag: "macapp-v0.2.0"))
        XCTAssertFalse(ReleaseChannel.cli.matches(tag: "v"))
        XCTAssertFalse(ReleaseChannel.cli.matches(tag: "release-1"))
    }

    func testMacAppMatching() {
        XCTAssertTrue(ReleaseChannel.macApp.matches(tag: "macapp-v0.2.0"))
        XCTAssertFalse(ReleaseChannel.macApp.matches(tag: "v0.6.0"))
    }

    func testVersionExtraction() {
        XCTAssertEqual(ReleaseChannel.cli.version(from: "v0.6.0"), Semver(major: 0, minor: 6, patch: 0))
        XCTAssertEqual(ReleaseChannel.macApp.version(from: "macapp-v1.3.4"), Semver(major: 1, minor: 3, patch: 4))
    }
}

final class UpdatesPickLatestTests: XCTestCase {
    private func rel(_ tag: String, draft: Bool = false, pre: Bool = false) -> [String: Any] {
        ["tag_name": tag, "draft": draft, "prerelease": pre,
         "html_url": "https://github.com/nikships/skills-registry/releases/tag/\(tag)"]
    }

    func testPicksHighestCLIIgnoringMacApp() {
        let releases = [
            rel("macapp-v9.9.9"),   // wrong channel, must be ignored
            rel("v0.5.30"),
            rel("v0.6.0"),
            rel("v0.5.31"),
        ]
        let info = Updates.pickLatest(from: releases, channel: .cli)
        XCTAssertEqual(info?.tag, "v0.6.0")
        XCTAssertEqual(info?.version, Semver(major: 0, minor: 6, patch: 0))
    }

    func testPicksHighestMacApp() {
        let releases = [rel("v1.0.0"), rel("macapp-v0.1.0"), rel("macapp-v0.2.0")]
        XCTAssertEqual(Updates.pickLatest(from: releases, channel: .macApp)?.tag, "macapp-v0.2.0")
    }

    func testSkipsDraftAndPrerelease() {
        let releases = [rel("v0.7.0", draft: true), rel("v0.6.5", pre: true), rel("v0.6.0")]
        XCTAssertEqual(Updates.pickLatest(from: releases, channel: .cli)?.tag, "v0.6.0")
    }

    func testEmptyWhenNoneMatch() {
        XCTAssertNil(Updates.pickLatest(from: [rel("macapp-v0.2.0")], channel: .cli))
    }

    func testIsNewer() {
        XCTAssertTrue(Updates.isNewer(installed: "v0.5.30", than: Semver("0.6.0")!))
        XCTAssertFalse(Updates.isNewer(installed: "v0.6.0", than: Semver("0.6.0")!))
        XCTAssertFalse(Updates.isNewer(installed: "skills-registry version v0.7.0", than: Semver("0.6.0")!))
        // Unknown / dev install → assume an update is available.
        XCTAssertTrue(Updates.isNewer(installed: "dev", than: Semver("0.6.0")!))
        XCTAssertTrue(Updates.isNewer(installed: nil, than: Semver("0.6.0")!))
    }
}

final class CheckThrottleTests: XCTestCase {
    func testNeverCheckedAlwaysChecks() {
        XCTAssertTrue(CheckThrottle.shouldCheck(lastCheck: 0, now: 1_000_000, interval: 300))
    }

    func testFreshCheckSkips() {
        XCTAssertFalse(CheckThrottle.shouldCheck(lastCheck: 1_000_000, now: 1_000_100, interval: 300))
    }

    func testStaleCheckRuns() {
        XCTAssertTrue(CheckThrottle.shouldCheck(lastCheck: 1_000_000, now: 1_000_301, interval: 300))
    }

    func testBoundaryChecks() {
        XCTAssertTrue(CheckThrottle.shouldCheck(lastCheck: 1_000_000, now: 1_000_300, interval: 300))
    }
}

/// `installTag` must never resolve the ambiguous `releases/latest` endpoint:
/// a failed lookup surfaces the underlying error and an empty channel reports
/// `noReleases`. Every response is scripted through a `URLProtocol` stub, so
/// nothing here touches the network.
final class InstallTagTests: XCTestCase {
    override func tearDown() {
        StubReleases.reset()
        super.tearDown()
    }

    private func session() -> URLSession {
        let cfg = URLSessionConfiguration.ephemeral
        cfg.protocolClasses = [StubReleasesProtocol.self]
        return URLSession(configuration: cfg)
    }

    private func rel(_ tag: String) -> [String: Any] {
        ["tag_name": tag, "draft": false, "prerelease": false]
    }

    func testResolvesHighestCLITagIgnoringMacApp() async throws {
        StubReleases.outcome = .releases([rel("macapp-v9.9.9"), rel("v0.5.30"), rel("v0.6.0")])
        let tag = try await Updates.installTag(repo: "o/r", session: session())
        XCTAssertEqual(tag, "v0.6.0")
        XCTAssertEqual(StubReleases.requests.count, 1)
        XCTAssertTrue(StubReleases.requests[0].url?.path.hasSuffix("/releases") ?? false)
    }

    func testEmptyChannelThrowsNoReleases() async {
        for releases in [[rel("macapp-v0.2.0")], []] {
            StubReleases.outcome = .releases(releases)
            do {
                _ = try await Updates.installTag(repo: "o/r", session: session())
                XCTFail("expected noReleases for \(releases)")
            } catch let error as CLIResolveError {
                XCTAssertEqual(error, .noReleases)
            } catch {
                XCTFail("wrong error: \(error)")
            }
        }
    }

    func testNetworkErrorPropagatesForTheToast() async {
        StubReleases.outcome = .failure(URLError(.notConnectedToInternet))
        do {
            _ = try await Updates.installTag(repo: "o/r", session: session())
            XCTFail("expected the network error")
        } catch let error as URLError {
            XCTAssertEqual(error.code, .notConnectedToInternet)
        } catch {
            XCTFail("wrong error: \(error)")
        }
    }

    func testNon200Throws() async {
        StubReleases.outcome = .status(500)
        do {
            _ = try await Updates.installTag(repo: "o/r", session: session())
            XCTFail("expected a GitHubError")
        } catch let error as GitHubError {
            XCTAssertEqual(error.status, 500)
        } catch {
            XCTFail("wrong error: \(error)")
        }
    }
}

/// Process-global scripted `/releases` state (URLProtocol has no instance context).
private enum StubReleases {
    enum Outcome {
        case releases([[String: Any]])
        case status(Int)
        case failure(Error)
    }

    static var outcome: Outcome = .releases([])
    static var requests: [URLRequest] = []

    static func reset() {
        outcome = .releases([])
        requests = []
    }
}

private final class StubReleasesProtocol: URLProtocol {
    override class func canInit(with request: URLRequest) -> Bool { true }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }
    override func startLoading() {
        StubReleases.requests.append(request)
        switch StubReleases.outcome {
        case .failure(let error):
            client?.urlProtocol(self, didFailWithError: error)
        case .status(let code):
            let resp = HTTPURLResponse(url: request.url!, statusCode: code,
                                       httpVersion: "HTTP/1.1", headerFields: nil)!
            client?.urlProtocol(self, didReceive: resp, cacheStoragePolicy: .notAllowed)
            client?.urlProtocol(self, didLoad: Data())
            client?.urlProtocolDidFinishLoading(self)
        case .releases(let arr):
            let payload = (try? JSONSerialization.data(withJSONObject: arr)) ?? Data()
            let resp = HTTPURLResponse(url: request.url!, statusCode: 200,
                                       httpVersion: "HTTP/1.1",
                                       headerFields: ["Content-Type": "application/json"])!
            client?.urlProtocol(self, didReceive: resp, cacheStoragePolicy: .notAllowed)
            client?.urlProtocol(self, didLoad: payload)
            client?.urlProtocolDidFinishLoading(self)
        }
    }
    override func stopLoading() {}
}
