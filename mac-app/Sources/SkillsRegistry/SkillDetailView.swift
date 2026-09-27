import SwiftUI
import AppKit
import MarkdownUI
import SkillsRegistryCore

struct SkillDetailView: View {
    @EnvironmentObject var state: AppState
    let slug: String

    @State private var detail: SkillDetail?
    @State private var loading = true
    @State private var error: String?
    @State private var confirmRemove = false
    @State private var showInstall = false
    @State private var isEditing = false
    @State private var draft = ""
    @State private var saving = false

    // Multi-file browsing. SKILL.md renders from `detail.markdown`; other files
    // are fetched lazily into `auxText`.
    @State private var selectedFile = "SKILL.md"
    @State private var auxText: String?
    @State private var auxLoading = false
    @State private var auxError: String?

    var body: some View {
        GeometryReader { geo in
            let compact = geo.size.width < DetailLayout.compactWidth
            VStack(spacing: 0) {
                header(compact: compact)
                Divider().overlay(Brand.border)
                content(compact: compact)
            }
            .frame(maxWidth: .infinity, maxHeight: .infinity)
        }
        .background(Brand.bg)
        .task(id: slug) { await load() }
        .confirmationDialog("Remove \(slug) from the registry?",
                            isPresented: $confirmRemove, titleVisibility: .visible) {
            Button("Remove", role: .destructive) { Task { await state.remove(slug) } }
            Button("Cancel", role: .cancel) {}
        } message: {
            Text("This deletes the \(slug)/ folder from \(state.repo?.fullName ?? "the repo"), clears its local download, and removes it from your agent folders. It can't be undone from here.")
        }
        .sheet(isPresented: $showInstall) {
            AgentPickerSheet(
                title: "Install \(detail?.name ?? slug)",
                subtitle: "Copy this skill's files into the agents you pick, at <agent>/skills/\(slug)/.",
                confirmLabel: "Install"
            ) { targets in
                Task { await state.installRegistrySkill(slug, targets: targets) }
            }
        }
    }

    private func header(compact: Bool) -> some View {
        VStack(alignment: .leading, spacing: 10) {
            HStack(alignment: .top) {
                VStack(alignment: .leading, spacing: 6) {
                    Text(detail?.name ?? slug)
                        .font(.system(size: compact ? 18 : 22, weight: .semibold)).foregroundStyle(Brand.fg)
                    Pill(text: slug, dot: Brand.accent)
                }
                Spacer()
                actions(compact: compact)
            }
            if let d = detail, !d.description.isEmpty {
                Text(d.description).font(.system(size: 13)).foregroundStyle(Brand.muted)
                    .fixedSize(horizontal: false, vertical: true)
            }
        }
        .padding(20)
    }

    private func actions(compact: Bool) -> some View {
        HStack(spacing: 8) {
            if isEditing {
                Button("Cancel") { cancelEditing() }
                    .buttonStyle(GhostButtonStyle())
                    .disabled(saving)
                    .accessibilityIdentifier("cancelSkillEdit")
                Button { Task { await saveEditing() } } label: {
                    HStack(spacing: 6) {
                        if saving { ProgressView().controlSize(.small) }
                        Text(saving ? "Saving…" : "Save")
                    }
                }
                .buttonStyle(PrimaryButtonStyle(tint: Brand.accent))
                .disabled(saving || draft == detail?.markdown)
                .keyboardShortcut("s", modifiers: .command)
                .accessibilityIdentifier("saveSkillEdit")
            } else {
                Button { beginEditing() } label: {
                    if compact {
                        Image(systemName: "pencil").font(.system(size: 12))
                    } else {
                        Label("Edit", systemImage: "pencil").font(.system(size: 12))
                    }
                }
                .buttonStyle(GhostButtonStyle())
                .disabled(detail == nil)
                .help("Edit SKILL.md")
                .accessibilityIdentifier("editSkill")
            }
            if !isEditing && !state.isDemo {
                Button { showInstall = true } label: {
                    if compact {
                        Image(systemName: "arrow.down.circle").font(.system(size: 12))
                    } else {
                        Label("Install", systemImage: "arrow.down.circle").font(.system(size: 12))
                    }
                }
                .buttonStyle(GhostButtonStyle())
                .disabled(detail == nil)
                .help("Install this skill into your agents")
                .accessibilityIdentifier("installSkill")
            }
            if !isEditing {
                Button { openOnGitHub() } label: {
                    if compact {
                        Image(systemName: "arrow.up.right.square").font(.system(size: 12))
                    } else {
                        Label("GitHub", systemImage: "arrow.up.right.square").font(.system(size: 12))
                    }
                }
                .buttonStyle(GhostButtonStyle())
                .help("Open \(slug) on GitHub")
                Button { if let d = detail { Clipboard.copy(d.markdown) ; state.showToast("Copied SKILL.md", .ok) } } label: {
                    Image(systemName: "doc.on.doc").font(.system(size: 12))
                }
                .buttonStyle(GhostButtonStyle())
                .help("Copy SKILL.md")
            }
            if !isEditing && !state.isDemo {
                Button { confirmRemove = true } label: {
                    Image(systemName: "trash").font(.system(size: 12))
                }
                .buttonStyle(GhostButtonStyle())
                .help("Remove this skill from the registry")
                .accessibilityIdentifier("removeSkill")
            }
        }
        .fixedSize(horizontal: true, vertical: false)
    }

    @ViewBuilder private func content(compact: Bool) -> some View {
        if loading {
            VStack { Spacer(); ProgressView().tint(Brand.accent); Spacer() }
                .frame(maxWidth: .infinity)
        } else if let error {
            EmptyState(icon: "exclamationmark.triangle", title: "Couldn't load", subtitle: error)
        } else if let d = detail {
            if compact && d.files.count > 1 {
                VStack(spacing: 0) {
                    fileMenu(d.files)
                    Divider().overlay(Brand.border)
                    fileViewer(d)
                }
            } else {
                HStack(spacing: 0) {
                    fileViewer(d)
                    if d.files.count > 1 {
                        Divider().overlay(Brand.border)
                        fileRail(d.files)
                    }
                }
            }
        }
    }

    @ViewBuilder private func fileViewer(_ d: SkillDetail) -> some View {
        if selectedFile == "SKILL.md" {
            if isEditing {
                TextEditor(text: $draft)
                    .font(Brand.monoSized(13))
                    .foregroundStyle(Brand.fg)
                    .scrollContentBackground(.hidden)
                    .padding(18)
                    .background(Brand.bg)
                    .accessibilityLabel("SKILL.md editor")
                    .accessibilityIdentifier("skillEditor")
            } else {
                ScrollView {
                    // Render the body only — the frontmatter's name/description
                    // already appear in the header. "Copy" still copies the raw
                    // file (frontmatter included).
                    Markdown(Frontmatter.body(d.markdown))
                        .markdownTheme(.brand)
                        .textSelection(.enabled)
                        .padding(24)
                        .frame(maxWidth: DetailLayout.readingWidth, alignment: .leading)
                        .frame(maxWidth: .infinity, alignment: .leading)
                }
            }
        } else if auxLoading {
            VStack { Spacer(); ProgressView().tint(Brand.accent); Spacer() }
                .frame(maxWidth: .infinity)
        } else if let auxError {
            EmptyState(icon: "exclamationmark.triangle", title: "Couldn't load file", subtitle: auxError)
        } else if let auxText {
            ScrollView {
                if selectedFile.hasSuffix(".md") {
                    Markdown(auxText)
                        .markdownTheme(.brand)
                        .textSelection(.enabled)
                        .padding(24)
                        .frame(maxWidth: DetailLayout.readingWidth, alignment: .leading)
                        .frame(maxWidth: .infinity, alignment: .leading)
                } else {
                    Text(auxText)
                        .font(Brand.monoSized(12)).foregroundStyle(Brand.fg2)
                        .textSelection(.enabled)
                        .padding(24)
                        .frame(maxWidth: DetailLayout.readingWidth, alignment: .leading)
                        .frame(maxWidth: .infinity, alignment: .leading)
                }
            }
        }
    }

    private func fileRail(_ files: [String]) -> some View {
        VStack(alignment: .leading, spacing: 0) {
            Text("FILES").font(Brand.monoSized(10)).tracking(1.2).foregroundStyle(Brand.meta)
                .padding(.horizontal, 14).padding(.top, 16).padding(.bottom, 8)
            ScrollView {
                VStack(alignment: .leading, spacing: 2) {
                    ForEach(files, id: \.self) { f in
                        fileRow(f)
                    }
                }
                .padding(.horizontal, 6)
            }
        }
        .frame(width: DetailLayout.fileRailWidth)
        .background(Brand.surface)
    }

    /// Compact replacement for the file rail: a single menu row pinned under
    /// the header. Used below ``DetailLayout/compactWidth`` where the fixed
    /// rail would squeeze the reading column to ~140pt.
    private func fileMenu(_ files: [String]) -> some View {
        Menu {
            ForEach(files, id: \.self) { f in
                Button(f) { selectFile(f) }
                    .disabled(isEditing && f != selectedFile)
            }
        } label: {
            HStack(spacing: 8) {
                Image(systemName: icon(for: selectedFile))
                    .font(.system(size: 11))
                    .foregroundStyle(Brand.accent)
                    .frame(width: 14)
                Text(selectedFile)
                    .font(Brand.monoSized(11))
                    .foregroundStyle(Brand.fg)
                    .lineLimit(1)
                    .truncationMode(.middle)
                Spacer()
                Image(systemName: "chevron.up.chevron.down")
                    .font(.system(size: 10))
                    .foregroundStyle(Brand.muted)
            }
            .padding(.horizontal, 12)
            .padding(.vertical, 8)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(Brand.surface)
            .clipShape(RoundedRectangle(cornerRadius: 7))
            .contentShape(Rectangle())
        }
        .menuStyle(.borderlessButton)
        .menuIndicator(.hidden)
        .disabled(isEditing)
        .help("Choose a file in this skill")
        .accessibilityIdentifier("fileMenu")
        .padding(.horizontal, 20)
        .padding(.vertical, 10)
    }

    private func fileRow(_ f: String) -> some View {
        Button { selectFile(f) } label: {
            HStack(spacing: 8) {
                Image(systemName: icon(for: f)).font(.system(size: 11))
                    .foregroundStyle(f == selectedFile ? Brand.accent : Brand.muted)
                    .frame(width: 14)
                Text(f).font(Brand.monoSized(11)).foregroundStyle(f == selectedFile ? Brand.fg : Brand.fg2)
                    .lineLimit(1).truncationMode(.middle)
                Spacer()
            }
            .padding(.horizontal, 8).padding(.vertical, 5)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(f == selectedFile ? Brand.surfaceRaised : Color.clear)
            .clipShape(RoundedRectangle(cornerRadius: 5))
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .disabled(isEditing && f != selectedFile)
        .accessibilityIdentifier("file-\(f)")
    }

    private func selectFile(_ f: String) {
        guard f != selectedFile else { return }
        selectedFile = f
        auxText = nil; auxError = nil
        guard f != "SKILL.md" else { return }
        auxLoading = true
        Task {
            do { auxText = try await state.fetchFile(slug: slug, path: f) }
            catch { auxError = error.localizedDescription }
            auxLoading = false
        }
    }

    private func icon(for file: String) -> String {
        if file.hasSuffix(".md") { return "doc.text" }
        if file.hasSuffix(".sh") || file.hasSuffix(".py") || file.hasSuffix(".js") { return "terminal" }
        if file.hasSuffix(".json") || file.hasSuffix(".toml") || file.hasSuffix(".yaml") || file.hasSuffix(".yml") { return "curlybraces" }
        return "doc"
    }

    private func openOnGitHub() {
        guard let repo = state.repo else { return }
        let url = URL(string: "https://github.com/\(repo.fullName)/tree/\(state.branch)/\(slug)")
        if let url { NSWorkspace.shared.open(url) }
    }

    private func beginEditing() {
        guard let detail else { return }
        selectedFile = "SKILL.md"
        auxText = nil
        auxError = nil
        draft = detail.markdown
        isEditing = true
    }

    private func cancelEditing() {
        draft = detail?.markdown ?? ""
        isEditing = false
    }

    private func saveEditing() async {
        guard let current = detail, draft != current.markdown else { return }
        saving = true
        defer { saving = false }
        do {
            let summary = try await state.saveSkillMarkdown(slug, markdown: draft)
            detail = SkillDetail(
                slug: slug,
                name: summary.name,
                description: summary.description,
                markdown: draft,
                files: current.files)
            isEditing = false
        } catch {
            state.showToast("Save failed: \(error.localizedDescription)", .error)
        }
    }

    private func load() async {
        loading = true; error = nil; isEditing = false; saving = false
        do {
            detail = try await state.fetchDetail(slug)
        } catch {
            self.error = error.localizedDescription
        }
        loading = false
    }
}

/// Width constants for the Browse detail pane.
enum DetailLayout {
    /// Below this detail-pane width the file rail collapses into a menu and
    /// header actions go icon-only. The sidebar (248) + list (340) columns are
    /// fixed, so this is ~1100pt of window width.
    static let compactWidth: CGFloat = 510
    /// Max line length for the markdown reading column (Settings uses 760).
    static let readingWidth: CGFloat = 740
    /// Fixed width of the file rail on wide layouts.
    static let fileRailWidth: CGFloat = 210
}
