import Foundation

/// Static, public configuration for the Skills Registry macOS app.
///
/// The GitHub App **client id** is public by design (it is not a secret).
/// Authentication uses the GitHub App Device Flow to mint a *user-to-server*
/// token; no client secret is ever embedded or required, which is what keeps
/// this app self-contained.
public enum AppConfig {
    /// GitHub App client id (Device Flow). The `Iv23li…` prefix marks this as
    /// a GitHub App (not a classic OAuth App).
    public static let githubClientID = "Iv23liKPKypuQdJBJveT"

    /// The GitHub App slug, used to build install / management URLs.
    /// Must match the app the `githubClientID` above belongs to. The external
    /// slug is historical and must not be changed as part of product naming.
    public static let githubAppSlug = "skills-registry-mcp"

    /// owner/repo of the project itself — source of CLI release tarballs.
    public static let projectRepo = "nikships/skills-registry"

    /// Where the one-click CLI installer drops the binary (mirrors install.sh).
    public static var cliInstallDir: URL {
        FileManager.default.homeDirectoryForCurrentUser
            .appendingPathComponent(".local/bin", isDirectory: true)
    }

    /// GitHub App installation URL for granting the app repository access.
    public static var appInstallURL: URL {
        URL(string: "https://github.com/apps/\(githubAppSlug)/installations/new")!
    }
}
