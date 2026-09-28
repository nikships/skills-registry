import SwiftUI
import SkillsRegistryCore

@main
struct SkillsRegistryApp: App {
    @StateObject private var state: AppState
    @StateObject private var theme = ThemeManager()
    @StateObject private var updater = UpdaterManager()

    init() {
        let args = ProcessInfo.processInfo.arguments
        let env = ProcessInfo.processInfo.environment
        let demo = args.contains("--demo")
            || args.contains("--demo-setup")
            || args.contains("--demo-setup-loading")
            || env["SKILLS_APP_DEMO"] == "1"
            || env["SKILLS_APP_DEMO_SETUP"] == "1"
            || env["SKILLS_APP_DEMO_SETUP"] == "loading"
        let preview: AuthPreview?
        if args.contains("--demo-auth-expired") {
            preview = .expired
        } else if args.contains("--demo-auth-offline") {
            preview = .offline
        } else {
            preview = nil
        }
        let setup: DemoSetup
        if args.contains("--demo-setup-loading") || env["SKILLS_APP_DEMO_SETUP"] == "loading" {
            setup = .loading
        } else if args.contains("--demo-setup") || env["SKILLS_APP_DEMO_SETUP"] == "1" {
            setup = .loaded
        } else {
            setup = .none
        }
        _state = StateObject(wrappedValue: AppState(demo: demo, authPreview: preview, demoSetup: setup))
        _theme = StateObject(wrappedValue: ThemeManager(demo: demo))
    }

    var body: some Scene {
        WindowGroup {
            RootView()
                .environmentObject(state)
                .environmentObject(theme)
                .environmentObject(updater)
                .frame(minWidth: 940, minHeight: 620)
                .background(Brand.bg)
                .preferredColorScheme(.dark)
                .task { await state.bootstrap() }
        }
        .windowStyle(.titleBar)
        .windowToolbarStyle(.unified(showsTitle: false))
        .defaultSize(width: 1100, height: 740)
        .commands {
            CommandGroup(after: .appInfo) {
                CheckForUpdatesCommand(updater: updater)
            }
        }
    }
}

/// Top-level router: switches on the auth/setup phase and overlays toasts.
struct RootView: View {
    @EnvironmentObject var state: AppState
    @EnvironmentObject var theme: ThemeManager

    var body: some View {
        ZStack {
            Brand.bg.ignoresSafeArea()
            phaseContent
                // Rebuild the palette-dependent tree when the accent changes,
                // without re-running the root `bootstrap` task.
                .id(theme.accent)
        }
        .toastOverlay(state.toast)
        .sheet(isPresented: Binding(
            get: { state.deviceCode != nil || state.authInProgress },
            set: { if !$0 { state.cancelLogin() } }
        )) {
            DeviceCodeSheet()
                .environmentObject(state)
        }
        .tint(Brand.accent)
        .foregroundStyle(Brand.fg)
    }

    @ViewBuilder private var phaseContent: some View {
        switch state.phase {
        case .loading:
            LoadingView()
        case .signedOut:
            LoginView()
        case .setup:
            SetupView()
        case .ready:
            HomeView()
        }
    }
}

struct LoadingView: View {
    var body: some View {
        VStack(spacing: 16) {
            ProgressView().controlSize(.large).tint(Brand.accent)
            Text("Loading…").font(Brand.monoSized(12)).foregroundStyle(Brand.muted)
        }
    }
}
