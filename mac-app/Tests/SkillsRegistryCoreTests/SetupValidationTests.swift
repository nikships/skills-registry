import XCTest
@testable import SkillsRegistryCore

final class SetupValidationTests: XCTestCase {
    func testRepoNameAcceptsValidNames() {
        for name in ["skills-registry", "a", "my.repo_name-1", "UPPER.lower_9-"] {
            XCTAssertNil(SetupValidation.repoNameHint(name), "expected valid: \(name)")
        }
    }

    func testRepoNameEmptyHasNoHint() {
        XCTAssertNil(SetupValidation.repoNameHint(""))
        XCTAssertNil(SetupValidation.repoNameHint("   "))
    }

    func testRepoNameRejectsBadCharacters() {
        for name in ["bad name", "bad/name", "bad!name", "bad@name", "semi;colon"] {
            XCTAssertNotNil(SetupValidation.repoNameHint(name), "expected hint: \(name)")
        }
    }

    func testRepoNameRejectsLeadingTrailingDots() {
        XCTAssertNotNil(SetupValidation.repoNameHint(".leading"))
        XCTAssertNotNil(SetupValidation.repoNameHint("trailing."))
        XCTAssertNil(SetupValidation.repoNameHint("mid.dle"))
    }

    func testRepoNameRejectsOverlongNames() {
        XCTAssertNotNil(SetupValidation.repoNameHint(String(repeating: "a", count: 101)))
        XCTAssertNil(SetupValidation.repoNameHint(String(repeating: "a", count: 100)))
    }

    func testRepoRefAcceptsOwnerSlashName() {
        XCTAssertNil(SetupValidation.repoRefHint("octocat/skills-registry"))
        XCTAssertNil(SetupValidation.repoRefHint("  octocat/skills-registry  "))
    }

    func testRepoRefEmptyHasNoHint() {
        XCTAssertNil(SetupValidation.repoRefHint(""))
    }

    func testRepoRefRejectsMalformedRefs() {
        for ref in ["not a repo", "noslash", "a/b/c", "/name", "owner/", "owner/ "] {
            XCTAssertNotNil(SetupValidation.repoRefHint(ref), "expected hint: \(ref)")
        }
    }
}
