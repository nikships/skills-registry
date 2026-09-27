import XCTest
@testable import SkillsRegistryCore

/// The per-pane snapshots hoisted into `AppState` (see PaneState.swift) are
/// plain value types so their transitions can be pinned here: the views bind
/// to them, and navigation must never be able to disturb them.
final class PaneStateTests: XCTestCase {
    private func result(_ name: String) -> DiscoverResult {
        DiscoverResult(name: name, skillURL: "https://github.com/x/y/blob/main/\(name)")
    }

    private func local(_ slug: String) -> LocalSkill {
        LocalSkill(slug: slug, name: slug, description: "", folder: "/tmp/\(slug)", source: "test")
    }

    // MARK: Discover

    func testSearchedReplacesResultsSelectsFirstClearsError() {
        let pane = PaneState.Discover(query: "pdf", searchError: "stale")
        let next = pane.searched([result("a"), result("b")])
        XCTAssertEqual(next.results.map(\.name), ["a", "b"])
        XCTAssertEqual(next.selectedName, "a")
        XCTAssertEqual(next.selected?.name, "a")
        XCTAssertNil(next.searchError)
        XCTAssertTrue(next.didSearch)
        // The snapshot carries the query/mode along untouched.
        XCTAssertEqual(next.query, "pdf")
    }

    func testFailedClearsListAndKeepsMessage() {
        let pane = PaneState.Discover(query: "pdf", results: [result("a")],
                                      selectedName: "a", didSearch: true)
        let next = pane.failed("boom")
        XCTAssertTrue(next.results.isEmpty)
        XCTAssertNil(next.selectedName)
        XCTAssertNil(next.selected)
        XCTAssertEqual(next.searchError, "boom")
        XCTAssertTrue(next.didSearch)
    }

    func testSelectedResolvesAgainstLiveResults() {
        var pane = PaneState.Discover()
        XCTAssertNil(pane.selected)
        pane.results = [result("a"), result("b")]
        pane.selectedName = "b"
        XCTAssertEqual(pane.selected?.skillURL, "https://github.com/x/y/blob/main/b")
        // A selection with no matching row (replaced results) yields nothing.
        pane.selectedName = "gone"
        XCTAssertNil(pane.selected)
    }

    // MARK: Add

    func testFetchedPreSelectsEverything() {
        let pane = PaneState.Add(source: "owner/repo", fetchFailed: true)
        let next = pane.fetched([local("a"), local("b")])
        XCTAssertEqual(next.discovered.map(\.slug), ["a", "b"])
        XCTAssertEqual(next.selected, ["a", "b"])
        XCTAssertFalse(next.fetchFailed)
        XCTAssertTrue(next.didFetch)
        XCTAssertEqual(next.source, "owner/repo")
    }

    func testFetchedNilFlagsFailure() {
        let pane = PaneState.Add(source: "bogus")
        let next = pane.fetched(nil)
        XCTAssertTrue(next.discovered.isEmpty)
        XCTAssertTrue(next.selected.isEmpty)
        XCTAssertTrue(next.fetchFailed)
        XCTAssertTrue(next.didFetch)
    }

    func testPublishedClearsStaleDiscovery() {
        let pane = PaneState.Add(source: "owner/repo", discovered: [local("a")],
                                 selected: ["a"], didFetch: true)
        let next = pane.published()
        XCTAssertTrue(next.discovered.isEmpty)
        XCTAssertTrue(next.selected.isEmpty)
        XCTAssertTrue(next.didFetch)
        // The source stays so re-fetching after a publish is one tap.
        XCTAssertEqual(next.source, "owner/repo")
    }

    // MARK: Import

    func testRescannedPreSelectsEverything() {
        let pane = PaneState.Import()
        let next = pane.rescanned([local("a"), local("b")])
        XCTAssertEqual(next.locals.map(\.slug), ["a", "b"])
        XCTAssertEqual(next.selected, ["a", "b"])
        XCTAssertTrue(next.scanned)
    }

    func testRescanPreservesDeselectionWhenSnapshotKept() {
        // Navigating away and back must not re-run the scan: the pane keeps
        // the struct it had, including the user's deselection.
        var pane = PaneState.Import().rescanned([local("a"), local("b")])
        pane.selected.remove("b")
        XCTAssertEqual(pane.selected, ["a"])
        XCTAssertTrue(pane.scanned)
    }
}
