// swift-tools-version: 6.0
import PackageDescription

let package = Package(
    name: "SkillsRegistry",
    platforms: [
        .macOS(.v14)
    ],
    products: [
        .executable(name: "SkillsRegistry", targets: ["SkillsRegistry"]),
        .library(name: "SkillsRegistryCore", targets: ["SkillsRegistryCore"]),
    ],
    dependencies: [
        .package(url: "https://github.com/gonzalezreal/swift-markdown-ui", from: "2.4.0"),
        .package(url: "https://github.com/sparkle-project/Sparkle", from: "2.5.0"),
    ],
    targets: [
        // Pure-Foundation logic: auth, GitHub I/O, registry contracts, scan,
        // CLI install. No SwiftUI — fast to compile and unit-test, and the
        // single source of truth the UI layer drives.
        .target(
            name: "SkillsRegistryCore"
        ),
        // SwiftUI app: depends on Core + MarkdownUI. Holds @main, theme, and
        // every view. Editor draft/save behavior is covered by
        // SkillsRegistryTests (demo mode, no network). The rpath lets that
        // xctest bundle load Sparkle.framework, which SwiftPM links into the
        // app but does not stage beside the test runner.
        .executableTarget(
            name: "SkillsRegistry",
            dependencies: [
                "SkillsRegistryCore",
                .product(name: "MarkdownUI", package: "swift-markdown-ui"),
                .product(name: "Sparkle", package: "Sparkle"),
            ]
        ),
        .testTarget(
            name: "SkillsRegistryCoreTests",
            dependencies: ["SkillsRegistryCore"]
        ),
        .testTarget(
            name: "SkillsRegistryTests",
            dependencies: ["SkillsRegistry"],
            linkerSettings: [
                // swiftc, not ld, sees these flags. Point the xctest bundle at
                // Products/Debug, where SwiftPM drops Sparkle.framework.
                .unsafeFlags(["-Xlinker", "-rpath", "-Xlinker", "@loader_path/../../../"]),
            ]
        ),
    ],
    swiftLanguageModes: [.v5]
)
