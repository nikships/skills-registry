import SwiftUI
import SkillsRegistryCore

/// Reusable agent multi-select used by "Install" (registry skill → local),
/// "Add" (external source → publish + install), and "Discover" (index row →
/// import + install). Lists the home-based
/// agents from `Agents.all()` (`underHome == true`) plus the universal
/// `.agents` target. The app uses the home directory as the universal target's
/// install base, so that row writes to `~/.agents/skills`.
///
/// Locations are never preselected. Existing folders are shown as detected
/// information only; the user explicitly chooses every install destination.
///
/// When `emptyConfirmLabel` is set, confirming with nothing selected is
/// allowed and reads as a registry-only publish (the Add flow's default for
/// untrusted sources): the button shows that label at zero selected and
/// `confirmLabel` otherwise. Callers that must install somewhere (the skill
/// Install flow) leave it nil and keep confirm disabled until a row is
/// picked.
///
/// "Select all detected" selects only the visible rows whose folders exist on
/// disk, so tools the user never installed are never bulk-selected (and no
/// junk dot-folders are created); per-row opt-in for the rest is unchanged. A
/// filter field narrows the list by display name or dot-folder and scopes
/// select-all to the visible rows.
struct AgentPickerSheet: View {
    let title: String
    let subtitle: String
    let confirmLabel: String
    /// The confirm button's label when nothing is selected, and the signal that
    /// confirming empty is meaningful at all: non-nil lets the user confirm with
    /// no agent picked (an empty target list, which the caller reads as "skip
    /// the install") and says so on the button. Nil keeps the confirm disabled
    /// until a destination is chosen — the Install and Add flows, whose confirm
    /// is meaningless without one.
    var emptyConfirmLabel: String?
    let onConfirm: ([AgentTarget]) -> Void

    @Environment(\.dismiss) private var dismiss
    @State private var selected: Set<String> = []
    @State private var targets: [AgentTarget] = []
    @State private var filter: String = ""

    /// The app's home directory is also the install base for `.agents`.
    private var home: String { FileManager.default.homeDirectoryForCurrentUser.path }

    /// Rows matching the filter text (blank matches everything).
    private var visible: [AgentTarget] { Agents.matching(targets, query: filter) }

    /// Visible rows whose folders exist — the "Select all detected" set.
    private var selectable: [AgentTarget] { visible.filter(folderExists) }

    private var allSelectableSelected: Bool {
        !selectable.isEmpty && selectable.allSatisfy { selected.contains($0.dotDir) }
    }

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
                HStack(spacing: 8) {
                    Image(systemName: "magnifyingglass").font(.system(size: 12)).foregroundStyle(Brand.muted)
                    TextField("Filter agents…", text: $filter)
                        .textFieldStyle(.plain).font(.system(size: 13))
                        .accessibilityIdentifier("agentPickerFilter")
                    if !filter.isEmpty {
                        Button { filter = "" } label: { Image(systemName: "xmark.circle.fill") }
                            .buttonStyle(.plain).foregroundStyle(Brand.meta)
                    }
                }
                .padding(.horizontal, 12).padding(.vertical, 9)
                .background(Brand.surfaceWarm)
                .overlay(RoundedRectangle(cornerRadius: 8).strokeBorder(Brand.border, lineWidth: 1))
                .clipShape(RoundedRectangle(cornerRadius: 8))
                HStack(spacing: 10) {
                    Button {
                        if allSelectableSelected {
                            // With no filter this clears everything (true
                            // "Deselect all"); with a filter it clears only
                            // the visible rows, preserving off-screen picks.
                            if filter.trimmingCharacters(in: .whitespaces).isEmpty {
                                selected = []
                            } else {
                                selected.subtract(visible.map(\.dotDir))
                            }
                        } else {
                            selected.formUnion(selectable.map(\.dotDir))
                        }
                    } label: {
                        Text(allSelectableSelected ? "Deselect all" : "Select all detected")
                    }
                    .buttonStyle(.plain).foregroundStyle(Brand.accent).font(.system(size: 12))
                    .disabled(selectable.isEmpty)
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
        } else if visible.isEmpty {
            EmptyState(icon: "magnifyingglass",
                       title: "No matches",
                       subtitle: "No agents match “\(filter)” — try a different filter.")
        } else {
            ScrollView {
                LazyVStack(spacing: 0) {
                    ForEach(visible, id: \.dotDir) { t in
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
            } label: { Text(selected.isEmpty ? (emptyConfirmLabel ?? confirmLabel) : confirmLabel) }
            .buttonStyle(PrimaryButtonStyle())
            .disabled(selected.isEmpty && emptyConfirmLabel == nil)
            .accessibilityIdentifier("agentPickerConfirm")
        }
        .padding(16)
    }

    private func load() {
        targets = Agents.all().filter { $0.underHome || $0.universal }
        selected = []
    }

    private func folderExists(_ t: AgentTarget) -> Bool {
        let base = (home as NSString).appendingPathComponent(t.dotDir)
        var isDir: ObjCBool = false
        return FileManager.default.fileExists(atPath: base, isDirectory: &isDir) && isDir.boolValue
    }
}
