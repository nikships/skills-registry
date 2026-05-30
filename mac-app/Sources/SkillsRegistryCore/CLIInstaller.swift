import Foundation

/// One-click install of the `skills-registry` Go CLI. Mirrors `install.sh`:
/// download the matching darwin/arm64 release tarball from GitHub Releases,
/// extract the binary, drop it into `~/.local/bin`, mark it executable.
public enum CLIInstaller {
    public static var binaryPath: URL {
        AppConfig.cliInstallDir.appendingPathComponent("skills-registry")
    }

    /// Is the CLI installed in our managed dir or anywhere on PATH?
    public static func isInstalled() -> Bool {
        installedPath() != nil
    }

    public static func installedPath() -> URL? {
        let fm = FileManager.default
        if fm.isExecutableFile(atPath: binaryPath.path) { return binaryPath }
        let env = ProcessInfo.processInfo.environment
        let path = env["PATH"] ?? "/usr/local/bin:/usr/bin:/bin:/opt/homebrew/bin"
        for dir in path.split(separator: ":") {
            let p = URL(fileURLWithPath: String(dir)).appendingPathComponent("skills-registry")
            if fm.isExecutableFile(atPath: p.path) { return p }
        }
        return nil
    }

    /// Best-effort installed version via `skills-registry --version`. nil if
    /// not installed or the call fails.
    public static func installedVersion() async -> String? {
        guard let path = installedPath() else { return nil }
        guard let out = try? await Subprocess.run(path.path, ["--version"]) else { return nil }
        let line = out.stdout.trimmingCharacters(in: .whitespacesAndNewlines)
        return line.isEmpty ? nil : line
    }

    /// Download + extract + install. Returns the installed binary path.
    @discardableResult
    public static func install(version: String = "latest") async throws -> URL {
        let asset = "skills-registry_darwin_arm64.tar.gz"
        let urlString = version == "latest"
            ? "https://github.com/\(AppConfig.projectRepo)/releases/latest/download/\(asset)"
            : "https://github.com/\(AppConfig.projectRepo)/releases/download/\(version)/\(asset)"
        guard let url = URL(string: urlString) else {
            throw GitHubError(status: 0, message: "Bad release URL", endpoint: urlString)
        }

        let fm = FileManager.default
        let (tmpDownload, resp) = try await URLSession.shared.download(from: url)
        let code = (resp as? HTTPURLResponse)?.statusCode ?? 0
        guard code == 200 else {
            throw GitHubError(status: code, message: "Download failed: \(urlString)", endpoint: urlString)
        }

        let work = fm.temporaryDirectory.appendingPathComponent("skills-registry-install-\(UUID().uuidString)")
        try fm.createDirectory(at: work, withIntermediateDirectories: true)
        defer { try? fm.removeItem(at: work) }

        let tarball = work.appendingPathComponent(asset)
        try fm.moveItem(at: tmpDownload, to: tarball)

        let result = try await Subprocess.run("/usr/bin/tar",
                                              ["-xzf", tarball.path, "-C", work.path, "skills-registry"])
        guard result.exitCode == 0 else {
            throw GitHubError(status: 0, message: "tar failed: \(result.stderr)", endpoint: "tar")
        }
        let extracted = work.appendingPathComponent("skills-registry")
        guard fm.fileExists(atPath: extracted.path) else {
            throw GitHubError(status: 0, message: "Binary not found in archive", endpoint: "tar")
        }

        try fm.createDirectory(at: AppConfig.cliInstallDir, withIntermediateDirectories: true)
        if fm.fileExists(atPath: binaryPath.path) { try? fm.removeItem(at: binaryPath) }
        try fm.moveItem(at: extracted, to: binaryPath)
        try fm.setAttributes([.posixPermissions: 0o755], ofItemAtPath: binaryPath.path)
        return binaryPath
    }

    /// Whether `~/.local/bin` is on the user's PATH (best-effort).
    public static var installDirOnPath: Bool {
        let env = ProcessInfo.processInfo.environment
        let path = env["PATH"] ?? ""
        let target = AppConfig.cliInstallDir.path
        return path.split(separator: ":").contains { String($0) == target }
    }
}
