import XCTest
import SkillsRegistryCore
@testable import SkillsRegistry

/// AppState's editor seams: the demo-mode save path (frontmatter re-parse,
/// row upsert + re-sort, toast, draft clearing) and the per-slug draft store
/// that preserves unsaved edits across navigation.
@MainActor
final class EditorDraftSafetyTests: XCTestCase {
    private func demoState() -> AppState {
        let state = AppState(demo: true)
        state.skills = [
            SkillSummary(slug: "b_skill", name: "B", description: "bee"),
            SkillSummary(slug: "a_skill", name: "A", description: "aye"),
        ]
        return state
    }

    func testDemoSaveUpsertsNameAndDescriptionFromFrontmatter() async throws {
        let state = demoState()
        let markdown = """
            ---
            name: Renamed B
            description: A brand-new description.
            ---

            # body
            """
        let summary = try await state.saveSkillMarkdown("b_skill", markdown: markdown)

        XCTAssertEqual(summary.slug, "b_skill")
        XCTAssertEqual(summary.name, "Renamed B")
        XCTAssertEqual(summary.description, "A brand-new description.")
        // The list re-sorts by slug and the edited row carries the new copy.
        XCTAssertEqual(state.skills.map(\.slug), ["a_skill", "b_skill"])
        XCTAssertEqual(state.skills[1].name, "Renamed B")
        XCTAssertEqual(state.skills[1].description, "A brand-new description.")
        // Untouched rows keep their content.
        XCTAssertEqual(state.skills[0], SkillSummary(slug: "a_skill", name: "A", description: "aye"))
        XCTAssertEqual(state.toast?.message, "Saved b_skill")
    }

    func testDemoSaveWithoutFrontmatterFallsBackToSlug() async throws {
        let state = demoState()
        let markdown = "Just a body paragraph, no frontmatter block.\n"
        let summary = try await state.saveSkillMarkdown("b_skill", markdown: markdown)

        XCTAssertEqual(summary.name, "b_skill")
        XCTAssertEqual(summary.description, "Just a body paragraph, no frontmatter block.")
        XCTAssertEqual(state.skills.map(\.slug), ["a_skill", "b_skill"])
        XCTAssertEqual(state.toast?.message, "Saved b_skill")
    }

    func testDraftPreservationRoundTrip() {
        let state = demoState()

        XCTAssertNil(state.draft(for: "b_skill"))
        state.saveDraft("half-written edit", for: "b_skill")
        XCTAssertEqual(state.draft(for: "b_skill"), "half-written edit")
        // Other slugs are unaffected.
        XCTAssertNil(state.draft(for: "a_skill"))
        state.clearDraft(for: "b_skill")
        XCTAssertNil(state.draft(for: "b_skill"))
    }

    func testDemoSaveClearsPreservedDraft() async throws {
        let state = demoState()
        state.saveDraft("stale text", for: "b_skill")
        state.saveDraft("keep me", for: "a_skill")

        _ = try await state.saveSkillMarkdown("b_skill", markdown: "# saved\n")

        XCTAssertNil(state.draft(for: "b_skill"))
        // A save only drops the slug that was saved.
        XCTAssertEqual(state.draft(for: "a_skill"), "keep me")
        XCTAssertEqual(state.toast?.message, "Saved b_skill")
    }
}
