import Foundation

/// Friendly presentation for an auth/bootstrap failure on the login screen.
///
/// The login screen shows `message` prominently, keeps the raw `detail`
/// (when it differs) as secondary copy, and offers a Retry button only when
/// `retryable` is true. Everything here is offline and allocation-free of
/// I/O, so both the app and its tests can call it directly.
public struct AuthPresentation: Sendable, Equatable {
    public var message: String
    public var detail: String?
    public var retryable: Bool

    public init(message: String, detail: String? = nil, retryable: Bool = false) {
        self.message = message
        self.detail = detail
        self.retryable = retryable
    }

    /// An expired or revoked token always routes back to login with the same
    /// copy, wherever the 401 surfaced.
    public static func expired(detail: String? = nil) -> AuthPresentation {
        AuthPresentation(message: "Your GitHub session expired. Sign in again.",
                         detail: Self.nonEmpty(detail), retryable: false)
    }

    /// Map a bootstrap/device-flow/profile failure to login copy. Offline,
    /// timeout, and 5xx failures are retryable; anything unrecognized passes
    /// through verbatim as its own message with no separate detail.
    public static func resolve(_ error: Error) -> AuthPresentation {
        if let api = error as? GitHubError {
            if api.isUnauthorized { return .expired(detail: api.message) }
            if (500...599).contains(api.status) {
                return AuthPresentation(
                    message: "GitHub didn't respond. Try again in a bit.",
                    detail: Self.nonEmpty(api.message), retryable: true)
            }
            return AuthPresentation(message: api.message.isEmpty
                ? "GitHub request failed (HTTP \(api.status))." : api.message)
        }
        let url = (error as? URLError) ?? Self.underlyingURLError(error)
        if let url {
            switch url.code {
            case .notConnectedToInternet, .networkConnectionLost, .cannotConnectToHost:
                return AuthPresentation(
                    message: "You're offline. Check your connection and try again.",
                    detail: Self.nonEmpty(url.localizedDescription), retryable: true)
            case .timedOut:
                return AuthPresentation(
                    message: "GitHub didn't respond. Try again in a bit.",
                    detail: Self.nonEmpty(url.localizedDescription), retryable: true)
            default:
                break
            }
        }
        return AuthPresentation(message: error.localizedDescription)
    }

    private static func underlyingURLError(_ error: Error) -> URLError? {
        (error as NSError).userInfo[NSUnderlyingErrorKey] as? URLError
    }

    private static func nonEmpty(_ s: String?) -> String? {
        guard let s, !s.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty else { return nil }
        return s
    }
}
