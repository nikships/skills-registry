import SwiftUI
import SkillsRegistryCore

/// Dismissible prompts shown at the top of the main content area:
///  • a new `skills-registry` CLI release is available, and
///  • the `skills-registry` meta-skill is missing or out of date in one or
///    more detected agents.
///
/// The macOS app's own self-update is handled by Sparkle's native UI, not here.
/// When nothing is actionable the banner renders nothing (no empty gap).
struct UpdateBanner: View {
    @EnvironmentObject var state: AppState
    @State private var workingCLI = false
    @State private var workingSkill = false

    private var showCLI: Bool {
        guard let key = state.cliUpdateKey else { return false }
        return state.cliUpdate != nil && !state.isDismissed(key)
    }

    private var showSkill: Bool {
        state.metaSkill.needsAction && !state.isDismissed(state.metaSkillKey)
    }

    var body: some View {
        if showCLI || showSkill {
            VStack(spacing: 10) {
                if showCLI { cliRow }
                if showSkill { skillRow }
            }
            .padding(.horizontal, 24)
            .padding(.top, 16)
        }
    }

    @ViewBuilder private var cliRow: some View {
        if let update = state.cliUpdate, let key = state.cliUpdateKey {
            let have = Semver.firstIn(state.cliVersion ?? "")?.string
            row(
                icon: "arrow.down.circle.fill",
                title: "CLI update available",
                detail: "skills-registry \(update.version.string) is out" + (have.map { " — you have \($0)" } ?? "") + ".",
                actionTitle: "Update",
                working: workingCLI,
                action: { workingCLI = true; Task { await state.installCLI(); workingCLI = false } },
                dismiss: { state.dismiss(key) })
        }
    }

    @ViewBuilder private var skillRow: some View {
        let missing = state.metaSkill.targets.filter { $0.state == .missing }.count
        let outdated = state.metaSkill.targets.filter { $0.state == .outdated }.count
        let detected = state.metaSkill.detectedCount
        row(
            icon: "sparkles",
            title: state.metaSkill.anyMissing ? "Install the skills-registry skill" : "Refresh the skills-registry skill",
            detail: skillDetail(missing: missing, outdated: outdated, detected: detected),
            actionTitle: state.metaSkill.anyMissing ? "Install in all" : "Refresh all",
            working: workingSkill,
            action: { workingSkill = true; Task { await state.installMetaSkill(); workingSkill = false } },
            dismiss: { state.dismiss(state.metaSkillKey) })
    }

    private func skillDetail(missing: Int, outdated: Int, detected: Int) -> String {
        let agents = "\(detected) detected agent" + (detected == 1 ? "" : "s")
        if missing > 0 && outdated > 0 {
            return "Missing in \(missing) and out of date in \(outdated) of \(agents). It teaches each agent to reach your registry."
        } else if missing > 0 {
            return "Not installed in \(missing) of \(agents). It teaches each agent to reach your registry."
        } else {
            return "Out of date in \(outdated) of \(agents) — refresh to match the latest template."
        }
    }

    private func row(
        icon: String,
        title: String,
        detail: String,
        actionTitle: String,
        working: Bool,
        action: @escaping () -> Void,
        dismiss: @escaping () -> Void
    ) -> some View {
        HStack(spacing: 12) {
            Image(systemName: icon)
                .font(.system(size: 16, weight: .semibold))
                .foregroundStyle(Brand.accent)
                .frame(width: 22)
            VStack(alignment: .leading, spacing: 2) {
                Text(title).font(.system(size: 13, weight: .semibold)).foregroundStyle(Brand.fg)
                Text(detail).font(.system(size: 12)).foregroundStyle(Brand.muted)
                    .fixedSize(horizontal: false, vertical: true)
            }
            Spacer(minLength: 12)
            Button(action: action) {
                HStack(spacing: 6) {
                    if working { ProgressView().controlSize(.small) }
                    Text(actionTitle)
                }
            }
            .buttonStyle(PrimaryButtonStyle(tint: Brand.accent))
            .disabled(working)
            Button(action: dismiss) {
                Image(systemName: "xmark").font(.system(size: 11, weight: .bold)).foregroundStyle(Brand.muted)
            }
            .buttonStyle(.plain)
            .help("Dismiss")
        }
        .padding(.horizontal, 14)
        .padding(.vertical, 11)
        .background(Brand.surfaceWarm)
        .overlay(RoundedRectangle(cornerRadius: 10).strokeBorder(Brand.accent.opacity(0.35), lineWidth: 1))
        .clipShape(RoundedRectangle(cornerRadius: 10))
    }
}
