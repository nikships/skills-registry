import SwiftUI

// MARK: - Toast

struct ToastView: View {
    let item: ToastItem
    var body: some View {
        HStack(spacing: 10) {
            Image(systemName: icon).foregroundStyle(color)
            Text(item.message).font(.system(size: 13)).foregroundStyle(Brand.fg)
                .fixedSize(horizontal: false, vertical: true)
        }
        .padding(.horizontal, 16).padding(.vertical, 12)
        .background(Brand.surfaceRaised)
        .overlay(RoundedRectangle(cornerRadius: 10).strokeBorder(color.opacity(0.45), lineWidth: 1))
        .clipShape(RoundedRectangle(cornerRadius: 10))
        .shadow(color: .black.opacity(0.5), radius: 18, y: 8)
        .frame(maxWidth: 420)
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

struct GitHubMark: View {
    var size: CGFloat = 16
    var body: some View {
        Image(systemName: "chevron.left.forwardslash.chevron.right")
            .font(.system(size: size, weight: .bold))
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
