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
    /// Must match the app the `githubClientID` above belongs to
    /// ("Skills Registry MCP"). The bare `skills-registry` slug is a
    /// different, unrelated GitHub App.
    public static let githubAppSlug = "skills-registry-mcp"

    /// Hosted MCP endpoint users paste into their MCP client config. Mirrors
    /// `cli/internal/bootstrap/install.go`'s `HostedMCPURL`.
    public static let hostedMCPURL = "https://mcp.skills-registry.dev/mcp"

    /// owner/repo of the project itself — source of CLI release tarballs.
    public static let projectRepo = "anand-92/skills-registry"

    /// Where the one-click CLI installer drops the binary (mirrors install.sh).
    public static var cliInstallDir: URL {
        FileManager.default.homeDirectoryForCurrentUser
            .appendingPathComponent(".local/bin", isDirectory: true)
    }

    /// GitHub App installation URL (handoff so the hosted MCP can serve the repo).
    public static var appInstallURL: URL {
        URL(string: "https://github.com/apps/\(githubAppSlug)/installations/new")!
    }

    /// JSON snippet for an MCP client config. Mirrors `MCPJSONSnippet()`.
    public static var mcpJSONSnippet: String {
        """
        {
          "mcpServers": {
            "skills-registry": {
              "url": "\(hostedMCPURL)"
            }
          }
        }
        """
    }
}
