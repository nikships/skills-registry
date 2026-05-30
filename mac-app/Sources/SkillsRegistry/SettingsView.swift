import SwiftUI
import AppKit
import SkillsRegistryCore

struct SettingsView: View {
    @EnvironmentObject var state: AppState
    @State private var installing = false

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 20) {
                VStack(alignment: .leading, spacing: 8) {
                    Eyebrow(text: "Settings")
                    Text("Connect your agents").font(.system(size: 22, weight: .semibold)).foregroundStyle(Brand.fg)
                }
                cliCard
                mcpCard
                registryCard
            }
            .padding(24)
            .frame(maxWidth: 760, alignment: .leading)
            .frame(maxWidth: .infinity, alignment: .leading)
        }
        .background(Brand.bg)
        .task { await state.refreshCLIStatus() }
    }

    private var cliCard: some View {
        Card {
            VStack(alignment: .leading, spacing: 14) {
                HStack {
                    Label("Command-line tool", systemImage: "terminal").font(.system(size: 15, weight: .semibold)).foregroundStyle(Brand.fg)
                    Spacer()
                    if state.cliInstalled {
                        Pill(text: state.cliVersion ?? "installed", dot: Brand.success)
                    } else {
                        Pill(text: "not installed", dot: Brand.warn)
                    }
                }
                Text("The `skills-registry` CLI does bulk operations and works offline. One click downloads the latest darwin/arm64 release to ~/.local/bin.")
                    .font(.system(size: 13)).foregroundStyle(Brand.muted).fixedSize(horizontal: false, vertical: true)
                HStack(spacing: 10) {
                    Button {
                        installing = true
                        Task { await state.installCLI(); installing = false }
                    } label: {
                        HStack(spacing: 8) {
                            if installing { ProgressView().controlSize(.small) }
                            Text(state.cliInstalled ? "Reinstall / update" : "Install CLI")
                        }
                    }
                    .buttonStyle(PrimaryButtonStyle())
                    .disabled(installing)
                    .accessibilityIdentifier("installCLI")

                    Button {
                        Clipboard.copy("curl -fsSL https://raw.githubusercontent.com/\(AppConfig.projectRepo)/main/install.sh | sh")
                        state.showToast("Copied install command", .ok)
                    } label: { Label("Copy install command", systemImage: "doc.on.doc") }
                        .buttonStyle(GhostButtonStyle())
                }
                if state.cliInstalled && !CLIInstaller.installDirOnPath {
                    Text("Note: ~/.local/bin isn't on your PATH. Add it to use `skills-registry` from any shell.")
                        .font(Brand.monoSized(11)).foregroundStyle(Brand.warn).fixedSize(horizontal: false, vertical: true)
                }
            }
        }
    }

    private var mcpCard: some View {
        Card {
            VStack(alignment: .leading, spacing: 14) {
                Label("MCP server for coding agents", systemImage: "point.3.connected.trianglepath.dotted")
                    .font(.system(size: 15, weight: .semibold)).foregroundStyle(Brand.fg)
                Text("Add this to your MCP client (Claude, Cursor, …) to let agents search and read your skills. The hosted server is read-only and authorizes via the GitHub App you installed.")
                    .font(.system(size: 13)).foregroundStyle(Brand.muted).fixedSize(horizontal: false, vertical: true)

                Text(AppConfig.mcpJSONSnippet)
                    .font(Brand.monoSized(12)).foregroundStyle(Brand.fg2)
                    .padding(14)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .background(Brand.surfaceWarm)
                    .overlay(RoundedRectangle(cornerRadius: 8).strokeBorder(Brand.border, lineWidth: 1))
                    .clipShape(RoundedRectangle(cornerRadius: 8))
                    .textSelection(.enabled)

                HStack(spacing: 10) {
                    Button {
                        Clipboard.copy(AppConfig.mcpJSONSnippet)
                        state.showToast("Copied MCP config", .ok)
                    } label: { Label("Copy JSON", systemImage: "doc.on.doc") }
                        .buttonStyle(PrimaryButtonStyle())
                        .accessibilityIdentifier("copyMCP")
                    Button {
                        Clipboard.copy(AppConfig.hostedMCPURL)
                        state.showToast("Copied MCP URL", .ok)
                    } label: { Label("Copy URL", systemImage: "link") }
                        .buttonStyle(GhostButtonStyle())
                    Button {
                        NSWorkspace.shared.open(AppConfig.appInstallURL)
                    } label: { Label("Manage app access", systemImage: "arrow.up.right.square") }
                        .buttonStyle(GhostButtonStyle())
                }
            }
        }
    }

    private var registryCard: some View {
        Card {
            VStack(alignment: .leading, spacing: 12) {
                Label("Registry", systemImage: "shippingbox").font(.system(size: 15, weight: .semibold)).foregroundStyle(Brand.fg)
                if let repo = state.repo {
                    HStack {
                        VStack(alignment: .leading, spacing: 3) {
                            Text(repo.fullName).font(Brand.monoSized(13)).foregroundStyle(Brand.fg)
                            Text("branch \(state.branch) · \(state.skills.count) skills").font(Brand.monoSized(11)).foregroundStyle(Brand.meta)
                        }
                        Spacer()
                        Button { NSWorkspace.shared.open(repo.htmlURL) } label: {
                            Label("Open", systemImage: "arrow.up.right.square")
                        }.buttonStyle(GhostButtonStyle())
                    }
                }
                Divider().overlay(Brand.border)
                HStack {
                    Text("Signed in as @\(state.identity?.login ?? "")").font(Brand.monoSized(12)).foregroundStyle(Brand.muted)
                    Spacer()
                    Button("Sign out", role: .destructive) { state.logout() }
                        .buttonStyle(GhostButtonStyle())
                }
            }
        }
    }
}
