package bootstrap

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/anand-92/skills-registry/cli/internal/agents"
)

func TestInstallSkillMdWritesEverywhere(t *testing.T) {
	home := t.TempDir()
	cwd := t.TempDir()
	targets := []agents.Target{
		{DotDir: ".claude", UnderHome: true},
		{DotDir: ".agents", UnderHome: false},
	}
	paths, err := InstallSkillMd(home, cwd, "alice/skills", targets)
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 2 {
		t.Fatalf("expected 2 paths, got %d", len(paths))
	}
	for _, p := range paths {
		body, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(body), "alice/skills") {
			t.Fatalf("registry repo missing from %s", p)
		}
		if !strings.HasSuffix(p, filepath.Join("skills-registry", "SKILL.md")) {
			t.Fatalf("unexpected path: %s", p)
		}
	}
}

func TestMCPJSONSnippetPointsAtHostedServer(t *testing.T) {
	out := MCPJSONSnippet()
	if !strings.Contains(out, HostedMCPURL) {
		t.Fatalf("snippet missing hosted URL %q: %s", HostedMCPURL, out)
	}
	if !strings.Contains(out, "\"skills-registry\":") {
		t.Fatalf("snippet missing server name: %s", out)
	}
	if strings.Contains(out, "\"command\"") || strings.Contains(out, "\"args\"") {
		t.Fatalf("hosted snippet must not include stdio command/args: %s", out)
	}
	if !strings.Contains(out, "\"url\":") {
		t.Fatalf("hosted snippet must declare a url: %s", out)
	}
}

func TestHostedMCPURLIsHTTPS(t *testing.T) {
	if !strings.HasPrefix(HostedMCPURL, "https://") {
		t.Fatalf("HostedMCPURL must be HTTPS, got %q", HostedMCPURL)
	}
}
