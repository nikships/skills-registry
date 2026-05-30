import SwiftUI
import SkillsRegistryCore

struct ImportView: View {
    @EnvironmentObject var state: AppState
    @State private var locals: [LocalSkill] = []
    @State private var selected: Set<String> = []
    @State private var scanned = false
    @State private var importing = false
    @State private var progress: (Int, Int) = (0, 0)

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            head
            Divider().overlay(Brand.border)
            body0
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
        .background(Brand.bg)
        .task { if !scanned { rescan() } }
    }

    private var head: some View {
        VStack(alignment: .leading, spacing: 10) {
            Eyebrow(text: "Bulk import")
            Text("Import local skills").font(.system(size: 22, weight: .semibold)).foregroundStyle(Brand.fg)
            Text("We scanned your AI tool folders (\(Agents.dotDirs().count) known locations) for skills not yet in your registry. Select the ones to publish in a single commit.")
                .font(.system(size: 13)).foregroundStyle(Brand.muted)
                .fixedSize(horizontal: false, vertical: true)
            HStack(spacing: 10) {
                Button { rescan() } label: { Label("Rescan", systemImage: "arrow.clockwise") }
                    .buttonStyle(GhostButtonStyle())
                if !locals.isEmpty {
                    Button {
                        selected = selected.count == locals.count ? [] : Set(locals.map(\.slug))
                    } label: {
                        Text(selected.count == locals.count ? "Deselect all" : "Select all")
                    }.buttonStyle(.plain).foregroundStyle(Brand.accent).font(.system(size: 13))
                }
                Spacer()
                Button {
                    runImport()
                } label: {
                    HStack(spacing: 8) {
                        if importing { ProgressView().controlSize(.small) }
                        Text(importing ? "Importing \(progress.0)/\(progress.1)…" : "Import \(selected.count) selected")
                    }
                }
                .buttonStyle(PrimaryButtonStyle())
                .disabled(selected.isEmpty || importing)
                .accessibilityIdentifier("importSelected")
            }
        }
        .padding(20)
    }

    @ViewBuilder private var body0: some View {
        if locals.isEmpty {
            EmptyState(icon: "checkmark.seal",
                       title: scanned ? "Nothing to import" : "Scanning…",
                       subtitle: scanned ? "Every local skill is already in your registry." : "Looking through your AI tool folders.")
        } else {
            ScrollView {
                LazyVStack(spacing: 0) {
                    ForEach(locals) { sk in
                        row(sk)
                        Divider().overlay(Brand.border).padding(.leading, 48)
                    }
                }
                .padding(.vertical, 4)
            }
        }
    }

    private func row(_ sk: LocalSkill) -> some View {
        Button {
            if selected.contains(sk.slug) { selected.remove(sk.slug) } else { selected.insert(sk.slug) }
        } label: {
            HStack(alignment: .top, spacing: 12) {
                Image(systemName: selected.contains(sk.slug) ? "checkmark.square.fill" : "square")
                    .font(.system(size: 16))
                    .foregroundStyle(selected.contains(sk.slug) ? Brand.accent : Brand.muted)
                    .padding(.top, 1)
                VStack(alignment: .leading, spacing: 3) {
                    HStack(spacing: 8) {
                        Text(sk.name).font(.system(size: 14, weight: .semibold)).foregroundStyle(Brand.fg)
                        Text(sk.slug).font(Brand.monoSized(10)).foregroundStyle(Brand.accent.opacity(0.9))
                    }
                    Text(sk.description).font(.system(size: 12)).foregroundStyle(Brand.muted)
                        .lineLimit(2).fixedSize(horizontal: false, vertical: true)
                    Text(sk.folder).font(Brand.monoSized(10)).foregroundStyle(Brand.meta)
                        .lineLimit(1).truncationMode(.middle)
                }
                Spacer()
            }
            .padding(.horizontal, 20).padding(.vertical, 12)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
    }

    private func rescan() {
        locals = state.scanLocal()
        selected = Set(locals.map(\.slug))
        scanned = true
    }

    private func runImport() {
        let chosen = locals.filter { selected.contains($0.slug) }
        guard !chosen.isEmpty else { return }
        importing = true
        progress = (0, chosen.count)
        Task {
            await state.importSkills(chosen) { done, total in
                Task { @MainActor in self.progress = (done, total) }
            }
            importing = false
            rescan()
        }
    }
}
