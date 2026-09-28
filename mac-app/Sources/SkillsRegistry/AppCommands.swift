import SwiftUI

/// The app's menu/shortcut command layer (design-a11y-4).
///
/// Commands resolve against `AppState` directly rather than focused values, so
/// they work whenever the main window is up — even when no field has keyboard
/// focus (the common case right after launch or a section switch, when
/// focused values vend nothing and the menu would come out empty).
/// Per-pane effects (search focus, refresh) flow through request counters the
/// visible pane observes; only the visible pane is in the hierarchy, so only
/// it responds.
struct AppCommands: View {
    @ObservedObject var state: AppState

    /// Sections with a search/source field Cmd-F can focus.
    private static let searchable: Set<NavSection> = [.browse, .discover, .add]
    /// Sections with refreshable content for Cmd-R.
    private static let refreshable: Set<NavSection> = [.browse, .discover, .importLocal]

    private var inMainWindow: Bool { state.phase == .ready }

    var body: some View {
        Button("Find in This Section") { state.focusSearchRequest += 1 }
            .keyboardShortcut("f", modifiers: .command)
            .disabled(!inMainWindow || !Self.searchable.contains(state.section))
        Divider()
        Button("Refresh") { state.refreshRequest += 1 }
            .keyboardShortcut("r", modifiers: .command)
            .disabled(!inMainWindow || !Self.refreshable.contains(state.section))
        Divider()
        ForEach(Array(NavSection.allCases.enumerated()), id: \.element.id) { index, item in
            Button("Show \(item.rawValue)") { state.section = item }
                .keyboardShortcut(KeyEquivalent(Character("\(index + 1)")), modifiers: .command)
                .disabled(!inMainWindow)
        }
        Divider()
        Button("Settings") { state.section = .settings }
            .keyboardShortcut(",", modifiers: .command)
            .disabled(!inMainWindow)
    }
}
