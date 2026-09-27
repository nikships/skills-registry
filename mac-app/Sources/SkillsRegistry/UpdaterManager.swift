import SwiftUI
import Combine
import Sparkle

/// One failed Sparkle update cycle. Fresh `id` per failure so SwiftUI's
/// `onChange` fires even when the message repeats.
struct UpdateCheckFailure: Equatable, Identifiable {
    let id = UUID()
    let message: String
    let date = Date()
}

/// SPUUpdaterDelegate that funnels every finished update cycle — success or
/// failure — into one callback. Sparkle calls both `didAbortWithError` and
/// `didFinishUpdateCycleForUpdateCheck` for a single failure, so only the
/// latter is implemented: exactly one signal per cycle, and successes clear
/// the failure state. Retained by UpdaterManager (SPUUpdater keeps it weakly).
final class UpdateCheckDelegate: NSObject, SPUUpdaterDelegate {
    var onFinish: ((NSError?) -> Void)?

    func updater(_ updater: SPUUpdater, didFinishUpdateCycleFor updateCheck: SPUUpdateCheck, error: Error?) {
        onFinish?(error as NSError?)
    }
}

/// SwiftUI-friendly wrapper around Sparkle's standard updater.
///
/// Sparkle owns the macOS app's self-update entirely: it reads `SUFeedURL` /
/// `SUPublicEDKey` from Info.plist, runs the scheduled background check
/// (`SUScheduledCheckInterval`), verifies the EdDSA signature, downloads, swaps
/// the `.app`, and relaunches. We only surface a manual "Check for Updates…"
/// affordance, an automatic-checks toggle, and the last failure (toast + a
/// Settings hint) when a check errors.
@MainActor
final class UpdaterManager: ObservableObject {
    private let controller: SPUStandardUpdaterController
    private let delegate: UpdateCheckDelegate

    /// False while a check is already running (drives button enablement).
    @Published var canCheckForUpdates = false
    /// Mirrors Sparkle's persisted "check automatically" preference.
    @Published var automaticallyChecksForUpdates: Bool
    /// The latest failed check, if the most recent finished cycle errored.
    /// Cleared by the next successful cycle.
    @Published var lastCheckFailure: UpdateCheckFailure?

    init() {
        let delegate = UpdateCheckDelegate()
        self.delegate = delegate
        controller = SPUStandardUpdaterController(
            startingUpdater: true, updaterDelegate: delegate, userDriverDelegate: nil)
        automaticallyChecksForUpdates = controller.updater.automaticallyChecksForUpdates
        controller.updater.publisher(for: \.canCheckForUpdates)
            .assign(to: &$canCheckForUpdates)
        delegate.onFinish = { [weak self] error in
            Task { @MainActor in self?.finishCycle(error: error) }
        }
    }

    private func finishCycle(error: NSError?) {
        guard let error else { lastCheckFailure = nil; return }
        // "No update found" and user-deferred/cancelled installs are normal
        // outcomes, not failures worth surfacing (Sparkle itself doesn't log
        // them either).
        if error.domain == SUSparkleErrorDomain,
           [SUError.noUpdateError, .installationCanceledError, .installationAuthorizeLaterError]
            .map(\.rawValue).contains(OSStatus(error.code)) {
            lastCheckFailure = nil
            return
        }
        lastCheckFailure = UpdateCheckFailure(message: error.localizedDescription)
    }

    /// Show Sparkle's update UI now (no-op outside a proper .app bundle).
    func checkForUpdates() {
        controller.updater.checkForUpdates()
    }

    func setAutomaticChecks(_ enabled: Bool) {
        controller.updater.automaticallyChecksForUpdates = enabled
        automaticallyChecksForUpdates = enabled
    }

    /// Demo-only: seed a failure so the Settings hint + toast are reachable
    /// offline (see the demo trigger in Settings → App). Never called from
    /// production paths.
    func simulateCheckFailure() {
        lastCheckFailure = UpdateCheckFailure(
            message: "Demo: couldn't reach the update feed (simulated).")
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
