import SwiftUI
import AppKit
import SkillsRegistryCore

enum NavSection: String, CaseIterable, Identifiable {
    case browse = "Browse"
    case add = "Add"
    case importLocal = "Import"
    case settings = "Settings"
    var id: String { rawValue }
    var icon: String {
        switch self {
        case .browse: return "square.grid.2x2"
        case .add: return "square.and.arrow.down"
        case .importLocal: return "tray.and.arrow.down"
        case .settings: return "gearshape"
        }
    }
}

struct HomeView: View {
    @EnvironmentObject var state: AppState
    @State private var section: NavSection = .browse

    var body: some View {
        HStack(spacing: 0) {
            sidebar
            Divider().overlay(Brand.border)
            content
        }
        .background(Brand.bg)
        .task { await state.checkForUpdates() }
    }

    private var sidebar: some View {
        VStack(alignment: .leading, spacing: 0) {
            VStack(alignment: .leading, spacing: 10) {
                Wordmark(size: 15)
                if let repo = state.repo {
                    VStack(alignment: .leading, spacing: 4) {
                        Text(repo.fullName)
                            .font(Brand.monoSized(12, weight: .medium)).foregroundStyle(Brand.fg2)
                            .lineLimit(1).truncationMode(.middle)
                        HStack(spacing: 6) {
                            Pill(text: "\(state.skills.count) skills", dot: Brand.accent)
                            Pill(text: state.branch)
                        }
                    }
                }
            }
            .padding(16)

            Divider().overlay(Brand.border)

            VStack(spacing: 4) {
                ForEach(NavSection.allCases) { item in
                    navButton(item)
                }
            }
            .padding(10)

            Spacer()

            Divider().overlay(Brand.border)
            accountFooter.padding(12)
        }
        .frame(width: 248)
        .background(Brand.surface)
    }

    private func navButton(_ item: NavSection) -> some View {
        Button { withAnimation(.easeInOut(duration: 0.22)) { section = item } } label: {
            HStack(spacing: 10) {
                Image(systemName: item.icon).font(.system(size: 13)).frame(width: 18)
                Text(item.rawValue).font(.system(size: 13, weight: .medium))
                Spacer()
            }
            .padding(.horizontal, 12).padding(.vertical, 9)
            .foregroundStyle(section == item ? Brand.fg : Brand.muted)
            .background(section == item ? Brand.surfaceRaised : Color.clear)
            .clipShape(RoundedRectangle(cornerRadius: 7))
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .accessibilityIdentifier("nav-\(item.rawValue)")
    }

    private var accountFooter: some View {
        HStack(spacing: 10) {
            avatar
            VStack(alignment: .leading, spacing: 1) {
                Text(state.identity?.displayName ?? "—").font(.system(size: 12, weight: .medium)).foregroundStyle(Brand.fg)
                Text("@\(state.identity?.login ?? "")").font(Brand.monoSized(10)).foregroundStyle(Brand.meta)
            }
            Spacer()
            Menu {
                Button("Open repo on GitHub") {
                    if let repo = state.repo { NSWorkspace.shared.open(repo.htmlURL) }
                }
                Button("Sign out", role: .destructive) { state.logout() }
            } label: {
                Image(systemName: "ellipsis").font(.system(size: 13)).foregroundStyle(Brand.muted)
            }
            .menuStyle(.borderlessButton).menuIndicator(.hidden).frame(width: 22)
        }
    }

    /// The signed-in user's GitHub avatar, falling back to their initial while
    /// loading or if the image can't be fetched.
    private var avatar: some View {
        Group {
            if let url = state.identity?.avatarURL {
                AsyncImage(url: url) { phase in
                    if let image = phase.image {
                        image.resizable().scaledToFill()
                    } else {
                        avatarFallback
                    }
                }
            } else {
                avatarFallback
            }
        }
        .frame(width: 28, height: 28)
        .clipShape(Circle())
    }

    private var avatarFallback: some View {
        Circle().fill(Brand.surfaceRaised)
            .overlay(Text(initials).font(.system(size: 11, weight: .bold)).foregroundStyle(Brand.accent))
    }

    private var initials: String {
        let n = state.identity?.displayName ?? "?"
        return String(n.prefix(1)).uppercased()
    }

    @ViewBuilder private var content: some View {
        VStack(spacing: 0) {
            UpdateBanner()
            Group {
                switch section {
                case .browse: BrowseView()
                case .add: AddView()
                case .importLocal: ImportView()
                case .settings: SettingsView()
                }
            }
            .id(section)
            .transition(.opacity.combined(with: .move(edge: .trailing)))
        }
    }
}
