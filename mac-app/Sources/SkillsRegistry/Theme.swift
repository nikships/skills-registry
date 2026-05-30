import SwiftUI

/// Brand palette + type, lifted from the website design tokens
/// (`website/app/globals.css`): near-black surfaces, warm cream foreground,
/// hot-pink accent, mono numerics.
enum Brand {
    static let bg = Color(hex: 0x000000)
    static let surface = Color(hex: 0x0D0D0D)
    static let surfaceWarm = Color(hex: 0x141414)
    static let surfaceRaised = Color(hex: 0x171717)
    static let fg = Color(hex: 0xF5F3EE)
    static let fg2 = Color(hex: 0xF5F3EE).opacity(0.86)
    static let muted = Color(hex: 0x8A8A85)
    static let meta = Color(hex: 0xF5F3EE).opacity(0.40)
    static let border = Color(hex: 0x1F1F1F)
    static let borderSoft = Color(hex: 0x141414)
    /// The accent threads through buttons, links, and highlights. It's the one
    /// palette token the user can re-theme (see `AccentTheme` / `ThemeManager`),
    /// so it reads from the active selection rather than being a fixed `let`.
    static var accent: Color { AppTheme.current.accent }
    static var accentSoft: Color { AppTheme.current.accentSoft }
    static let success = Color(hex: 0x16A34A)
    static let warn = Color(hex: 0xEAB308)
    static let danger = Color(hex: 0xDC2626)

    static let mono = Font.system(.body, design: .monospaced)
    static func monoSized(_ size: CGFloat, weight: Font.Weight = .regular) -> Font {
        .system(size: size, weight: weight, design: .monospaced)
    }
}

// MARK: - Accent theming

/// Selectable accent palettes layered over the fixed dark surfaces. A true
/// light theme would clash with the hardcoded near-black surfaces, so the user
/// re-themes the accent only.
enum AccentTheme: String, CaseIterable, Identifiable {
    case pink, blue, green, amber, violet

    var id: String { rawValue }

    var label: String {
        switch self {
        case .pink: return "Pink"
        case .blue: return "Blue"
        case .green: return "Green"
        case .amber: return "Amber"
        case .violet: return "Violet"
        }
    }

    var accent: Color {
        switch self {
        case .pink: return Color(hex: 0xFF4D8D)
        case .blue: return Color(hex: 0x3B82F6)
        case .green: return Color(hex: 0x22C55E)
        case .amber: return Color(hex: 0xF59E0B)
        case .violet: return Color(hex: 0x8B5CF6)
        }
    }

    var accentSoft: Color {
        switch self {
        case .pink: return Color(hex: 0xFF9EC2)
        case .blue: return Color(hex: 0x93C5FD)
        case .green: return Color(hex: 0x86EFAC)
        case .amber: return Color(hex: 0xFCD34D)
        case .violet: return Color(hex: 0xC4B5FD)
        }
    }
}

/// Holds the live accent so `Brand.accent` can read it synchronously from
/// anywhere. Mutated only by `ThemeManager`.
enum AppTheme {
    static var current: AccentTheme = .pink
}

/// Persists the user's accent choice and republishes on change so SwiftUI
/// rebuilds the palette-dependent view tree.
@MainActor
final class ThemeManager: ObservableObject {
    private static let key = "accentTheme"

    @Published var accent: AccentTheme {
        didSet {
            AppTheme.current = accent
            UserDefaults.standard.set(accent.rawValue, forKey: Self.key)
        }
    }

    init() {
        let raw = UserDefaults.standard.string(forKey: Self.key)
        let theme = raw.flatMap(AccentTheme.init(rawValue:)) ?? .pink
        accent = theme
        AppTheme.current = theme
    }
}

extension Color {
    init(hex: UInt32) {
        let r = Double((hex >> 16) & 0xFF) / 255.0
        let g = Double((hex >> 8) & 0xFF) / 255.0
        let b = Double(hex & 0xFF) / 255.0
        self.init(.sRGB, red: r, green: g, blue: b, opacity: 1)
    }
}

// MARK: - Reusable styles

/// A card surface with subtle border.
struct Card<Content: View>: View {
    var padding: CGFloat = 20
    @ViewBuilder var content: Content
    var body: some View {
        content
            .padding(padding)
            .background(Brand.surface)
            .overlay(RoundedRectangle(cornerRadius: 12).strokeBorder(Brand.border, lineWidth: 1))
            .clipShape(RoundedRectangle(cornerRadius: 12))
    }
}

/// Primary (filled cream) button style.
struct PrimaryButtonStyle: ButtonStyle {
    var tint: Color = Brand.fg
    var fg: Color = Color(hex: 0x0A0A0A)
    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .font(.system(size: 14, weight: .semibold))
            .padding(.horizontal, 16).padding(.vertical, 9)
            .background(tint.opacity(configuration.isPressed ? 0.85 : 1))
            .foregroundStyle(fg)
            .clipShape(RoundedRectangle(cornerRadius: 7))
            .contentShape(Rectangle())
    }
}

/// Ghost (outline) button style.
struct GhostButtonStyle: ButtonStyle {
    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .font(.system(size: 14, weight: .medium))
            .padding(.horizontal, 16).padding(.vertical, 9)
            .foregroundStyle(Brand.fg)
            .background(configuration.isPressed ? Brand.surfaceWarm : Color.clear)
            .overlay(RoundedRectangle(cornerRadius: 7).strokeBorder(Brand.meta, lineWidth: 1))
            .clipShape(RoundedRectangle(cornerRadius: 7))
            .contentShape(Rectangle())
    }
}

/// A small monospaced "eyebrow" / pill label.
struct Pill: View {
    let text: String
    var dot: Color? = nil
    var body: some View {
        HStack(spacing: 6) {
            if let dot { Circle().fill(dot).frame(width: 5, height: 5) }
            Text(text).font(Brand.monoSized(11)).foregroundStyle(Brand.fg2)
        }
        .padding(.horizontal, 10).padding(.vertical, 4)
        .background(Brand.surface)
        .overlay(Capsule().strokeBorder(Brand.border, lineWidth: 1))
        .clipShape(Capsule())
    }
}
