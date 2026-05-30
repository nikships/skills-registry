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
    static let accent = Color(hex: 0xFF4D8D)
    static let accentSoft = Color(hex: 0xFF9EC2)
    static let success = Color(hex: 0x16A34A)
    static let warn = Color(hex: 0xEAB308)
    static let danger = Color(hex: 0xDC2626)

    static let mono = Font.system(.body, design: .monospaced)
    static func monoSized(_ size: CGFloat, weight: Font.Weight = .regular) -> Font {
        .system(size: size, weight: weight, design: .monospaced)
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
