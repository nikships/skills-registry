import XCTest
@testable import SkillsRegistryCore

final class KeychainTests: XCTestCase {
    override func tearDown() {
        // Never leak in-memory mode into other tests (or leave fixture data).
        // Enable the flag first so the cleanup deletes provably hit the
        // dictionary, never the real Keychain — even if a test failed early.
        Keychain.inMemoryOnly = true
        Keychain.delete(account: "demo-isolation-test")
        Keychain.delete()
        Keychain.inMemoryOnly = false
        super.tearDown()
    }

    func testInMemoryRoundTrip() {
        Keychain.inMemoryOnly = true
        XCTAssertNil(Keychain.get())
        Keychain.set("tok-123")
        XCTAssertEqual(Keychain.get(), "tok-123")
        Keychain.delete()
        XCTAssertNil(Keychain.get())
    }

    func testInMemoryIsPerAccount() {
        Keychain.inMemoryOnly = true
        Keychain.set("a", account: "demo-isolation-test")
        XCTAssertEqual(Keychain.get(account: "demo-isolation-test"), "a")
        // The default token account is untouched by the other account's write.
        XCTAssertNil(Keychain.get())
    }
}
