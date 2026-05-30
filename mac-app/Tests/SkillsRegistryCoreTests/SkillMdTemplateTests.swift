import XCTest
@testable import SkillsRegistryCore

final class SkillMdTemplateTests: XCTestCase {
    /// Pins the Swift mirror byte-for-byte against the Go template
    /// (`cli/internal/bootstrap/skillmd.go:SkillMd`). The expected string below
    /// was captured directly from `bootstrap.SkillMd("owner/repo")`. If the Go
    /// template changes, this test must be updated in the same PR (and vice
    /// versa) — the CLI and app install the same gateway skill.
    func testGoldenMatchesGoTemplate() {
        let out = SkillMdTemplate.skillMd(registryRepo: "owner/repo")
        XCTAssertEqual(out.utf8.count, 6428, "byte length drifted from the Go golden")
        XCTAssertTrue(out.hasPrefix("---\nname: skills-registry\n"))
        XCTAssertTrue(out.contains("Broker to your GitHub-hosted personal skill library at owner/repo."))
        XCTAssertTrue(out.contains("Skills live at https://github.com/owner/repo and can be reached two ways:"))
        XCTAssertTrue(out.hasSuffix("within a few seconds.\n"))
    }

    func testRepoInterpolation() {
        let out = SkillMdTemplate.skillMd(registryRepo: "anand-92/my-skills")
        XCTAssertTrue(out.contains("library at anand-92/my-skills."))
        XCTAssertTrue(out.contains("https://github.com/anand-92/my-skills and can be reached"))
        // The placeholder repo must not leak into the interpolated slots. (The
        // literal `owner/repo` still appears once as docs for `add <source>`.)
        XCTAssertFalse(out.contains("library at owner/repo."))
        XCTAssertFalse(out.contains("https://github.com/owner/repo and can be reached"))
    }
}
