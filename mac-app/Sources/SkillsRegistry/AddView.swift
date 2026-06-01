import SwiftUI
import SkillsRegistryCore

/// "Add" flow: pull skills from an external source (local path, `owner/repo`,
/// a full GitHub/GitLab/git URL, or a GitHub `/tree/<ref>/<subpath>` deep
/// link), multi-select which to take, then publish them to the registry and
/// durably install them into chosen agents. Mirrors `skills-registry add`.
struct AddView: View {
    @EnvironmentObject var state: AppState
    @State private var source = ""
    @State private var fetching = false
    @State private var discovered: [LocalSkill] = []
    @State private var selected: Set<String> = []
    @State private var didFetch = false
    @State private var fetchFailed = false
    @State private var showPicker = false
    @State private var publishing = false
    @State private var progress: (Int, Int) = (0, 0)

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            head
            Divider().overlay(Brand.border)
            results
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
        .background(Brand.bg)
        .sheet(isPresented: $showPicker) {
            AgentPickerSheet(
                title: "Install into which agents?",
                subtitle: "\(selected.count) skill\(selected.count == 1 ? "" : "s") will be published to your registry, then installed into the agents you pick.",
                confirmLabel: "Publish + install"
            ) { targets in
                runAdd(targets: targets)
            }
        }
    }

    private var head: some View {
        VStack(alignment: .leading, spacing: 10) {
            Eyebrow(text: "Add from source")
            Text("Add skills").font(.system(size: 22, weight: .semibold)).foregroundStyle(Brand.fg)
            Text("Pull skills from a local folder, a GitHub `owner/repo`, a full git URL, or a GitHub `/tree/<branch>/<path>` link. Pick what to publish, then install them into your agents.")
                .font(.system(size: 13)).foregroundStyle(Brand.muted)
                .fixedSize(horizontal: false, vertical: true)

            HStack(spacing: 8) {
                Image(systemName: "link").font(.system(size: 12)).foregroundStyle(Brand.muted)
                TextField("owner/repo · https://github.com/… · ./local/path", text: $source)
                    .textFieldStyle(.plain).font(.system(size: 13))
                    .onSubmit { fetch() }
                    .accessibilityIdentifier("addSourceField")
                if !source.isEmpty {
                    Button { source = "" } label: { Image(systemName: "xmark.circle.fill") }
                        .buttonStyle(.plain).foregroundStyle(Brand.meta)
                }
            }
            .padding(.horizontal, 12).padding(.vertical, 9)
            .background(Brand.surfaceWarm)
            .overlay(RoundedRectangle(cornerRadius: 8).strokeBorder(Brand.border, lineWidth: 1))
            .clipShape(RoundedRectangle(cornerRadius: 8))

            HStack(spacing: 10) {
                Button { fetch() } label: {
                    HStack(spacing: 8) {
                        if fetching { ProgressView().controlSize(.small) }
                        Text(fetching ? "Fetching…" : "Fetch")
                    }
                }
                .buttonStyle(PrimaryButtonStyle())
                .disabled(source.trimmingCharacters(in: .whitespaces).isEmpty || fetching || publishing)
                .accessibilityIdentifier("addFetch")

                Button { chooseLocalFolder() } label: {
                    Label("Browse…", systemImage: "folder")
                }
                .buttonStyle(GhostButtonStyle())
                .disabled(fetching || publishing)

                if !discovered.isEmpty {
                    Button {
                        selected = selected.count == discovered.count ? [] : Set(discovered.map(\.slug))
                    } label: {
                        Text(selected.count == discovered.count ? "Deselect all" : "Select all")
                    }.buttonStyle(.plain).foregroundStyle(Brand.accent).font(.system(size: 13))
                }
                Spacer()
                Button { showPicker = true } label: {
                    HStack(spacing: 8) {
                        if publishing { ProgressView().controlSize(.small) }
                        Text(publishing ? "Adding \(progress.0)/\(progress.1)…" : "Add \(selected.count) selected")
                    }
                }
                .buttonStyle(PrimaryButtonStyle())
                .disabled(selected.isEmpty || publishing || fetching)
                .accessibilityIdentifier("addSelected")
            }
        }
        .padding(20)
    }

    @ViewBuilder private var results: some View {
        if fetching && discovered.isEmpty {
            EmptyState(icon: "square.and.arrow.down",
                       title: "Fetching…",
                       subtitle: "Resolving the source and scanning it for skills.")
        } else if fetchFailed {
            EmptyState(icon: "exclamationmark.triangle",
                       title: "Fetch failed",
                       subtitle: "Couldn't resolve or scan that source — check the path or URL and try again.")
        } else if discovered.isEmpty {
            EmptyState(icon: didFetch ? "tray" : "square.and.arrow.down",
                       title: didFetch ? "Nothing new to add" : "Fetch a source to begin",
                       subtitle: didFetch
                        ? "No SKILL.md files found, or every discovered skill is already in your registry."
                        : "Enter a source above and press Fetch — we'll list the skills it contains.")
        } else {
            ScrollView {
                LazyVStack(spacing: 0) {
                    ForEach(discovered) { sk in
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
                }
                Spacer()
            }
            .padding(.horizontal, 20).padding(.vertical, 12)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
    }

    /// `trusted` skips the relative-only path guard for a directory the user
    /// picked via the native panel (which always yields an absolute path).
    private func fetch(trusted: Bool = false) {
        // onSubmit bypasses the disabled buttons, so guard here too: re-running
        // mid-publish would tear down the temp clone the publish is reading.
        guard !fetching && !publishing else { return }
        let src = source.trimmingCharacters(in: .whitespaces)
        guard !src.isEmpty else { return }
        fetching = true
        fetchFailed = false
        Task {
            if let found = await state.resolveAndScan(src, trustedLocalDir: trusted) {
                discovered = found
                selected = Set(found.map(\.slug))
            } else {
                discovered = []
                selected = []
                fetchFailed = true
            }
            didFetch = true
            fetching = false
        }
    }

    private func chooseLocalFolder() {
        let panel = NSOpenPanel()
        panel.canChooseDirectories = true
        panel.canChooseFiles = false
        panel.allowsMultipleSelection = false
        panel.prompt = "Use folder"
        panel.message = "Choose a folder containing skills"
        if panel.runModal() == .OK, let url = panel.url {
            source = url.path
            fetch(trusted: true)
        }
    }

    private func runAdd(targets: [AgentTarget]) {
        let chosen = discovered.filter { selected.contains($0.slug) }
        guard !chosen.isEmpty else { return }
        publishing = true
        progress = (0, chosen.count)
        Task {
            await state.publishAndInstall(chosen, targets: targets) { done, total in
                Task { @MainActor in self.progress = (done, total) }
            }
            publishing = false
            // The temp clone is gone now; clear discovery so stale folder paths
            // aren't reused.
            discovered = []
            selected = []
            didFetch = true
        }
    }
}
