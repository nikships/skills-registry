import SwiftUI
import AppKit
import SkillsRegistryCore

/// Posts VoiceOver announcements. SwiftUI has no announcement API, so the
/// toast and section switches go through NSAccessibility directly.
enum AccessibilityAnnouncer {
    /// - Parameter priority: `.high` interrupts current speech (errors);
    ///   `.medium` waits its turn (confirmations, navigation).
    static func post(_ message: String, priority: NSAccessibilityPriorityLevel = .medium) {
        let text = message.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !text.isEmpty else { return }
        // Priority has to be an NSNumber. A raw Swift integer does not bridge
        // into the announcement userInfo VoiceOver actually reads.
        NSAccessibility.post(
            element: NSApplication.shared,
            notification: .announcementRequested,
            userInfo: [
                .announcement: text,
                .priority: NSNumber(value: priority.rawValue),
            ])
    }
}

// MARK: - Accessible list row

/// A list row that is a real button, so keyboard, Full Keyboard Access, and
/// VoiceOver can activate it. Hover and pressed are drawn here; the label
/// only lays out content. `.combine` publishes the label's texts as one AX
/// label (name, slug or grades, description) instead of a pile of static texts.
struct ListRowButton<Label: View>: View {
    var selected: Bool
    var hint: String
    var identifier: String
    /// Demo-only (`--demo-hover`). Production passes false; a real pointer
    /// sets `hovering` instead. The UI driver cannot move the OS cursor in
    /// window scope, so screenshots of the hover treatment use this flag.
    var previewHover: Bool = false
    var action: () -> Void
    @ViewBuilder var label: () -> Label

    @State private var hovering = false
    @FocusState private var focused: Bool

    private var highlighted: Bool { hovering || focused || previewHover }

    var body: some View {
        Button(action: action) {
            label()
                .padding(.horizontal, 14)
                .padding(.vertical, 11)
                .frame(maxWidth: .infinity, alignment: .leading)
                .background(fill)
                .overlay(alignment: .leading) {
                    if highlighted {
                        Rectangle()
                            .fill(Brand.accent)
                            .frame(width: 3)
                            .accessibilityHidden(true)
                    }
                }
                .contentShape(Rectangle())
                .onHover { hovering = $0 }
        }
        .buttonStyle(RowButtonStyle())
        .focused($focused)
        .accessibilityElement(children: .combine)
        .accessibilityHint(hint)
        .accessibilityAddTraits(selected ? .isSelected : [])
        .accessibilityIdentifier(identifier)
        .animation(.easeInOut(duration: 0.12), value: highlighted)
    }

    private var fill: Color {
        if highlighted && !selected { return Brand.surfaceHover }
        if selected { return Brand.surfaceRaised }
        return .clear
    }
}

// MARK: - Toast

struct ToastView: View {
    let item: ToastItem
    var body: some View {
        HStack(spacing: 10) {
            Image(systemName: icon).foregroundStyle(color).accessibilityHidden(true)
            Text(item.message).font(.system(size: 13)).foregroundStyle(Brand.fg)
                .fixedSize(horizontal: false, vertical: true)
        }
        .padding(.horizontal, 16).padding(.vertical, 12)
        .background(Brand.surfaceRaised)
        .overlay(RoundedRectangle(cornerRadius: 10).strokeBorder(color.opacity(0.45), lineWidth: 1))
        .clipShape(RoundedRectangle(cornerRadius: 10))
        .shadow(color: .black.opacity(0.5), radius: 18, y: 8)
        .frame(maxWidth: 420)
        .accessibilityElement(children: .combine)
    }

    private var icon: String {
        switch item.kind {
        case .ok: return "checkmark.circle.fill"
        case .error: return "exclamationmark.triangle.fill"
        case .info: return "info.circle.fill"
        }
    }
    private var color: Color {
        switch item.kind {
        case .ok: return Brand.success
        case .error: return Brand.danger
        case .info: return Brand.accent
        }
    }
}

extension View {
    func toastOverlay(_ toast: ToastItem?) -> some View {
        overlay(alignment: .bottom) {
            if let toast {
                ToastView(item: toast)
                    .padding(.bottom, 24)
                    .transition(.move(edge: .bottom).combined(with: .opacity))
                    .id(toast.id)
            }
        }
        .animation(.spring(response: 0.35, dampingFraction: 0.85), value: toast?.id)
    }
}

// MARK: - Text inputs

extension View {
    /// Rounded input container with an accent focus ring (design-a11y-11).
    /// At rest it matches the previous static `Brand.border` stroke exactly;
    /// while focused it draws a 1.5pt accent stroke plus a soft outer glow so
    /// keyboard focus is visible without relying on the caret alone.
    func inputContainer(focused: Bool, cornerRadius: CGFloat = 8) -> some View {
        self
            .background(Brand.surfaceWarm)
            .overlay(RoundedRectangle(cornerRadius: cornerRadius)
                .strokeBorder(focused ? Brand.accent : Brand.border, lineWidth: focused ? 1.5 : 1))
            .clipShape(RoundedRectangle(cornerRadius: cornerRadius))
            .shadow(color: focused ? Brand.accent.opacity(0.35) : .clear, radius: 6)
    }
}

/// The app's shared search/source field chrome (design-a11y-11): leading
/// icon, plain text field, and clear button in an `inputContainer`. Replaces
/// the copy-pasted variants in Browse/Discover/Add. Panes own the
/// `@FocusState` and pass its binding down so Cmd-F can drive focus.
struct SearchField: View {
    let icon: String
    let placeholder: String
    @Binding var text: String
    let focused: FocusState<Bool>.Binding
    var accessibilityID: String = ""
    var onSubmit: () -> Void = {}

    var body: some View {
        HStack(spacing: 8) {
            Image(systemName: icon).font(.system(size: 12)).foregroundStyle(Brand.muted)
            TextField(placeholder, text: $text)
                .textFieldStyle(.plain).font(.system(size: 13))
                .focused(focused)
                .onSubmit(onSubmit)
                .accessibilityIdentifier(accessibilityID)
            if !text.isEmpty {
                Button { text = "" } label: { Image(systemName: "xmark.circle.fill") }
                    .buttonStyle(.plain).foregroundStyle(Brand.meta)
            }
        }
        .padding(.horizontal, 12).padding(.vertical, 9)
        .inputContainer(focused: focused.wrappedValue)
    }
}

// MARK: - Misc

struct Eyebrow: View {
    let text: String
    var body: some View {
        HStack(spacing: 8) {
            Circle().fill(Brand.accent).frame(width: 6, height: 6)
            Text(text.uppercased())
                .font(Brand.monoSized(11))
                .tracking(1.2)
                .foregroundStyle(Brand.muted)
        }
    }
}

/// The wordmark used across the app.
struct Wordmark: View {
    var size: CGFloat = 17
    var body: some View {
        HStack(spacing: 8) {
            RoundedRectangle(cornerRadius: 5)
                .fill(Brand.accent)
                .frame(width: size + 5, height: size + 5)
                .overlay(
                    Image(systemName: "square.stack.3d.up.fill")
                        .font(.system(size: size * 0.62, weight: .bold))
                        .foregroundStyle(.white)
                )
            Text("Skills Registry")
                .font(.system(size: size, weight: .semibold))
                .foregroundStyle(Brand.fg)
        }
    }
}

/// The GitHub mark for the sign-in button. SF Symbols has no GitHub logo, so
/// the mark ships as a bundled vector (`Resources/GitHubMark.pdf`, converted
/// from the Simple Icons GitHub path) and renders as a template tinted by the
/// button foreground. Falls back to a generic glyph when the asset is missing
/// (e.g. running the raw SwiftPM binary instead of the bundled .app).
struct GitHubMark: View {
    var size: CGFloat = 16

    private static var cached: NSImage? = {
        guard let url = Bundle.main.url(forResource: "GitHubMark", withExtension: "pdf"),
              let image = NSImage(contentsOf: url) else { return nil }
        image.isTemplate = true
        return image
    }()

    var body: some View {
        if let image = Self.markImage(size: size) {
            image
        } else {
            Image(systemName: "chevron.left.forwardslash.chevron.right")
                .font(.system(size: size, weight: .bold))
        }
    }

    private static func markImage(size: CGFloat) -> Image? {
        guard let mark = cached else { return nil }
        mark.size = NSSize(width: size, height: size)
        return Image(nsImage: mark)
    }
}

/// The import-gate blocker warning shared by the Discover confirmation sheet
/// and the Add results banner: the block summary plus the acknowledgement
/// checkbox that clears it. Neither consent implies an install.
struct GateBlockWarning: View {
    let review: ImportReview
    @Binding var acknowledged: Bool
    let toggleID: String

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack(spacing: 8) {
                Image(systemName: "exclamationmark.triangle.fill")
                    .font(.system(size: 12)).foregroundStyle(Brand.danger)
                Text(review.displaySummary).font(.system(size: 12, weight: .medium)).foregroundStyle(Brand.fg)
                    .fixedSize(horizontal: false, vertical: true)
            }
            Toggle(isOn: $acknowledged) {
                Text("I have read the source and want to import it anyway")
                    .font(.system(size: 12)).foregroundStyle(Brand.fg2)
            }
            .toggleStyle(.checkbox)
            .accessibilityIdentifier(toggleID)
            .accessibilityLabel("I have read the source and want to import it anyway")
        }
        .padding(12)
        .background(Brand.surfaceWarm)
        .overlay(RoundedRectangle(cornerRadius: 8).strokeBorder(Brand.danger.opacity(0.45), lineWidth: 1))
        .clipShape(RoundedRectangle(cornerRadius: 8))
    }
}

/// The local scan's hits, or the clean-scan line when there are none. Shared
/// by the Add banner and the Discover confirmation so both surfaces quote the
/// same lines and the same disclaimer. A slug prefix appears only when more
/// than one skill contributed a hit.
struct ScanFindingsList: View {
    let rows: [(slug: String, finding: SkillFinding)]

    var body: some View {
        let total = rows.count
        let slugs = Set(rows.map(\.slug))
        VStack(alignment: .leading, spacing: 6) {
            if total == 0 {
                Text("Local scan: no suspicious patterns. \(ImportGate.scanDisclaimer)")
                    .font(.system(size: 11)).foregroundStyle(Brand.meta)
                    .fixedSize(horizontal: false, vertical: true)
            } else {
                Text("Local scan: \(total) suspicious line\(total == 1 ? "" : "s") in \(Scan.mainFileName)")
                    .font(.system(size: 12, weight: .medium)).foregroundStyle(Brand.fg)
                    .fixedSize(horizontal: false, vertical: true)
                ForEach(Array(rows.enumerated()), id: \.offset) { _, row in
                    let prefix = slugs.count > 1 ? "\(row.slug): " : ""
                    Text("· \(prefix)\(row.finding.description)")
                        .font(Brand.monoSized(11)).foregroundStyle(Brand.fg2)
                        .fixedSize(horizontal: false, vertical: true)
                        .textSelection(.enabled)
                }
                Text(ImportGate.scanDisclaimer)
                    .font(.system(size: 11)).foregroundStyle(Brand.meta)
                    .fixedSize(horizontal: false, vertical: true)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .accessibilityIdentifier("scanFindings")
    }
}

/// An empty/placeholder state.
struct EmptyState: View {
    let icon: String
    let title: String
    let subtitle: String
    var body: some View {
        VStack(spacing: 10) {
            Image(systemName: icon).font(.system(size: 34)).foregroundStyle(Brand.meta)
            Text(title).font(.system(size: 16, weight: .semibold)).foregroundStyle(Brand.fg2)
            Text(subtitle).font(.system(size: 13)).foregroundStyle(Brand.muted)
                .multilineTextAlignment(.center).frame(maxWidth: 340)
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
    }
}
