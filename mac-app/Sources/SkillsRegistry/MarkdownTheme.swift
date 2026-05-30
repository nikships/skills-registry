import SwiftUI
import MarkdownUI

extension MarkdownUI.Theme {
    /// Dark, brand-matched markdown theme for rendering SKILL.md. Computed so
    /// link/code accents follow the active `AccentTheme`.
    static var brand: MarkdownUI.Theme {
        MarkdownUI.Theme()
        .text {
            ForegroundColor(Brand.fg2)
            FontSize(15)
        }
        .code {
            FontFamilyVariant(.monospaced)
            FontSize(.em(0.86))
            ForegroundColor(Brand.accentSoft)
            BackgroundColor(Brand.surfaceWarm)
        }
        .strong { FontWeight(.semibold); ForegroundColor(Brand.fg) }
        .link { ForegroundColor(Brand.accent) }
        .heading1 { configuration in
            VStack(alignment: .leading, spacing: 0) {
                configuration.label
                    .relativePadding(.bottom, length: .em(0.3))
                    .markdownMargin(top: 24, bottom: 14)
                    .markdownTextStyle { FontWeight(.semibold); FontSize(.em(1.9)); ForegroundColor(Brand.fg) }
                Divider().overlay(Brand.border)
            }
        }
        .heading2 { configuration in
            VStack(alignment: .leading, spacing: 0) {
                configuration.label
                    .relativePadding(.bottom, length: .em(0.3))
                    .markdownMargin(top: 22, bottom: 12)
                    .markdownTextStyle { FontWeight(.semibold); FontSize(.em(1.45)); ForegroundColor(Brand.fg) }
                Divider().overlay(Brand.border)
            }
        }
        .heading3 { configuration in
            configuration.label
                .markdownMargin(top: 20, bottom: 10)
                .markdownTextStyle { FontWeight(.semibold); FontSize(.em(1.2)); ForegroundColor(Brand.fg) }
        }
        .heading4 { configuration in
            configuration.label
                .markdownMargin(top: 18, bottom: 8)
                .markdownTextStyle { FontWeight(.semibold); ForegroundColor(Brand.fg) }
        }
        .paragraph { configuration in
            configuration.label
                .fixedSize(horizontal: false, vertical: true)
                .relativeLineSpacing(.em(0.28))
                .markdownMargin(top: 0, bottom: 14)
        }
        .blockquote { configuration in
            HStack(spacing: 0) {
                RoundedRectangle(cornerRadius: 6).fill(Brand.accent.opacity(0.7))
                    .relativeFrame(width: .em(0.2))
                configuration.label
                    .markdownTextStyle { ForegroundColor(Brand.muted) }
                    .relativePadding(.horizontal, length: .em(1))
            }
            .fixedSize(horizontal: false, vertical: true)
        }
        .codeBlock { configuration in
            ScrollView(.horizontal, showsIndicators: false) {
                configuration.label
                    .fixedSize(horizontal: false, vertical: true)
                    .relativeLineSpacing(.em(0.22))
                    .markdownTextStyle { FontFamilyVariant(.monospaced); FontSize(.em(0.85)); ForegroundColor(Brand.fg2) }
                    .padding(16)
            }
            .background(Brand.surfaceWarm)
            .overlay(RoundedRectangle(cornerRadius: 8).strokeBorder(Brand.border, lineWidth: 1))
            .clipShape(RoundedRectangle(cornerRadius: 8))
            .markdownMargin(top: 4, bottom: 16)
        }
        .listItem { configuration in
            configuration.label.markdownMargin(top: .em(0.2))
        }
        .table { configuration in
            configuration.label
                .fixedSize(horizontal: false, vertical: true)
                .markdownTableBorderStyle(.init(color: Brand.border))
                .markdownTableBackgroundStyle(.alternatingRows(Brand.bg, Brand.surface))
                .markdownMargin(top: 0, bottom: 16)
        }
        .tableCell { configuration in
            configuration.label
                .markdownTextStyle {
                    if configuration.row == 0 { FontWeight(.semibold); ForegroundColor(Brand.fg) }
                }
                .fixedSize(horizontal: false, vertical: true)
                .padding(.vertical, 6).padding(.horizontal, 12)
        }
        .thematicBreak {
            Divider().overlay(Brand.border).markdownMargin(top: 20, bottom: 20)
        }
    }
}
