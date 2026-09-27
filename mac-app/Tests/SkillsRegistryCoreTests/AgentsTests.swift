import XCTest
@testable import SkillsRegistryCore

final class AgentsTests: XCTestCase {
    private var targets: [AgentTarget]!

    override func setUp() {
        targets = [
            AgentTarget(dotDir: ".agents", display: "Universal (.agents/skills)", universal: true, underHome: false),
            AgentTarget(dotDir: ".claude", display: "Claude Code"),
            AgentTarget(dotDir: ".claude-code", display: "Claude Code (legacy)"),
            AgentTarget(dotDir: ".cursor", display: "Cursor"),
        ]
    }

    func testBlankQueryMatchesEverything() {
        XCTAssertEqual(Agents.matching(targets, query: "").count, 4)
        XCTAssertEqual(Agents.matching(targets, query: "   ").count, 4)
    }

    func testMatchesDisplayNameCaseInsensitively() {
        let got = Agents.matching(targets, query: "claude")
        XCTAssertEqual(got.map(\.dotDir), [".claude", ".claude-code"])
    }

    func testMatchesDotDir() {
        let got = Agents.matching(targets, query: ".cursor")
        XCTAssertEqual(got.map(\.dotDir), [".cursor"])
        // Leading-dot-optional: "cursor" also hits the display name.
        XCTAssertEqual(Agents.matching(targets, query: "cursor").map(\.dotDir), [".cursor"])
    }

    func testNoMatchYieldsEmpty() {
        XCTAssertTrue(Agents.matching(targets, query: "zencoder").isEmpty)
    }

    func testSurroundingWhitespaceIsIgnored() {
        let got = Agents.matching(targets, query: "  cursor ")
        XCTAssertEqual(got.map(\.dotDir), [".cursor"])
    }
}
