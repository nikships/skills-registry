package bootstrap

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/anand-92/skills-registry/cli/internal/agents"
)

// InstallSkillMd writes the generated SKILL.md into each selected agent
// dot-folder's `skills/skill-registry/SKILL.md` path. Returns the list of
// written file paths.
func InstallSkillMd(home, cwd, registryRepo string, targets []agents.Target) ([]string, error) {
	body := SkillMd(registryRepo)
	var written []string
	for _, t := range targets {
		dir := filepath.Join(t.SkillsDir(home, cwd), "skill-registry")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return written, fmt.Errorf("create %s: %w", dir, err)
		}
		path := filepath.Join(dir, "SKILL.md")
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			return written, fmt.Errorf("write %s: %w", path, err)
		}
		written = append(written, path)
	}
	return written, nil
}

// MCPJSONSnippet returns the JSON config blob to paste into client MCP files.
func MCPJSONSnippet(mcpBinaryAbs string) string {
	if mcpBinaryAbs == "" {
		mcpBinaryAbs = "skill-registry-mcp"
	}
	return fmt.Sprintf(`{
  "mcpServers": {
    "skill-registry": {
      "command": %q
    }
  }
}`, mcpBinaryAbs)
}

// CodexTOMLSnippet returns the equivalent for Codex's TOML config.
func CodexTOMLSnippet(mcpBinaryAbs string) string {
	if mcpBinaryAbs == "" {
		mcpBinaryAbs = "skill-registry-mcp"
	}
	return fmt.Sprintf(`[mcp_servers.skill-registry]
command = %q
`, mcpBinaryAbs)
}
