import SwiftUI
import SkillsRegistryCore

/// Reusable agent multi-select used by both "Install" (registry skill → local)
/// and "Add" (external source → publish + install). Lists the home-based
/// agents from `Agents.all()` (`underHome == true`) and pre-checks those whose
/// `<dot>` folder already exists on disk, so an existing setup is the default.
///
/// The cwd-based universal `.agents` target is intentionally skipped — a
/// desktop app has no meaningful project working directory, the same rationale
/// as `MetaSkill.detectedTargets`. This is a deliberate, documented divergence
/// from the CLI install picker (whose universal `.agents/skills` target is
/// always-on).
struct AgentPickerSheet: View {
    let title: String
    let subtitle: String
    let confirmLabel: String
    let onConfirm: ([AgentTarget]) -> Void

    @Environment(\.dismiss) private var dismiss
    @State private var selected: Set<String> = []
    @State private var targets: [AgentTarget] = []

    /// Home-based agents only (skip the cwd universal target).
    private var home: String { FileManager.default.homeDirectoryForCurrentUser.path }

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            header
            Divider().overlay(Brand.border)
            list
            Divider().overlay(Brand.border)
            footer
        }
        .frame(width: 460, height: 520)
        .background(Brand.bg)
        .onAppear(perform: load)
    }

    private var header: some View {
        VStack(alignment: .leading, spacing: 8) {
            Eyebrow(text: "Install location")
            Text(title).font(.system(size: 18, weight: .semibold)).foregroundStyle(Brand.fg)
            Text(subtitle).font(.system(size: 12)).foregroundStyle(Brand.muted)
                .fixedSize(horizontal: false, vertical: true)
            if !targets.isEmpty {
                HStack(spacing: 10) {
                    Button {
                        selected = selected.count == targets.count ? [] : Set(targets.map(\.dotDir))
                    } label: {
                        Text(selected.count == targets.count ? "Deselect all" : "Select all")
                    }
                    .buttonStyle(.plain).foregroundStyle(Brand.accent).font(.system(size: 12))
                    Spacer()
                    Text("\(selected.count) selected").font(Brand.monoSized(11)).foregroundStyle(Brand.meta)
                }
            }
        }
        .padding(20)
    }

    @ViewBuilder private var list: some View {
        if targets.isEmpty {
            EmptyState(icon: "questionmark.folder",
                       title: "No agents detected",
                       subtitle: "Create an AI tool folder (e.g. ~/.claude) first, then try again.")
        } else {
            ScrollView {
                LazyVStack(spacing: 0) {
                    ForEach(targets, id: \.dotDir) { t in
                        row(t)
                        Divider().overlay(Brand.border).padding(.leading, 44)
                    }
                }
                .padding(.vertical, 4)
            }
        }
    }

    private func row(_ t: AgentTarget) -> some View {
        Button {
            if selected.contains(t.dotDir) { selected.remove(t.dotDir) } else { selected.insert(t.dotDir) }
        } label: {
            HStack(spacing: 12) {
                Image(systemName: selected.contains(t.dotDir) ? "checkmark.square.fill" : "square")
                    .font(.system(size: 16))
                    .foregroundStyle(selected.contains(t.dotDir) ? Brand.accent : Brand.muted)
                VStack(alignment: .leading, spacing: 2) {
                    Text(t.display).font(.system(size: 13, weight: .medium)).foregroundStyle(Brand.fg)
                    Text("\(t.dotDir)/skills").font(Brand.monoSized(10)).foregroundStyle(Brand.meta)
                }
                Spacer()
                if folderExists(t) {
                    Text("detected").font(Brand.monoSized(10)).foregroundStyle(Brand.success)
                }
            }
            .padding(.horizontal, 20).padding(.vertical, 10)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .accessibilityIdentifier("agent-\(t.dotDir)")
    }

    private var footer: some View {
        HStack(spacing: 10) {
            Spacer()
            Button("Cancel") { dismiss() }.buttonStyle(GhostButtonStyle())
            Button {
                onConfirm(targets.filter { selected.contains($0.dotDir) })
                dismiss()
            } label: { Text(confirmLabel) }
            .buttonStyle(PrimaryButtonStyle())
            .disabled(selected.isEmpty)
            .accessibilityIdentifier("agentPickerConfirm")
        }
        .padding(16)
    }

    private func load() {
        targets = Agents.all().filter(\.underHome)
        // Pre-check agents whose base dot-folder already exists on disk.
        selected = Set(targets.filter { folderExists($0) }.map(\.dotDir))
    }

    private func folderExists(_ t: AgentTarget) -> Bool {
        let base = (home as NSString).appendingPathComponent(t.dotDir)
        var isDir: ObjCBool = false
        return FileManager.default.fileExists(atPath: base, isDirectory: &isDir) && isDir.boolValue
    }
}
