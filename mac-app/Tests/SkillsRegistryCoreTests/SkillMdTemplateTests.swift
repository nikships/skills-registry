import XCTest
@testable import SkillsRegistryCore

final class SkillMdTemplateTests: XCTestCase {
    func testCLIOnlyTemplate() {
        let out = SkillMdTemplate.skillMd(registryRepo: "owner/repo")
        XCTAssertTrue(out.hasPrefix("---\nname: skills-registry\n"))
        XCTAssertTrue(out.contains("personal skill library at owner/repo."))
        XCTAssertTrue(out.contains("Skills live at https://github.com/owner/repo."))
        XCTAssertTrue(out.contains("~/.cache/skills-registry/skills/<slug>/"))
        XCTAssertTrue(out.contains("~/.config/skills-registry/registry.toml"))
        XCTAssertFalse(out.contains("search_skills"))
        XCTAssertTrue(out.hasSuffix("after installation.\n"))
    }

    func testRepoInterpolation() {
        let out = SkillMdTemplate.skillMd(registryRepo: "nikships/my-skills")
        XCTAssertTrue(out.contains("library at nikships/my-skills."))
        XCTAssertTrue(out.contains("https://github.com/nikships/my-skills."))
        // The placeholder repo must not leak into the interpolated slots. (The
        // literal `owner/repo` still appears once as docs for `add <source>`.)
        XCTAssertFalse(out.contains("library at owner/repo."))
        XCTAssertFalse(out.contains("https://github.com/owner/repo."))
    }
}
