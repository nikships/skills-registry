import Foundation

/// Per-pane navigation state, hoisted out of the SwiftUI view layer so it
/// survives both accent-driven rebuilds (`RootView .id(theme.accent)`) and
/// sidebar section switches (`HomeView .id(section)`). Plain value types —
/// the UI snapshots are the single source of truth the pane views bind to.
public enum PaneState {
    /// Browse pane: the fuzzy-search text plus the selected skill slug.
    public struct Browse: Sendable, Equatable {
        public var query: String
        public var selectedSlug: String?

        public init(query: String = "", selectedSlug: String? = nil) {
            self.query = query
            self.selectedSlug = selectedSlug
        }
    }

    /// Discover pane: the query field, ranking mode, last results, selection,
    /// and whether a search has run (which decides the empty state).
    public struct Discover: Sendable, Equatable {
        public var query: String
        public var mode: DiscoverMode
        public var results: [DiscoverResult]
        public var selectedName: String?
        public var didSearch: Bool
        public var searchError: String?

        public init(query: String = "", mode: DiscoverMode = .keyword,
                    results: [DiscoverResult] = [], selectedName: String? = nil,
                    didSearch: Bool = false, searchError: String? = nil) {
            self.query = query
            self.mode = mode
            self.results = results
            self.selectedName = selectedName
            self.didSearch = didSearch
            self.searchError = searchError
        }

        /// The selected row, resolved against the current results. Names are
        /// unique within one index response, and re-resolving (rather than
        /// storing the row) keeps the selection pointing at the live list.
        public var selected: DiscoverResult? {
            guard let selectedName else { return nil }
            return results.first { $0.name == selectedName }
        }

        /// The new snapshot after a search completes: results replaced,
        /// selection reset to the first row, the error cleared.
        public func searched(_ results: [DiscoverResult]) -> Discover {
            var next = self
            next.results = results
            next.selectedName = results.first?.name
            next.searchError = nil
            next.didSearch = true
            return next
        }

        /// The new snapshot after a search fails closed: no partial list
        /// survives, so results and selection are cleared and the error kept.
        public func failed(_ message: String) -> Discover {
            var next = self
            next.results = []
            next.selectedName = nil
            next.searchError = message
            next.didSearch = true
            return next
        }
    }

    /// Add pane: the source string, fetched skills, multi-selection, and
    /// whether a fetch has run (which decides the empty state). The fetch
    /// failure reason and the import-gate acknowledgement live here too, so a
    /// section switch cannot silently drop either.
    public struct Add: Sendable, Equatable {
        public var source: String
        public var discovered: [LocalSkill]
        public var selected: Set<String>
        public var didFetch: Bool
        public var fetchFailed: Bool
        /// The fetch failure reason, kept past the transient toast.
        public var fetchError: String?
        /// Whether the user acknowledged a `Poor`-safety blocker.
        public var acknowledgedBlock: Bool

        public init(source: String = "", discovered: [LocalSkill] = [],
                    selected: Set<String> = [], didFetch: Bool = false,
                    fetchFailed: Bool = false, fetchError: String? = nil,
                    acknowledgedBlock: Bool = false) {
            self.source = source
            self.discovered = discovered
            self.selected = selected
            self.didFetch = didFetch
            self.fetchFailed = fetchFailed
            self.fetchError = fetchError
            self.acknowledgedBlock = acknowledgedBlock
        }

        /// The new snapshot after a fetch completes: discovery replaced and
        /// fully pre-selected, or the failure recorded with its reason.
        public func fetched(_ found: [LocalSkill]?, error: String? = nil) -> Add {
            var next = self
            if let found {
                next.discovered = found
                next.selected = Set(found.map(\.slug))
                next.fetchFailed = false
                next.fetchError = nil
                next.acknowledgedBlock = false
            } else {
                next.discovered = []
                next.selected = []
                next.fetchFailed = true
                next.fetchError = error
            }
            next.didFetch = true
            return next
        }

        /// The new snapshot after publishing: the temp clone is gone, so stale
        /// folder paths must not be reused and the list clears. Demo keeps its
        /// fixtures, since clearing them would read as data loss.
        public func published(keepList: Bool = false) -> Add {
            var next = self
            if !keepList {
                next.discovered = []
                next.selected = []
            }
            next.didFetch = true
            return next
        }
    }

    /// Import pane: the scanned locals plus the multi-selection.
    public struct Import: Sendable, Equatable {
        public var locals: [LocalSkill]
        public var selected: Set<String>
        public var scanned: Bool

        public init(locals: [LocalSkill] = [], selected: Set<String> = [],
                    scanned: Bool = false) {
            self.locals = locals
            self.selected = selected
            self.scanned = scanned
        }

        /// The new snapshot after a rescan: locals replaced, all pre-selected.
        public func rescanned(_ locals: [LocalSkill]) -> Import {
            var next = self
            next.locals = locals
            next.selected = Set(locals.map(\.slug))
            next.scanned = true
            return next
        }
    }
}
