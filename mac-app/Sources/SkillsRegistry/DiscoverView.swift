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
    @State private var category = ""
    @AppStorage("discoverLimit") private var limit = DiscoverClient.defaultLimit
    /// The submitted query the list on screen answers, so the header can name
    /// it and notice when the field drifts away from it (finding: stale rows
    /// with no query label looked current).
    @State private var lastSearched: DiscoverQuery?
    @State private var searching = false
    @State private var importing = false
    @State private var pending: PendingImport?
    @State private var pickerFor: PendingImport?
    /// Destinations chosen in the picker. Nil means the picker has not run yet.
    @State private var pickedTargets: [AgentTarget]?
    @State private var searchTask: Task<Void, Never>?
    @FocusState private var queryFocused: Bool

    /// Query, mode, results, selection, and search history live in `AppState`
    /// so switching sections or re-theming the accent keeps the search intact.
    /// Only transient UI (spinners, the sheet, the in-flight task) stays local.
    private var query: Binding<String> {
        Binding(get: { state.discoverPane.query }, set: { state.discoverPane.query = $0 })
    }
    private var mode: Binding<DiscoverMode> {
        Binding(get: { state.discoverPane.mode }, set: { state.discoverPane.mode = $0 })
    }

    /// The query demo mode arrives with.
    private static let demoQuery = "pdf"

    /// The result-cap stops the limit control offers. They mirror the CLI's
    /// `--limit` range (default 10, capped at 50).
    private static let resultLimits = [10, 25, 50]

    /// A row the user asked to import, held while the confirmation sheet is up.
    /// `installIntoAgents` starts false, which is what makes registry-only the
    /// default rather than a setting the user has to find. `scanned` is nil
    /// until the post-fetch scan has run; a non-nil value, including empty,
    /// means that consent was given with the findings in front of the user.
    private struct PendingImport: Identifiable, Equatable {
        let result: DiscoverResult
        var installIntoAgents = false
        var acknowledgedBlock = false
        var scanned: [SkillFinding]?
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
        .sheet(item: $pending) { _ in
            if let item = pending {
                confirmSheet(item).id(item.scanned?.count ?? -1)
            }
        }
        // Cmd-F focuses the query field, Cmd-R re-runs the search.
        .onChange(of: state.focusSearchRequest) { queryFocused = true }
        .onChange(of: state.refreshRequest) { search() }
        // Demo mode drives the whole app offline, so the pane arrives with a
        // query already run rather than requiring synthetic keystrokes. Only
        // for the very first appearance: once a search has run — by demo or by
        // the user — the hoisted results survive navigation untouched.
        .onAppear {
            if !Self.resultLimits.contains(limit) { limit = DiscoverClient.defaultLimit }
            guard state.isDemo, !state.discoverPane.didSearch, state.discoverPane.query.isEmpty else { return }
            state.discoverPane.query = Self.demoQuery
            search()
            // `--demo-scan-sheet` opens the post-fetch hold directly, so the
            // scan-hit confirmation can be shown without a second click. The
            // findings still come from `SkillScan`, not a hand-written list.
            if ProcessInfo.processInfo.arguments.contains("--demo-scan-sheet"),
               let row = AppState.demoDiscoverResults.first(where: { $0.name == "pdf-scraper" }) {
                pending = PendingImport(result: row, scanned: AppState.demoScanFindings(for: row.skillURL))
            }
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

            SearchField(icon: "sparkle.magnifyingglass",
                        placeholder: "pdf · summarize a youtube video · kubernetes",
                        text: query, focused: $queryFocused,
                        accessibilityID: "discoverQueryField") { search() }

            HStack(spacing: 10) {
                categoryField
                limitStepper
                Spacer()
            }

            HStack(spacing: 10) {
                Button { search() } label: {
                    HStack(spacing: 8) {
                        if searching { ProgressView().controlSize(.small) }
                        Text(searching ? "Searching…" : "Search")
                    }
                }
                .buttonStyle(PrimaryButtonStyle())
                .disabled(state.discoverPane.query.trimmingCharacters(in: .whitespaces).isEmpty || searching || importing)
                .accessibilityIdentifier("discoverSearch")

                modeToggle
                Spacer()
                resultsHeader
            }
        }
        .padding(20)
    }

    /// Optional category filter, threading straight into `DiscoverQuery` like
    /// the CLI's `--category`. Empty means the whole index.
    private var categoryField: some View {
        HStack(spacing: 8) {
            Image(systemName: "tag").font(.system(size: 11)).foregroundStyle(Brand.muted)
            TextField("Category (optional)", text: $category)
                .textFieldStyle(.plain).font(.system(size: 12))
                .onSubmit { search() }
                .accessibilityIdentifier("discoverCategoryField")
            if !category.isEmpty {
                Button { category = "" } label: { Image(systemName: "xmark.circle.fill") }
                    .buttonStyle(.plain).foregroundStyle(Brand.meta)
            }
        }
        .padding(.horizontal, 12).padding(.vertical, 7)
        .background(Brand.surfaceWarm)
        .overlay(RoundedRectangle(cornerRadius: 8).strokeBorder(Brand.border, lineWidth: 1))
        .clipShape(RoundedRectangle(cornerRadius: 8))
        .frame(maxWidth: 240)
    }

    /// Result cap, mirroring the CLI's `--limit`. Switching stops re-runs a
    /// query that already returned, like the mode toggle, and the choice
    /// persists across launches.
    private var limitStepper: some View {
        HStack(spacing: 2) {
            Text("Limit").font(.system(size: 12)).foregroundStyle(Brand.muted)
                .padding(.leading, 8)
            ForEach(Self.resultLimits, id: \.self) { n in
                Button {
                    guard limit != n else { return }
                    limit = n
                    if state.discoverPane.didSearch { search() }
                } label: {
                    Text("\(n)")
                        .font(.system(size: 12, weight: .medium))
                        .padding(.horizontal, 10).padding(.vertical, 5)
                        .foregroundStyle(limit == n ? Brand.fg : Brand.muted)
                        .background(limit == n ? Brand.surfaceRaised : Color.clear)
                        .clipShape(RoundedRectangle(cornerRadius: 6))
                        .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                .accessibilityIdentifier("discoverLimit-\(n)")
            }
        }
        .padding(2)
        .background(Brand.surfaceWarm)
        .overlay(RoundedRectangle(cornerRadius: 8).strokeBorder(Brand.border, lineWidth: 1))
        .clipShape(RoundedRectangle(cornerRadius: 8))
    }

    /// What the list shows and which submitted query produced it. When the
    /// field (or a filter) drifts from the submitted query, the label gives
    /// way to an explicit re-search affordance instead of letting old rows
    /// pass as current.
    private var resultsHeader: some View {
        HStack(spacing: 8) {
            if searching && !state.discoverPane.results.isEmpty {
                ProgressView().controlSize(.small)
            }
            if isStale {
                Button { search() } label: {
                    Text("Search to update")
                        .font(.system(size: 11, weight: .medium))
                        .foregroundStyle(Brand.accent)
                }
                .buttonStyle(.plain)
                .accessibilityIdentifier("discoverRefreshStale")
            } else if !state.discoverPane.results.isEmpty, let last = lastSearched {
                Text("Results for \"\(last.text)\" · \(state.discoverPane.results.count) result\(state.discoverPane.results.count == 1 ? "" : "s")")
                    .font(Brand.monoSized(11)).foregroundStyle(Brand.muted)
                    .lineLimit(1).truncationMode(.middle)
            } else if !state.discoverPane.results.isEmpty {
                Text("\(state.discoverPane.results.count) result\(state.discoverPane.results.count == 1 ? "" : "s")")
                    .font(Brand.monoSized(11)).foregroundStyle(Brand.muted)
            }
        }
    }

    /// The field or the filters no longer describe the list on screen: the
    /// submitted query (or its category/mode) differs from what is typed.
    /// Suppressed while a search is in flight — the fresh list is on its way.
    private var isStale: Bool {
        guard state.discoverPane.didSearch, !searching, let last = lastSearched else { return false }
        return query.wrappedValue.trimmingCharacters(in: .whitespaces) != last.text
            || category.trimmingCharacters(in: .whitespacesAndNewlines) != last.category
            || mode.wrappedValue != last.mode
    }

    /// Keyword vs vector ranking. Switching mode re-runs a query that already
    /// returned, so the toggle reads as a property of the search rather than a
    /// setting to remember to apply.
    private var modeToggle: some View {
        HStack(spacing: 2) {
            ForEach(DiscoverMode.allCases) { m in
                Button {
                    guard state.discoverPane.mode != m else { return }
                    state.discoverPane.mode = m
                    if state.discoverPane.didSearch { search() }
                } label: {
                    Text(m.label)
                        .font(.system(size: 12, weight: .medium))
                        .padding(.horizontal, 10).padding(.vertical, 5)
                        .foregroundStyle(state.discoverPane.mode == m ? Brand.fg : Brand.muted)
                        .background(state.discoverPane.mode == m ? Brand.surfaceRaised : Color.clear)
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
        if searching && state.discoverPane.results.isEmpty {
            VStack { Spacer(); ProgressView().tint(Brand.accent); Spacer() }
        } else if let searchError = state.discoverPane.searchError {
            errorState(searchError)
        } else if state.discoverPane.results.isEmpty {
            EmptyState(icon: state.discoverPane.didSearch ? "magnifyingglass" : "sparkle.magnifyingglass",
                       title: state.discoverPane.didSearch ? "Nothing matched" : "Search the index",
                       subtitle: state.discoverPane.didSearch
                        ? "The index had no hit for that. Try \(DiscoverMode.vector.label) mode.wrappedValue to search by meaning instead of literal terms."
                        : "Type what you need above. Results carry the index's own grades and an importable GitHub URL.")
        } else {
            // The old list stays up during a re-search rather than flashing
            // away, but dimmed with a spinner over it so it never reads as
            // the fresh answer; a stale (edited, unsubmitted) field dims it
            // the same way.
            ScrollView {
                LazyVStack(spacing: 0) {
                    ForEach(state.discoverPane.results) { row in
                        ListRowButton(
                            selected: state.discoverPane.selected?.id == row.id,
                            hint: "Selects this result",
                            identifier: "discoverRow-\(row.name)",
                            action: {
                                withAnimation(.easeInOut(duration: 0.2)) { state.discoverPane.selectedName = row.name }
                            }
                        ) {
                            DiscoverRow(result: row)
                        }
                        Divider().overlay(Brand.border).padding(.leading, 14)
                    }
                }
            }
            .opacity(isStale || (searching && !state.discoverPane.results.isEmpty) ? 0.55 : 1.0)
            .animation(.easeInOut(duration: 0.2), value: isStale)
            .overlay {
                if searching && !state.discoverPane.results.isEmpty {
                    ProgressView().tint(Brand.accent)
                        .frame(maxWidth: .infinity, maxHeight: .infinity)
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
            if let row = state.discoverPane.selected {
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
        let findings = item.scanned ?? []
        let review = ImportReview.evaluate(slug: item.result.name, scores: item.result.scores,
                                           findings: findings)
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
                // The sheet states the verdict; the disclaimer states what the
                // verdict is worth. The detail pane already shows this line —
                // the confirmation must not be the one place that omits it.
                Text(ImportGate.gradeDisclaimer).font(.system(size: 11)).foregroundStyle(Brand.muted)
                    .fixedSize(horizontal: false, vertical: true)
                Toggle(isOn: Binding(
                    get: { pending?.installIntoAgents ?? false },
                    set: { pending?.installIntoAgents = $0 })) {
                    VStack(alignment: .leading, spacing: 2) {
                        Text("Also install into agents").font(.system(size: 12, weight: .medium))
                            .foregroundStyle(Brand.fg)
                        Text("Off by default. When on, Import asks which agents load this SKILL.md each session.")
                            .font(.system(size: 11)).foregroundStyle(Brand.meta)
                    }
                }
                .toggleStyle(.checkbox)
                .accessibilityIdentifier("discoverInstallToggle")
                .accessibilityLabel("Also install into agents")

                if !findings.isEmpty {
                    ScanFindingsList(rows: findings.map { (item.result.name, $0) })
                        .accessibilityIdentifier("discoverScanFindings")
                }

                if review.blocked {
                    blockWarning(review)
                }
            }
            .padding(20)

            Divider().overlay(Brand.border)

            HStack(spacing: 10) {
                Spacer()
                Button("Cancel") {
                    state.scanBlockedImport = nil
                    pending = nil
                    pickedTargets = nil
                }.buttonStyle(GhostButtonStyle())
                    .keyboardShortcut(.cancelAction)
                Button {
                    // `pending` is the live toggle state: the sheet's content
                    // rebuilds as the toggles flip, and the bindings write back
                    // into it, so reading it here is what the user just chose.
                    Task { await advanceImport() }
                } label: {
                    HStack(spacing: 8) {
                        if importing { ProgressView().controlSize(.small) }
                        Text(importing ? (item.scanned == nil ? "Scanning…" : "Importing…") : "Import")
                    }
                }
                .buttonStyle(PrimaryButtonStyle())
                .keyboardShortcut(.defaultAction)
                .disabled(importing || (review.blocked && !(pending?.acknowledgedBlock ?? false)))
                .accessibilityIdentifier("discoverConfirmImport")
            }
            .padding(16)
        }
        .frame(width: findings.isEmpty ? 480 : 540)
        .background(Brand.bg)
        // Nested over the confirmation: cancelling the picker falls back to
        // the confirmation rather than abandoning the import. Nothing is
        // preselected, and confirming with none picked imports registry-only
        // with a toast that says the install was skipped.
        .sheet(item: $pickerFor) { pick in
            AgentPickerSheet(
                title: "Install into which agents?",
                subtitle: "\(pick.result.name) will be imported into your registry, then installed into the agents you pick. Confirm with none state.discoverPane.selected for a registry-only import.",
                confirmLabel: "Import + install",
                emptyConfirmLabel: "Import registry-only"
            ) { targets in
                pickedTargets = targets
                pickerFor = nil
                Task { await advanceImport() }
            }
        }
    }

    private func blockWarning(_ review: ImportReview) -> some View {
        GateBlockWarning(
            review: review,
            acknowledged: Binding(
                get: { pending?.acknowledgedBlock ?? false },
                set: { pending?.acknowledgedBlock = $0 }),
            toggleID: "discoverAllowUnsafe")
    }

    // MARK: - actions

    private func search() {
        let text = state.discoverPane.query.trimmingCharacters(in: .whitespaces)
        guard !text.isEmpty else { return }
        // Trim the category the same way as text so the submitted query and
        // isStale's comparison agree; storing it raw made a padded category
        // (" Security ") read as permanently stale.
        let cat = category.trimmingCharacters(in: .whitespacesAndNewlines)
        searchTask?.cancel()
        searching = true
        state.discoverPane.searchError = nil
        let q = DiscoverQuery(text: text, mode: state.discoverPane.mode, category: cat, limit: limit)
        lastSearched = q
        searchTask = Task {
            do {
                let resp = try await state.discoverSearch(q)
                guard !Task.isCancelled else { return }
                state.discoverPane = state.discoverPane.searched(resp.results)
            } catch {
                guard !Task.isCancelled else { return }
                // Fail closed: no partial list survives a failed search.
                state.discoverPane = state.discoverPane.failed(error.localizedDescription)
            }
            searching = false
        }
    }

    /// Confirm, then pick install targets if the user opted in, then fetch and
    /// scan, then write. The sheet stays up across the scan: a hit is drawn
    /// into this same confirmation and the acknowledgement is cleared, so the
    /// second click is the consent that was given with the findings visible. A
    /// clean scan publishes on the first click.
    ///
    /// Opting into the durable agent install opens the picker first, so the
    /// install goes only where the user chose instead of spraying every
    /// detected folder. Confirming the picker with nothing selected is a
    /// registry-only import, and the toast says the install was skipped.
    private func advanceImport() async {
        guard let item = pending else { return }
        let seenFindings = item.scanned != nil
        let decision = ImportDecision(
            url: item.result.skillURL,
            scores: item.result.scores,
            installIntoAgents: item.installIntoAgents,
            allowUnsafe: item.acknowledgedBlock)
        // A grade block still has to be acknowledged before we fetch. A scan
        // hit cannot be known yet, so it is not part of this check.
        if !seenFindings && !decision.permitted { return }
        if decision.installPermitted && pickedTargets == nil {
            // The opt-in chooses destinations rather than spraying every
            // detected folder: the picker opens on confirm. Its picks survive
            // a scan hold, so a held import is not asked twice.
            pickerFor = item
            return
        }
        await writeImport(item, seenFindings: seenFindings)
    }

    /// Write the confirmed import. `targets` are the agents the user picked (or
    /// empty for registry-only); `pickedNoAgents` is true only when the picker
    /// was shown and nothing was chosen, so the toast can say so.
    private func writeImport(_ item: PendingImport, seenFindings: Bool) async {
        let targets = pickedTargets ?? []
        let pickedNoAgents = pickedTargets != nil && targets.isEmpty
        importing = true
        let published = await state.importDiscovered(
            item.result, targets: targets,
            pickedNoAgents: pickedNoAgents,
            allowUnsafe: item.acknowledgedBlock,
            scanAcknowledged: seenFindings && item.acknowledgedBlock)
        importing = false
        guard pending?.id == item.id else { return }
        if published {
            state.scanBlockedImport = nil
            pending = nil
            pickedTargets = nil
            return
        }
        if let held = state.scanBlockedImport, held.result.id == item.result.id {
            var updated = item
            updated.scanned = held.refusal.findings
            updated.acknowledgedBlock = false
            pending = updated
            state.scanBlockedImport = nil
        }
    }
}

/// One index row in the result list.
struct DiscoverRow: View {
    let result: DiscoverResult

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
