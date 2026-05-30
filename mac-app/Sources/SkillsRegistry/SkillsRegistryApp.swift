import SwiftUI
import SkillsRegistryCore

@main
struct SkillsRegistryApp: App {
    @StateObject private var state: AppState
    @StateObject private var theme = ThemeManager()
    @StateObject private var updater = UpdaterManager()

    init() {
        let demo = ProcessInfo.processInfo.arguments.contains("--demo")
            || ProcessInfo.processInfo.environment["SKILLS_APP_DEMO"] == "1"
        _state = StateObject(wrappedValue: AppState(demo: demo))
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
