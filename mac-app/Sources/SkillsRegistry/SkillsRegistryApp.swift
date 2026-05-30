import SwiftUI
import SkillsRegistryCore

@main
struct SkillsRegistryApp: App {
    @StateObject private var state: AppState

    init() {
        let demo = ProcessInfo.processInfo.arguments.contains("--demo")
            || ProcessInfo.processInfo.environment["SKILLS_APP_DEMO"] == "1"
        _state = StateObject(wrappedValue: AppState(demo: demo))
    }

    var body: some Scene {
        WindowGroup {
            RootView()
                .environmentObject(state)
                .frame(minWidth: 940, minHeight: 620)
                .background(Brand.bg)
                .preferredColorScheme(.dark)
                .task { await state.bootstrap() }
        }
        .windowStyle(.titleBar)
        .windowToolbarStyle(.unified(showsTitle: false))
        .defaultSize(width: 1100, height: 740)
        .commands {
            CommandGroup(replacing: .appInfo) {
                Button("About Skills Registry") {}
            }
        }
    }
}

/// Top-level router: switches on the auth/setup phase and overlays toasts.
struct RootView: View {
    @EnvironmentObject var state: AppState

    var body: some View {
        ZStack {
            Brand.bg.ignoresSafeArea()
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
}

struct LoadingView: View {
    var body: some View {
        VStack(spacing: 16) {
            ProgressView().controlSize(.large).tint(Brand.accent)
            Text("Loading…").font(Brand.monoSized(12)).foregroundStyle(Brand.muted)
        }
    }
}
