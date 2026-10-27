import XCTest
@testable import SkillsRegistry

final class NavigationTests: XCTestCase {
    func testSidebarSectionsAndShortcutOrder() {
        XCTAssertEqual(NavSection.allCases.map(\.rawValue), ["Browse", "Add", "Import", "Settings"])
    }
}
