import SwiftUI
import AppKit
import SkillsRegistryCore

struct SettingsView: View {
    @EnvironmentObject var state: AppState
    @EnvironmentObject var theme: ThemeManager
    @EnvironmentObject var updater: UpdaterManager
    @State private var installing = false
    @State private var installingSkill = false

    private var appVersion: String {
        (Bundle.main.infoDictionary?["CFBundleShortVersionString"] as? String) ?? "dev"
    }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 20) {
                VStack(alignment: .leading, spacing: 8) {
                    Eyebrow(text: "Settings")
                    Text("Connect your agents").font(.system(size: 22, weight: .semibold)).foregroundStyle(Brand.fg)
                }
                appearanceCard
                appCard
                agentSkillCard
                cliCard
                mcpCard
                registryCard
            }
            .padding(24)
            .frame(maxWidth: 760, alignment: .leading)
            .frame(maxWidth: .infinity, alignment: .leading)
        }
        .background(Brand.bg)
        .task { await state.checkForUpdates() }
    }

    private var appCard: some View {
        Card {
            VStack(alignment: .leading, spacing: 14) {
                HStack {
                    Label("App", systemImage: "app.badge").font(.system(size: 15, weight: .semibold)).foregroundStyle(Brand.fg)
                    Spacer()
                    Pill(text: "v\(appVersion)", dot: Brand.success)
                }
                Text("Skills Registry updates itself in the background and verifies each release's signature before installing. You can also check now.")
                    .font(.system(size: 13)).foregroundStyle(Brand.muted).fixedSize(horizontal: false, vertical: true)
                HStack(spacing: 10) {
                    Button { updater.checkForUpdates() } label: {
                        Label("Check for updates", systemImage: "arrow.triangle.2.circlepath")
                    }
                    .buttonStyle(PrimaryButtonStyle())
                    .disabled(!updater.canCheckForUpdates)
                    .accessibilityIdentifier("checkAppUpdates")
                    Spacer()
                    Toggle("Check automatically", isOn: Binding(
                        get: { updater.automaticallyChecksForUpdates },
                        set: { updater.setAutomaticChecks($0) }))
                        .toggleStyle(.switch)
                        .font(.system(size: 12)).foregroundStyle(Brand.muted)
                }
            }
        }
    }

    private var agentSkillCard: some View {
        Card {
            VStack(alignment: .leading, spacing: 14) {
                HStack {
                    Label("Agent skill", systemImage: "sparkles").font(.system(size: 15, weight: .semibold)).foregroundStyle(Brand.fg)
                    Spacer()
                    agentSkillPill
                }
                Text("The `skills-registry` skill teaches each agent how to discover, fetch, and publish skills from your registry. Install it into every detected agent in one click.")
                    .font(.system(size: 13)).foregroundStyle(Brand.muted).fixedSize(horizontal: false, vertical: true)

                if state.metaSkill.detectedCount > 0 {
                    VStack(alignment: .leading, spacing: 6) {
                        ForEach(state.metaSkill.targets) { row in
                            HStack(spacing: 8) {
                                Circle().fill(stateColor(row.state)).frame(width: 6, height: 6)
                                Text(row.target.display).font(Brand.monoSized(12)).foregroundStyle(Brand.fg2)
                                Spacer()
                                Text(stateLabel(row.state)).font(Brand.monoSized(11)).foregroundStyle(Brand.meta)
                            }
                        }
                    }
                    .padding(12)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .background(Brand.surfaceWarm)
                    .overlay(RoundedRectangle(cornerRadius: 8).strokeBorder(Brand.border, lineWidth: 1))
                    .clipShape(RoundedRectangle(cornerRadius: 8))
                } else {
                    Text("No agents detected in your home folder yet.")
                        .font(Brand.monoSized(11)).foregroundStyle(Brand.meta)
                }

                Button {
                    installingSkill = true
                    Task { await state.installMetaSkill(); installingSkill = false }
                } label: {
                    HStack(spacing: 8) {
                        if installingSkill { ProgressView().controlSize(.small) }
                        Text(state.metaSkill.anyMissing ? "Install in all agents" : "Reinstall / refresh")
                    }
                }
                .buttonStyle(PrimaryButtonStyle())
                .disabled(installingSkill || state.metaSkill.detectedCount == 0)
                .accessibilityIdentifier("installMetaSkill")
            }
        }
    }

    @ViewBuilder private var agentSkillPill: some View {
        if state.metaSkill.detectedCount == 0 {
            Pill(text: "no agents", dot: Brand.meta)
        } else if state.metaSkill.anyMissing {
            Pill(text: "action needed", dot: Brand.warn)
        } else if state.metaSkill.anyOutdated {
            Pill(text: "update available", dot: Brand.warn)
        } else {
            Pill(text: "installed", dot: Brand.success)
        }
    }

    private func stateColor(_ s: MetaSkill.State) -> Color {
        switch s {
        case .missing: return Brand.danger
        case .outdated: return Brand.warn
        case .current: return Brand.success
        }
    }

    private func stateLabel(_ s: MetaSkill.State) -> String {
        switch s {
        case .missing: return "not installed"
        case .outdated: return "out of date"
        case .current: return "installed"
        }
    }

    private var appearanceCard: some View {
        Card {
            VStack(alignment: .leading, spacing: 14) {
                Label("Appearance", systemImage: "paintpalette").font(.system(size: 15, weight: .semibold)).foregroundStyle(Brand.fg)
                Text("Pick an accent color. Surfaces stay dark; the accent threads through buttons, links, and highlights.")
                    .font(.system(size: 13)).foregroundStyle(Brand.muted).fixedSize(horizontal: false, vertical: true)
                HStack(spacing: 14) {
                    ForEach(AccentTheme.allCases) { swatch($0) }
                    Spacer()
                }
            }
        }
    }

    private func swatch(_ t: AccentTheme) -> some View {
        let isSelected = theme.accent == t
        return Button { theme.accent = t } label: {
            VStack(spacing: 6) {
                Circle().fill(t.accent).frame(width: 26, height: 26)
                    .overlay(Circle().strokeBorder(Brand.fg, lineWidth: isSelected ? 2 : 0))
                    .overlay(Circle().strokeBorder(Brand.border, lineWidth: 1))
                Text(t.label).font(Brand.monoSized(10)).foregroundStyle(isSelected ? Brand.fg : Brand.meta)
            }
        }
        .buttonStyle(.plain)
        .accessibilityIdentifier("theme-\(t.rawValue)")
    }

    private var cliCard: some View {
        Card {
            VStack(alignment: .leading, spacing: 14) {
                HStack {
                    Label("Command-line tool", systemImage: "terminal").font(.system(size: 15, weight: .semibold)).foregroundStyle(Brand.fg)
                    Spacer()
                    if let update = state.cliUpdate {
                        Pill(text: "update → \(update.version.string)", dot: Brand.warn)
                    } else if state.cliInstalled {
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
