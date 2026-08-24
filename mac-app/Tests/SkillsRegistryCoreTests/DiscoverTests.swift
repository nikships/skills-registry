import XCTest
@testable import SkillsRegistryCore

/// Swift mirror of `cli/internal/discover/discover_test.go`. The two clients
/// consume the same index and publish the same contract, so the assertions are
/// kept in lockstep. No test contacts the live index.
final class DiscoverClientTests: XCTestCase {
    // MARK: - test transport

    /// Records the request it was handed and replays a scripted response, so a
    /// test can assert on the exact URL and headers without a network hop.
    private final class FakeTransport: DiscoverTransporting, @unchecked Sendable {
        enum Outcome {
            case ok(status: Int, body: Data)
            case failure(Error)
        }

        private(set) var requests: [URLRequest] = []
        var outcome: Outcome

        init(status: Int = 200, body: String) {
            outcome = .ok(status: status, body: Data(body.utf8))
        }

        init(failure: Error) {
            outcome = .failure(failure)
        }

        func get(_ request: URLRequest) async throws -> (Data, HTTPURLResponse) {
            requests.append(request)
            switch outcome {
            case .failure(let error):
                throw error
            case .ok(let status, let body):
                let url = request.url ?? URL(string: "http://index.test/v1/search")!
                let response = HTTPURLResponse(url: url, statusCode: status,
                                               httpVersion: "HTTP/1.1", headerFields: nil)!
                return (body, response)
            }
        }
    }

    private static let base = "http://index.test/v1/search"

    private func client(_ transport: FakeTransport) -> DiscoverClient {
        DiscoverClient(baseURL: Self.base, transport: transport)
    }

    /// One well-formed payload, in the index's own wire shape.
    private static let payload = """
    {"data":[
      {"skill_name":"summarize","skill_description":"Summarize URLs and PDFs.","author":"openclaw",
       "category":"AIGC","skill_url":"https://github.com/openclaw/openclaw/blob/abc123/skills/summarize",
       "stars":4213,
       "evaluation":{"safety":{"level":"Good","reason":"n/a"},"completeness":{"level":"Average"},
                     "executability":{"level":"Good"},"cost":{"level":"Good"}}}
    ]}
    """

    // MARK: - decoding the published contract

    func testSearchDecodesTheContract() async throws {
        let transport = FakeTransport(body: Self.payload)
        let resp = try await client(transport).search(DiscoverQuery(text: "pdf"))

        XCTAssertEqual(resp.source, DiscoverClient.source)
        XCTAssertEqual(resp.query, "pdf")
        XCTAssertEqual(resp.mode, DiscoverMode.keyword.rawValue)
        XCTAssertEqual(resp.results.count, 1)

        let row = resp.results[0]
        XCTAssertEqual(row.name, "summarize")
        XCTAssertEqual(row.description, "Summarize URLs and PDFs.")
        XCTAssertEqual(row.author, "openclaw")
        XCTAssertEqual(row.category, "AIGC")
        XCTAssertEqual(row.skillURL, "https://github.com/openclaw/openclaw/blob/abc123/skills/summarize")
        XCTAssertEqual(row.safety, "Good")
        XCTAssertEqual(row.completeness, "Average")
        XCTAssertEqual(row.executability, "Good")
    }

    /// The published JSON keys must match `skills-registry discover --json`
    /// byte for byte, so a consumer can read either surface.
    func testResultEncodesThePublishedKeys() throws {
        let row = DiscoverResult(name: "summarize", description: "d", author: "a", category: "c",
                                 skillURL: "https://github.com/o/r/blob/abc/skills/s",
                                 safety: "Good", completeness: "", executability: "Poor")
        let encoded = try JSONEncoder().encode(row)
        let obj = try XCTUnwrap(try JSONSerialization.jsonObject(with: encoded) as? [String: Any])
        XCTAssertEqual(Set(obj.keys), [
            "name", "description", "author", "category",
            "skill_url", "safety", "completeness", "executability",
        ])
        XCTAssertEqual(obj["skill_url"] as? String, "https://github.com/o/r/blob/abc/skills/s")
        XCTAssertEqual(obj["completeness"] as? String, "")
    }

    /// An absent grade stays empty through decoding, and renders as
    /// `unscored`, never as a pass and never as a blank.
    func testAbsentGradesStayEmptyAndRenderUnscored() async throws {
        let body = """
        {"data":[
          {"skill_name":"ungraded","skill_url":"https://github.com/o/r/blob/main/skills/ungraded"},
          {"skill_name":"partial","skill_url":"https://github.com/o/r/blob/main/skills/partial",
           "evaluation":{"safety":{"level":""},"completeness":{"level":"Good"}}}
        ]}
        """
        let resp = try await client(FakeTransport(body: body)).search(DiscoverQuery(text: "x"))
        XCTAssertEqual(resp.results.count, 2)

        let ungraded = resp.results[0]
        XCTAssertEqual(ungraded.safety, "")
        XCTAssertEqual(ungraded.completeness, "")
        XCTAssertEqual(ungraded.executability, "")
        XCTAssertFalse(ungraded.scores.any)
        XCTAssertEqual(ungraded.scores.lines.map(\.level),
                       [ImportGate.unscoredLabel, ImportGate.unscoredLabel, ImportGate.unscoredLabel])
        XCTAssertFalse(ungraded.scores.safetyIsPoor, "unscored is not Poor")

        let partial = resp.results[1]
        XCTAssertEqual(partial.completeness, "Good")
        XCTAssertEqual(partial.scores.lines.map(\.level),
                       [ImportGate.unscoredLabel, "Good", ImportGate.unscoredLabel])
        XCTAssertTrue(partial.scores.any)
    }

    /// Clones of one skill vendored by several repositories collapse, and the
    /// index's own ranking order survives.
    func testDuplicateRowsCollapseAndOrderSurvives() async throws {
        let body = """
        {"data":[
          {"skill_name":"a","skill_url":"https://github.com/o/r/blob/main/a"},
          {"skill_name":"b","skill_url":"https://github.com/o/r/blob/main/b"},
          {"skill_name":"a","skill_url":"https://github.com/o/r/blob/main/a"},
          {"skill_name":"a","skill_url":"https://github.com/o/other/blob/main/a"}
        ]}
        """
        let resp = try await client(FakeTransport(body: body)).search(DiscoverQuery(text: "x"))
        XCTAssertEqual(resp.results.map(\.name), ["a", "b", "a"])
        XCTAssertEqual(resp.results.map(\.skillURL), [
            "https://github.com/o/r/blob/main/a",
            "https://github.com/o/r/blob/main/b",
            "https://github.com/o/other/blob/main/a",
        ])
    }

    func testRowsWithNoIdentityAreDropped() async throws {
        let body = """
        {"data":[
          {"skill_description":"no name, no url"},
          {"skill_name":"  ","skill_url":"  "},
          {"skill_name":"keeper","skill_url":"https://github.com/o/r/blob/main/keeper"}
        ]}
        """
        let resp = try await client(FakeTransport(body: body)).search(DiscoverQuery(text: "x"))
        XCTAssertEqual(resp.results.map(\.name), ["keeper"])
    }

    func testEmptyResultsAreAnEmptyArray() async throws {
        let resp = try await client(FakeTransport(body: #"{"data":[]}"#))
            .search(DiscoverQuery(text: "nothing"))
        XCTAssertEqual(resp.results, [])
    }

    func testMissingDataKeyIsAnEmptyResultSet() async throws {
        let resp = try await client(FakeTransport(body: "{}")).search(DiscoverQuery(text: "x"))
        XCTAssertEqual(resp.results, [])
    }

    // MARK: - request shape

    func testRequestCarriesQueryModeAndLimit() async throws {
        let transport = FakeTransport(body: #"{"data":[]}"#)
        _ = try await client(transport).search(
            DiscoverQuery(text: "  pdf forms  ", mode: .vector, category: "Productivity", limit: 25))
        let url = try XCTUnwrap(transport.requests.first?.url)
        let items = try XCTUnwrap(URLComponents(url: url, resolvingAgainstBaseURL: false)?.queryItems)
        let params = Dictionary(uniqueKeysWithValues: items.map { ($0.name, $0.value ?? "") })
        XCTAssertEqual(params["q"], "pdf forms")
        XCTAssertEqual(params["mode"], "vector")
        XCTAssertEqual(params["limit"], "25")
        XCTAssertEqual(params["category"], "Productivity")
        XCTAssertEqual(transport.requests.first?.httpMethod, "GET")
    }

    func testLimitIsDefaultedAndClamped() async throws {
        for (asked, want) in [(0, DiscoverClient.defaultLimit), (-5, DiscoverClient.defaultLimit),
                              (999, DiscoverClient.maxLimit), (7, 7)] {
            let transport = FakeTransport(body: #"{"data":[]}"#)
            _ = try await client(transport).search(DiscoverQuery(text: "x", limit: asked))
            let url = try XCTUnwrap(transport.requests.first?.url)
            let items = URLComponents(url: url, resolvingAgainstBaseURL: false)?.queryItems ?? []
            XCTAssertEqual(items.first { $0.name == "limit" }?.value, String(want),
                           "limit \(asked) should become \(want)")
        }
    }

    /// A fixture or a mirror may pin its own parameters; those survive, and the
    /// search parameters are not duplicated onto them.
    func testEndpointQueryParametersSurvive() async throws {
        let transport = FakeTransport(body: #"{"data":[]}"#)
        let pinned = DiscoverClient(baseURL: "http://index.test/v1/search?tenant=abc&q=stale",
                                    transport: transport)
        _ = try await pinned.search(DiscoverQuery(text: "pdf"))
        let url = try XCTUnwrap(transport.requests.first?.url)
        let items = try XCTUnwrap(URLComponents(url: url, resolvingAgainstBaseURL: false)?.queryItems)
        XCTAssertEqual(items.filter { $0.name == "tenant" }.map(\.value), ["abc"])
        XCTAssertEqual(items.filter { $0.name == "q" }.map(\.value), ["pdf"],
                       "the caller's query replaces a stale pinned one exactly once")
    }

    func testEmptyQueryNeverReachesTheTransport() async {
        let transport = FakeTransport(body: #"{"data":[]}"#)
        for text in ["", "   ", "\n\t"] {
            do {
                _ = try await client(transport).search(DiscoverQuery(text: text))
                XCTFail("expected emptyQuery for \(text.debugDescription)")
            } catch let e as DiscoverError {
                XCTAssertEqual(e, .emptyQuery)
            } catch {
                XCTFail("unexpected error \(error)")
            }
        }
        XCTAssertTrue(transport.requests.isEmpty, "an empty query must not be sent")
    }

    // MARK: - the security assertion behind plain HTTP

    /// The index endpoint is plain HTTP (its certificate does not match the
    /// host), so the request must carry no token, no GitHub auth, and no
    /// cookie: a plaintext hop may leak nothing but the query.
    func testRequestNeverCarriesCredentials() async throws {
        let transport = FakeTransport(body: #"{"data":[]}"#)
        _ = try await client(transport).search(DiscoverQuery(text: "pdf"))
        let request = try XCTUnwrap(transport.requests.first)

        for header in ["Authorization", "Proxy-Authorization", "Cookie",
                       "X-Github-Token", "X-Api-Key", "Private-Token"] {
            XCTAssertNil(request.value(forHTTPHeaderField: header),
                         "request carried \(header) — credentials must never reach the index")
        }
        for (name, value) in request.allHTTPHeaderFields ?? [:] {
            XCTAssertFalse(value.contains("ghp_"), "header \(name) leaked a GitHub token: \(value)")
            XCTAssertFalse(value.contains("Bearer "), "header \(name) leaked a bearer token: \(value)")
        }
        XCTAssertEqual(request.allHTTPHeaderFields?.keys.sorted(), ["Accept", "User-Agent"])
        XCTAssertEqual(request.value(forHTTPHeaderField: "User-Agent"), DiscoverClient.userAgent)
        XCTAssertFalse(request.httpShouldHandleCookies)
        XCTAssertNil(request.httpBody)

        let raw = try XCTUnwrap(request.url?.absoluteString.lowercased())
        for key in ["token", "access_token", "api_key", "authorization"] {
            XCTAssertFalse(raw.contains(key), "URL \(raw) contains credential parameter \(key)")
        }
    }

    /// The shipped session cannot pick up a credential or a cookie from
    /// anywhere else in the app.
    func testShippedSessionStoresNoCredentials() {
        let cfg = DiscoverClient.sessionConfiguration()
        XCTAssertNil(cfg.httpCookieStorage)
        XCTAssertNil(cfg.urlCredentialStorage)
        XCTAssertFalse(cfg.httpShouldSetCookies)
        XCTAssertEqual(cfg.httpCookieAcceptPolicy, .never)
        XCTAssertEqual(cfg.timeoutIntervalForRequest, DiscoverClient.timeout)
    }

    // MARK: - fail closed

    func testTimeoutFailsClosed() async {
        let timeout = URLError(.timedOut)
        do {
            _ = try await client(FakeTransport(failure: timeout)).search(DiscoverQuery(text: "pdf"))
            XCTFail("expected the timeout to surface")
        } catch let e as DiscoverError {
            guard case .unreachable = e else { return XCTFail("got \(e)") }
            XCTAssertTrue(e.localizedDescription.contains("Couldn't reach"), e.localizedDescription)
        } catch {
            XCTFail("unexpected error \(error)")
        }
    }

    func testUnreachableHostFailsClosed() async {
        do {
            _ = try await client(FakeTransport(failure: URLError(.cannotFindHost)))
                .search(DiscoverQuery(text: "pdf"))
            XCTFail("expected unreachable")
        } catch let e as DiscoverError {
            guard case .unreachable = e else { return XCTFail("got \(e)") }
        } catch {
            XCTFail("unexpected error \(error)")
        }
    }

    /// A 5xx returning an HTML page must not dump the page into the UI, and
    /// must not yield a partial result set.
    func testServerErrorFailsClosedWithASummarizedBody() async {
        let html = "<html>\n  <body>\n    <h1>502 Bad Gateway</h1>\n  </body>\n</html>"
        do {
            _ = try await client(FakeTransport(status: 502, body: html))
                .search(DiscoverQuery(text: "pdf"))
            XCTFail("expected a status error")
        } catch let e as DiscoverError {
            guard case .status(let code, let summary) = e else { return XCTFail("got \(e)") }
            XCTAssertEqual(code, 502)
            XCTAssertFalse(summary.contains("\n"), "the body must be collapsed to one line: \(summary)")
            XCTAssertTrue(summary.contains("502 Bad Gateway"), summary)
        } catch {
            XCTFail("unexpected error \(error)")
        }
    }

    func testNonJSONBodyFailsClosed() async {
        for body in ["not json at all", "<html><body>hi</body></html>", ""] {
            do {
                _ = try await client(FakeTransport(body: body)).search(DiscoverQuery(text: "pdf"))
                XCTFail("expected notJSON for \(body.debugDescription)")
            } catch let e as DiscoverError {
                guard case .notJSON = e else { return XCTFail("got \(e)") }
            } catch {
                XCTFail("unexpected error \(error)")
            }
        }
    }

    /// A 2xx whose JSON does not match the contract is still a failure, not an
    /// empty list dressed up as a successful search.
    func testWrongShapedJSONFailsClosed() async {
        do {
            _ = try await client(FakeTransport(body: #"{"data":"not an array"}"#))
                .search(DiscoverQuery(text: "pdf"))
            XCTFail("expected notJSON")
        } catch let e as DiscoverError {
            guard case .notJSON = e else { return XCTFail("got \(e)") }
        } catch {
            XCTFail("unexpected error \(error)")
        }
    }

    func testOversizedBodyIsRejected() async {
        let huge = String(repeating: "a", count: DiscoverClient.maxBodyBytes + 1)
        do {
            _ = try await client(FakeTransport(body: huge)).search(DiscoverQuery(text: "pdf"))
            XCTFail("expected bodyTooLarge")
        } catch let e as DiscoverError {
            XCTAssertEqual(e, .bodyTooLarge(DiscoverClient.maxBodyBytes))
        } catch {
            XCTFail("unexpected error \(error)")
        }
    }

    /// Every failure names the offline path, so an unreachable index never
    /// reads as "you cannot import this skill".
    func testFallbackHintNamesTheAddPane() {
        XCTAssertTrue(DiscoverError.fallbackHint.contains("Add"), DiscoverError.fallbackHint)
    }

    // MARK: - endpoint resolution

    func testBaseURLOverrideIsHonored() {
        XCTAssertEqual(
            DiscoverClient.resolvedBaseURL(environment: [DiscoverClient.baseURLEnv: "http://mirror.test/s"]),
            "http://mirror.test/s")
        XCTAssertEqual(DiscoverClient.resolvedBaseURL(environment: [DiscoverClient.baseURLEnv: "   "]),
                       DiscoverClient.defaultBaseURL)
        XCTAssertEqual(DiscoverClient.resolvedBaseURL(environment: [:]), DiscoverClient.defaultBaseURL)
    }

    /// Documents that the shipped default is the public SkillNet endpoint over
    /// plain HTTP, and that no credential is attached to compensate. Kept in
    /// lockstep with Go `TestNewFallsBackToDefaultEndpoint`.
    func testDefaultEndpointMatchesTheGoClient() {
        XCTAssertEqual(DiscoverClient.defaultBaseURL, "http://api-skillnet.openkg.cn/v1/search")
        XCTAssertTrue(DiscoverClient.defaultBaseURL.hasPrefix("http://"))
        XCTAssertEqual(DiscoverClient.baseURLEnv, "SKILLS_DISCOVER_URL")
        XCTAssertEqual(DiscoverClient.source, "skillnet")
        XCTAssertEqual(DiscoverClient.defaultLimit, 10)
        XCTAssertEqual(DiscoverClient.maxLimit, 50)
    }

    func testInvalidEndpointIsReportedNotCrashed() async {
        do {
            _ = try await DiscoverClient(baseURL: "http://[oops",
                                         transport: FakeTransport(body: "{}"))
                .search(DiscoverQuery(text: "pdf"))
            XCTFail("expected invalidEndpoint")
        } catch let e as DiscoverError {
            guard case .invalidEndpoint = e else { return XCTFail("got \(e)") }
        } catch {
            XCTFail("unexpected error \(error)")
        }
    }

    /// A result row feeds straight into the import path with no rewriting:
    /// `skill_url` is exactly the shape `GitHubTarget` accepts.
    func testResultURLsParseAsFolderTargets() async throws {
        let resp = try await client(FakeTransport(body: Self.payload)).search(DiscoverQuery(text: "pdf"))
        let target = try XCTUnwrap(GitHubTarget.parse(resp.results[0].skillURL))
        XCTAssertTrue(target.isFolder)
        XCTAssertEqual(target.fullName, "openclaw/openclaw")
        XCTAssertEqual(target.path, "skills/summarize")
    }
}
