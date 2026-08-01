import XCTest
@testable import SkillsRegistryCore

final class CLIInstallerTests: XCTestCase {
    func testInstallDirOnPathUsesShellPathWhenAppPathOmitsIt() {
        XCTAssertTrue(CLIInstaller.isInstallDirOnPath(
            processPath: "/usr/bin:/bin",
            shellPath: "/opt/homebrew/bin:/Users/me/.local/bin",
            target: "/Users/me/.local/bin"))
    }

    func testInstallDirOnPathChecksBothPathSources() {
        XCTAssertTrue(CLIInstaller.isInstallDirOnPath(
            processPath: "/Users/me/.local/bin:/usr/bin",
            shellPath: nil,
            target: "/Users/me/.local/bin"))
        XCTAssertFalse(CLIInstaller.isInstallDirOnPath(
            processPath: "/usr/bin:/bin",
            shellPath: "/opt/homebrew/bin",
            target: "/Users/me/.local/bin"))
    }

    func testInstallDirOnPathRequiresAnExactPathEntry() {
        XCTAssertFalse(CLIInstaller.isInstallDirOnPath(
            processPath: "/Users/me/.local/bin-extra:/usr/bin",
            shellPath: nil,
            target: "/Users/me/.local/bin"))
    }
}
