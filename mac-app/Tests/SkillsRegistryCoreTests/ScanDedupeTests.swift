import XCTest
@testable import SkillsRegistryCore

final class ScanDedupeTests: XCTestCase {
    private func local(_ slug: String) -> LocalSkill {
        LocalSkill(slug: slug, name: slug, description: "", folder: "/tmp/\(slug)", source: "test")
    }

    func testExactMatchesAreFiltered() {
        let locals = [local("alpha"), local("beta"), local("gamma")]
        let got = Scan.dedupeAgainst(locals, remoteSlugs: ["beta"])
        XCTAssertEqual(got.map(\.slug), ["alpha", "gamma"])
    }

    /// Mirrors Go TestDedupeAgainstNormalizesSeparatorsAndCase: a local
    /// "simplify_swarm" must dedupe against registry "simplify-swarm".
    /// Before normalization the literal lookup missed and import offered to
    /// re-push an already-published skill as a second folder.
    func testSeparatorAndCaseVariantsAreFiltered() {
        let locals = [
            local("simplify_swarm"), // on disk under ~/.claude/skills/simplify_swarm
            local("code_review"),    // genuinely absent upstream
            local("other_skill"),    // registry stores it hyphenated + capitalized
        ]
        // Registry folder names as returned by the tree listing — note the
        // hyphen and the differing case, neither of which defeats the match.
        let got = Scan.dedupeAgainst(locals, remoteSlugs: ["simplify-swarm", "Other-Skill"])
        XCTAssertEqual(got.map(\.slug), ["code_review"])
    }

    func testEmptyRegistryKeepsEverythingInOrder() {
        let locals = [local("beta"), local("alpha")]
        let got = Scan.dedupeAgainst(locals, remoteSlugs: [])
        XCTAssertEqual(got.map(\.slug), ["beta", "alpha"])
    }

    func testEmptyLocalsStayEmpty() {
        XCTAssertTrue(Scan.dedupeAgainst([], remoteSlugs: ["alpha"]).isEmpty)
    }
}
