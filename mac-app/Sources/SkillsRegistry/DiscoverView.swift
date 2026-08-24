import SwiftUI
import AppKit
import SkillsRegistryCore

/// "Discover" pane: search the public SkillNet index and import one row into
/// the registry. Counterpart to Browse (which lists only the user's own
/// registry) and Add (which takes a URL the user already has).
///
/// The index is read through `DiscoverClient`, the same JSON contract
/// `skills-registry discover --json` publishes, so the pane needs neither the
/// CLI binary nor a credential. Nothing is written until the user confirms an
/// import: searching, selecting, and previewing a row are all read-only.
struct DiscoverView: View {
    @EnvironmentObject var state: AppState
    @State private var query = ""
    @State private var mode: DiscoverMode = .keyword
    @State private var results: [DiscoverResult] = []
    @State private var selected: DiscoverResult?
    @State private var searching = false
    @State private var didSearch = false
    @State private var searchError: String?
    @State private var importing = false
    @State private var pending: PendingImport?
    @State private var searchTask: Task<Void, Never>?

    /// The query demo mode arrives with.
    private static let demoQuery = "pdf"

    /// A row the user asked to import, held while the confirmation sheet is up.
    /// `installIntoAgents` starts false, which is what makes registry-only the
    /// default rather than a setting the user has to find.
    private struct PendingImport: Identifiable {
        let result: DiscoverResult
        var installIntoAgents = false
        var acknowledgedBlock = false
        var id: String { result.id }
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            head
            Divider().overlay(Brand.border)
            HStack(spacing: 0) {
                resultsColumn
                Divider().overlay(Brand.border)
                detailColumn
            }
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
        .background(Brand.bg)
        .sheet(item: $pending) { item in
            confirmSheet(item)
        }
        // Demo mode drives the whole app offline, so the pane arrives with a
        // query already run rather than requiring synthetic keystrokes.
        .onAppear {
            guard state.isDemo, !didSearch, query.isEmpty else { return }
            query = Self.demoQuery
            search()
        }
        .onDisappear { searchTask?.cancel() }
    }

    // MARK: - head

    private var head: some View {
        VStack(alignment: .leading, spacing: 10) {
            Eyebrow(text: "Public skill index")
            Text("Discover skills").font(.system(size: 22, weight: .semibold)).foregroundStyle(Brand.fg)
            Text("Search a public index of third-party skills and import one into your registry. Browse lists only your own registry; Add takes a URL you already have.")
                .font(.system(size: 13)).foregroundStyle(Brand.muted)
                .fixedSize(horizontal: false, vertical: true)

            HStack(spacing: 8) {
                Image(systemName: "sparkle.magnifyingglass").font(.system(size: 12)).foregroundStyle(Brand.muted)
                TextField("pdf · summarize a youtube video · kubernetes", text: $query)
                    .textFieldStyle(.plain).font(.system(size: 13))
                    .onSubmit { search() }
                    .accessibilityIdentifier("discoverQueryField")
                if !query.isEmpty {
                    Button { query = "" } label: { Image(systemName: "xmark.circle.fill") }
                        .buttonStyle(.plain).foregroundStyle(Brand.meta)
                }
            }
            .padding(.horizontal, 12).padding(.vertical, 9)
            .background(Brand.surfaceWarm)
            .overlay(RoundedRectangle(cornerRadius: 8).strokeBorder(Brand.border, lineWidth: 1))
            .clipShape(RoundedRectangle(cornerRadius: 8))

            HStack(spacing: 10) {
                Button { search() } label: {
                    HStack(spacing: 8) {
                        if searching { ProgressView().controlSize(.small) }
                        Text(searching ? "Searching…" : "Search")
                    }
                }
                .buttonStyle(PrimaryButtonStyle())
                .disabled(query.trimmingCharacters(in: .whitespaces).isEmpty || searching || importing)
                .accessibilityIdentifier("discoverSearch")

                modeToggle
                Spacer()
                if !results.isEmpty {
                    Text("\(results.count) result\(results.count == 1 ? "" : "s")")
                        .font(Brand.monoSized(11)).foregroundStyle(Brand.muted)
                }
            }
        }
        .padding(20)
    }

    /// Keyword vs vector ranking. Switching mode re-runs a query that already
    /// returned, so the toggle reads as a property of the search rather than a
    /// setting to remember to apply.
    private var modeToggle: some View {
        HStack(spacing: 2) {
            ForEach(DiscoverMode.allCases) { m in
                Button {
                    guard mode != m else { return }
                    mode = m
                    if didSearch { search() }
                } label: {
                    Text(m.label)
                        .font(.system(size: 12, weight: .medium))
                        .padding(.horizontal, 10).padding(.vertical, 5)
                        .foregroundStyle(mode == m ? Brand.fg : Brand.muted)
                        .background(mode == m ? Brand.surfaceRaised : Color.clear)
                        .clipShape(RoundedRectangle(cornerRadius: 6))
                        .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                .help(m.hint)
                .accessibilityIdentifier("discoverMode-\(m.rawValue)")
            }
        }
        .padding(2)
        .background(Brand.surfaceWarm)
        .overlay(RoundedRectangle(cornerRadius: 8).strokeBorder(Brand.border, lineWidth: 1))
        .clipShape(RoundedRectangle(cornerRadius: 8))
    }

    // MARK: - results

    @ViewBuilder private var resultsColumn: some View {
        VStack(spacing: 0) {
            resultsBody
        }
        .frame(width: 340)
        .background(Brand.bg)
    }

    /// An unreachable index and an index with no match must never look alike,
    /// so a failed search renders the error and no list at all.
    @ViewBuilder private var resultsBody: some View {
        if searching && results.isEmpty {
            VStack { Spacer(); ProgressView().tint(Brand.accent); Spacer() }
        } else if let searchError {
            errorState(searchError)
        } else if results.isEmpty {
            EmptyState(icon: didSearch ? "magnifyingglass" : "sparkle.magnifyingglass",
                       title: didSearch ? "Nothing matched" : "Search the index",
                       subtitle: didSearch
                        ? "The index had no hit for that. Try \(DiscoverMode.vector.label) mode to search by meaning instead of literal terms."
                        : "Type what you need above. Results carry the index's own grades and an importable GitHub URL.")
        } else {
            ScrollView {
                LazyVStack(spacing: 0) {
                    ForEach(results) { row in
                        // A tap gesture rather than a Button, matching how
                        // BrowseView selects a row: a plain Button publishes
                        // one opaque element and drops the row's name, grade,
                        // and description out of the accessibility tree, which
                        // both VoiceOver and the UI driver that verifies this
                        // pane read.
                        DiscoverRow(result: row, selected: selected?.id == row.id)
                            .onTapGesture {
                                withAnimation(.easeInOut(duration: 0.2)) { selected = row }
                            }
                            .accessibilityIdentifier("discoverRow-\(row.name)")
                        Divider().overlay(Brand.border).padding(.leading, 14)
                    }
                }
            }
        }
    }

    /// Inline failure. The pane stays usable: the query field, the mode
    /// toggle, and every other section are untouched, and the message names
    /// the Add pane as the way to import without the index.
    private func errorState(_ message: String) -> some View {
        VStack(alignment: .leading, spacing: 10) {
            HStack(spacing: 8) {
                Image(systemName: "exclamationmark.triangle.fill")
                    .font(.system(size: 13)).foregroundStyle(Brand.danger)
                Text("Index unavailable").font(.system(size: 14, weight: .semibold)).foregroundStyle(Brand.fg)
            }
            Text(message).font(.system(size: 12)).foregroundStyle(Brand.muted)
                .fixedSize(horizontal: false, vertical: true)
                .textSelection(.enabled)
            Text(DiscoverError.fallbackHint).font(.system(size: 12)).foregroundStyle(Brand.meta)
                .fixedSize(horizontal: false, vertical: true)
            Button { search() } label: { Label("Try again", systemImage: "arrow.clockwise") }
                .buttonStyle(GhostButtonStyle())
                .disabled(searching)
        }
        .padding(16)
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .top)
        .accessibilityIdentifier("discoverError")
    }

    // MARK: - detail

    @ViewBuilder private var detailColumn: some View {
        ZStack {
            if let row = selected {
                detail(row).id(row.id).transition(.opacity)
            } else {
                EmptyState(icon: "square.stack.3d.up",
                           title: "Select a result",
                           subtitle: "Pick a row to read its description, category, grades, and source URL before importing anything.")
                    .transition(.opacity)
            }
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
    }

    private func detail(_ row: DiscoverResult) -> some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 16) {
                VStack(alignment: .leading, spacing: 8) {
                    Text(row.name.isEmpty ? "(unnamed)" : row.name)
                        .font(.system(size: 20, weight: .semibold)).foregroundStyle(Brand.fg)
                        .fixedSize(horizontal: false, vertical: true)
                    HStack(spacing: 6) {
                        if !row.category.isEmpty { Pill(text: row.category) }
                        if !row.author.isEmpty { Pill(text: "@\(row.author)") }
                    }
                    if !row.description.isEmpty {
                        Text(row.description).font(.system(size: 13)).foregroundStyle(Brand.fg2)
                            .fixedSize(horizontal: false, vertical: true)
                    }
                }

                gradeCard(row)
                sourceCard(row)

                HStack(spacing: 10) {
                    Button { pending = PendingImport(result: row) } label: {
                        HStack(spacing: 8) {
                            if importing { ProgressView().controlSize(.small) }
                            Text(importing ? "Importing…" : "Import to registry")
                        }
                    }
                    .buttonStyle(PrimaryButtonStyle())
                    .disabled(importing || row.skillURL.isEmpty)
                    .accessibilityIdentifier("discoverImport")

                    Button {
                        if let url = URL(string: row.skillURL) { NSWorkspace.shared.open(url) }
                    } label: { Label("View on GitHub", systemImage: "arrow.up.right.square") }
                        .buttonStyle(GhostButtonStyle())
                        .disabled(row.skillURL.isEmpty)
                }
            }
            .padding(20)
            .frame(maxWidth: .infinity, alignment: .leading)
        }
    }

    private func gradeCard(_ row: DiscoverResult) -> some View {
        Card(padding: 16) {
            VStack(alignment: .leading, spacing: 10) {
                Text("Index grades").font(.system(size: 12, weight: .semibold)).foregroundStyle(Brand.fg2)
                // All three grades, always: a confirmation screen that silently
                // omits one reads as a pass.
                ForEach(row.scores.lines, id: \.name) { line in
                    HStack(spacing: 8) {
                        Text(line.name).font(Brand.monoSized(11)).foregroundStyle(Brand.muted)
                            .frame(width: 96, alignment: .leading)
                        GradeBadge(level: line.level)
                        Spacer()
                    }
                }
                Text(ImportGate.gradeDisclaimer).font(.system(size: 11)).foregroundStyle(Brand.meta)
                    .fixedSize(horizontal: false, vertical: true)
            }
            .frame(maxWidth: .infinity, alignment: .leading)
        }
    }

    /// The source URL, wrapped rather than truncated: it is the one field the
    /// user may want to read in full, and a long monorepo URL must not push
    /// the pane horizontally in a narrow window.
    private func sourceCard(_ row: DiscoverResult) -> some View {
        Card(padding: 16) {
            VStack(alignment: .leading, spacing: 8) {
                HStack {
                    Text("Source").font(.system(size: 12, weight: .semibold)).foregroundStyle(Brand.fg2)
                    Spacer()
                    Button { Clipboard.copy(row.skillURL) } label: {
                        Label("Copy", systemImage: "doc.on.doc").font(.system(size: 11))
                    }.buttonStyle(.plain).foregroundStyle(Brand.accent)
                }
                Text(row.skillURL.isEmpty ? "(the index gave no URL)" : row.skillURL)
                    .font(Brand.monoSized(11)).foregroundStyle(Brand.fg2)
                    .fixedSize(horizontal: false, vertical: true)
                    .textSelection(.enabled)
                Text("Only this folder is fetched, over the GitHub Contents API — importing one skill out of a monorepo never clones the repository.")
                    .font(.system(size: 11)).foregroundStyle(Brand.meta)
                    .fixedSize(horizontal: false, vertical: true)
            }
            .frame(maxWidth: .infinity, alignment: .leading)
        }
    }

    // MARK: - confirmation

    /// The import confirmation. A row out of the index is untrusted whatever
    /// its URL shape, so this states what will be written, keeps the durable
    /// install opt-in, and requires a second acknowledgement for a blocker.
    private func confirmSheet(_ item: PendingImport) -> some View {
        let review = item.result.scores.any
            ? ImportReview.evaluate(slug: item.result.name, scores: item.result.scores)
            : ImportReview(slug: item.result.name, scores: item.result.scores)
        return VStack(alignment: .leading, spacing: 0) {
            VStack(alignment: .leading, spacing: 10) {
                Eyebrow(text: "Untrusted import")
                Text("Import \(item.result.name)?")
                    .font(.system(size: 18, weight: .semibold)).foregroundStyle(Brand.fg)
                Text("Picked from the public skill index, so it is third-party whatever its URL. \(ImportGate.registryOnlyExplanation)")
                    .font(.system(size: 12)).foregroundStyle(Brand.muted)
                    .fixedSize(horizontal: false, vertical: true)
                Text(item.result.skillURL).font(Brand.monoSized(11)).foregroundStyle(Brand.fg2)
                    .fixedSize(horizontal: false, vertical: true)
            }
            .padding(20)

            Divider().overlay(Brand.border)

            VStack(alignment: .leading, spacing: 12) {
                ForEach(item.result.scores.lines, id: \.name) { line in
                    HStack(spacing: 8) {
                        Text(line.name).font(Brand.monoSized(11)).foregroundStyle(Brand.muted)
                            .frame(width: 96, alignment: .leading)
                        GradeBadge(level: line.level)
                        Spacer()
                    }
                }
                Toggle(isOn: Binding(
                    get: { pending?.installIntoAgents ?? false },
                    set: { pending?.installIntoAgents = $0 })) {
                    VStack(alignment: .leading, spacing: 2) {
                        Text("Also install into agents").font(.system(size: 12, weight: .medium))
                            .foregroundStyle(Brand.fg)
                        Text("Off by default. Every agent then loads this SKILL.md each session.")
                            .font(.system(size: 11)).foregroundStyle(Brand.meta)
                    }
                }
                .toggleStyle(.checkbox)
                .accessibilityIdentifier("discoverInstallToggle")

                if review.blocked {
                    blockWarning(review)
                }
            }
            .padding(20)

            Divider().overlay(Brand.border)

            HStack(spacing: 10) {
                Spacer()
                Button("Cancel") { pending = nil }.buttonStyle(GhostButtonStyle())
                Button {
                    let confirmed = item
                    pending = nil
                    runImport(confirmed)
                } label: { Text("Import") }
                .buttonStyle(PrimaryButtonStyle())
                .disabled(review.blocked && !(pending?.acknowledgedBlock ?? false))
                .accessibilityIdentifier("discoverConfirmImport")
            }
            .padding(16)
        }
        .frame(width: 480)
        .background(Brand.bg)
    }

    private func blockWarning(_ review: ImportReview) -> some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack(spacing: 8) {
                Image(systemName: "exclamationmark.triangle.fill")
                    .font(.system(size: 12)).foregroundStyle(Brand.danger)
                Text(review.summary).font(.system(size: 12, weight: .medium)).foregroundStyle(Brand.fg)
                    .fixedSize(horizontal: false, vertical: true)
            }
            Toggle(isOn: Binding(
                get: { pending?.acknowledgedBlock ?? false },
                set: { pending?.acknowledgedBlock = $0 })) {
                Text("I have read the source and want to import it anyway")
                    .font(.system(size: 12)).foregroundStyle(Brand.fg2)
            }
            .toggleStyle(.checkbox)
            .accessibilityIdentifier("discoverAllowUnsafe")
        }
        .padding(12)
        .background(Brand.surfaceWarm)
        .overlay(RoundedRectangle(cornerRadius: 8).strokeBorder(Brand.danger.opacity(0.45), lineWidth: 1))
        .clipShape(RoundedRectangle(cornerRadius: 8))
    }

    // MARK: - actions

    private func search() {
        let text = query.trimmingCharacters(in: .whitespaces)
        guard !text.isEmpty else { return }
        searchTask?.cancel()
        searching = true
        searchError = nil
        let q = DiscoverQuery(text: text, mode: mode)
        searchTask = Task {
            do {
                let resp = try await state.discoverSearch(q)
                guard !Task.isCancelled else { return }
                results = resp.results
                selected = resp.results.first
                searchError = nil
            } catch {
                guard !Task.isCancelled else { return }
                // Fail closed: no partial list survives a failed search.
                results = []
                selected = nil
                searchError = error.localizedDescription
            }
            didSearch = true
            searching = false
        }
    }

    private func runImport(_ item: PendingImport) {
        let decision = ImportDecision(
            url: item.result.skillURL,
            scores: item.result.scores,
            installIntoAgents: item.installIntoAgents,
            allowUnsafe: item.acknowledgedBlock)
        guard decision.permitted else { return }
        importing = true
        Task {
            let targets = decision.installPermitted
                ? Agents.all().filter { $0.underHome || $0.universal }.filter(installTargetExists)
                : []
            await state.importDiscovered(item.result, targets: targets)
            importing = false
        }
    }

    /// The durable install writes into agent folders that already exist. A
    /// user who opted in wants their agents to load the skill, not a new dot
    /// folder per catalogue entry.
    private func installTargetExists(_ target: AgentTarget) -> Bool {
        let home = FileManager.default.homeDirectoryForCurrentUser.path
        let base = (home as NSString).appendingPathComponent(target.dotDir)
        var isDir: ObjCBool = false
        return FileManager.default.fileExists(atPath: base, isDirectory: &isDir) && isDir.boolValue
    }
}

/// One index row in the result list.
struct DiscoverRow: View {
    let result: DiscoverResult
    let selected: Bool

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack(spacing: 8) {
                Text(result.name.isEmpty ? "(unnamed)" : result.name)
                    .font(.system(size: 13, weight: .semibold)).foregroundStyle(Brand.fg)
                    .lineLimit(1)
                Spacer(minLength: 8)
                GradeBadge(level: ImportGate.label(result.safety), compact: true)
            }
            HStack(spacing: 6) {
                if !result.category.isEmpty {
                    Text(result.category).font(Brand.monoSized(10)).foregroundStyle(Brand.accent.opacity(0.9))
                }
                if !result.author.isEmpty {
                    Text("@\(result.author)").font(Brand.monoSized(10)).foregroundStyle(Brand.meta)
                        .lineLimit(1).truncationMode(.middle)
                }
            }
            if !result.description.isEmpty {
                Text(result.description).font(.system(size: 12)).foregroundStyle(Brand.muted)
                    .lineLimit(2).fixedSize(horizontal: false, vertical: true)
            }
        }
        .padding(.horizontal, 14).padding(.vertical, 11)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(selected ? Brand.surfaceRaised : Color.clear)
        .contentShape(Rectangle())
    }
}

/// One grade, tinted by level. `unscored` is deliberately not neutral-grey
/// alongside a pass: an ungraded skill is unvetted, not fine.
struct GradeBadge: View {
    let level: String
    var compact = false

    var body: some View {
        HStack(spacing: 5) {
            Circle().fill(tint).frame(width: 5, height: 5)
            Text(ImportGate.label(level))
                .font(Brand.monoSized(compact ? 10 : 11)).foregroundStyle(Brand.fg2)
        }
        .padding(.horizontal, 8).padding(.vertical, 3)
        .background(Brand.surfaceWarm)
        .overlay(Capsule().strokeBorder(tint.opacity(0.45), lineWidth: 1))
        .clipShape(Capsule())
    }

    private var tint: Color {
        switch ImportGate.label(level) {
        case ImportGate.levelGood: return Brand.success
        case ImportGate.levelAverage: return Brand.warn
        case ImportGate.levelPoor: return Brand.danger
        default: return Brand.muted
        }
    }
}
