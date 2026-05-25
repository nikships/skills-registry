// Package agents lists every AI tool dot-folder we know about and the human
// display name we use for it in the bootstrap multi-select. This is the
// canonical list for the project — the Python side no longer carries one
// (the legacy `gather` command was its only consumer).
package agents

import "sort"

// Target is one row in the agent multi-select.
type Target struct {
	DotDir    string // e.g. ".claude" (relative under $HOME or .)
	Display   string // shown in the TUI, e.g. "Claude Code"
	Universal bool   // true if selected by default and can't be toggled off
	UnderHome bool   // true if the install path lives under $HOME (vs cwd)
}

// SkillsDir returns the absolute SKILL.md folder for this target.
// The skills-registry SKILL.md goes at SkillsDir(home, cwd) + "/skills-registry".
func (t Target) SkillsDir(home, cwd string) string {
	if t.UnderHome {
		return home + "/" + t.DotDir + "/skills"
	}
	return cwd + "/" + t.DotDir + "/skills"
}

// All returns every known agent target, sorted by display name. The
// `.agents` project-local folder is marked Universal so it's always
// selected in the multi-select.
func All() []Target {
	out := make([]Target, 0, len(known))
	out = append(out, known...)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Universal != out[j].Universal {
			return out[i].Universal
		}
		return out[i].Display < out[j].Display
	})
	return out
}

// known is the curated list. Display names sourced from each tool's
// documentation / brand guidelines.
var known = []Target{
	// Universal (project-local, picked up by most agents)
	{DotDir: ".agents", Display: "Universal (.agents/skills)", Universal: true, UnderHome: false},

	// Home-directory agents
	{DotDir: ".claude", Display: "Claude Code", UnderHome: true},
	{DotDir: ".claude-code", Display: "Claude Code (legacy)", UnderHome: true},
	{DotDir: ".factory", Display: "Factory", UnderHome: true},
	{DotDir: ".codex", Display: "Codex CLI", UnderHome: true},
	{DotDir: ".cursor", Display: "Cursor", UnderHome: true},
	{DotDir: ".junie", Display: "Junie", UnderHome: true},
	{DotDir: ".aider", Display: "Aider", UnderHome: true},
	{DotDir: ".continue", Display: "Continue", UnderHome: true},
	{DotDir: ".windsurf", Display: "Windsurf", UnderHome: true},
	{DotDir: ".codeium", Display: "Codeium", UnderHome: true},
	{DotDir: ".zed", Display: "Zed", UnderHome: true},
	{DotDir: ".anthropic", Display: "Anthropic", UnderHome: true},
	{DotDir: ".openai", Display: "OpenAI", UnderHome: true},
	{DotDir: ".cline", Display: "Cline", UnderHome: true},
	{DotDir: ".roo", Display: "Roo", UnderHome: true},
	{DotDir: ".roocode", Display: "Roo Code", UnderHome: true},
	{DotDir: ".gemini", Display: "Gemini", UnderHome: true},
	{DotDir: ".antigravity", Display: "Antigravity", UnderHome: true},
	{DotDir: ".aider-desk", Display: "Aider Desk", UnderHome: true},
	{DotDir: ".augment", Display: "Augment", UnderHome: true},
	{DotDir: ".bob", Display: "Bob", UnderHome: true},
	{DotDir: ".codeartsdoer", Display: "CodeArts Doer", UnderHome: true},
	{DotDir: ".codebuddy", Display: "CodeBuddy", UnderHome: true},
	{DotDir: ".codemaker", Display: "CodeMaker", UnderHome: true},
	{DotDir: ".codestudio", Display: "Code Studio", UnderHome: true},
	{DotDir: ".commandcode", Display: "Command Code", UnderHome: true},
	{DotDir: ".copilot", Display: "GitHub Copilot", UnderHome: true},
	{DotDir: ".cortex", Display: "Cortex", UnderHome: true},
	{DotDir: ".crush", Display: "Crush", UnderHome: true},
	{DotDir: ".deepagents", Display: "DeepAgents", UnderHome: true},
	{DotDir: ".devin", Display: "Devin", UnderHome: true},
	{DotDir: ".firebender", Display: "Firebender", UnderHome: true},
	{DotDir: ".forge", Display: "Forge", UnderHome: true},
	{DotDir: ".goose", Display: "Goose", UnderHome: true},
	{DotDir: ".iflow", Display: "iFlow", UnderHome: true},
	{DotDir: ".kilocode", Display: "Kilo Code", UnderHome: true},
	{DotDir: ".kiro", Display: "Kiro", UnderHome: true},
	{DotDir: ".kode", Display: "Kode", UnderHome: true},
	{DotDir: ".mcpjam", Display: "MCPJam", UnderHome: true},
	{DotDir: ".mux", Display: "Mux", UnderHome: true},
	{DotDir: ".opencode", Display: "OpenCode", UnderHome: true},
	{DotDir: ".openhands", Display: "OpenHands", UnderHome: true},
	{DotDir: ".pi", Display: "Pi", UnderHome: true},
	{DotDir: ".qoder", Display: "Qoder", UnderHome: true},
	{DotDir: ".qwen", Display: "Qwen Code", UnderHome: true},
	{DotDir: ".rovodev", Display: "Rovo Dev", UnderHome: true},
	{DotDir: ".tabnine", Display: "Tabnine", UnderHome: true},
	{DotDir: ".trae", Display: "Trae", UnderHome: true},
	{DotDir: ".trae-cn", Display: "Trae CN", UnderHome: true},
	{DotDir: ".vibe", Display: "Vibe", UnderHome: true},
	{DotDir: ".zencoder", Display: "Zencoder", UnderHome: true},
	{DotDir: ".neovate", Display: "Neovate", UnderHome: true},
	{DotDir: ".pochi", Display: "Pochi", UnderHome: true},
	{DotDir: ".adal", Display: "Adal", UnderHome: true},
	{DotDir: ".snowflake", Display: "Snowflake", UnderHome: true},
}
