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
        XCTAssertTrue(out.hasSuffix("after installation.\n"))
    }

    /// The hosted MCP service was removed from this repo. A gateway that still
    /// advertised it would point agents at an endpoint that does not exist.
    func testTemplateIsCLIOnly() {
        let out = SkillMdTemplate.skillMd(registryRepo: "owner/repo")
        for banned in ["mcp.skills-registry.dev", "mcpServers", "search_skills", "get_skill", "MCP"] {
            XCTAssertFalse(out.contains(banned), "the gateway must stay CLI-only but mentions \(banned)")
        }
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

    /// The personal registry is searched first; the public index is offered
    /// only after that misses. Reversing the two would send every user prompt
    /// to a third-party index.
    func testSearchesLocallyBeforeTheIndex() throws {
        let out = SkillMdTemplate.skillMd(registryRepo: "owner/repo")
        let local = try XCTUnwrap(out.range(of: "## 1. Search this registry first"))
        let publicIndex = try XCTUnwrap(out.range(of: "## 2. On a local miss, offer the public index"))
        XCTAssertTrue(local.lowerBound < publicIndex.lowerBound)
        XCTAssertTrue(out.contains("Only after `search` comes up empty"))
        XCTAssertTrue(out.contains("never let a registry lookup stall the work"))
    }

    /// The safety half of the discover step: an agent may not import a
    /// stranger's skill, durably install it, or clear a safety block on its
    /// own initiative.
    func testRequiresConfirmationBeforeAdding() {
        let out = SkillMdTemplate.skillMd(registryRepo: "owner/repo")
        for line in [
            "skills-registry discover <query>",
            "skills-registry add <skill_url>",
            "**ask the user first**",
            "Never run `add` on a URL the user has not approved.",
            "Do **not** pass `--install`",
            "`--allow-unsafe`. Never add that flag on your own",
            "`unscored`",
            "Nothing fetched is ever executed.",
            "`skills-registry discover <query> --json`",
        ] {
            XCTAssertTrue(out.contains(line), "the gateway is missing \(line)")
        }
    }

    /// The gateway is loaded into every agent session, so its size is a
    /// product constraint rather than an implementation detail. The Go side's
    /// `TestSkillMdMatchesSwiftTemplate` pins the exact byte-for-byte match
    /// between the two languages; this ceiling catches unbounded growth on
    /// either side of that contract.
    func testTemplateStaysTerse() {
        let out = SkillMdTemplate.skillMd(registryRepo: "owner/repo")
        XCTAssertLessThan(out.utf8.count, 8_000,
                          "the always-loaded gateway grew to \(out.utf8.count) bytes; move detail into references/")
    }
}
