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
                    // While a non-empty list refreshes, the count doubles as
                    // a loading status so the activity reads even in a still.
                    Text(state.skillsLoading && !state.skills.isEmpty ? "Refreshing…" : "\(filtered.count) skill\(filtered.count == 1 ? "" : "s")")
                        .font(Brand.monoSized(11)).foregroundStyle(Brand.muted)
                    Spacer()
                    Button { Task { await state.refreshSkills() } } label: {
                        Image(systemName: "arrow.clockwise").font(.system(size: 11))
                            .rotationEffect(.degrees(state.skillsLoading ? 360 : 0))
                            .animation(
                                state.skillsLoading
                                    ? .linear(duration: 0.8).repeatForever(autoreverses: false)
                                    : .default,
                                value: state.skillsLoading)
                            // Static dim to go with the spin, so the loading
                            // state reads even in a still frame.
                            .opacity(state.skillsLoading ? 0.45 : 1)
                    }
                    .buttonStyle(.plain).foregroundStyle(Brand.muted)
                    .disabled(state.skillsLoading)
                    .accessibilityIdentifier("refreshButton")
                    Button { publish() } label: {
                        Label("Publish", systemImage: "plus").font(.system(size: 11, weight: .medium))
                    }
                    .buttonStyle(.plain).foregroundStyle(Brand.accent)
                    .accessibilityIdentifier("publishButton")
                }

                // A failed refresh keeps the stale list on screen and surfaces
                // the failure here, with the full error on hover.
                if state.skillsError != nil, !state.skills.isEmpty {
                    HStack(spacing: 8) {
                        Image(systemName: "exclamationmark.triangle.fill")
                            .font(.system(size: 11)).foregroundStyle(Brand.danger)
                        Text("Refresh failed")
                            .font(.system(size: 12, weight: .medium)).foregroundStyle(Brand.fg2)
                        Spacer()
                        Button("Retry") { Task { await state.refreshSkills() } }
                            .buttonStyle(.plain)
                            .font(.system(size: 12, weight: .semibold)).foregroundStyle(Brand.accent)
                            .accessibilityIdentifier("refreshRetryButton")
                    }
                    .padding(.horizontal, 10).padding(.vertical, 7)
                    .background(Brand.danger.opacity(0.12))
                    .overlay(RoundedRectangle(cornerRadius: 8).strokeBorder(Brand.danger.opacity(0.45), lineWidth: 1))
                    .clipShape(RoundedRectangle(cornerRadius: 8))
                    .help(state.skillsError ?? "")
                }
            }
            .padding(14)

            Divider().overlay(Brand.border)

            if state.skillsLoading && state.skills.isEmpty {
                VStack { Spacer(); ProgressView().tint(Brand.accent); Spacer() }
            } else if state.skills.isEmpty, let err = state.skillsError {
                // No stale list to keep — error with a retry below it.
                VStack(spacing: 0) {
                    EmptyState(icon: "exclamationmark.triangle", title: "Couldn't load skills", subtitle: err)
                    Button("Retry") { Task { await state.refreshSkills() } }
                        .buttonStyle(GhostButtonStyle())
                        .padding(.bottom, 24)
                        .accessibilityIdentifier("refreshRetryButtonEmpty")
                }
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
