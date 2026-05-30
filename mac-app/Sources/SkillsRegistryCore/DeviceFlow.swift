import Foundation

/// GitHub App **Device Flow** — the secure browser-based login for a native
/// app that can't hold a client secret. We request a device code, the user
/// authorizes in their browser, and we poll for a user-to-server token.
///
/// Mirrors the spirit of the MCP's GitHub OAuth handshake (browser GitHub
/// auth), but uses Device Flow + the GitHub App so the resulting token is
/// scoped to the repos where the App is installed.
public struct DeviceCode: Sendable {
    public var deviceCode: String
    public var userCode: String
    public var verificationURI: URL
    public var expiresIn: Int
    public var interval: Int
}

public struct TokenResult: Sendable {
    public var accessToken: String
    /// Present only when the App has token expiry enabled. The app is built
    /// for non-expiring tokens; if this is set we surface a re-auth prompt on
    /// 401 rather than attempting a secret-bearing refresh.
    public var refreshToken: String?
    public var expiresIn: Int?
}

public enum DeviceFlowError: Error, LocalizedError {
    case http(Int, String)
    case malformedResponse
    case accessDenied
    case expired
    case slowDownExhausted

    public var errorDescription: String? {
        switch self {
        case .http(let code, let body): return "GitHub returned HTTP \(code): \(body)"
        case .malformedResponse: return "Unexpected response from GitHub."
        case .accessDenied: return "Authorization was denied. Try signing in again."
        case .expired: return "The device code expired. Start sign-in again."
        case .slowDownExhausted: return "Polling timed out. Start sign-in again."
        }
    }
}

public struct DeviceFlow: Sendable {
    let clientID: String
    let session: URLSession

    public init(clientID: String = AppConfig.githubClientID, session: URLSession = .shared) {
        self.clientID = clientID
        self.session = session
    }

    private static let codeURL = URL(string: "https://github.com/login/device/code")!
    private static let tokenURL = URL(string: "https://github.com/login/oauth/access_token")!

    /// Step 1: request a device + user code.
    public func requestCode() async throws -> DeviceCode {
        let req = Self.formRequest(url: Self.codeURL, fields: ["client_id": clientID])
        let json = try await Self.sendJSON(req, session: session)
        guard
            let deviceCode = json["device_code"] as? String,
            let userCode = json["user_code"] as? String,
            let veri = json["verification_uri"] as? String,
            let veriURL = URL(string: veri)
        else { throw DeviceFlowError.malformedResponse }
        let expires = (json["expires_in"] as? Int) ?? 900
        let interval = (json["interval"] as? Int) ?? 5
        return DeviceCode(deviceCode: deviceCode, userCode: userCode,
                          verificationURI: veriURL, expiresIn: expires, interval: interval)
    }

    /// Step 2: poll until the user authorizes (or a terminal error). Honors
    /// `slow_down` by widening the interval. Cancellation-aware.
    public func pollForToken(_ code: DeviceCode) async throws -> TokenResult {
        var interval = max(code.interval, 1)
        let deadline = Date().addingTimeInterval(TimeInterval(code.expiresIn))
        while Date() < deadline {
            try await Task.sleep(nanoseconds: UInt64(interval) * 1_000_000_000)
            try Task.checkCancellation()
            let req = Self.formRequest(url: Self.tokenURL, fields: [
                "client_id": clientID,
                "device_code": code.deviceCode,
                "grant_type": "urn:ietf:params:oauth:grant-type:device_code",
            ])
            let json = try await Self.sendJSON(req, session: session)
            if let token = json["access_token"] as? String {
                return TokenResult(
                    accessToken: token,
                    refreshToken: json["refresh_token"] as? String,
                    expiresIn: json["expires_in"] as? Int
                )
            }
            switch json["error"] as? String {
            case "authorization_pending":
                continue
            case "slow_down":
                interval += 5
                continue
            case "expired_token":
                throw DeviceFlowError.expired
            case "access_denied":
                throw DeviceFlowError.accessDenied
            default:
                throw DeviceFlowError.malformedResponse
            }
        }
        throw DeviceFlowError.expired
    }

    // MARK: - helpers

    static func formRequest(url: URL, fields: [String: String]) -> URLRequest {
        var req = URLRequest(url: url)
        req.httpMethod = "POST"
        req.setValue("application/json", forHTTPHeaderField: "Accept")
        req.setValue("application/x-www-form-urlencoded", forHTTPHeaderField: "Content-Type")
        let body = fields.map { key, value in
            let v = value.addingPercentEncoding(withAllowedCharacters: .alphanumerics) ?? value
            return "\(key)=\(v)"
        }.joined(separator: "&")
        req.httpBody = Data(body.utf8)
        return req
    }

    static func sendJSON(_ req: URLRequest, session: URLSession) async throws -> [String: Any] {
        let (data, resp) = try await session.data(for: req)
        let code = (resp as? HTTPURLResponse)?.statusCode ?? 0
        guard (200..<300).contains(code) else {
            throw DeviceFlowError.http(code, String(data: data, encoding: .utf8) ?? "")
        }
        guard let obj = try JSONSerialization.jsonObject(with: data) as? [String: Any] else {
            throw DeviceFlowError.malformedResponse
        }
        return obj
    }
}
