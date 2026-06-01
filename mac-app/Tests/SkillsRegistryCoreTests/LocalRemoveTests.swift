import XCTest
@testable import SkillsRegistryCore

final class LocalRemoveTests: XCTestCase {
    private var home: String!

    override func setUpWithError() throws {
        home = NSTemporaryDirectory() + "localremove-" + UUID().uuidString
        try FileManager.default.createDirectory(atPath: home, withIntermediateDirectories: true)
    }

    override func tearDownWithError() throws {
        try? FileManager.default.removeItem(atPath: home)
        unsetenv("XDG_CACHE_HOME")
    }

    // MARK: cacheRoot

    func testCacheRootHonorsXDG() {
        setenv("XDG_CACHE_HOME", "/tmp/xdg", 1)
        defer { unsetenv("XDG_CACHE_HOME") }
        XCTAssertEqual(LocalRemove.cacheRoot(), "/tmp/xdg/skills-mcp/skills")
    }

    func testCacheRootFallsBackToHomeCache() {
        unsetenv("XDG_CACHE_HOME")
        let root = LocalRemove.cacheRoot()
        XCTAssertTrue(root.hasSuffix("/.cache/skills-mcp/skills"), "got \(root)")
    }

    // MARK: removeFromCache

    func testRemoveFromCacheClearsDirAndMeta() throws {
        let cacheBase = "\(home!)/cache"
        setenv("XDG_CACHE_HOME", cacheBase, 1)
        let root = LocalRemove.cacheRoot()
        let skillDir = "\(root)/demo"
        let metaFile = "\(root)/demo.meta.json"
        try FileManager.default.createDirectory(atPath: skillDir, withIntermediateDirectories: true)
        try "{}".write(toFile: metaFile, atomically: true, encoding: .utf8)

        XCTAssertTrue(LocalRemove.removeFromCache(slug: "demo"))
        XCTAssertFalse(FileManager.default.fileExists(atPath: skillDir))
        XCTAssertFalse(FileManager.default.fileExists(atPath: metaFile))
    }

    func testRemoveFromCacheReturnsFalseWhenAbsent() {
        setenv("XDG_CACHE_HOME", "\(home!)/cache", 1)
        XCTAssertFalse(LocalRemove.removeFromCache(slug: "never-cached"))
    }

    // MARK: removeFromDotFolders

    private func seedSkill(dot: String, folder: String) throws {
        let dir = "\(home!)/\(dot)/skills/\(folder)"
        try FileManager.default.createDirectory(atPath: dir, withIntermediateDirectories: true)
        try "x".write(toFile: "\(dir)/SKILL.md", atomically: true, encoding: .utf8)
    }

    func testRemovesLiteralMatch() throws {
        try seedSkill(dot: ".claude", folder: "demo")
        let deleted = LocalRemove.removeFromDotFolders(slug: "demo", home: home, cwd: home)
        XCTAssertEqual(deleted.count, 1)
        XCTAssertFalse(FileManager.default.fileExists(atPath: "\(home!)/.claude/skills/demo"))
    }

    func testRemovesSlugifiedMatch() throws {
        // Folder name "agp-9-upgrade" should match canonical slug "agp_9_upgrade".
        try seedSkill(dot: ".cursor", folder: "agp-9-upgrade")
        let deleted = LocalRemove.removeFromDotFolders(slug: "agp_9_upgrade", home: home, cwd: home)
        XCTAssertEqual(deleted.count, 1)
        XCTAssertFalse(FileManager.default.fileExists(atPath: "\(home!)/.cursor/skills/agp-9-upgrade"))
    }

    func testSweepsMultipleAgents() throws {
        try seedSkill(dot: ".claude", folder: "demo")
        try seedSkill(dot: ".factory", folder: "demo")
        let deleted = LocalRemove.removeFromDotFolders(slug: "demo", home: home, cwd: home)
        XCTAssertEqual(deleted.count, 2)
        // Sorted output.
        XCTAssertEqual(deleted, deleted.sorted())
    }

    func testNoMatchLeavesEverythingAlone() throws {
        try seedSkill(dot: ".claude", folder: "keep_me")
        let deleted = LocalRemove.removeFromDotFolders(slug: "demo", home: home, cwd: home)
        XCTAssertTrue(deleted.isEmpty)
        XCTAssertTrue(FileManager.default.fileExists(atPath: "\(home!)/.claude/skills/keep_me"))
    }
}
