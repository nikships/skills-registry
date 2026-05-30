import SwiftUI
import AppKit
import SkillsRegistryCore

struct SetupView: View {
    @EnvironmentObject var state: AppState
    @State private var newName = "skills-registry"
    @State private var isPrivate = true
    @State private var connectManual = ""

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 24) {
                header

                if state.setupLoading && state.installRepos.isEmpty {
                    HStack(spacing: 10) {
                        ProgressView().controlSize(.small).tint(Brand.accent)
                        Text("Checking your GitHub App installation…")
                            .font(Brand.monoSized(12)).foregroundStyle(Brand.muted)
                    }
                    .padding(.vertical, 20)
                }

                createCard
                connectCard
                installAppCard
            }
            .padding(40)
            .frame(maxWidth: 720, alignment: .leading)
            .frame(maxWidth: .infinity)
        }
    }

    private var header: some View {
        VStack(alignment: .leading, spacing: 12) {
            Wordmark(size: 20)
            Eyebrow(text: "Set up your registry")
            Text("Pick where your skills live")
                .font(.system(size: 28, weight: .semibold)).foregroundStyle(Brand.fg)
            Text("Create a fresh registry repository, or connect one the Skills Registry app can already access. This is shared with the CLI and the hosted MCP server.")
                .font(.system(size: 14)).foregroundStyle(Brand.muted)
                .fixedSize(horizontal: false, vertical: true)
            if let id = state.identity {
                Pill(text: "Signed in as \(id.login)", dot: Brand.success)
            }
        }
    }

    private var createCard: some View {
        Card {
            VStack(alignment: .leading, spacing: 14) {
                Label("Create a new registry", systemImage: "plus.circle.fill")
                    .font(.system(size: 15, weight: .semibold)).foregroundStyle(Brand.fg)
                HStack(spacing: 12) {
                    TextField("repo name", text: $newName)
                        .textFieldStyle(.plain)
                        .font(Brand.monoSized(13))
                        .padding(.horizontal, 12).padding(.vertical, 9)
                        .background(Brand.surfaceWarm)
                        .overlay(RoundedRectangle(cornerRadius: 7).strokeBorder(Brand.border, lineWidth: 1))
                        .clipShape(RoundedRectangle(cornerRadius: 7))
                        .accessibilityIdentifier("newRepoName")
                    Picker("", selection: $isPrivate) {
                        Text("Private").tag(true)
                        Text("Public").tag(false)
                    }
                    .pickerStyle(.segmented)
                    .frame(width: 160)
                }
                Button {
                    Task { await state.createRegistry(name: newName.trimmingCharacters(in: .whitespaces), isPrivate: isPrivate) }
                } label: {
                    HStack(spacing: 8) {
                        if state.setupLoading { ProgressView().controlSize(.small) }
                        Text("Create \(state.identity?.login ?? "you")/\(newName)")
                    }
                }
                .buttonStyle(PrimaryButtonStyle())
                .disabled(newName.trimmingCharacters(in: .whitespaces).isEmpty || state.setupLoading)
                .accessibilityIdentifier("createRegistry")

                Text("Needs the app's Administration permission. If it's not granted, we'll open github.com to create it, then you can connect it below.")
                    .font(Brand.monoSized(11)).foregroundStyle(Brand.meta)
                    .fixedSize(horizontal: false, vertical: true)
            }
        }
    }

    private var connectCard: some View {
        Card {
            VStack(alignment: .leading, spacing: 14) {
                HStack {
                    Label("Connect an existing repo", systemImage: "link")
                        .font(.system(size: 15, weight: .semibold)).foregroundStyle(Brand.fg)
                    Spacer()
                    Button { Task { await state.loadInstallations() } } label: {
                        Image(systemName: "arrow.clockwise").font(.system(size: 12))
                    }.buttonStyle(.plain).foregroundStyle(Brand.muted)
                }

                if state.installRepos.isEmpty {
                    Text("No repositories accessible yet. Install the app below, then refresh.")
                        .font(.system(size: 13)).foregroundStyle(Brand.muted)
                } else {
                    VStack(spacing: 0) {
                        ForEach(state.installRepos, id: \.fullName) { r in
                            Button {
                                if let ref = RepoRef(fullName: r.fullName) {
                                    Task { await state.connect(ref, branch: r.defaultBranch) }
                                }
                            } label: {
                                HStack(spacing: 10) {
                                    Image(systemName: r.isPrivate ? "lock.fill" : "book.closed")
                                        .font(.system(size: 12)).foregroundStyle(Brand.muted)
                                    Text(r.fullName).font(Brand.monoSized(13)).foregroundStyle(Brand.fg)
                                    Spacer()
                                    Text(r.defaultBranch).font(Brand.monoSized(11)).foregroundStyle(Brand.meta)
                                    Image(systemName: "arrow.right").font(.system(size: 11)).foregroundStyle(Brand.accent)
                                }
                                .padding(.vertical, 10).padding(.horizontal, 12)
                                .contentShape(Rectangle())
                            }
                            .buttonStyle(.plain)
                            .background(Brand.surfaceWarm.opacity(0.0001))
                            if r.fullName != state.installRepos.last?.fullName {
                                Divider().overlay(Brand.border)
                            }
                        }
                    }
                    .background(Brand.surfaceWarm)
                    .overlay(RoundedRectangle(cornerRadius: 8).strokeBorder(Brand.border, lineWidth: 1))
                    .clipShape(RoundedRectangle(cornerRadius: 8))
                }

                HStack(spacing: 8) {
                    TextField("owner/repo", text: $connectManual)
                        .textFieldStyle(.plain).font(Brand.monoSized(13))
                        .padding(.horizontal, 12).padding(.vertical, 8)
                        .background(Brand.surfaceWarm)
                        .overlay(RoundedRectangle(cornerRadius: 7).strokeBorder(Brand.border, lineWidth: 1))
                        .clipShape(RoundedRectangle(cornerRadius: 7))
                    Button("Connect") {
                        if let ref = RepoRef(fullName: connectManual.trimmingCharacters(in: .whitespaces)) {
                            Task { await state.connect(ref, branch: "main") }
                        } else {
                            state.showToast("Enter owner/repo", .error)
                        }
                    }
                    .buttonStyle(GhostButtonStyle())
                }
            }
        }
    }

    private var installAppCard: some View {
        Card {
            VStack(alignment: .leading, spacing: 10) {
                Label("Don't see your repo?", systemImage: "puzzlepiece.extension")
                    .font(.system(size: 14, weight: .semibold)).foregroundStyle(Brand.fg)
                Text("Install the Skills Registry GitHub App on the repository you want, then refresh. This is also what lets the hosted MCP server serve your skills to coding agents.")
                    .font(.system(size: 13)).foregroundStyle(Brand.muted)
                    .fixedSize(horizontal: false, vertical: true)
                HStack {
                    Button {
                        NSWorkspace.shared.open(AppConfig.appInstallURL)
                    } label: { Label("Install GitHub App", systemImage: "arrow.up.right.square") }
                        .buttonStyle(GhostButtonStyle())
                    Button { Task { await state.loadInstallations() } } label: {
                        Label("Refresh", systemImage: "arrow.clockwise")
                    }.buttonStyle(.plain).foregroundStyle(Brand.muted).font(.system(size: 13))
                }
            }
        }
    }
}
