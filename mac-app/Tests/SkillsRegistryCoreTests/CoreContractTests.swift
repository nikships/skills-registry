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

    // Mirrors TestScoreAndSortCrossLanguageCorpus (Go). Same case names,
    // inputs, and expected scores. Both scorers normalize to NFC before
    // matching, so a precomposed accent and a combining mark score the same.
    func testCrossLanguageCorpus() {
        // Exact scores pin each bonus and the gap penalty. Changing a
        // constant without updating both suites fails here.
        let scoreCases: [(name: String, query: String, text: String, want: Int)] = [
            ("boundary-word-start", "git", "git tools", 69),
            ("buried-midword", "git", "legitimate", 61),
            ("camel-bonus", "ab", "aB", 53),
            ("camel-absent", "ab", "ab", 47),
            ("consecutive-run", "bc", "abc", 39),
            ("consecutive-broken", "bc", "abxc", 32),
            ("exact-case", "Git", "Git Tools", 69),
            ("folded-case", "Git", "git tools", 68),
            ("gap-one", "git", "gXit", 62),
            ("gap-two", "git", "gXXit", 60),
            // Thirty gaps between a and b drive the penalty below zero,
            // which both scorers clamp to a non-match.
            ("gap-floor", "ab", "a" + String(repeating: "x", count: 30) + "b", 0),
        ]
        for tc in scoreCases {
            XCTAssertEqual(
                fuzzyScore(tc.query, tc.text), tc.want,
                "\(tc.name): fuzzyScore(\(tc.query), \(tc.text))"
            )
        }

        // U+00E9 is the precomposed é. U+0301 is the combining acute.
        let nfdCafe = "cafe\u{0301}"
        let nfcCafe = "caf\u{00E9}"
        let nfcText = "Caf\u{00E9} Tools"
        let nfdText = "Cafe\u{0301} Tools"
        for pair in [(nfdCafe, nfcText), (nfcCafe, nfcText), (nfcCafe, nfdText), (nfdCafe, nfdText)] {
            XCTAssertEqual(
                fuzzyScore(pair.0, pair.1), 90,
                "nfc-equals-nfd: fuzzyScore(\(pair.0), \(pair.1))"
            )
        }
        let cafeSummaries = [
            SkillSummary(slug: "cafe", name: "Caf\u{00E9} Helper", description: "drinks"),
            SkillSummary(slug: "other", name: "Other", description: "unrelated"),
        ]
        for q in [nfdCafe, nfcCafe] {
            XCTAssertEqual(
                scoreAndSort(cafeSummaries, query: q).map(\.slug), ["cafe"],
                "nfc-equals-nfd query \(q)"
            )
        }

        // Input order is the reverse of the expected rank, so a scorer
        // that forgets field weights cannot pass by preserving input order.
        let weighted = [
            SkillSummary(slug: "desc_hit", name: "unrelated", description: "git"),
            SkillSummary(slug: "name_hit", name: "git", description: "unrelated"),
        ]
        XCTAssertEqual(
            scoreAndSort(weighted, query: "git").map(\.slug),
            ["name_hit", "desc_hit"],
            "name-outranks-description"
        )

        let ties = [
            SkillSummary(slug: "zeta", name: "Tool", description: "x"),
            SkillSummary(slug: "alpha", name: "Tool", description: "x"),
        ]
        XCTAssertEqual(
            scoreAndSort(ties, query: "tool").map(\.slug),
            ["alpha", "zeta"],
            "slug-tiebreak"
        )

        // Inserted high slug first. Equal scores sort by slug, then the
        // eleventh result (s11) is dropped.
        var many: [SkillSummary] = []
        for i in stride(from: 11, through: 1, by: -1) {
            many.append(SkillSummary(slug: String(format: "s%02d", i), name: "Match", description: "x"))
        }
        XCTAssertEqual(
            scoreAndSort(many, query: "match").map(\.slug),
            ["s01", "s02", "s03", "s04", "s05", "s06", "s07", "s08", "s09", "s10"],
            "top-10-cutoff"
        )

        let one = [SkillSummary(slug: "alpha", name: "Alpha", description: "x")]
        for q in ["", "   ", " \t\n"] {
            XCTAssertTrue(scoreAndSort(one, query: q).isEmpty, "empty-query \(q.debugDescription)")
        }

        let summaries = [
            SkillSummary(slug: "alpha_git", name: "Alpha Git", description: "Git helpers"),
            SkillSummary(slug: "beta_python", name: "Beta Python", description: "Python tooling"),
            SkillSummary(slug: "gamma_js", name: "Gamma JS", description: "JavaScript tooling"),
        ]
        XCTAssertEqual(scoreAndSort(summaries, query: "git").map(\.slug), ["alpha_git"], "sample-registry git")
        XCTAssertEqual(
            scoreAndSort(summaries, query: "tool").map(\.slug),
            ["beta_python", "gamma_js"],
            "sample-registry tool"
        )
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

    /// The default limit keeps the cross-language top-N contract pinned: with
    /// more matches than searchTopN, a default call still returns exactly N.
    func testDefaultLimitStaysPinnedAtTen() {
        let summaries = Self.zzztestCorpus(count: 15)
        let got = scoreAndSort(summaries, query: "zzztest")
        XCTAssertEqual(got.count, FuzzyConst.searchTopN)
        XCTAssertEqual(FuzzyConst.searchTopN, 10)
    }

    /// An explicit limit ranks past the default cap without reordering: every
    /// match is returned, still score-descending with slug-ascending ties.
    func testExplicitLimitRanksAllMatches() {
        let summaries = Self.zzztestCorpus(count: 15)
        let got = scoreAndSort(summaries, query: "zzztest", limit: summaries.count)
        XCTAssertEqual(got.count, 15)
        // Same head as the default call, then the previously truncated tail.
        let head = scoreAndSort(summaries, query: "zzztest").map(\.slug)
        XCTAssertEqual(got.map(\.slug).prefix(10), head[...])
        // Ordering contract holds across the full ranking.
        let scores = got.map { scoreSkill("zzztest", $0) }
        for i in got.indices.dropFirst() {
            let prev = got[i - 1], cur = got[i]
            XCTAssertTrue(
                scores[i - 1] > scores[i]
                    || (scores[i - 1] == scores[i] && prev.slug < cur.slug),
                "out of order: \(prev.slug) before \(cur.slug)")
        }
    }

    func testExplicitSmallLimit() {
        let summaries = Self.zzztestCorpus(count: 15)
        let got = scoreAndSort(summaries, query: "zzztest", limit: 3)
        XCTAssertEqual(got.count, 3)
        XCTAssertEqual(got.map(\.slug),
                       Array(scoreAndSort(summaries, query: "zzztest").map(\.slug).prefix(3)))
    }

    /// Synthetic skills that all match "zzztest" (mirrors the demo fixture).
    private static func zzztestCorpus(count: Int) -> [SkillSummary] {
        (1...count).map { k in
            let num = String(format: "%02d", k)
            return SkillSummary(slug: "zzztest_skill_\(num)", name: "Zzztest Skill \(num)",
                                description: "Synthetic demo skill \(num) matching zzztest queries.")
        }
    }
}

final class FrontmatterTests: XCTestCase {
    /// Cross-language flat-parser corpus: the same inputs and expectations
    /// the Go suite asserts verbatim in `corpus`
    /// (`cli/internal/frontmatter/frontmatter_test.go`). Divergence here
    /// means a skill lists differently in the app than in the CLI, so change
    /// both tables together.
    func testFlatParserCorpus() {
        let cases: [(name: String, lines: [String], want: [String: String])] = [
            ("double-quoted", ["name: \"Quoted Name\""], ["name": "Quoted Name"]),
            ("single-quoted", ["description: 'single quoted'"], ["description": "single quoted"]),
            ("nested-quotes-strip-fully", ["name: \"''deep''\""], ["name": "deep"]),
            ("mismatched-quotes-strip-fully", ["name: \"'mixed'\""], ["name": "mixed"]),
            ("unbalanced-quote-strips", ["name: \"abc"], ["name": "abc"]),
            ("quote-escapes-not-interpreted", ["name: 'it''s'"], ["name": "it''s"]),
            ("numeric-stays-verbatim", ["name: 123", "description: 4.5"], ["name": "123", "description": "4.5"]),
            ("bool-stays-verbatim", ["description: true"], ["description": "true"]),
            ("flow-list-stays-verbatim", ["description: [a, b]"], ["description": "[a, b]"]),
            ("flow-map-stays-verbatim", ["description: {a: b}"], ["description": "{a: b}"]),
            ("folded-block-scalar", ["description: >", "  line one", "  line two"],
             ["description": "line one line two"]),
            ("literal-block-scalar", ["description: |", "  line one", "  line two"],
             ["description": "line one\nline two"]),
            ("plain-multiline-continuation", ["description: line one", "  line two"],
             ["description": "line one line two"]),
            ("duplicate-keys-last-wins", ["name: first", "name: second"], ["name": "second"]),
            ("comments-and-blanks-skipped",
             ["# leading comment", "", "name: kept", "  # indented comment"], ["name": "kept"]),
            ("nested-mapping-read-flat", ["metadata:", "  foo: bar"], ["metadata": "", "foo": "bar"]),
            ("empty-value", ["name:"], ["name": ""]),
            ("colon-in-value", ["description: a: b"], ["description": "a: b"]),
            ("comment-after-block-marker", ["description: > # multi-line", "  folded here"],
             ["description": "folded here"]),
        ]
        for c in cases {
            XCTAssertEqual(Frontmatter.parseFlatYAML(c.lines), c.want, c.name)
        }
    }

    /// The parse path strips every surrounding quote character (Go
    /// `strings.Trim`), while the merge emptiness check strips one matched
    /// pair (Go `frontmatterValue`): `category: '"'` is empty when read but
    /// keeps its value when stamped over.
    func testParseStripsAllQuotesWhileMergeStripsOnePair() {
        let md = "---\nname: x\ncategory: '\"'\n---\nBody\n"
        let lines = md.components(separatedBy: "\n")
        let end = lines.dropFirst().firstIndex(where: {
            $0.trimmingCharacters(in: .whitespaces) == "---"
        })!
        XCTAssertEqual(Frontmatter.parseFlatYAML(Array(lines[1..<end]))["category"], "")
        XCTAssertNil(Frontmatter.merging(md, keys: [
            Frontmatter.Key(name: Frontmatter.categoryKey, value: "AIGC"),
        ]))
    }

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

    func testHasUnclosedFence() {
        XCTAssertTrue(Frontmatter.hasUnclosedFence("---\nname: x\nno closing fence\n"))
        XCTAssertTrue(Frontmatter.hasUnclosedFence("---\nname: x\ndescription: y"))
        XCTAssertFalse(Frontmatter.hasUnclosedFence("---\nname: x\n---\nBody\n"))
        XCTAssertFalse(Frontmatter.hasUnclosedFence("# Just a body\n"))
        XCTAssertFalse(Frontmatter.hasUnclosedFence(""))
    }

    func testDisplayBodyStripsMatchingH1() {
        // Frontmatter gone and the repeated H1 gone; the rest untouched.
        let md = "---\nname: React Code Review\n---\n# React Code Review\n\nBody text.\n"
        XCTAssertEqual(Frontmatter.displayBody(md, name: "React Code Review"), "Body text.\n")
        // Case-insensitive, and tolerates an ATX closing sequence.
        XCTAssertEqual(Frontmatter.displayBody("# react code review\n\nBody\n", name: "React Code Review"), "Body\n")
        XCTAssertEqual(Frontmatter.displayBody("# React Code Review #\n\nBody\n", name: "React Code Review"), "Body\n")
    }

    func testDisplayBodyKeepsNonMatchingHeadings() {
        // A different H1, an H2, and a glued `#` are all real content.
        XCTAssertEqual(Frontmatter.displayBody("# Other Title\n\nBody\n", name: "React Code Review"), "# Other Title\n\nBody\n")
        XCTAssertEqual(Frontmatter.displayBody("## React Code Review\n\nBody\n", name: "React Code Review"), "## React Code Review\n\nBody\n")
        XCTAssertEqual(Frontmatter.displayBody("#React Code Review\n\nBody\n", name: "React Code Review"), "#React Code Review\n\nBody\n")
        // No heading at all: unchanged.
        XCTAssertEqual(Frontmatter.displayBody("Just body.\n", name: "Plain Notes"), "Just body.\n")
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

    /// CRLF line endings (a Windows-edited SKILL.md) parse identically to LF:
    /// the closing fence still matches, values lose their trailing `\r`, the
    /// body strips, and provenance keys merge and round-trip. Mirrors the Go
    /// CRLF cases in `registry_test.go` / `provenance_test.go`.
    func testCRLFFrontmatterParsesLikeLF() throws {
        let md = "---\r\nname: My Skill\r\ndescription: A short description here.\r\n---\r\n# Heading\r\n\r\nBody text.\r\n"
        let (name, desc) = Frontmatter.parseSummary(md, slug: "my_skill")
        XCTAssertEqual(name, "My Skill")
        XCTAssertEqual(desc, "A short description here.")
        XCTAssertEqual(Frontmatter.body(md), "# Heading\r\n\r\nBody text.\r\n")

        let merged = try XCTUnwrap(Frontmatter.merging(md, keys: [
            Frontmatter.Key(name: Frontmatter.categoryKey, value: "AIGC"),
        ]))
        XCTAssertTrue(merged.contains("category: AIGC\n---\r\n"), merged)
        let lines = merged.components(separatedBy: "\n")
        let end = try XCTUnwrap(lines.dropFirst().firstIndex(where: {
            $0.trimmingCharacters(in: .whitespacesAndNewlines) == "---"
        }))
        let meta = Frontmatter.parseFlatYAML(Array(lines[1..<end]))
        XCTAssertEqual(meta[Frontmatter.categoryKey], "AIGC")
        XCTAssertEqual(meta["name"], "My Skill")
        XCTAssertEqual(meta["description"], "A short description here.")
        let (rname, rdesc) = Frontmatter.parseSummary(merged, slug: "x")
        XCTAssertEqual(rname, "My Skill")
        XCTAssertEqual(rdesc, "A short description here.")
    }

    /// Mirrors Go `TestParseSummary_TruncatesByRunesNotBytes`: the 300-wide cap
    /// counts Unicode scalars (Go runes), so both sides clip a multibyte
    /// rune on the boundary at the same scalar, byte for byte.
    func testParseSummaryTruncatesOnAScalarBoundary() {
        let desc = String(repeating: "a", count: 299) + "🇺🇸" + String(repeating: "b", count: 50)
        let md = "---\nname: x\ndescription: \(desc)\n---\nBody\n"
        let (_, got) = Frontmatter.parseSummary(md, slug: "x")
        XCTAssertEqual(got.unicodeScalars.count, 300)
        // 299 "a" scalars plus the flag's first regional indicator: the same
        // bytes Go's rune clip produces.
        let firstRegional = String(Character("🇺🇸".unicodeScalars.first!))
        let want = String(repeating: "a", count: 299) + firstRegional
        XCTAssertEqual(got, want)
    }

    /// Mirrors Go `TestParseSummary_CollapsesUnicodeWhitespace`: collapsing
    /// covers the full Unicode space set, matching Go `strings.Fields`.
    func testParseSummaryCollapsesUnicodeWhitespace() {
        let nbsp = String(Unicode.Scalar(0x00A0)!)
        let vt = String(Unicode.Scalar(0x000B)!)
        let ff = String(Unicode.Scalar(0x000C)!)
        let md = "---\nname: x\ndescription: one\(nbsp)two\(vt)three\(ff)four\n---\nBody\n"
        let (_, got) = Frontmatter.parseSummary(md, slug: "x")
        XCTAssertEqual(got, "one two three four")
    }

    /// Mirrors Go `TestYAMLScalarEscapesControlCharacters`: a value carrying
    /// a C0/C1 control or DEL is quoted, and escaped exactly the way Go
    /// `strconv.Quote` escapes it, so the byte never reaches frontmatter raw.
    func testYAMLScalarEscapesControlCharacters() {
        let cases: [(scalar: UInt32, escaped: String)] = [
            (0x00, "\\x00"), (0x07, "\\a"), (0x08, "\\b"), (0x0B, "\\v"),
            (0x0C, "\\f"), (0x1B, "\\x1b"), (0x7F, "\\x7f"), (0x85, "\\u0085"),
        ]
        for c in cases {
            let input = "ab" + String(Unicode.Scalar(c.scalar)!) + "cd"
            let want = "\"ab" + c.escaped + "cd\""
            XCTAssertEqual(Frontmatter.yamlScalar(input), want,
                           "yamlScalar(U+\(String(c.scalar, radix: 16, uppercase: true)))")
        }
        // Belt and braces: no raw control scalar may survive in any of them.
        for c in cases {
            let input = "ab" + String(Unicode.Scalar(c.scalar)!) + "cd"
            let got = Frontmatter.yamlScalar(input)
            XCTAssertFalse(got.unicodeScalars.contains(where: {
                $0.value < 0x20 || $0.value == 0x7F || (0x80...0x9F).contains($0.value)
            }), got)
        }
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
