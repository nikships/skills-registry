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
            Text("This deletes the \(slug)/ folder from \(state.repo?.fullName ?? "the repo"). It can't be undone from here.")
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
                if d.files.count > 1 {
                    Divider().overlay(Brand.border)
                    fileRail(d.files)
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
                        HStack(spacing: 8) {
                            Image(systemName: icon(for: f)).font(.system(size: 11))
                                .foregroundStyle(f == "SKILL.md" ? Brand.accent : Brand.muted)
                                .frame(width: 14)
                            Text(f).font(Brand.monoSized(11)).foregroundStyle(Brand.fg2)
                                .lineLimit(1).truncationMode(.middle)
                        }
                        .padding(.horizontal, 14).padding(.vertical, 5)
                        .frame(maxWidth: .infinity, alignment: .leading)
                    }
                }
            }
        }
        .frame(width: 210)
        .background(Brand.surface)
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
