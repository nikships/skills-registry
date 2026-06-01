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
            Text("This deletes the \(slug)/ folder from \(state.repo?.fullName ?? "the repo"), clears the MCP cache, and removes it from your agent folders. It can't be undone from here.")
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
            if !state.isDemo {
                Button { showInstall = true } label: {
                    Label("Install", systemImage: "arrow.down.circle").font(.system(size: 12))
                }
                .buttonStyle(GhostButtonStyle())
                .disabled(detail == nil)
                .accessibilityIdentifier("installSkill")
            }
            Button { openOnGitHub() } label: {
                Label("GitHub", systemImage: "arrow.up.right.square").font(.system(size: 12))
            }.buttonStyle(GhostButtonStyle())
            Button { if let d = detail { Clipboard.copy(d.markdown) ; state.showToast("Copied SKILL.md", .ok) } } label: {
                Image(systemName: "doc.on.doc").font(.system(size: 12))
            }.buttonStyle(GhostButtonStyle())
            if !state.isDemo {
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

    private func load() async {
        loading = true; error = nil
        do {
            detail = try await state.fetchDetail(slug)
        } catch {
            self.error = error.localizedDescription
        }
        loading = false
    }
}
