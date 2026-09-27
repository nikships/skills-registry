import SwiftUI
import AppKit
import SkillsRegistryCore

struct BrowseView: View {
    @EnvironmentObject var state: AppState
    @Binding var section: NavSection
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
            } else if filtered.isEmpty && !query.isEmpty {
                EmptyState(icon: "tray", title: "No matches", subtitle: "Try a different search term.")
            } else if state.skills.isEmpty {
                WelcomeCard(section: $section) { publish() }
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

/// First-run card shown when the registry has no skills yet: names the
/// registry so the user knows where they are, and offers the three ways to
/// fill it. Import/Discover deep-link to their sidebar sections; Publish
/// reuses the same folder picker as the list header.
struct WelcomeCard: View {
    @EnvironmentObject var state: AppState
    @Binding var section: NavSection
    let onPublish: () -> Void

    var body: some View {
        VStack {
            Spacer()
            Card {
                VStack(alignment: .leading, spacing: 14) {
                    Image(systemName: "square.stack.3d.up.fill")
                        .font(.system(size: 28))
                        .foregroundStyle(Brand.accent)
                    VStack(alignment: .leading, spacing: 6) {
                        Text("Welcome to your registry")
                            .font(.system(size: 16, weight: .semibold))
                            .foregroundStyle(Brand.fg)
                        if let repo = state.repo {
                            Link(destination: repo.htmlURL) {
                                Text(repo.fullName)
                                    .font(Brand.monoSized(12))
                                    .foregroundStyle(Brand.accent)
                            }
                            .accessibilityIdentifier("welcomeRepoLink")
                        }
                    }
                    Text("Your registry is empty. Pick a starting point — each takes under a minute.")
                        .font(.system(size: 13))
                        .foregroundStyle(Brand.muted)
                        .fixedSize(horizontal: false, vertical: true)
                    VStack(spacing: 8) {
                        welcomeButton(title: "Import local skills",
                                      subtitle: "Bulk-import from your agent folders",
                                      systemImage: "tray.and.arrow.down",
                                      id: "welcomeImport") { section = .importLocal }
                        welcomeButton(title: "Publish a folder",
                                      subtitle: "Publish a SKILL.md folder",
                                      systemImage: "plus",
                                      id: "welcomePublish") { onPublish() }
                        welcomeButton(title: "Discover public skills",
                                      subtitle: "Search the public index",
                                      systemImage: "sparkle.magnifyingglass",
                                      id: "welcomeDiscover") { section = .discover }
                    }
                }
            }
            .padding(.horizontal, 14)
            .accessibilityIdentifier("welcomeCard")
            Spacer()
        }
    }

    private func welcomeButton(title: String, subtitle: String, systemImage: String,
                               id: String, action: @escaping () -> Void) -> some View {
        Button(action: action) {
            HStack(spacing: 10) {
                Image(systemName: systemImage)
                    .font(.system(size: 14))
                    .foregroundStyle(Brand.accent)
                    .frame(width: 20)
                VStack(alignment: .leading, spacing: 1) {
                    Text(title)
                        .font(.system(size: 13, weight: .semibold))
                        .foregroundStyle(Brand.fg)
                    Text(subtitle)
                        .font(.system(size: 11))
                        .foregroundStyle(Brand.muted)
                }
                Spacer()
                Image(systemName: "chevron.right")
                    .font(.system(size: 11, weight: .semibold))
                    .foregroundStyle(Brand.meta)
            }
            .padding(.horizontal, 12)
            .padding(.vertical, 10)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(Brand.surfaceWarm)
            .overlay(RoundedRectangle(cornerRadius: 8).strokeBorder(Brand.border, lineWidth: 1))
            .clipShape(RoundedRectangle(cornerRadius: 8))
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .accessibilityIdentifier(id)
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
