import Foundation

/// Client for the public SkillNet skill index. Swift mirror of Go
/// `cli/internal/discover` (`discover.Client`, `discover.Response`), so the
/// macOS Discover pane reads the same rows `skills-registry discover --json`
/// prints without shelling out to the CLI binary.
///
/// Two transport facts drive this code, exactly as they do in Go:
///
///   - The endpoint is plain HTTP. SkillNet serves a certificate that does not
///     match the host, so HTTPS cannot be verified and the query travels in
///     plaintext. Only the search terms ever leave the machine.
///   - No credential of any kind is attached. The request is built here rather
///     than through `GitHubAPI`, so there is no path by which the user's
///     GitHub token could reach the index, and the session is ephemeral with
///     cookies and credential storage switched off.
///
/// Every failure (unreachable host, timeout, non-2xx, non-JSON body) throws
/// with no partial response: the caller fails closed and shows an error rather
/// than a half-populated list.
public struct DiscoverClient: Sendable {
    /// The public SkillNet search endpoint. Plain HTTP is intentional.
    public static let defaultBaseURL = "http://api-skillnet.openkg.cn/v1/search"

    /// Overrides `defaultBaseURL`. Tests point it at a fixture, and the app
    /// can point it at a mirror. Same variable the Go client reads.
    public static let baseURLEnv = "SKILLS_DISCOVER_URL"

    /// Labels every response, so a consumer that later gains a second index
    /// can tell the rows apart.
    public static let source = "skillnet"

    public static let defaultLimit = 10
    public static let maxLimit = 50

    /// Hard ceiling on one search, DNS through body read.
    public static let timeout: TimeInterval = 10

    /// Identifies the app to the index. It carries no user identity.
    static let userAgent = "skills-registry-mac-discover"

    /// Caps how much of a response body is accepted, so a hostile or broken
    /// endpoint cannot exhaust memory.
    static let maxBodyBytes = 8 << 20

    public let baseURL: String
    let transport: DiscoverTransporting

    /// `baseURL` defaults to `resolvedBaseURL()`; `transport` defaults to an
    /// ephemeral, credential-free `URLSession`.
    public init(baseURL: String? = nil, transport: DiscoverTransporting? = nil) {
        self.baseURL = baseURL ?? Self.resolvedBaseURL()
        self.transport = transport ?? URLSessionDiscoverTransport()
    }

    /// Resolve the endpoint from `baseURLEnv`, falling back to
    /// `defaultBaseURL`.
    public static func resolvedBaseURL(
        environment: [String: String] = ProcessInfo.processInfo.environment
    ) -> String {
        let override = (environment[baseURLEnv] ?? "").trimmingCharacters(in: .whitespacesAndNewlines)
        return override.isEmpty ? defaultBaseURL : override
    }

    /// Run one search and return the mapped rows.
    public func search(_ query: DiscoverQuery) async throws -> DiscoverResponse {
        let q = try query.normalized()
        let request = try Self.searchRequest(base: baseURL, query: q)
        let (data, response) = try await fetch(request)
        guard (200..<300).contains(response.statusCode) else {
            throw DiscoverError.status(response.statusCode, Self.summarize(data))
        }
        guard let payload = try? JSONDecoder().decode(APIResponse.self, from: data) else {
            throw DiscoverError.notJSON(Self.summarize(data))
        }
        return DiscoverResponse(
            source: Self.source,
            query: q.text,
            mode: q.mode.rawValue,
            results: Self.mapResults(payload.data))
    }

    private func fetch(_ request: URLRequest) async throws -> (Data, HTTPURLResponse) {
        let (data, response): (Data, HTTPURLResponse)
        do {
            (data, response) = try await transport.get(request)
        } catch let e as DiscoverError {
            throw e
        } catch {
            throw DiscoverError.unreachable(request.url?.absoluteString ?? baseURL,
                                            error.localizedDescription)
        }
        guard data.count <= Self.maxBodyBytes else {
            throw DiscoverError.bodyTooLarge(Self.maxBodyBytes)
        }
        return (data, response)
    }

    /// Build the search request. Query parameters the endpoint already carries
    /// are preserved (a fixture or a mirror may pin its own), and the only
    /// headers set are `Accept` and `User-Agent` — never a credential.
    static func searchRequest(base: String, query: DiscoverQuery) throws -> URLRequest {
        guard var comps = URLComponents(string: base) else {
            throw DiscoverError.invalidEndpoint(base)
        }
        var params = (comps.queryItems ?? []).filter {
            !["q", "mode", "limit", "category"].contains($0.name)
        }
        params.append(URLQueryItem(name: "q", value: query.text))
        params.append(URLQueryItem(name: "mode", value: query.mode.rawValue))
        params.append(URLQueryItem(name: "limit", value: String(query.limit)))
        if !query.category.isEmpty {
            params.append(URLQueryItem(name: "category", value: query.category))
        }
        comps.queryItems = params
        guard let url = comps.url else { throw DiscoverError.invalidEndpoint(base) }

        var req = URLRequest(url: url)
        req.httpMethod = "GET"
        req.setValue("application/json", forHTTPHeaderField: "Accept")
        req.setValue(userAgent, forHTTPHeaderField: "User-Agent")
        req.httpShouldHandleCookies = false
        req.timeoutInterval = timeout
        return req
    }

    /// Session configuration for the shipped transport: ephemeral, with cookie
    /// and credential storage switched off so nothing the app authenticated
    /// elsewhere can ride along on a plaintext request.
    static func sessionConfiguration() -> URLSessionConfiguration {
        let cfg = URLSessionConfiguration.ephemeral
        cfg.timeoutIntervalForRequest = timeout
        cfg.timeoutIntervalForResource = timeout
        cfg.httpShouldSetCookies = false
        cfg.httpCookieAcceptPolicy = .never
        cfg.httpCookieStorage = nil
        cfg.urlCredentialStorage = nil
        cfg.requestCachePolicy = .reloadIgnoringLocalCacheData
        return cfg
    }

    /// Project the index payload onto `DiscoverResult`, dropping rows with no
    /// usable identity and collapsing duplicates.
    ///
    /// The index returns clones of the same skill when several repositories
    /// vendor it, so rows are deduplicated on (name, skill_url) with the first
    /// occurrence winning: that keeps the index's own ranking order intact,
    /// and the app never re-sorts by repository popularity.
    static func mapResults(_ data: [APISkill]) -> [DiscoverResult] {
        var out: [DiscoverResult] = []
        var seen = Set<String>()
        out.reserveCapacity(data.count)
        for s in data {
            let row = DiscoverResult(
                name: s.name.trimmed,
                description: s.description.trimmed,
                author: s.author.trimmed,
                category: s.category.trimmed,
                skillURL: s.skillURL.trimmed,
                safety: s.evaluation.safety.level.trimmed,
                completeness: s.evaluation.completeness.level.trimmed,
                executability: s.evaluation.executability.level.trimmed)
            if row.name.isEmpty && row.skillURL.isEmpty { continue }
            let key = row.name + "\u{0}" + row.skillURL
            if seen.contains(key) { continue }
            seen.insert(key)
            out.append(row)
        }
        return out
    }

    /// Render an error body as one short line, so a 500 that returns an HTML
    /// page does not end up inside the UI.
    static func summarize(_ body: Data) -> String {
        let text = String(data: body.prefix(4096), encoding: .utf8) ?? ""
        let collapsed = text.split(whereSeparator: { $0.isWhitespace || $0.isNewline }).joined(separator: " ")
        if collapsed.isEmpty { return "(empty body)" }
        if collapsed.count > 120 { return String(collapsed.prefix(119)) + "…" }
        return collapsed
    }
}

// MARK: - query

/// SkillNet's ranking strategy: literal keyword matching or embedding
/// similarity.
public enum DiscoverMode: String, Sendable, CaseIterable, Identifiable {
    case keyword
    case vector

    public var id: String { rawValue }

    /// Label for a mode toggle.
    public var label: String {
        switch self {
        case .keyword: return "Keyword"
        case .vector: return "Meaning"
        }
    }

    /// One-line explanation of what the mode does.
    public var hint: String {
        switch self {
        case .keyword: return "Literal term matching."
        case .vector: return "Embedding similarity — describe what you want."
        }
    }
}

/// One search request. Defaults and clamping mirror Go `discover.Query`.
public struct DiscoverQuery: Sendable, Equatable {
    public var text: String
    public var mode: DiscoverMode
    public var category: String
    public var limit: Int

    public init(text: String, mode: DiscoverMode = .keyword,
                category: String = "", limit: Int = DiscoverClient.defaultLimit) {
        self.text = text
        self.mode = mode
        self.category = category
        self.limit = limit
    }

    /// Fill in defaults and clamp `limit`. Throws on an empty query, so a
    /// stray keystroke never turns into a request.
    public func normalized() throws -> DiscoverQuery {
        var q = self
        q.text = text.trimmed
        guard !q.text.isEmpty else { throw DiscoverError.emptyQuery }
        q.category = category.trimmed
        if q.limit <= 0 { q.limit = DiscoverClient.defaultLimit }
        if q.limit > DiscoverClient.maxLimit { q.limit = DiscoverClient.maxLimit }
        return q
    }
}

// MARK: - published contract

/// One skill in the index. Field names match the published JSON contract of
/// `skills-registry discover --json`, so the two surfaces cannot drift.
///
/// The three score fields carry SkillNet's `evaluation.<score>.level`
/// (`Good`, `Average`, or `Poor`) and are **empty when the index has no
/// score**. An empty score means unscored and must never render as a pass;
/// `ImportGate.label` is what turns it into text.
public struct DiscoverResult: Sendable, Hashable, Identifiable, Codable {
    public var name: String
    public var description: String
    public var author: String
    public var category: String
    public var skillURL: String
    public var safety: String
    public var completeness: String
    public var executability: String

    public init(name: String, description: String = "", author: String = "",
                category: String = "", skillURL: String = "",
                safety: String = "", completeness: String = "", executability: String = "") {
        self.name = name
        self.description = description
        self.author = author
        self.category = category
        self.skillURL = skillURL
        self.safety = safety
        self.completeness = completeness
        self.executability = executability
    }

    /// Stable identity for a SwiftUI list. The index can return two rows with
    /// the same name from different repositories, so the URL is part of it.
    public var id: String { skillURL + "\u{0}" + name }

    /// The index's grades for this row, in the shape the import gate reads.
    public var scores: ImportScores {
        ImportScores(safety: safety, completeness: completeness, executability: executability)
    }

    enum CodingKeys: String, CodingKey {
        case name, description, author, category
        case skillURL = "skill_url"
        case safety, completeness, executability
    }
}

/// The published search payload. Mirrors Go `discover.Response`.
public struct DiscoverResponse: Sendable, Codable, Equatable {
    public var source: String
    public var query: String
    public var mode: String
    public var results: [DiscoverResult]

    public init(source: String, query: String, mode: String, results: [DiscoverResult]) {
        self.source = source
        self.query = query
        self.mode = mode
        self.results = results
    }
}

// MARK: - errors

/// A search failure. Distinct cases so the UI can say what went wrong without
/// parsing a message.
public enum DiscoverError: Error, LocalizedError, Equatable {
    case emptyQuery
    case invalidEndpoint(String)
    case unreachable(String, String)
    case status(Int, String)
    case notJSON(String)
    case bodyTooLarge(Int)

    /// Appended to every failure surfaced to the user. The index is a
    /// convenience, not a dependency: a user who already has a GitHub URL can
    /// still import it from the Add pane.
    public static let fallbackHint =
        "The skill index is optional — you can still import any skill from a GitHub URL in Add."

    public var errorDescription: String? {
        switch self {
        case .emptyQuery:
            return "Enter something to search for."
        case .invalidEndpoint(let base):
            return "Invalid skill index URL \(base)."
        case .unreachable(_, let why):
            return "Couldn't reach the skill index: \(why)"
        case .status(let code, let body):
            return "The skill index returned HTTP \(code): \(body)"
        case .notJSON(let body):
            return "The skill index returned a non-JSON response: \(body)"
        case .bodyTooLarge(let limit):
            return "The skill index returned more than \(limit) bytes."
        }
    }
}

// MARK: - transport

/// One GET against the index. `DiscoverClient` depends on this rather than on
/// `URLSession` directly so tests can inspect the exact request and script
/// failures without touching the network.
public protocol DiscoverTransporting: Sendable {
    func get(_ request: URLRequest) async throws -> (Data, HTTPURLResponse)
}

struct URLSessionDiscoverTransport: DiscoverTransporting {
    let session: URLSession

    init(session: URLSession? = nil) {
        self.session = session ?? URLSession(configuration: DiscoverClient.sessionConfiguration())
    }

    func get(_ request: URLRequest) async throws -> (Data, HTTPURLResponse) {
        let (data, response) = try await session.data(for: request)
        guard let http = response as? HTTPURLResponse else {
            throw DiscoverError.unreachable(request.url?.absoluteString ?? "", "no HTTP response")
        }
        return (data, http)
    }
}

// MARK: - wire models

/// The subset of SkillNet's payload this client reads. The index's own extra
/// fields (repository star count, per-score prose, cost and maintainability
/// grades) are deliberately dropped, matching the Go client: star counts
/// belong to the host repository rather than the individual skill.
struct APIResponse: Decodable {
    var data: [APISkill] = []

    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        // A payload with no `data` key is an empty result set, matching Go's
        // `json.Unmarshal` into a nil slice. A `data` of the wrong type still
        // throws, so a 2xx that does not honor the contract fails closed.
        data = try c.decodeIfPresent([APISkill].self, forKey: .data) ?? []
    }

    enum CodingKeys: String, CodingKey { case data }
}

struct APISkill: Decodable {
    var name = ""
    var description = ""
    var author = ""
    var category = ""
    var skillURL = ""
    var evaluation = APIEvaluation()

    enum CodingKeys: String, CodingKey {
        case name = "skill_name"
        case description = "skill_description"
        case author, category
        case skillURL = "skill_url"
        case evaluation
    }

    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        name = try c.decodeIfPresent(String.self, forKey: .name) ?? ""
        description = try c.decodeIfPresent(String.self, forKey: .description) ?? ""
        author = try c.decodeIfPresent(String.self, forKey: .author) ?? ""
        category = try c.decodeIfPresent(String.self, forKey: .category) ?? ""
        skillURL = try c.decodeIfPresent(String.self, forKey: .skillURL) ?? ""
        evaluation = try c.decodeIfPresent(APIEvaluation.self, forKey: .evaluation) ?? APIEvaluation()
    }

    init(name: String, skillURL: String) {
        self.name = name
        self.skillURL = skillURL
    }
}

struct APIEvaluation: Decodable {
    var safety = APIScore()
    var completeness = APIScore()
    var executability = APIScore()

    init() {}

    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        safety = try c.decodeIfPresent(APIScore.self, forKey: .safety) ?? APIScore()
        completeness = try c.decodeIfPresent(APIScore.self, forKey: .completeness) ?? APIScore()
        executability = try c.decodeIfPresent(APIScore.self, forKey: .executability) ?? APIScore()
    }

    enum CodingKeys: String, CodingKey { case safety, completeness, executability }
}

/// One grade. A missing `level` stays empty: an unscored skill must not be
/// promoted to a passing grade.
struct APIScore: Decodable {
    var level = ""

    init() {}

    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        level = try c.decodeIfPresent(String.self, forKey: .level) ?? ""
    }

    enum CodingKeys: String, CodingKey { case level }
}

extension String {
    var trimmed: String { trimmingCharacters(in: .whitespacesAndNewlines) }
}
