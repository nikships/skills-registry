package bootstrap

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nikships/skills-registry/cli/internal/agents"
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

// TestRefreshSkillMdRewritesOnlyExistingCopies is the regression test for
// the stale-meta-skill bug: after the registry repo changes, only the
// dot-folders that already have the meta-skill installed get rewritten to
// the new slug — folders without a copy are never created.
func TestRefreshSkillMdRewritesOnlyExistingCopies(t *testing.T) {
	home := t.TempDir()
	cwd := t.TempDir()
	installed := agents.Target{DotDir: ".claude", UnderHome: true}
	absent := agents.Target{DotDir: ".cursor", UnderHome: true}

	// Seed an existing copy that references the old repo.
	if _, err := InstallSkillMd(home, cwd, "old-owner/skills", []agents.Target{installed}); err != nil {
		t.Fatalf("seed install: %v", err)
	}

	written, err := RefreshSkillMd(home, cwd, "new-owner/skills",
		[]agents.Target{installed, absent})
	if err != nil {
		t.Fatalf("RefreshSkillMd: %v", err)
	}
	if len(written) != 1 {
		t.Fatalf("rewrote %d copies, want 1 (only the pre-installed one)", len(written))
	}

	// The pre-installed copy now references the new repo, not the old one.
	body, err := os.ReadFile(written[0])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "new-owner/skills") {
		t.Errorf("refreshed copy missing new repo slug:\n%s", body)
	}
	if strings.Contains(string(body), "old-owner/skills") {
		t.Errorf("refreshed copy still references old repo slug:\n%s", body)
	}

	// The dot-folder that never had a copy must not have one created.
	absentPath := filepath.Join(absent.SkillsDir(home, cwd), "skills-registry", "SKILL.md")
	if _, err := os.Stat(absentPath); !os.IsNotExist(err) {
		t.Errorf("RefreshSkillMd created a copy in a folder that had none: %s (err=%v)", absentPath, err)
	}
}

// TestRefreshSkillMdNoExistingCopiesIsNoop confirms a clean machine (no
// meta-skill installed anywhere) yields no writes and no error.
func TestRefreshSkillMdNoExistingCopiesIsNoop(t *testing.T) {
	home := t.TempDir()
	cwd := t.TempDir()
	written, err := RefreshSkillMd(home, cwd, "owner/skills", agents.All())
	if err != nil {
		t.Fatalf("RefreshSkillMd on clean machine: %v", err)
	}
	if len(written) != 0 {
		t.Fatalf("rewrote %d copies on a clean machine, want 0", len(written))
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
