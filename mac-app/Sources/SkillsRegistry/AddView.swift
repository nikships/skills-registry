import SwiftUI
import SkillsRegistryCore

/// "Add" flow: pull skills from an external source (local path, `owner/repo`,
/// a full GitHub/GitLab/git URL, or a GitHub `{tree|blob}/<ref>/<dir>` folder
/// link), multi-select which to take, then publish them to the registry and
/// durably install them into chosen agents. Mirrors `skills-registry add`.
/// A folder link fetches only that folder over the Contents API, so importing
/// one skill out of a monorepo never clones the repository.
struct AddView: View {
    @EnvironmentObject var state: AppState
    @State private var fetching = false

    @State private var showPicker = false
    @State private var publishing = false
    @State private var progress: (Int, Int) = (0, 0)
    @FocusState private var sourceFocused: Bool

    /// Whether the fetched source is under the import gate.
    private var untrusted: Bool { state.addGate?.untrusted ?? false }

    /// The blocked reviews among the selected skills, if any.
    private var selectedBlocked: [ImportReview] {
        guard let gate = state.addGate else { return [] }
        return state.addPane.selected.compactMap { gate.review(slug: $0) }.filter(\.blocked)
    }

    /// Source, discovery, and selection live in `AppState` so switching
    /// sections or re-theming the accent keeps the fetch and its picks.
    /// Only transient UI (spinners, sheet, progress) stays local.
    private var source: Binding<String> {
        Binding(get: { state.addPane.source }, set: { state.addPane.source = $0 })
    }

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
                subtitle: "\(state.addPane.selected.count) skill\(state.addPane.selected.count == 1 ? "" : "s") will be published to your registry, then installed into the agents you pick.",
                confirmLabel: "Publish + install",
                emptyConfirmLabel: "Publish"
            ) { targets in
                runAdd(targets: targets)
            }
        }
        // Demo mode drives the whole app offline, so the pane arrives with an
        // untrusted source already fetched rather than requiring synthetic
        // keystrokes — the same pattern as Discover's demo auto-search.
        .onAppear {
            guard state.isDemo, !state.addPane.didFetch, source.wrappedValue.isEmpty else { return }
            source.wrappedValue = AppState.demoAddSource
            fetch()
        }
        // Add has a source field (Cmd-F) but nothing to refresh.
        .onChange(of: state.focusSearchRequest) { sourceFocused = true }
    }

    /// The picker subtitle states the registry-only default for untrusted
    /// sources; confirming with zero agents publishes without installing.
    private var pickerSubtitle: String {
        let n = state.addPane.selected.count
        if untrusted {
            return "\(n) skill\(n == 1 ? "" : "s"). \(ImportGate.registryOnlyExplanation)"
        }
        return "\(n) skill\(n == 1 ? "" : "s") will be published to your registry, then installed into the agents you pick."
    }

    private var head: some View {
        VStack(alignment: .leading, spacing: 10) {
            Eyebrow(text: "Add from source")
            Text("Add skills").font(.system(size: 22, weight: .semibold)).foregroundStyle(Brand.fg)
            Text("Pull skills from a local folder, a GitHub `owner/repo`, a full git URL, or a GitHub `/tree/` or `/blob/` folder link (fetched without cloning the repo). Pick what to publish, then install them into your agents.")
                .font(.system(size: 13)).foregroundStyle(Brand.muted)
                .fixedSize(horizontal: false, vertical: true)

            SearchField(icon: "link",
                        placeholder: "owner/repo · https://github.com/… · ./local/path",
                        text: source, focused: $sourceFocused,
                        accessibilityID: "addSourceField") { fetch() }

            HStack(spacing: 10) {
                Button { fetch() } label: {
                    HStack(spacing: 8) {
                        if fetching { ProgressView().controlSize(.small) }
                        Text(fetching ? "Fetching…" : "Fetch")
                    }
                }
                .buttonStyle(PrimaryButtonStyle())
                .disabled(state.addPane.source.trimmingCharacters(in: .whitespaces).isEmpty || fetching || publishing)
                .accessibilityIdentifier("addFetch")

                Button { chooseLocalFolder() } label: {
                    Label("Browse…", systemImage: "folder")
                }
                .buttonStyle(GhostButtonStyle())
                .disabled(fetching || publishing)

                if !state.addPane.discovered.isEmpty {
                    Button {
                        state.addPane.selected = state.addPane.selected.count == state.addPane.discovered.count ? [] : Set(state.addPane.discovered.map(\.slug))
                    } label: {
                        Text(state.addPane.selected.count == state.addPane.discovered.count ? "Deselect all" : "Select all")
                    }.buttonStyle(.plain).foregroundStyle(Brand.accent).font(.system(size: 13))
                }
                Spacer()
                Button { showPicker = true } label: {
                    HStack(spacing: 8) {
                        if publishing { ProgressView().controlSize(.small) }
                        Text(publishing ? "Adding \(progress.0)/\(progress.1)…" : "Add \(state.addPane.selected.count) selected")
                    }
                }
                .buttonStyle(PrimaryButtonStyle())
                .disabled(state.addPane.selected.isEmpty || publishing || fetching
                    || (!selectedBlocked.isEmpty && !state.addPane.acknowledgedBlock))
                .accessibilityIdentifier("addSelected")
            }
        }
        .padding(20)
    }

    @ViewBuilder private var results: some View {
        if fetching && state.addPane.discovered.isEmpty {
            EmptyState(icon: "square.and.arrow.down",
                       title: "Fetching…",
                       subtitle: "Resolving the source and scanning it for skills.")
        } else if let fetchError = state.addPane.fetchError {
            EmptyState(icon: "exclamationmark.triangle",
                       title: "Fetch failed",
                       subtitle: "Couldn't resolve or scan that source — check the path or URL and try again. \(fetchError)")
        } else if state.addPane.discovered.isEmpty {
            EmptyState(icon: state.addPane.didFetch ? "tray" : "square.and.arrow.down",
                       title: state.addPane.didFetch ? "Nothing new to add" : "Fetch a source to begin",
                       subtitle: state.addPane.didFetch
                        ? "No SKILL.md files found, or every discovered skill is already in your registry."
                        : "Enter a source above and press Fetch — we'll list the skills it contains.")
        } else {
            ScrollView {
                LazyVStack(spacing: 0) {
                    if let gate = state.addGate, gate.untrusted {
                        gateBanner(gate)
                        Divider().overlay(Brand.border)
                    }
                    ForEach(state.addPane.discovered) { sk in
                        row(sk)
                        Divider().overlay(Brand.border).padding(.leading, 48)
                    }
                }
                .padding(.vertical, 4)
            }
        }
    }

    /// Distinct hits across the gate's reviews. Identical lines (the demo
    /// fixture stamps the same file onto every skill) are listed once; the
    /// acknowledgement still covers every selected skill that matched.
    private func scanRows(_ gate: AddGate) -> [(slug: String, finding: SkillFinding)] {
        var seen = Set<String>()
        var out: [(slug: String, finding: SkillFinding)] = []
        for review in gate.reviews {
            for finding in review.findings {
                let key = "\(finding.rule)\u{0}\(finding.line)\u{0}\(finding.excerpt)"
                guard seen.insert(key).inserted else { continue }
                out.append((review.slug, finding))
            }
        }
        return out
    }

    /// The import-gate banner for an untrusted source: the origin, the
    /// index's grades (or the unscored disclaimer when the index has no row),
    /// the local scan, the registry-only default, and the acknowledgement
    /// when a selected skill is blocked. Mirrors the CLI's `renderGate`.
    private func gateBanner(_ gate: AddGate) -> some View {
        VStack(alignment: .leading, spacing: 10) {
            HStack(spacing: 8) {
                Image(systemName: "exclamationmark.shield.fill")
                    .font(.system(size: 13)).foregroundStyle(Brand.warn)
                Text("Untrusted source — \(gate.assessment.reason)")
                    .font(.system(size: 13, weight: .semibold)).foregroundStyle(Brand.fg)
                    .fixedSize(horizontal: false, vertical: true)
            }
            Text("Public skill index grades:")
                .font(.system(size: 12, weight: .medium)).foregroundStyle(Brand.fg2)
            ForEach(gate.scores.lines, id: \.name) { line in
                HStack(spacing: 8) {
                    Text(line.name).font(Brand.monoSized(11)).foregroundStyle(Brand.muted)
                        .frame(width: 96, alignment: .leading)
                    GradeBadge(level: line.level)
                    Spacer()
                }
            }
            if !gate.indexed {
                Text("(the index has no row for this folder; unscored means unvetted, not safe)")
                    .font(.system(size: 11)).foregroundStyle(Brand.meta)
                    .fixedSize(horizontal: false, vertical: true)
            } else {
                Text(ImportGate.gradeDisclaimer)
                    .font(.system(size: 11)).foregroundStyle(Brand.meta)
                    .fixedSize(horizontal: false, vertical: true)
            }
            Text("Default: publish to your registry only. No agent folder is written unless you opt in, and nothing under scripts/ is ever run.")
                .font(.system(size: 11)).foregroundStyle(Brand.meta)
                .fixedSize(horizontal: false, vertical: true)
            ScanFindingsList(rows: scanRows(gate))
            .accessibilityIdentifier("addScanFindings")
            if let first = selectedBlocked.first {
                GateBlockWarning(review: first, acknowledged: Binding(
                    get: { state.addPane.acknowledgedBlock },
                    set: { state.addPane.acknowledgedBlock = $0 }),
                                 toggleID: "addAllowUnsafe")
            }
        }
        .padding(16)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(Brand.surfaceWarm)
        .accessibilityIdentifier("addGateBanner")
    }

    private func row(_ sk: LocalSkill) -> some View {
        Button {
            if state.addPane.selected.contains(sk.slug) { state.addPane.selected.remove(sk.slug) } else { state.addPane.selected.insert(sk.slug) }
        } label: {
            HStack(alignment: .top, spacing: 12) {
                Image(systemName: state.addPane.selected.contains(sk.slug) ? "checkmark.square.fill" : "square")
                    .font(.system(size: 16))
                    .foregroundStyle(state.addPane.selected.contains(sk.slug) ? Brand.accent : Brand.muted)
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
        let src = state.addPane.source.trimmingCharacters(in: .whitespaces)
        guard !src.isEmpty else { return }
        fetching = true
        state.addPane.fetchError = nil
        state.addPane.acknowledgedBlock = false
        Task {
            do {
                let found = try await state.resolveAndScan(src, trustedLocalDir: trusted)
                state.addPane = state.addPane.fetched(found)
            } catch {
                // Fail closed: no stale list survives a failed fetch, and the
                // reason outlives the 3.5s toast in the empty state below.
                state.addPane = state.addPane.fetched(nil, error: error.localizedDescription)
            }
            state.addPane.didFetch = true
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
            state.addPane.source = url.path
            fetch(trusted: true)
        }
    }

    private func runAdd(targets: [AgentTarget]) {
        let chosen = state.addPane.discovered.filter { state.addPane.selected.contains($0.slug) }
        guard !chosen.isEmpty else { return }
        // Belt and braces: the Add button stays disabled until a blocker is
        // acknowledged, and publishAndInstall refuses again on its own.
        guard selectedBlocked.isEmpty || state.addPane.acknowledgedBlock else { return }
        publishing = true
        progress = (0, chosen.count)
        Task {
            await state.publishAndInstall(chosen, targets: targets,
                                          allowUnsafe: state.addPane.acknowledgedBlock) { done, total in
                Task { @MainActor in self.progress = (done, total) }
            }
            publishing = false
            state.addPane.acknowledgedBlock = false
            // The temp clone is gone now; clear discovery so stale folder paths
            // aren't reused. Demo has no clone and only simulated the publish,
            // so keep the fixtures (clearing would look like data loss).
            state.addPane = state.addPane.published(keepList: state.isDemo)
            state.addPane.didFetch = true
        }
    }
}
