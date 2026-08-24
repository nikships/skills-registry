import XCTest
@testable import SkillsRegistryCore

/// The shipped `Info.plist` must carry an App Transport Security exception for
/// the skill index host. `DiscoverClient` talks plain HTTP because port 443 on
/// that host serves an unrelated site behind a certificate issued for a
/// different name, so without the exception ATS cancels every search and the
/// Discover pane only ever renders "requires the use of a secure connection".
///
/// `DiscoverTests` all inject a fake transport, so none of them touch ATS.
/// These assertions are the guard: they fail if the endpoint host changes
/// without the plist following, or if the exception is dropped or widened.
final class ATSExceptionContractTests: XCTestCase {
    /// `Resources/Info.plist`, located relative to this source file so the test
    /// reads the file `scripts/bundle.sh` actually copies into the bundle.
    private func infoPlist(file: StaticString = #filePath) throws -> [String: Any] {
        let testsDir = URL(fileURLWithPath: "\(file)")
            .deletingLastPathComponent()   // SkillsRegistryCoreTests
            .deletingLastPathComponent()   // Tests
            .deletingLastPathComponent()   // mac-app
        let url = testsDir
            .appendingPathComponent("Resources")
            .appendingPathComponent("Info.plist")
        let data = try Data(contentsOf: url)
        guard let plist = try PropertyListSerialization.propertyList(
            from: data, options: [], format: nil) as? [String: Any] else {
            return [:]
        }
        return plist
    }

    /// The host `DiscoverClient` will actually contact by default.
    private func defaultIndexHost() throws -> String {
        let url = try XCTUnwrap(URL(string: DiscoverClient.defaultBaseURL))
        return try XCTUnwrap(url.host)
    }

    func testInfoPlistExemptsTheDefaultIndexHost() throws {
        let plist = try infoPlist()
        let ats = try XCTUnwrap(
            plist["NSAppTransportSecurity"] as? [String: Any],
            "Info.plist has no NSAppTransportSecurity block, so plain-HTTP index searches are cancelled by ATS")
        let domains = try XCTUnwrap(
            ats["NSExceptionDomains"] as? [String: Any],
            "NSAppTransportSecurity has no NSExceptionDomains")

        let host = try defaultIndexHost()
        let entry = try XCTUnwrap(
            domains[host] as? [String: Any],
            "No ATS exception for \(host); DiscoverClient.defaultBaseURL and Info.plist have drifted")
        XCTAssertEqual(entry["NSExceptionAllowsInsecureHTTPLoads"] as? Bool, true,
                       "The \(host) exception must allow insecure HTTP loads")
    }

    /// The exception is a targeted hole, not a global opt-out. `NSAllowsArbitraryLoads`
    /// would disable ATS for every host the app talks to, including the Sparkle feed.
    func testExceptionIsScopedRatherThanGlobal() throws {
        let plist = try infoPlist()
        let ats = try XCTUnwrap(plist["NSAppTransportSecurity"] as? [String: Any])

        for blanket in ["NSAllowsArbitraryLoads",
                        "NSAllowsArbitraryLoadsInWebContent",
                        "NSAllowsLocalNetworking"] {
            XCTAssertNotEqual(ats[blanket] as? Bool, true,
                              "\(blanket) disables ATS far beyond the index host")
        }

        let domains = try XCTUnwrap(ats["NSExceptionDomains"] as? [String: Any])
        XCTAssertEqual(domains.count, 1,
                       "Only the index host should be exempt; found \(domains.keys.sorted())")
    }

    /// Sparkle's appcast is fetched over HTTPS and must stay that way: an
    /// insecure update feed would let a network attacker serve a malicious
    /// appcast. Guarded here because it shares the plist with the ATS change.
    func testUpdateFeedStaysHTTPS() throws {
        let plist = try infoPlist()
        let feed = try XCTUnwrap(plist["SUFeedURL"] as? String)
        XCTAssertTrue(feed.hasPrefix("https://"), "SUFeedURL must be HTTPS, got \(feed)")
    }
}
