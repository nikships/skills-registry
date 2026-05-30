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
         "html_url": "https://github.com/anand-92/skills-registry/releases/tag/\(tag)"]
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
