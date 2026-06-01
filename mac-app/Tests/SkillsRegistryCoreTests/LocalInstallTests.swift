import XCTest
@testable import SkillsRegistryCore

final class LocalInstallTests: XCTestCase {
    private var home: String!

    override func setUpWithError() throws {
        home = NSTemporaryDirectory() + "localinstall-" + UUID().uuidString
        try FileManager.default.createDirectory(atPath: home, withIntermediateDirectories: true)
    }

    override func tearDownWithError() throws {
        try? FileManager.default.removeItem(atPath: home)
    }

    private func target(_ dot: String) -> AgentTarget {
        AgentTarget(dotDir: dot, display: dot, universal: false, underHome: true)
    }

    func testWritesIntoEveryTarget() throws {
        let files: [String: Data] = [
            "SKILL.md": Data("# hi".utf8),
            "scripts/run.sh": Data("#!/bin/sh\necho hi".utf8),
        ]
        let targets = [target(".claude"), target(".cursor")]
        let written = try LocalInstall.install(slug: "git_helper", files: files,
                                               targets: targets, home: home, cwd: home)
        XCTAssertEqual(written.count, 2)
        for t in targets {
            let base = "\(home!)/\(t.dotDir)/skills/git_helper"
            XCTAssertEqual(try String(contentsOfFile: "\(base)/SKILL.md", encoding: .utf8), "# hi")
            XCTAssertTrue(FileManager.default.fileExists(atPath: "\(base)/scripts/run.sh"))
        }
    }

    func testReinstallOverwritesExisting() throws {
        let t = target(".claude")
        _ = try LocalInstall.install(slug: "demo", files: ["SKILL.md": Data("v1".utf8)],
                                     targets: [t], home: home, cwd: home)
        _ = try LocalInstall.install(slug: "demo", files: ["SKILL.md": Data("v2".utf8)],
                                     targets: [t], home: home, cwd: home)
        let path = "\(home!)/.claude/skills/demo/SKILL.md"
        XCTAssertEqual(try String(contentsOfFile: path, encoding: .utf8), "v2")
    }

    func testSlugIsCanonicalized() throws {
        let written = try LocalInstall.install(slug: "Git Helper", files: ["SKILL.md": Data("x".utf8)],
                                               targets: [target(".claude")], home: home, cwd: home)
        XCTAssertEqual((written[0] as NSString).lastPathComponent, "git_helper")
    }

    func testNoTargetsThrows() {
        XCTAssertThrowsError(try LocalInstall.install(slug: "demo", files: ["SKILL.md": Data()],
                                                      targets: [], home: home, cwd: home)) {
            XCTAssertEqual($0 as? LocalInstall.InstallError, .noTargets)
        }
    }

    func testRejectsTraversalPaths() {
        let bad: [[String: Data]] = [
            ["../escape.md": Data()],
            ["a/../../escape.md": Data()],
            ["/abs.md": Data()],
            ["": Data()],
        ]
        for files in bad {
            XCTAssertThrowsError(try LocalInstall.install(slug: "demo", files: files,
                                                          targets: [target(".claude")], home: home, cwd: home),
                                 "expected rejection for \(files.keys)")
        }
        // Nothing should have been written.
        XCTAssertFalse(FileManager.default.fileExists(atPath: "\(home!)/.claude/skills/demo"))
    }

    func testRejectsSymlinkDest() throws {
        let skillsDir = "\(home!)/.claude/skills"
        try FileManager.default.createDirectory(atPath: skillsDir, withIntermediateDirectories: true)
        // Pre-create the <slug> dir as a symlink pointing elsewhere.
        let realElsewhere = "\(home!)/elsewhere"
        try FileManager.default.createDirectory(atPath: realElsewhere, withIntermediateDirectories: true)
        let symSlug = "\(skillsDir)/demo"
        try FileManager.default.createSymbolicLink(atPath: symSlug, withDestinationPath: realElsewhere)

        XCTAssertThrowsError(try LocalInstall.install(slug: "demo", files: ["SKILL.md": Data("x".utf8)],
                                                      targets: [target(".claude")], home: home, cwd: home)) {
            guard case LocalInstall.InstallError.symlinkRejected = $0 else {
                return XCTFail("expected symlinkRejected, got \($0)")
            }
        }
    }

    func testValidateRelPathCleaning() throws {
        XCTAssertEqual(try LocalInstall.validateRelPath("a/./b.md"), "a/b.md")
        XCTAssertEqual(try LocalInstall.validateRelPath("a/b/../c.md"), "a/c.md")
        XCTAssertEqual(try LocalInstall.validateRelPath("scripts\\run.sh"), "scripts/run.sh")
        XCTAssertThrowsError(try LocalInstall.validateRelPath("../x"))
        XCTAssertThrowsError(try LocalInstall.validateRelPath("/x"))
        XCTAssertThrowsError(try LocalInstall.validateRelPath(""))
    }
}
