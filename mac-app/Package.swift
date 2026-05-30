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
        // every view. Not unit-tested (exercised via cua-driver in demo mode).
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
    ],
    swiftLanguageModes: [.v5]
)
