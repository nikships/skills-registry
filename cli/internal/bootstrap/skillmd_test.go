package bootstrap

import (
	"strings"
	"testing"
)

// TestSkillMdInterpolatesRepo confirms the registry repo lands in both
// the frontmatter description and the prose body. Two interpolations
// are intentional: one for the human-readable description, one for the
// GitHub URL hint.
func TestSkillMdInterpolatesRepo(t *testing.T) {
	body := SkillMd("alice/registry")
	if c := strings.Count(body, "alice/registry"); c != 2 {
		t.Fatalf("expected 2 occurrences of alice/registry, got %d", c)
	}
}

// TestSkillMdDocumentsRemove pins down that the remove subcommand is
// documented in the generated SKILL.md. Without this the agent has no
// way to learn the destructive-cleanup workflow F4.1 added.
func TestSkillMdDocumentsRemove(t *testing.T) {
	body := SkillMd("alice/registry")
	if !strings.Contains(body, "skills-registry remove") {
		t.Fatalf("SKILL.md is missing the `remove` subcommand section")
	}
	if !strings.Contains(body, "atomic commit") {
		t.Fatalf("SKILL.md should explain remove deletes the slug atomically")
	}
}

// TestSkillMdDocumentsJSONFlag verifies the --json table is present so
// programmatic callers learn the JSON shape of every subcommand without
// having to scrape the source. Each row of the F4.2 contract is checked
// explicitly so a future rewrite that drops one is flagged here.
func TestSkillMdDocumentsJSONFlag(t *testing.T) {
	body := SkillMd("alice/registry")
	if !strings.Contains(body, "--json") {
		t.Fatalf("SKILL.md is missing the --json flag section")
	}
	for _, cmd := range []string{
		"skills-registry list --json",
		"skills-registry search <query> --json",
		"skills-registry get <slug> --json",
		"skills-registry publish <path> --json",
		"skills-registry sync --json",
		"skills-registry remove <slug> --json",
	} {
		if !strings.Contains(body, cmd) {
			t.Fatalf("SKILL.md is missing the %q row from the --json table", cmd)
		}
	}
}

// TestSkillMdDocumentsCurlInstaller documents the curl|sh installer.
// F1.2 added install.sh and F4.3 swapped the SKILL.md install hint
// from uvx to the curl one-liner; this test guards against a future
// edit that drops the new instruction.
func TestSkillMdDocumentsCurlInstaller(t *testing.T) {
	body := SkillMd("alice/registry")
	if !strings.Contains(body, "install.sh | sh") {
		t.Fatalf("SKILL.md is missing the curl|sh install instruction")
	}
	if !strings.Contains(body, "raw.githubusercontent.com/anand-92/skills-registry") {
		t.Fatalf("SKILL.md should reference the canonical install.sh URL")
	}
}
