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

    func testFolderName() {
        XCTAssertEqual(folderName("Git Helper"), "git-helper")
        XCTAssertEqual(folderName("keep-agent-mem"), "keep-agent-mem")
        XCTAssertEqual(folderName("keep_agent_mem"), "keep-agent-mem")
        XCTAssertEqual(folderName("agp-9-upgrade"), "agp-9-upgrade")
        XCTAssertEqual(folderName("***"), "skill")
        XCTAssertEqual(folderName(""), "skill")
        // Stable whether handed the raw name or its underscore slug.
        for name in ["keep-agent-mem", "Git Helper", "AGP-9 Upgrade"] {
            XCTAssertEqual(folderName(name), folderName(slugify(name)))
        }
    }

    func testNormalizeForMatch() {
        XCTAssertEqual(normalizeForMatch("simplify-swarm"), "simplifyswarm")
        XCTAssertEqual(normalizeForMatch("simplify_swarm"), "simplifyswarm")
        XCTAssertEqual(normalizeForMatch("Simplify Swarm"), "simplifyswarm")
        XCTAssertEqual(normalizeForMatch("SIMPLIFYSWARM"), "simplifyswarm")
        XCTAssertEqual(normalizeForMatch("AGP-9 Upgrade"), "agp9upgrade")
        XCTAssertEqual(normalizeForMatch("  trim  me  "), "trimme")
        XCTAssertEqual(normalizeForMatch("already-normal9"), "alreadynormal9")
        XCTAssertEqual(normalizeForMatch("simplify-swarm"), normalizeForMatch("simplify_swarm"))
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

    /// The provenance keys an untrusted import stamps on are unknown to this
    /// parser, and must stay that way: name and description keep coming from
    /// the same two keys.
    func testUnknownProvenanceKeysDoNotBreakParseSummary() {
        let md = """
        ---
        name: summarize
        description: Summarize URLs and PDFs.
        category: AIGC
        source_url: https://github.com/openclaw/openclaw/blob/abc123/skills/summarize
        ---
        Body text.
        """
        let (name, desc) = Frontmatter.parseSummary(md, slug: "summarize_slug")
        XCTAssertEqual(name, "summarize")
        XCTAssertEqual(desc, "Summarize URLs and PDFs.")
    }

    /// Round-trip: merge the two keys in, then parse the result back out. The
    /// stamped values survive and the upstream keys are untouched.
    func testMergingRoundTripsBothProvenanceKeys() throws {
        let md = """
        ---
        name: summarize
        description: Summarize URLs and PDFs.
        ---
        Body text.
        """
        let url = "https://github.com/openclaw/openclaw/tree/abc123/skills/summarize"
        let merged = try XCTUnwrap(Frontmatter.merging(md, keys: [
            Frontmatter.Key(name: Frontmatter.categoryKey, value: "AIGC"),
            Frontmatter.Key(name: Frontmatter.sourceURLKey, value: url),
        ]))
        let lines = merged.components(separatedBy: "\n")
        let end = try XCTUnwrap(lines.dropFirst().firstIndex(where: {
            $0.trimmingCharacters(in: .whitespaces) == "---"
        }))
        let meta = Frontmatter.parseFlatYAML(Array(lines[1..<end]))
        XCTAssertEqual(meta[Frontmatter.categoryKey], "AIGC")
        XCTAssertEqual(meta[Frontmatter.sourceURLKey], url)
        XCTAssertEqual(meta["name"], "summarize")
        XCTAssertEqual(meta["description"], "Summarize URLs and PDFs.")
        // The body is the upstream skill, unmodified.
        XCTAssertEqual(Frontmatter.body(merged), "Body text.")
        // The summary still reads name and description, not the new keys.
        let (name, desc) = Frontmatter.parseSummary(merged, slug: "x")
        XCTAssertEqual(name, "summarize")
        XCTAssertEqual(desc, "Summarize URLs and PDFs.")
    }

    /// Upstream key order and formatting survive; the new keys are appended
    /// just before the closing fence.
    func testMergingPreservesUpstreamLines() throws {
        let md = "---\nname: summarize\ndescription: Summarize.\n---\n# Body\n"
        let merged = try XCTUnwrap(Frontmatter.merging(md, keys: [
            Frontmatter.Key(name: Frontmatter.categoryKey, value: "AIGC"),
        ]))
        XCTAssertEqual(merged, "---\nname: summarize\ndescription: Summarize.\ncategory: AIGC\n---\n# Body\n")
    }

    func testMergingKeepsAnExistingValue() {
        let md = "---\nname: x\ncategory: Upstream Choice\n---\nBody\n"
        XCTAssertNil(Frontmatter.merging(md, keys: [
            Frontmatter.Key(name: Frontmatter.categoryKey, value: "AIGC"),
        ]))
    }

    func testMergingFillsAnEmptyValue() throws {
        for md in ["---\nname: x\ncategory:\n---\nBody\n", "---\nname: x\ncategory: \"\"\n---\nBody\n"] {
            let merged = try XCTUnwrap(Frontmatter.merging(md, keys: [
                Frontmatter.Key(name: Frontmatter.categoryKey, value: "AIGC"),
            ]))
            XCTAssertTrue(merged.contains("category: AIGC"), merged)
            XCTAssertEqual(merged.components(separatedBy: "category:").count - 1, 1, merged)
        }
    }

    /// An indented `category:` is a block scalar's text, not a top-level key,
    /// so the real key is still added and the text is left alone.
    func testMergingIgnoresIndentedKeys() throws {
        let md = "---\nname: x\ndescription: |\n  category: not a key\n---\nBody\n"
        let merged = try XCTUnwrap(Frontmatter.merging(md, keys: [
            Frontmatter.Key(name: Frontmatter.categoryKey, value: "AIGC"),
        ]))
        XCTAssertTrue(merged.contains("  category: not a key"), merged)
        XCTAssertTrue(merged.contains("\ncategory: AIGC\n"), merged)
    }

    func testMergingAddsABlockWhenFrontmatterIsAbsent() throws {
        let merged = try XCTUnwrap(Frontmatter.merging("# Just a body\n", keys: [
            Frontmatter.Key(name: Frontmatter.sourceURLKey, value: "https://example.test/x"),
        ]))
        XCTAssertEqual(merged, "---\nsource_url: https://example.test/x\n---\n# Just a body\n")
    }

    /// An unterminated block has no known end, so guessing where to insert
    /// would risk rewriting the body.
    func testMergingLeavesUnterminatedBlockAlone() {
        XCTAssertNil(Frontmatter.merging("---\nname: x\nno closing fence\n", keys: [
            Frontmatter.Key(name: Frontmatter.categoryKey, value: "AIGC"),
        ]))
        XCTAssertNil(Frontmatter.merging("---\nname: x\n---\nBody\n", keys: []))
    }

    /// Matches Go `yamlScalar`: a URL stays plain, and a value that would break
    /// the document (or smuggle a second key into it) is quoted and escaped.
    func testYAMLScalarQuotesOnlyWhenNeeded() {
        for v in ["https://github.com/o/r/tree/abc/skills/x", "AIGC", "Developer Tools", "a:b"] {
            XCTAssertEqual(Frontmatter.yamlScalar(v), v)
        }
        for v in ["", "AIGC\nname: hijacked", "has \"quotes\"", "trailing: ", "  leading", "# comment", "- listish"] {
            let got = Frontmatter.yamlScalar(v)
            XCTAssertTrue(got.hasPrefix("\""), "yamlScalar(\(v)) = \(got), want it quoted")
            XCTAssertFalse(got.dropFirst().dropLast().contains("\n"),
                           "yamlScalar(\(v)) = \(got), a newline must not survive unescaped")
        }
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
