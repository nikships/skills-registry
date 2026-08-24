import Foundation
import SkillsRegistryCore

/// Fixture data + entry for demo mode (`--demo` / `SKILLS_APP_DEMO=1`). Lets
/// the full authed UI be exercised by cua-driver without GitHub credentials.
extension AppState {
    func startDemo() {
        identity = Identity(login: "octocat", name: "Mona Octocat")
        repo = RepoRef(owner: "octocat", name: "skills-registry")
        branch = "main"
        skills = Self.demoSkills
        cliInstalled = false
        phase = .ready
    }

    static let demoSkills: [SkillSummary] = [
        SkillSummary(slug: "pdf_tools", name: "PDF Tools",
                     description: "Extract text, split, merge, and fill PDF forms. Use when the user works with PDF files or needs document automation.", treeSHA: "a1"),
        SkillSummary(slug: "react_review", name: "React Code Review",
                     description: "Opinionated React/TypeScript review checklist covering hooks, memoization, accessibility, and bundle size.", treeSHA: "b2"),
        SkillSummary(slug: "sql_optimizer", name: "SQL Optimizer",
                     description: "Diagnose slow Postgres queries: read EXPLAIN ANALYZE, suggest indexes, and rewrite N+1 access patterns.", treeSHA: "c3"),
        SkillSummary(slug: "git_surgeon", name: "Git Surgeon",
                     description: "Recover from rebases gone wrong, rewrite history safely, and untangle merge conflicts with confidence.", treeSHA: "d4"),
        SkillSummary(slug: "brand_voice", name: "Brand Voice",
                     description: "Rewrite copy in the company's voice — concise, warm, technically precise, never hyped.", treeSHA: "e5"),
        SkillSummary(slug: "k8s_debug", name: "Kubernetes Debugging",
                     description: "Triage CrashLoopBackOff, pending pods, and OOMKills. Walks the events → logs → describe → resources path.", treeSHA: "f6"),
    ]

    static let demoLocal: [LocalSkill] = [
        LocalSkill(slug: "terraform_lint", name: "Terraform Lint",
                   description: "Catch insecure defaults and drift in Terraform modules.",
                   folder: "~/.claude/skills/terraform-lint", source: "~/.claude/skills"),
        LocalSkill(slug: "changelog_writer", name: "Changelog Writer",
                   description: "Turn merged PRs into a clean, grouped changelog entry.",
                   folder: "~/.cursor/skills/changelog-writer", source: "~/.cursor/skills"),
    ]

    /// Fixture rows for the Discover pane. They carry the same grade shapes the
    /// real index returns, including a `Poor` row and one the index never
    /// graded, so both consent paths are reachable offline.
    static let demoDiscoverResults: [DiscoverResult] = [
        DiscoverResult(name: "pdf-form-filler", description: "Fill and flatten PDF AcroForms from a JSON payload, then verify every field landed.",
                       author: "openclaw", category: "Productivity",
                       skillURL: "https://github.com/openclaw/openclaw/blob/1300b22/skills/pdf-form-filler",
                       safety: "Good", completeness: "Good", executability: "Average"),
        DiscoverResult(name: "pdf-extract", description: "Pull text, tables, and embedded images out of scanned or digital PDFs with layout preserved.",
                       author: "docwrangler", category: "AIGC",
                       skillURL: "https://github.com/docwrangler/skills/blob/main/pdf-extract",
                       safety: "Average", completeness: "Good", executability: "Good"),
        DiscoverResult(name: "pdf-redact", description: "Redact names, addresses, and account numbers from a PDF before sharing it.",
                       author: "privacy-tools", category: "Security",
                       skillURL: "https://github.com/privacy-tools/agent-skills/blob/v2/skills/pdf-redact",
                       safety: "Good", completeness: "Average", executability: "Average"),
        DiscoverResult(name: "pdf-scraper", description: "Bulk-download PDFs from a site and pipe each one through a summarizer.",
                       author: "anon", category: "Data",
                       skillURL: "https://github.com/anon/pdf-scraper/blob/main/skills/pdf-scraper",
                       safety: "Poor", completeness: "Average", executability: "Poor"),
        DiscoverResult(name: "pdf-to-slides", description: "Turn a long PDF report into a deck outline with one slide per section.",
                       author: "deckbot", category: "Productivity",
                       skillURL: "https://github.com/deckbot/skills/blob/main/pdf-to-slides"),
    ]

    /// Demo-mode search: filters the fixtures on the query so the pane behaves
    /// like the real one, and reports a miss as an empty result set rather than
    /// an error.
    static func demoDiscoverResponse(_ query: DiscoverQuery) throws -> DiscoverResponse {
        let q = try query.normalized()
        let needle = q.text.lowercased()
        let hits = demoDiscoverResults.filter {
            $0.name.lowercased().contains(needle) || $0.description.lowercased().contains(needle)
                || $0.category.lowercased().contains(needle)
        }
        return DiscoverResponse(source: DiscoverClient.source, query: q.text,
                                mode: q.mode.rawValue, results: Array(hits.prefix(q.limit)))
    }

    static func demoDetail(_ slug: String) -> SkillDetail {
        let match = demoSkills.first { $0.slug == slug }
        let name = match?.name ?? slug
        let md = """
        ---
        name: \(name)
        description: \(match?.description ?? "A demo skill.")
        ---

        # \(name)

        This is a **demo** rendering of a `SKILL.md`. It shows how the macOS app
        presents skills with rich markdown.

        ## When to use

        - When the user asks for `\(slug)` capabilities
        - When you need a repeatable, reviewed procedure
        - When a one-off prompt would drift over time

        ## Steps

        1. Discover the skill via search.
        2. Read this `SKILL.md` top to bottom.
        3. Follow the references below.

        ```bash
        skills-registry get \(slug)
        ```

        > Tip: supporting files live alongside this document — check the file
        > list in the sidebar.

        | Field | Value |
        | --- | --- |
        | slug | `\(slug)` |
        | source | registry |

        See [the registry](https://github.com/octocat/skills-registry) for more.
        """
        return SkillDetail(
            slug: slug,
            name: name,
            description: match?.description ?? "",
            markdown: md,
            files: ["SKILL.md", "references/checklist.md", "scripts/run.sh"]
        )
    }

    static func demoFile(slug: String, path: String) -> String {
        if path.hasSuffix(".sh") {
            return "#!/usr/bin/env bash\nset -euo pipefail\n\n# Demo support script for \(slug)\necho \"running \(slug)\"\n"
        }
        return """
        # \(path)

        Demo contents for `\(path)` in **\(slug)**. Real registries serve the
        actual file from GitHub.

        - bullet one
        - bullet two
        """
    }
}
