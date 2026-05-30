import Foundation

/// One known AI-tool dot-folder. Port of `cli/internal/agents/agents.go`.
public struct AgentTarget: Hashable, Sendable {
    public var dotDir: String   // e.g. ".claude"
    public var display: String  // e.g. "Claude Code"
    public var universal: Bool  // project-local, always relevant
    public var underHome: Bool  // install path under $HOME vs cwd

    public init(dotDir: String, display: String, universal: Bool = false, underHome: Bool = true) {
        self.dotDir = dotDir
        self.display = display
        self.universal = universal
        self.underHome = underHome
    }

    /// Absolute `<dot>/skills` directory for this target.
    public func skillsDir(home: String, cwd: String) -> String {
        let base = underHome ? home : cwd
        return base + "/" + dotDir + "/skills"
    }
}

public enum Agents {
    /// Every known target, sorted (universal first, then by display name).
    public static func all() -> [AgentTarget] {
        known.sorted { a, b in
            if a.universal != b.universal { return a.universal }
            return a.display < b.display
        }
    }

    /// Just the dot-dir names — used to enumerate candidate source folders.
    public static func dotDirs() -> [String] { known.map(\.dotDir) }

    static let known: [AgentTarget] = [
        AgentTarget(dotDir: ".agents", display: "Universal (.agents/skills)", universal: true, underHome: false),
        AgentTarget(dotDir: ".claude", display: "Claude Code"),
        AgentTarget(dotDir: ".claude-code", display: "Claude Code (legacy)"),
        AgentTarget(dotDir: ".factory", display: "Factory"),
        AgentTarget(dotDir: ".codex", display: "Codex CLI"),
        AgentTarget(dotDir: ".cursor", display: "Cursor"),
        AgentTarget(dotDir: ".junie", display: "Junie"),
        AgentTarget(dotDir: ".aider", display: "Aider"),
        AgentTarget(dotDir: ".continue", display: "Continue"),
        AgentTarget(dotDir: ".windsurf", display: "Windsurf"),
        AgentTarget(dotDir: ".codeium", display: "Codeium"),
        AgentTarget(dotDir: ".zed", display: "Zed"),
        AgentTarget(dotDir: ".anthropic", display: "Anthropic"),
        AgentTarget(dotDir: ".openai", display: "OpenAI"),
        AgentTarget(dotDir: ".cline", display: "Cline"),
        AgentTarget(dotDir: ".roo", display: "Roo"),
        AgentTarget(dotDir: ".roocode", display: "Roo Code"),
        AgentTarget(dotDir: ".gemini", display: "Gemini"),
        AgentTarget(dotDir: ".antigravity", display: "Antigravity"),
        AgentTarget(dotDir: ".aider-desk", display: "Aider Desk"),
        AgentTarget(dotDir: ".augment", display: "Augment"),
        AgentTarget(dotDir: ".bob", display: "Bob"),
        AgentTarget(dotDir: ".codeartsdoer", display: "CodeArts Doer"),
        AgentTarget(dotDir: ".codebuddy", display: "CodeBuddy"),
        AgentTarget(dotDir: ".codemaker", display: "CodeMaker"),
        AgentTarget(dotDir: ".codestudio", display: "Code Studio"),
        AgentTarget(dotDir: ".commandcode", display: "Command Code"),
        AgentTarget(dotDir: ".copilot", display: "GitHub Copilot"),
        AgentTarget(dotDir: ".cortex", display: "Cortex"),
        AgentTarget(dotDir: ".crush", display: "Crush"),
        AgentTarget(dotDir: ".deepagents", display: "DeepAgents"),
        AgentTarget(dotDir: ".devin", display: "Devin"),
        AgentTarget(dotDir: ".firebender", display: "Firebender"),
        AgentTarget(dotDir: ".forge", display: "Forge"),
        AgentTarget(dotDir: ".goose", display: "Goose"),
        AgentTarget(dotDir: ".iflow", display: "iFlow"),
        AgentTarget(dotDir: ".kilocode", display: "Kilo Code"),
        AgentTarget(dotDir: ".kiro", display: "Kiro"),
        AgentTarget(dotDir: ".kode", display: "Kode"),
        AgentTarget(dotDir: ".mcpjam", display: "MCPJam"),
        AgentTarget(dotDir: ".mux", display: "Mux"),
        AgentTarget(dotDir: ".opencode", display: "OpenCode"),
        AgentTarget(dotDir: ".openhands", display: "OpenHands"),
        AgentTarget(dotDir: ".pi", display: "Pi"),
        AgentTarget(dotDir: ".qoder", display: "Qoder"),
        AgentTarget(dotDir: ".qwen", display: "Qwen Code"),
        AgentTarget(dotDir: ".rovodev", display: "Rovo Dev"),
        AgentTarget(dotDir: ".tabnine", display: "Tabnine"),
        AgentTarget(dotDir: ".trae", display: "Trae"),
        AgentTarget(dotDir: ".trae-cn", display: "Trae CN"),
        AgentTarget(dotDir: ".vibe", display: "Vibe"),
        AgentTarget(dotDir: ".zencoder", display: "Zencoder"),
        AgentTarget(dotDir: ".neovate", display: "Neovate"),
        AgentTarget(dotDir: ".pochi", display: "Pochi"),
        AgentTarget(dotDir: ".adal", display: "Adal"),
        AgentTarget(dotDir: ".snowflake", display: "Snowflake"),
    ]
}
