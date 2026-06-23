package bootstrap

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/nikships/skills-registry/cli/internal/agents"
)

// HostedMCPURL is the public Streamable-HTTP endpoint of the hosted
// FastMCP server. Wizards and `skills-registry bootstrap` print this URL
// inside the JSON snippet users paste into their MCP client config.
//
// The CLI never installs, boots, or otherwise touches an MCP server —
// the only MCP responsibility it has is producing this snippet.
const HostedMCPURL = "https://mcp.skills-registry.dev/mcp"

// InstallSkillMd writes the generated SKILL.md into each selected agent
// dot-folder's `skills/skills-registry/SKILL.md` path. Returns the list of
// written file paths.
func InstallSkillMd(home, cwd, registryRepo string, targets []agents.Target) ([]string, error) {
	body := SkillMd(registryRepo)
	var written []string
	for _, t := range targets {
		dir := filepath.Join(t.SkillsDir(home, cwd), "skills-registry")
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

// RefreshSkillMd rewrites the generated SKILL.md for every agent
// dot-folder that ALREADY has the skills-registry meta-skill installed,
// so a registry-repo change (e.g. editing the repo in Settings)
// propagates into the copies the user opted into. The generated body
// embeds the registry slug, so without this an installed copy keeps
// pointing at the old repo until it's manually reinstalled.
//
// Unlike InstallSkillMd this never creates a new copy: a dot-folder with
// no existing skills-registry/SKILL.md is skipped, so we don't silently
// fan the meta-skill out into agents the user never selected. Returns the
// list of rewritten file paths.
func RefreshSkillMd(home, cwd, registryRepo string, targets []agents.Target) ([]string, error) {
	body := SkillMd(registryRepo)
	var written []string
	for _, t := range targets {
		path := filepath.Join(t.SkillsDir(home, cwd), "skills-registry", "SKILL.md")
		if _, err := os.Stat(path); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return written, fmt.Errorf("stat %s: %w", path, err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			return written, fmt.Errorf("write %s: %w", path, err)
		}
		written = append(written, path)
	}
	return written, nil
}

// MCPJSONSnippet returns the JSON blob to paste into a desktop MCP
// client's config (`mcp.json` for Claude Code / Claude Desktop / Cursor /
// VS Code+Copilot). The snippet points at the hosted server; the user's
// MCP client handles the OAuth dance on first connect.
//
// Codex does not yet support remote MCP servers (its TOML config accepts
// only a `command` for stdio MCPs), so we deliberately do not emit a
// Codex snippet — calling code surfaces a one-line note instead.
func MCPJSONSnippet() string {
	return fmt.Sprintf(`{
  "mcpServers": {
    "skills-registry": {
      "url": %q
    }
  }
}`, HostedMCPURL)
}
