import XCTest
@testable import SkillsRegistryCore

final class SlugTests: XCTestCase {
    func testBasic() {
        XCTAssertEqual(slugify("Git Helper"), "git_helper")
        XCTAssertEqual(slugify("  Hello, World!  "), "hello_world")
        XCTAssertEqual(slugify("agp-9-upgrade"), "agp_9_upgrade")
        XCTAssertEqual(slugify("UPPER_case"), "upper_case")
        XCTAssertEqual(slugify("***"), "skill")
        XCTAssertEqual(slugify(""), "skill")
        XCTAssertEqual(slugify("a.b.c"), "a_b_c")
    }
}

final class FuzzyScoreTests: XCTestCase {
    func testOrderMatters() {
        XCTAssertGreaterThan(fuzzyScore("git", "git_tool"), 0)
        XCTAssertEqual(fuzzyScore("xyz", "git_tool"), 0)
    }

    func testWordBoundaryBeatsBuried() {
        // A query that starts on a word boundary outranks the same query
        // buried mid-word — the fzf V1 boundary bonus dominates.
        XCTAssertGreaterThan(fuzzyScore("git", "git tools"),
                             fuzzyScore("git", "legitimate"))
    }

    // Mirrors TestScoreAndSortCrossLanguageCorpus (Go) and
    // test_search_skills_cross_language_corpus (Python). Same summaries,
    // same queries, same expected ordering. Divergence here means the three
    // scorers drifted.
    func testCrossLanguageCorpus() {
        let summaries = [
            SkillSummary(slug: "alpha_git", name: "Alpha Git", description: "Git helpers"),
            SkillSummary(slug: "beta_python", name: "Beta Python", description: "Python tooling"),
            SkillSummary(slug: "gamma_js", name: "Gamma JS", description: "JavaScript tooling"),
        ]
        XCTAssertEqual(scoreAndSort(summaries, query: "git").map(\.slug), ["alpha_git"])
        XCTAssertEqual(scoreAndSort(summaries, query: "tool").map(\.slug), ["beta_python", "gamma_js"])
    }

    func testRanksByScoreAndSlug() {
        let summaries = [
            SkillSummary(slug: "git_tool", name: "Git Helper", description: "Git helper commands"),
            SkillSummary(slug: "js_lint", name: "JS Linter", description: "Ruff for JS"),
            SkillSummary(slug: "py_format", name: "Python Formatter", description: "Beautiful python formatting"),
        ]
        let got = scoreAndSort(summaries, query: "git")
        XCTAssertEqual(got.count, 1)
        XCTAssertEqual(got.first?.slug, "git_tool")
    }

    func testEmptyQueryReturnsEmpty() {
        let summaries = [SkillSummary(slug: "a", name: "A", description: "x")]
        XCTAssertTrue(scoreAndSort(summaries, query: "").isEmpty)
        XCTAssertTrue(scoreAndSort(summaries, query: "   ").isEmpty)
    }
}

final class FrontmatterTests: XCTestCase {
    func testFlatKeyValue() {
        let md = """
        ---
        name: My Skill
        description: A short description here.
        ---
        # Heading

        Body text.
        """
        let (name, desc) = Frontmatter.parseSummary(md, slug: "my_skill")
        XCTAssertEqual(name, "My Skill")
        XCTAssertEqual(desc, "A short description here.")
    }

    func testFoldedBlockScalar() {
        let md = """
        ---
        name: Folded
        description: |
          Broker to your library. Use when
          the user asks for a skill.
        ---
        Body
        """
        let (name, desc) = Frontmatter.parseSummary(md, slug: "folded")
        XCTAssertEqual(name, "Folded")
        XCTAssertTrue(desc.contains("Broker to your library"))
        XCTAssertTrue(desc.contains("Use when the user asks"))
    }

    func testNoFrontmatterFallsBackToFirstParagraph() {
        let md = """
        # Title

        First real paragraph wins.

        Second.
        """
        let (name, desc) = Frontmatter.parseSummary(md, slug: "no_fm")
        XCTAssertEqual(name, "no_fm")
        XCTAssertEqual(desc, "First real paragraph wins.")
    }

    func testBodyStripsFrontmatter() {
        let md = "---\nname: X\n---\n# Heading\n\ncontent"
        XCTAssertEqual(Frontmatter.body(md), "# Heading\n\ncontent")
    }
}

final class RegistryConfigTests: XCTestCase {
    func testParseAndValidate() throws {
        let cfg = try RegistryConfig.parseTOML("""
        # comment
        [registry]
        repo = "octocat/skills"
        default_branch = "main"
        """)
        XCTAssertEqual(cfg.repo, "octocat/skills")
        XCTAssertEqual(cfg.defaultBranch, "main")
        XCTAssertEqual(cfg.ref?.owner, "octocat")
        XCTAssertEqual(cfg.ref?.name, "skills")
    }

    func testParseEnvValue() {
        let (repo, branch) = RegistryConfig.parseEnvValue("a/b@dev")
        XCTAssertEqual(repo, "a/b")
        XCTAssertEqual(branch, "dev")
        let (repo2, branch2) = RegistryConfig.parseEnvValue("a/b")
        XCTAssertEqual(repo2, "a/b")
        XCTAssertEqual(branch2, "main")
    }

    func testValidateRejectsBad() {
        XCTAssertThrowsError(try RegistryConfig.validate("nope"))
        XCTAssertThrowsError(try RegistryConfig.validate("/x"))
        XCTAssertThrowsError(try RegistryConfig.validate(""))
        XCTAssertNoThrow(try RegistryConfig.validate("a/b"))
    }

    func testRoundTrip() throws {
        // Write to a temp XDG_CONFIG_HOME and read back.
        let tmp = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        try FileManager.default.createDirectory(at: tmp, withIntermediateDirectories: true)
        defer { try? FileManager.default.removeItem(at: tmp) }
        setenv("XDG_CONFIG_HOME", tmp.path, 1)
        defer { unsetenv("XDG_CONFIG_HOME") }

        let cfg = RegistryConfig(repo: "me/reg", defaultBranch: "main")
        let url = try cfg.save()
        XCTAssertTrue(FileManager.default.fileExists(atPath: url.path))
        let loaded = try RegistryConfig.load()
        XCTAssertEqual(loaded, cfg)
    }
}

final class MCPSnippetTests: XCTestCase {
    func testSnippetShape() {
        let s = AppConfig.mcpJSONSnippet
        XCTAssertTrue(s.contains("mcpServers"))
        XCTAssertTrue(s.contains("https://mcp.skills-registry.dev/mcp"))
    }
}
