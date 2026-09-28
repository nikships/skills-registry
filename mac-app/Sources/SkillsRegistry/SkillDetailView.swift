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
    @State private var confirmDiscard = false
    @FocusState private var editorFocused: Bool

    // Multi-file browsing. SKILL.md renders from `detail.markdown`; other files
    // are fetched lazily into `auxText`.
    @State private var selectedFile = "SKILL.md"
    @State private var auxText: String?
    @State private var auxLoading = false
    @State private var auxError: String?

    var body: some View {
        VStack(spacing: 0) {
            header
            Divider().overlay(Brand.border)
            content
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .background(Brand.bg)
        .task(id: slug) { await load() }
        .confirmationDialog("Remove \(slug) from the registry?",
                            isPresented: $confirmRemove, titleVisibility: .visible) {
            Button("Remove", role: .destructive) { Task { await state.remove(slug) } }
            Button("Cancel", role: .cancel) {}
        } message: {
            Text("This deletes the \(slug)/ folder from \(state.repo?.fullName ?? "the repo"), clears its local download, and removes it from your agent folders. It can't be undone from here.")
        }
        .confirmationDialog("Discard unsaved changes to \(slug)?",
                            isPresented: $confirmDiscard, titleVisibility: .visible) {
            Button("Discard Changes", role: .destructive) { discardEditing() }
                .accessibilityIdentifier("discardSkillEdit")
            Button("Keep Editing", role: .cancel) { refocusEditor() }
                .accessibilityIdentifier("keepEditingSkill")
        } message: {
            Text("Your edits to SKILL.md haven't been saved.")
        }
        // Esc cancels the edit (and asks first when dirty). Nil while the
        // discard sheet is up so that sheet keeps Escape for Keep Editing.
        .onExitCommand(perform: (isEditing && !confirmDiscard) ? { cancelEditing() } : nil)
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

    private var header: some View {
        VStack(alignment: .leading, spacing: 10) {
            HStack(alignment: .top) {
                VStack(alignment: .leading, spacing: 6) {
                    Text(detail?.name ?? slug)
                        .font(.system(size: 22, weight: .semibold)).foregroundStyle(Brand.fg)
                    Pill(text: slug, dot: Brand.accent)
                }
                Spacer()
                actions
            }
            if let d = detail, !d.description.isEmpty {
                Text(d.description).font(.system(size: 13)).foregroundStyle(Brand.muted)
                    .fixedSize(horizontal: false, vertical: true)
            }
        }
        .padding(20)
    }

    private var actions: some View {
        HStack(spacing: 8) {
            if isEditing {
                Button("Cancel") { cancelEditing() }
                    .buttonStyle(GhostButtonStyle())
                    // Disabled while the discard sheet is up so this button's
                    // Escape shortcut doesn't fight the sheet's own cancel.
                    .disabled(saving || confirmDiscard)
                    .keyboardShortcut(.cancelAction)
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
                    Label("Edit", systemImage: "pencil").font(.system(size: 12))
                }
                .buttonStyle(GhostButtonStyle())
                .disabled(detail == nil)
                .accessibilityIdentifier("editSkill")
            }
            if !isEditing && !state.isDemo {
                Button { showInstall = true } label: {
                    Label("Install", systemImage: "arrow.down.circle").font(.system(size: 12))
                }
                .buttonStyle(GhostButtonStyle())
                .disabled(detail == nil)
                .accessibilityIdentifier("installSkill")
            }
            if !isEditing {
                Button { openOnGitHub() } label: {
                    Label("GitHub", systemImage: "arrow.up.right.square").font(.system(size: 12))
                }
                .buttonStyle(GhostButtonStyle())
                .disabled(state.isDemo)
                .opacity(state.isDemo ? 0.45 : 1)
                .accessibilityIdentifier("openOnGitHub")
                .accessibilityHint(state.isDemo ? "Unavailable in demo mode" : "Opens this skill on GitHub")
                .hoverHelp(state.isDemo ? "Unavailable in demo mode" : "Open \(slug) on GitHub",
                           disabled: state.isDemo)
                Button {
                    if let target = copyTarget {
                        Clipboard.copy(target.text)
                        state.showToast("Copied \(target.name)", .ok)
                    }
                } label: {
                    Image(systemName: "doc.on.doc").font(.system(size: 12))
                }
                .buttonStyle(GhostButtonStyle())
                .disabled(copyTarget == nil)
                .accessibilityIdentifier("copySkillFile")
                .accessibilityLabel(copyTarget.map { "Copy \($0.name)" } ?? "Copy")
                .hoverHelp(copyHelp, disabled: copyTarget == nil)
            }
            if !isEditing && !state.isDemo {
                Button { confirmRemove = true } label: {
                    Image(systemName: "trash").font(.system(size: 12))
                }
                .buttonStyle(GhostButtonStyle())
                .accessibilityIdentifier("removeSkill")
            }
        }
    }

    @ViewBuilder private var content: some View {
        if loading {
            VStack { Spacer(); ProgressView().tint(Brand.accent); Spacer() }
                .frame(maxWidth: .infinity)
        } else if let error {
            EmptyState(icon: "exclamationmark.triangle", title: "Couldn't load", subtitle: error)
        } else if let d = detail {
            HStack(spacing: 0) {
                fileViewer(d)
                if d.files.count > 1 {
                    Divider().overlay(Brand.border)
                    fileRail(d.files)
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
                    .focused($editorFocused)
                    .onAppear { refocusEditor() }
                    .onChange(of: draft) { _, new in persistDraft(new) }
                    .onDisappear { persistDraft(draft) }
            } else {
                ScrollView {
                    // Render the body only — the frontmatter's name/description
                    // already appear in the header. "Copy" still copies the raw
                    // file (frontmatter included).
                    Markdown(Frontmatter.body(d.markdown))
                        .markdownTheme(.brand)
                        .textSelection(.enabled)
                        .padding(24)
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
                        .frame(maxWidth: .infinity, alignment: .leading)
                } else {
                    Text(auxText)
                        .font(Brand.monoSized(12)).foregroundStyle(Brand.fg2)
                        .textSelection(.enabled)
                        .padding(24)
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
        .frame(width: 210)
        .background(Brand.surface)
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
        // Demo points at the fixture repo. The button is disabled; this guard
        // is the backstop so a click can never open that URL.
        guard !state.isDemo else { return }
        guard let repo = state.repo else { return }
        let url = URL(string: "https://github.com/\(repo.fullName)/tree/\(state.branch)/\(slug)")
        if let url { NSWorkspace.shared.open(url) }
    }

    /// The file Copy acts on: SKILL.md normally, or the visible support file
    /// once loaded. Nil while the detail or a support file is still loading
    /// (or failed), which disables Copy.
    private var copyTarget: (name: String, text: String)? {
        guard let d = detail else { return nil }
        return SkillDetail.copyTarget(selectedFile: selectedFile, auxText: auxText, markdown: d.markdown)
    }

    /// Tooltip for Copy: the file it will copy, or why the button is disabled.
    private var copyHelp: String {
        if let name = copyTarget?.name { return "Copy \(name)" }
        if auxLoading { return "File still loading" }
        if auxError != nil { return "Couldn't load this file" }
        return "Nothing to copy yet"
    }

    private func beginEditing() {
        guard let detail else { return }
        selectedFile = "SKILL.md"
        auxText = nil
        auxError = nil
        // Restore a draft preserved across navigation when one exists;
        // otherwise start from the saved markdown. Focus lands in onAppear,
        // once the TextEditor is actually in the window.
        draft = state.draft(for: slug) ?? detail.markdown
        isEditing = true
    }

    /// Whether the editor holds edits that differ from the saved file.
    private var isDirty: Bool {
        guard isEditing, let saved = detail?.markdown else { return false }
        return draft != saved
    }

    private func cancelEditing() {
        // A clean cancel just exits; a dirty one asks first so a misclick
        // or Escape can't silently destroy the edit.
        guard isDirty else {
            discardEditing()
            return
        }
        confirmDiscard = true
    }

    private func discardEditing() {
        // Leave edit mode before touching the text so the editor's
        // onDisappear / onChange don't write the discarded draft back.
        // Clear last in case a callback still fires synchronously.
        isEditing = false
        draft = detail?.markdown ?? ""
        state.clearDraft(for: slug)
    }

    /// Keep an in-progress edit alive across skill and section switches.
    /// No-op once editing has ended, so a discard can't be undone by the
    /// view disappearing.
    private func persistDraft(_ text: String) {
        guard isEditing else { return }
        state.saveDraft(text, for: slug)
    }

    private func refocusEditor() {
        // The field is inserted in the same turn isEditing flips; defer so
        // it is in the window before we move first responder.
        DispatchQueue.main.async { editorFocused = true }
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
            // saveSkillMarkdown already dropped the draft; clear again in
            // case the editor's disappear callback raced and wrote it back.
            state.clearDraft(for: slug)
        } catch {
            state.showToast("Save failed: \(error.localizedDescription)", .error)
        }
    }

    private func load() async {
        loading = true; error = nil; isEditing = false; saving = false
        do {
            let fetched = try await state.fetchDetail(slug)
            detail = fetched
            // Resume a draft preserved across navigation (switching skill or
            // sidebar section recreates this view from scratch).
            if let saved = state.draft(for: slug), saved != fetched.markdown {
                draft = saved
                selectedFile = "SKILL.md"
                isEditing = true
            } else {
                // No divergence from the saved file — drop any stale copy so
                // a later Edit starts clean.
                state.clearDraft(for: slug)
            }
        } catch {
            self.error = error.localizedDescription
        }
        loading = false
    }
}

private extension View {
    /// Disabled controls do not receive hover, so a reason tooltip has to
    /// live on a clear overlay that does. The overlay stays enabled even when
    /// the button's disabled environment would otherwise swallow it.
    @ViewBuilder func hoverHelp(_ text: String, disabled: Bool) -> some View {
        if disabled {
            overlay {
                Color.clear
                    .contentShape(Rectangle())
                    .environment(\.isEnabled, true)
                    .help(text)
                    .accessibilityHidden(true)
            }
        } else {
            help(text)
        }
    }
}
