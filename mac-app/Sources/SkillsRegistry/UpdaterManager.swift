import SwiftUI
import Combine
import Sparkle

/// SwiftUI-friendly wrapper around Sparkle's standard updater.
///
/// Sparkle owns the macOS app's self-update entirely: it reads `SUFeedURL` /
/// `SUPublicEDKey` from Info.plist, runs the scheduled background check
/// (`SUScheduledCheckInterval`), verifies the EdDSA signature, downloads, swaps
/// the `.app`, and relaunches. We only surface a manual "Check for Updates…"
/// affordance and an automatic-checks toggle.
@MainActor
final class UpdaterManager: ObservableObject {
    private let controller: SPUStandardUpdaterController

    /// False while a check is already running (drives button enablement).
    @Published var canCheckForUpdates = false
    /// Mirrors Sparkle's persisted "check automatically" preference.
    @Published var automaticallyChecksForUpdates: Bool

    init() {
        controller = SPUStandardUpdaterController(
            startingUpdater: true, updaterDelegate: nil, userDriverDelegate: nil)
        automaticallyChecksForUpdates = controller.updater.automaticallyChecksForUpdates
        controller.updater.publisher(for: \.canCheckForUpdates)
            .assign(to: &$canCheckForUpdates)
    }

    /// Show Sparkle's update UI now (no-op outside a proper .app bundle).
    func checkForUpdates() {
        controller.updater.checkForUpdates()
    }

    func setAutomaticChecks(_ enabled: Bool) {
        controller.updater.automaticallyChecksForUpdates = enabled
        automaticallyChecksForUpdates = enabled
    }
}

/// Menu command (under the app menu) that triggers a manual Sparkle check.
struct CheckForUpdatesCommand: View {
    @ObservedObject var updater: UpdaterManager
    var body: some View {
        Button("Check for Updates…") { updater.checkForUpdates() }
            .disabled(!updater.canCheckForUpdates)
    }
}
