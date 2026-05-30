import SwiftUI
import AppKit
import SkillsRegistryCore

struct LoginView: View {
    @EnvironmentObject var state: AppState

    var body: some View {
        HStack(spacing: 0) {
            // Left: pitch
            VStack(alignment: .leading, spacing: 22) {
                Wordmark(size: 22)
                Spacer().frame(height: 6)
                Text("Your skills,\neverywhere your\nagents are.")
                    .font(.system(size: 44, weight: .semibold))
                    .foregroundStyle(Brand.fg)
                    .fixedSize(horizontal: false, vertical: true)
                Text("One GitHub-backed registry for the skills your AI tools share. Browse, publish, and import — all from a native app.")
                    .font(.system(size: 15))
                    .foregroundStyle(Brand.muted)
                    .frame(maxWidth: 380, alignment: .leading)
                    .fixedSize(horizontal: false, vertical: true)
                Spacer()
                HStack(spacing: 14) {
                    featureChip("magnifyingglass", "Fuzzy search")
                    featureChip("doc.richtext", "Rich markdown")
                    featureChip("arrow.up.circle", "1-click publish")
                }
            }
            .padding(48)
            .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .leading)

            // Right: sign-in card
            VStack {
                Spacer()
                Card(padding: 28) {
                    VStack(alignment: .leading, spacing: 18) {
                        Eyebrow(text: "Sign in")
                        Text("Connect your GitHub account")
                            .font(.system(size: 20, weight: .semibold))
                            .foregroundStyle(Brand.fg)
                        Text("We open GitHub in your browser to authorize the Skills Registry app. No passwords, no tokens to paste.")
                            .font(.system(size: 13))
                            .foregroundStyle(Brand.muted)
                            .fixedSize(horizontal: false, vertical: true)

                        Button {
                            state.beginLogin()
                        } label: {
                            HStack(spacing: 8) {
                                GitHubMark()
                                Text("Sign in with GitHub")
                            }
                            .frame(maxWidth: .infinity)
                        }
                        .buttonStyle(PrimaryButtonStyle())
                        .accessibilityIdentifier("signInWithGitHub")

                        if let err = state.authError {
                            Text(err).font(.system(size: 12)).foregroundStyle(Brand.danger)
                                .fixedSize(horizontal: false, vertical: true)
                        }

                        Divider().overlay(Brand.border)
                        Text("Uses the GitHub App's secure device flow. The app only sees the repositories you grant it.")
                            .font(Brand.monoSized(11))
                            .foregroundStyle(Brand.meta)
                            .fixedSize(horizontal: false, vertical: true)
                    }
                }
                .frame(width: 360)
                Spacer()
            }
            .frame(width: 440)
            .background(Brand.surface.opacity(0.4))
        }
    }

    private func featureChip(_ icon: String, _ text: String) -> some View {
        HStack(spacing: 7) {
            Image(systemName: icon).font(.system(size: 12)).foregroundStyle(Brand.accent)
            Text(text).font(Brand.monoSized(12)).foregroundStyle(Brand.fg2)
        }
    }
}

struct DeviceCodeSheet: View {
    @EnvironmentObject var state: AppState

    var body: some View {
        VStack(spacing: 20) {
            Wordmark(size: 16)

            if let code = state.deviceCode {
                Text("Authorize in your browser")
                    .font(.system(size: 18, weight: .semibold)).foregroundStyle(Brand.fg)
                Text("We opened GitHub and copied your code. Paste it there to finish signing in.")
                    .font(.system(size: 13)).foregroundStyle(Brand.muted)
                    .multilineTextAlignment(.center).fixedSize(horizontal: false, vertical: true)

                Text(code.userCode)
                    .font(Brand.monoSized(34, weight: .bold))
                    .tracking(8)
                    .foregroundStyle(Brand.accent)
                    .padding(.vertical, 16).padding(.horizontal, 24)
                    .frame(maxWidth: .infinity)
                    .background(Brand.surface)
                    .overlay(RoundedRectangle(cornerRadius: 10).strokeBorder(Brand.border, lineWidth: 1))
                    .clipShape(RoundedRectangle(cornerRadius: 10))
                    .accessibilityIdentifier("deviceUserCode")

                HStack(spacing: 10) {
                    Button {
                        Clipboard.copy(code.userCode)
                    } label: { Label("Copy code", systemImage: "doc.on.doc").frame(maxWidth: .infinity) }
                        .buttonStyle(GhostButtonStyle())
                    Button {
                        NSWorkspace.shared.open(code.verificationURI)
                    } label: { Label("Open GitHub", systemImage: "arrow.up.right.square").frame(maxWidth: .infinity) }
                        .buttonStyle(PrimaryButtonStyle())
                }

                HStack(spacing: 8) {
                    ProgressView().controlSize(.small).tint(Brand.accent)
                    Text("Waiting for authorization…").font(Brand.monoSized(12)).foregroundStyle(Brand.muted)
                }
            } else {
                ProgressView().controlSize(.large).tint(Brand.accent)
                Text("Requesting a device code…").font(Brand.monoSized(12)).foregroundStyle(Brand.muted)
            }

            Button("Cancel") { state.cancelLogin() }
                .buttonStyle(.plain)
                .font(.system(size: 12)).foregroundStyle(Brand.muted)
        }
        .padding(32)
        .frame(width: 420)
        .background(Brand.bg)
    }
}
