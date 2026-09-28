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
    @State private var source = ""
    @State private var fetching = false
    @State private var discovered: [LocalSkill] = []
    @State private var selected: Set<String> = []
    @State private var didFetch = false
    @State private var fetchError: String?
    @State private var showPicker = false
    @State private var publishing = false
    @State private var progress: (Int, Int) = (0, 0)
    @State private var acknowledgedBlock = false

    /// Whether the fetched source is under the import gate.
    private var untrusted: Bool { state.addGate?.untrusted ?? false }

    /// The blocked reviews among the selected skills, if any.
    private var selectedBlocked: [ImportReview] {
        guard let gate = state.addGate else { return [] }
        return selected.compactMap { gate.review(slug: $0) }.filter(\.blocked)
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
                subtitle: pickerSubtitle,
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
            guard state.isDemo, !didFetch, source.isEmpty else { return }
            source = AppState.demoAddSource
            fetch()
        }
    }

    /// The picker subtitle states the registry-only default for untrusted
    /// sources; confirming with zero agents publishes without installing.
    private var pickerSubtitle: String {
        let n = selected.count
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
                .disabled(selected.isEmpty || publishing || fetching
                    || (!selectedBlocked.isEmpty && !acknowledgedBlock))
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
        } else if let fetchError {
            EmptyState(icon: "exclamationmark.triangle",
                       title: "Fetch failed",
                       subtitle: "Couldn't resolve or scan that source — check the path or URL and try again. \(fetchError)")
        } else if discovered.isEmpty {
            EmptyState(icon: didFetch ? "tray" : "square.and.arrow.down",
                       title: didFetch ? "Nothing new to add" : "Fetch a source to begin",
                       subtitle: didFetch
                        ? "No SKILL.md files found, or every discovered skill is already in your registry."
                        : "Enter a source above and press Fetch — we'll list the skills it contains.")
        } else {
            ScrollView {
                LazyVStack(spacing: 0) {
                    if let gate = state.addGate, gate.untrusted {
                        gateBanner(gate)
                        Divider().overlay(Brand.border)
                    }
                    ForEach(discovered) { sk in
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
                GateBlockWarning(review: first, acknowledged: $acknowledgedBlock,
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
        fetchError = nil
        acknowledgedBlock = false
        Task {
            do {
                let found = try await state.resolveAndScan(src, trustedLocalDir: trusted)
                discovered = found
                selected = Set(found.map(\.slug))
            } catch {
                // Fail closed: no stale list survives a failed fetch, and the
                // reason outlives the 3.5s toast in the empty state below.
                discovered = []
                selected = []
                fetchError = error.localizedDescription
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
        // Belt and braces: the Add button stays disabled until a blocker is
        // acknowledged, and publishAndInstall refuses again on its own.
        guard selectedBlocked.isEmpty || acknowledgedBlock else { return }
        publishing = true
        progress = (0, chosen.count)
        Task {
            await state.publishAndInstall(chosen, targets: targets,
                                          allowUnsafe: acknowledgedBlock) { done, total in
                Task { @MainActor in self.progress = (done, total) }
            }
            publishing = false
            acknowledgedBlock = false
            // The temp clone is gone now; clear discovery so stale folder paths
            // aren't reused. Demo has no clone and only simulated the publish,
            // so keep the fixtures (clearing would look like data loss).
            if !state.isDemo {
                discovered = []
                selected = []
            }
            didFetch = true
        }
    }
}
