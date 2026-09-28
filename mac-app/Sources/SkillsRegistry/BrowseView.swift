import SwiftUI
import AppKit
import SkillsRegistryCore

struct BrowseView: View {
    @EnvironmentObject var state: AppState

    @Binding var section: NavSection

    /// Search text + selection live in `AppState` so switching sections or
    /// re-theming the accent keeps the list filtered and the detail open.
    private var query: Binding<String> {
        Binding(get: { state.browsePane.query }, set: { state.browsePane.query = $0 })
    }
    private var selected: Binding<String?> {
        Binding(get: { state.browsePane.selectedSlug }, set: { state.browsePane.selectedSlug = $0 })
    }
    @FocusState private var searchFocused: Bool

    private var filtered: [SkillSummary] {
        let q = state.browsePane.query.trimmingCharacters(in: .whitespaces)
        if q.isEmpty { return state.skills.sorted { $0.slug < $1.slug } }
        // Browse shows every match (like the CLI list TUI), so rank without
        // the headless-search top-N cap; the header count stays truthful.
        return scoreAndSort(state.skills, query: q, limit: state.skills.count)
    }

    var body: some View {
        HStack(spacing: 0) {
            listColumn
            Divider().overlay(Brand.border)
            detailColumn
        }
        .onChange(of: state.skills) { _, skills in
            // Reset the detail pane if the selected skill is gone (e.g. removed).
            if let s = state.browsePane.selectedSlug, !skills.contains(where: { $0.slug == s }) {
                state.browsePane.selectedSlug = nil
            }
        }
        .task {
            // Demo-only screenshot hook: preselect a skill so the detail pane
            // (including its compact layout) can be captured without clicks.
            if state.browsePane.selectedSlug == nil, let slug = DemoHooks.preselectedSkill,
               state.skills.contains(where: { $0.slug == slug }) {
                state.browsePane.selectedSlug = slug
            }
        }
        // Cmd-F focuses search, Cmd-R refreshes the list (design-a11y-4).
        .onChange(of: state.focusSearchRequest) { searchFocused = true }
        .onChange(of: state.refreshRequest) { Task { await state.refreshSkills() } }
    }

    private var listColumn: some View {
        VStack(spacing: 0) {
            // Search + actions
            VStack(spacing: 10) {
                SearchField(icon: "magnifyingglass", placeholder: "Search skills…",
                            text: query, focused: $searchFocused,
                            accessibilityID: "searchField")

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
                    .accessibilityLabel("Refresh skills")
                    .help("Refresh skill list")
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
            } else if state.skills.isEmpty {
                WelcomeCard(section: $section) { publish() }
            } else if filtered.isEmpty {
                EmptyState(icon: "tray", title: state.browsePane.query.isEmpty ? "No skills yet" : "No matches",
                           subtitle: state.browsePane.query.isEmpty ? "Publish one, or import your local skills." : "Try a different search term.")
            } else {
                ScrollView {
                    LazyVStack(spacing: 0) {
                        ForEach(filtered) { skill in
                            ListRowButton(
                                selected: state.browsePane.selectedSlug == skill.slug,
                                hint: "Selects this skill",
                                identifier: "skillRow-\(skill.slug)",
                                previewHover: state.demoHoverPreview && skill.slug == filtered.first?.slug,
                                action: {
                                    withAnimation(.easeInOut(duration: 0.2)) { state.browsePane.selectedSlug = skill.slug }
                                }
                            ) {
                                SkillRow(skill: skill)
                            }
                            Divider().overlay(Brand.border).padding(.leading, 14)
                        }
                    }
                }
                .animation(.easeInOut(duration: 0.2), value: state.browsePane.query)
            }
        }
        .frame(width: 340)
        .background(Brand.bg)
    }

    @ViewBuilder private var detailColumn: some View {
        ZStack {
            if let slug = state.browsePane.selectedSlug {
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
    }
}
