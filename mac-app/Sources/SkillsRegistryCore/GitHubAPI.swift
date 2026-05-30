import Foundation

/// Error from a GitHub REST call. `status` is the HTTP code (0 if the request
/// never completed). `isAuth` flags 401/403 so the UI can prompt re-auth.
public struct GitHubError: Error, LocalizedError {
    public var status: Int
    public var message: String
    public var endpoint: String

    public init(status: Int, message: String, endpoint: String) {
        self.status = status
        self.message = message
        self.endpoint = endpoint
    }

    public var isUnauthorized: Bool { status == 401 }
    public var isForbidden: Bool { status == 403 }
    public var isNotFound: Bool { status == 404 }
    public var isConflict: Bool { status == 409 || status == 422 }

    public var errorDescription: String? {
        if message.isEmpty { return "GitHub request failed (HTTP \(status))." }
        return message
    }
}

/// Low-level authenticated GitHub REST client. One instance per active token.
/// All higher-level operations (reads/writes) extend this type.
public struct GitHubAPI: Sendable {
    public let token: String
    let session: URLSession
    let apiBase = "https://api.github.com"

    public init(token: String, session: URLSession = .shared) {
        self.token = token
        self.session = session
    }

    static let apiVersion = "2022-11-28"

    func makeRequest(_ method: String, _ path: String, body: Data? = nil,
                     accept: String = "application/vnd.github+json") -> URLRequest {
        let url: URL
        if path.hasPrefix("http") {
            url = URL(string: path)!
        } else {
            url = URL(string: apiBase + "/" + path.trimmingCharacters(in: CharacterSet(charactersIn: "/")))!
        }
        var req = URLRequest(url: url)
        req.httpMethod = method
        req.setValue(accept, forHTTPHeaderField: "Accept")
        req.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")
        req.setValue(Self.apiVersion, forHTTPHeaderField: "X-GitHub-Api-Version")
        req.setValue("skills-registry-mac", forHTTPHeaderField: "User-Agent")
        if let body {
            req.httpBody = body
            req.setValue("application/json", forHTTPHeaderField: "Content-Type")
        }
        return req
    }

    /// Send and return raw body + HTTP response. Throws `GitHubError` on non-2xx.
    @discardableResult
    func send(_ req: URLRequest) async throws -> (Data, HTTPURLResponse) {
        let (data, resp) = try await session.data(for: req)
        guard let http = resp as? HTTPURLResponse else {
            throw GitHubError(status: 0, message: "No HTTP response", endpoint: req.url?.path ?? "")
        }
        guard (200..<300).contains(http.statusCode) else {
            throw GitHubError(status: http.statusCode,
                              message: Self.extractMessage(data, status: http.statusCode),
                              endpoint: req.url?.path ?? "")
        }
        return (data, http)
    }

    /// Send with retry on transient/secondary-rate-limit responses (403/429/5xx),
    /// honoring `Retry-After` when present. Conflict (409/422) is NOT retried
    /// here — callers handle that with a fresh-HEAD re-read.
    @discardableResult
    func sendRetrying(_ req: URLRequest, attempts: Int = 4) async throws -> (Data, HTTPURLResponse) {
        var lastError: GitHubError?
        for attempt in 0..<attempts {
            do {
                return try await send(req)
            } catch let err as GitHubError {
                let transient = err.status == 429 || err.status == 403 || (500...599).contains(err.status)
                if !transient || attempt == attempts - 1 {
                    throw err
                }
                lastError = err
                let delay = pow(2.0, Double(attempt)) * 0.8
                try await Task.sleep(nanoseconds: UInt64(delay * 1_000_000_000))
            }
        }
        throw lastError ?? GitHubError(status: 0, message: "retry exhausted", endpoint: req.url?.path ?? "")
    }

    // MARK: typed helpers

    func getDecoded<T: Decodable>(_ path: String, as type: T.Type) async throws -> T {
        let (data, _) = try await send(makeRequest("GET", path))
        return try JSONDecoder().decode(T.self, from: data)
    }

    func postDecoded<T: Decodable>(_ path: String, json: [String: Any], as type: T.Type) async throws -> T {
        let body = try JSONSerialization.data(withJSONObject: json)
        let (data, _) = try await sendRetrying(makeRequest("POST", path, body: body))
        return try JSONDecoder().decode(T.self, from: data)
    }

    static func extractMessage(_ data: Data, status: Int) -> String {
        if let obj = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
           let msg = obj["message"] as? String {
            return msg
        }
        let s = String(data: data, encoding: .utf8) ?? ""
        return s.isEmpty ? "HTTP \(status)" : s
    }
}

// MARK: - Wire response models

struct GHUser: Decodable {
    let login: String
    let name: String?
    let avatar_url: String?
}

struct GHRepo: Decodable {
    let name: String
    let full_name: String
    let `default_branch`: String?
    let `private`: Bool?
}

struct GHInstallationsResp: Decodable {
    let total_count: Int
    let installations: [GHInstallation]
}

struct GHInstallation: Decodable {
    let id: Int
    let app_slug: String?
}

struct GHInstallationReposResp: Decodable {
    let total_count: Int
    let repositories: [GHRepo]
}

struct GHTreeResp: Decodable {
    let sha: String
    let tree: [GHTreeEntry]
    let truncated: Bool?
}

struct GHTreeEntry: Decodable {
    let path: String
    let type: String   // "blob" | "tree"
    let sha: String
}

struct GHBlobResp: Decodable {
    let content: String
    let encoding: String
}

struct GHRefResp: Decodable {
    struct Obj: Decodable { let sha: String }
    let object: Obj
}

struct GHCommitResp: Decodable {
    struct TreeRef: Decodable { let sha: String }
    let sha: String
    let tree: TreeRef?
}

struct GHShaResp: Decodable {
    let sha: String
}
