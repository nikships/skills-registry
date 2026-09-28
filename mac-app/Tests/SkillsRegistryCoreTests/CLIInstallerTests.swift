import XCTest
@testable import SkillsRegistryCore

final class CLIInstallerTests: XCTestCase {
    func testDownloadURLIsPinnedToResolvedTag() {
        // The installer must never use the tag-agnostic /releases/latest
        // endpoint: it 404s whenever the repo's newest release overall is a
        // macOS app release, which carries no CLI binary.
        let url = CLIInstaller.downloadURL(version: "v0.5.50")?.absoluteString
        XCTAssertEqual(url, "https://github.com/nikships/skills-registry/releases/download/v0.5.50/skills-registry_darwin_arm64.tar.gz")
        XCTAssertFalse(url?.contains("releases/latest") ?? true)
    }

    func testInstallRejectsUnresolvedLatest() async {
        do {
            _ = try await CLIInstaller.install(version: "latest")
            XCTFail("install(version: \"latest\") must throw")
        } catch {
            XCTAssertTrue(error.localizedDescription.contains("latest"))
        }
    }
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
