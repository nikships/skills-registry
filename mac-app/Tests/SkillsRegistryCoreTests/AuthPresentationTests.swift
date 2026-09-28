import XCTest
@testable import SkillsRegistryCore

/// Login error copy: every auth/bootstrap failure maps to a friendly message
/// with the raw error kept as secondary detail, and only transient failures
/// offer a retry. No test touches the network.
final class AuthPresentationTests: XCTestCase {
    func testExpiredTokenCopy() {
        let presented = AuthPresentation.resolve(
            GitHubError(status: 401, message: "Bad credentials", endpoint: "/user"))
        XCTAssertEqual(presented.message, "Your GitHub session expired. Sign in again.")
        XCTAssertEqual(presented.detail, "Bad credentials")
        XCTAssertFalse(presented.retryable)
    }

    func testExpiredHelperSkipsEmptyDetail() {
        XCTAssertNil(AuthPresentation.expired().detail)
        XCTAssertNil(AuthPresentation.expired(detail: "  ").detail)
        XCTAssertFalse(AuthPresentation.expired().retryable)
    }

    func testOfflineMapsToFriendlyCopyWithRetry() {
        for code: URLError.Code in [.notConnectedToInternet, .networkConnectionLost, .cannotConnectToHost] {
            let presented = AuthPresentation.resolve(URLError(code))
            XCTAssertEqual(presented.message, "You're offline. Check your connection and try again.",
                           "code: \(code)")
            XCTAssertNotNil(presented.detail)
            XCTAssertTrue(presented.retryable)
        }
    }

    func testTimeoutMapsToGitHubCopyWithRetry() {
        let presented = AuthPresentation.resolve(URLError(.timedOut))
        XCTAssertEqual(presented.message, "GitHub didn't respond. Try again in a bit.")
        XCTAssertNotNil(presented.detail)
        XCTAssertTrue(presented.retryable)
    }

    func testServerErrorMapsToGitHubCopyWithRetry() {
        let presented = AuthPresentation.resolve(
            GitHubError(status: 502, message: "Bad Gateway", endpoint: "/user"))
        XCTAssertEqual(presented.message, "GitHub didn't respond. Try again in a bit.")
        XCTAssertEqual(presented.detail, "Bad Gateway")
        XCTAssertTrue(presented.retryable)
    }

    func testUnrecognizedURLErrorPassesThroughWithoutRetry() {
        let err = URLError(.cancelled)
        let presented = AuthPresentation.resolve(err)
        XCTAssertEqual(presented.message, err.localizedDescription)
        XCTAssertNil(presented.detail)
        XCTAssertFalse(presented.retryable)
    }

    func testNonAuthAPIErrorSurfacesMessageWithoutRetry() {
        let presented = AuthPresentation.resolve(
            GitHubError(status: 403, message: "API rate limit exceeded", endpoint: "/user"))
        XCTAssertEqual(presented.message, "API rate limit exceeded")
        XCTAssertNil(presented.detail)
        XCTAssertFalse(presented.retryable)
    }

    func testDeviceFlowErrorPassesThrough() {
        let presented = AuthPresentation.resolve(DeviceFlowError.accessDenied)
        XCTAssertEqual(presented.message, DeviceFlowError.accessDenied.localizedDescription)
        XCTAssertFalse(presented.retryable)
    }

    func testWrappedURLErrorStillMapsToOffline() {
        let wrapped = NSError(domain: "test", code: 1,
                              userInfo: [NSUnderlyingErrorKey: URLError(.notConnectedToInternet)])
        let presented = AuthPresentation.resolve(wrapped)
        XCTAssertEqual(presented.message, "You're offline. Check your connection and try again.")
        XCTAssertTrue(presented.retryable)
    }
}
