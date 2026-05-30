import XCTest
@testable import SkillsRegistryCore

final class MetaSkillTests: XCTestCase {
    private var home: String!

    override func setUpWithError() throws {
        home = NSTemporaryDirectory() + "metaskill-" + UUID().uuidString
        try FileManager.default.createDirectory(atPath: home, withIntermediateDirectories: true)
    }

    override func tearDownWithError() throws {
        try? FileManager.default.removeItem(atPath: home)
    }

    private func mkAgentDir(_ dot: String) throws {
        try FileManager.default.createDirectory(
            atPath: (home as NSString).appendingPathComponent(dot),
            withIntermediateDirectories: true)
    }

    func testDetectsOnlyExistingHomeAgents() throws {
        try mkAgentDir(".claude")
        try mkAgentDir(".cursor")
        let detected = MetaSkill.detectedTargets(home: home).map(\.dotDir)
        XCTAssertTrue(detected.contains(".claude"))
        XCTAssertTrue(detected.contains(".cursor"))
        XCTAssertFalse(detected.contains(".factory"))   // not created
        XCTAssertFalse(detected.contains(".agents"))     // cwd-based universal, skipped
    }

    func testMissingThenInstallThenCurrent() throws {
        try mkAgentDir(".claude")
        let repo = "anand-92/my-skills"

        var status = MetaSkill.status(home: home, registryRepo: repo)
        XCTAssertEqual(status.detectedCount, 1)
        XCTAssertTrue(status.anyMissing)
        XCTAssertTrue(status.needsAction)
        XCTAssertEqual(status.installedCount, 0)

        let written = try MetaSkill.install(home: home, registryRepo: repo)
        XCTAssertEqual(written, 1)

        // File landed at the expected path with the expected content.
        let path = MetaSkill.skillPath(for: MetaSkill.detectedTargets(home: home)[0], home: home)
        XCTAssertTrue(path.hasSuffix("/.claude/skills/skills-registry/SKILL.md"))
        let body = try String(contentsOfFile: path, encoding: .utf8)
        XCTAssertEqual(body, SkillMdTemplate.skillMd(registryRepo: repo))

        status = MetaSkill.status(home: home, registryRepo: repo)
        XCTAssertFalse(status.needsAction)
        XCTAssertEqual(status.installedCount, 1)
    }

    func testOutdatedDetectionAndRefresh() throws {
        try mkAgentDir(".claude")
        let repo = "anand-92/my-skills"
        let path = MetaSkill.skillPath(for: MetaSkill.detectedTargets(home: home)[0], home: home)
        try FileManager.default.createDirectory(
            atPath: (path as NSString).deletingLastPathComponent, withIntermediateDirectories: true)
        try "stale content".write(toFile: path, atomically: true, encoding: .utf8)

        var status = MetaSkill.status(home: home, registryRepo: repo)
        XCTAssertTrue(status.anyOutdated)
        XCTAssertFalse(status.anyMissing)
        XCTAssertEqual(status.installedCount, 1)

        let written = try MetaSkill.install(home: home, registryRepo: repo)
        XCTAssertEqual(written, 1)

        status = MetaSkill.status(home: home, registryRepo: repo)
        XCTAssertFalse(status.needsAction)
    }

    func testCurrentSkillIsNotRewritten() throws {
        try mkAgentDir(".claude")
        let repo = "anand-92/my-skills"
        _ = try MetaSkill.install(home: home, registryRepo: repo)
        // Second install should write nothing — everything is already current.
        let written = try MetaSkill.install(home: home, registryRepo: repo)
        XCTAssertEqual(written, 0)
    }
}
