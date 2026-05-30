import SwiftUI
import AppKit
import SkillsRegistryCore

struct BrowseView: View {
    @EnvironmentObject var state: AppState
    @State private var query = ""
    @State private var selected: String?

    private var filtered: [SkillSummary] {
        let q = query.trimmingCharacters(in: .whitespaces)
        if q.isEmpty { return state.skills.sorted { $0.slug < $1.slug } }
        return scoreAndSort(state.skills, query: q)
    }

    var body: some View {
        HStack(spacing: 0) {
            listColumn
            Divider().overlay(Brand.border)
            detailColumn
        }
        .onChange(of: state.skills) { _, skills in
            // Reset the detail pane if the selected skill is gone (e.g. removed).
            if let s = selected, !skills.contains(where: { $0.slug == s }) {
                selected = nil
            }
        }
    }

    private var listColumn: some View {
        VStack(spacing: 0) {
            // Search + actions
            VStack(spacing: 10) {
                HStack(spacing: 8) {
                    Image(systemName: "magnifyingglass").font(.system(size: 12)).foregroundStyle(Brand.muted)
                    TextField("Search skills…", text: $query)
                        .textFieldStyle(.plain).font(.system(size: 13))
                        .accessibilityIdentifier("searchField")
                    if !query.isEmpty {
                        Button { query = "" } label: { Image(systemName: "xmark.circle.fill") }
                            .buttonStyle(.plain).foregroundStyle(Brand.meta)
                    }
                }
                .padding(.horizontal, 12).padding(.vertical, 9)
                .background(Brand.surfaceWarm)
                .overlay(RoundedRectangle(cornerRadius: 8).strokeBorder(Brand.border, lineWidth: 1))
                .clipShape(RoundedRectangle(cornerRadius: 8))

                HStack {
                    Text("\(filtered.count) skill\(filtered.count == 1 ? "" : "s")")
                        .font(Brand.monoSized(11)).foregroundStyle(Brand.muted)
                    Spacer()
                    Button { Task { await state.refreshSkills() } } label: {
                        Image(systemName: "arrow.clockwise").font(.system(size: 11))
                    }.buttonStyle(.plain).foregroundStyle(Brand.muted)
                    Button { publish() } label: {
                        Label("Publish", systemImage: "plus").font(.system(size: 11, weight: .medium))
                    }
                    .buttonStyle(.plain).foregroundStyle(Brand.accent)
                    .accessibilityIdentifier("publishButton")
                }
            }
            .padding(14)

            Divider().overlay(Brand.border)

            if state.skillsLoading && state.skills.isEmpty {
                VStack { Spacer(); ProgressView().tint(Brand.accent); Spacer() }
            } else if let err = state.skillsError {
                EmptyState(icon: "exclamationmark.triangle", title: "Couldn't load skills", subtitle: err)
            } else if filtered.isEmpty {
                EmptyState(icon: "tray", title: query.isEmpty ? "No skills yet" : "No matches",
                           subtitle: query.isEmpty ? "Publish one, or import your local skills." : "Try a different search term.")
            } else {
                ScrollView {
                    LazyVStack(spacing: 0) {
                        ForEach(filtered) { skill in
                            SkillRow(skill: skill, selected: selected == skill.slug)
                                .onTapGesture {
                                    withAnimation(.easeInOut(duration: 0.2)) { selected = skill.slug }
                                }
                            Divider().overlay(Brand.border).padding(.leading, 14)
                        }
                    }
                }
                .animation(.easeInOut(duration: 0.2), value: query)
            }
        }
        .frame(width: 340)
        .background(Brand.bg)
    }

    @ViewBuilder private var detailColumn: some View {
        ZStack {
            if let slug = selected {
                SkillDetailView(slug: slug).id(slug)
                    .transition(.opacity)
            } else {
                EmptyState(icon: "doc.richtext",
                           title: "Select a skill",
                           subtitle: "Pick a skill on the left to read its SKILL.md with full markdown rendering.")
                    .transition(.opacity)
            }
        }
    }

    private func publish() {
        let panel = NSOpenPanel()
        panel.canChooseDirectories = true
        panel.canChooseFiles = false
        panel.allowsMultipleSelection = false
        panel.prompt = "Publish"
        panel.message = "Choose a folder containing a SKILL.md"
        if panel.runModal() == .OK, let url = panel.url {
            Task { await state.publishFolder(url) }
        }
    }
}

struct SkillRow: View {
    let skill: SkillSummary
    let selected: Bool
    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack(spacing: 8) {
                Text(skill.name).font(.system(size: 13, weight: .semibold)).foregroundStyle(Brand.fg)
                    .lineLimit(1)
                Spacer()
            }
            Text(skill.slug).font(Brand.monoSized(10)).foregroundStyle(Brand.accent.opacity(0.9))
            Text(skill.description).font(.system(size: 12)).foregroundStyle(Brand.muted)
                .lineLimit(2).fixedSize(horizontal: false, vertical: true)
        }
        .padding(.horizontal, 14).padding(.vertical, 11)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(selected ? Brand.surfaceRaised : Color.clear)
        .contentShape(Rectangle())
    }
}
