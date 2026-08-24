package bootstrap

import (
	"os"
	"path/filepath"
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
		"skills-registry discover <query> --json",
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

// TestSkillMdSearchesLocallyBeforeTheIndex pins the ordering the gateway
// teaches: the personal registry is searched first, and the public index is
// offered only after that misses. An always-on public search would send every
// user prompt to a third-party index, which is exactly what this ordering
// exists to prevent.
func TestSkillMdSearchesLocallyBeforeTheIndex(t *testing.T) {
	body := SkillMd("alice/registry")
	local := strings.Index(body, "## 1. Search this registry first")
	public := strings.Index(body, "## 2. On a local miss, offer the public index")
	if local < 0 {
		t.Fatalf("SKILL.md is missing the local-search-first step")
	}
	if public < 0 {
		t.Fatalf("SKILL.md is missing the public-index step")
	}
	if local > public {
		t.Fatalf("the public index step (%d) must come after local search (%d)", public, local)
	}
	if !strings.Contains(body, "Only after `search` comes up empty") {
		t.Fatalf("SKILL.md should gate the public index behind a local miss")
	}
	if !strings.Contains(body, "never let a registry lookup stall the work") {
		t.Fatalf("SKILL.md should say the lookup must not block the user's task")
	}
}

// TestSkillMdRequiresConfirmationBeforeAdding covers the safety half of the
// discover step: an agent must not import a stranger's skill, durably install
// it, or clear a safety block on its own initiative.
func TestSkillMdRequiresConfirmationBeforeAdding(t *testing.T) {
	body := SkillMd("alice/registry")
	for _, want := range []string{
		"skills-registry discover <query>",
		"skills-registry add <skill_url>",
		"**ask the user first**",
		"Never run `add` on a URL the user has not approved.",
		"Do **not** pass `--install`",
		"`--allow-unsafe`. Never add that flag on your own",
		"`unscored`",
		"Nothing fetched is ever executed.",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("SKILL.md is missing %q from the discover step", want)
		}
	}
}

// TestSkillMdIsCLIOnly keeps the hosted MCP service out of the generated
// gateway. The service was removed from this repo; a template that still
// advertises it would send agents at an endpoint that does not exist.
func TestSkillMdIsCLIOnly(t *testing.T) {
	body := SkillMd("alice/registry")
	for _, banned := range []string{
		"mcp.skills-registry.dev",
		"mcpServers",
		"search_skills",
		"get_skill",
		"MCP",
	} {
		if strings.Contains(body, banned) {
			t.Fatalf("SKILL.md must stay CLI-only but mentions %q", banned)
		}
	}
}

// TestSkillMdMatchesSwiftTemplate is the cross-language contract: the macOS
// app writes the same gateway file as the CLI, so the two templates must
// interpolate to the same bytes. It reads the Swift source rather than a
// checked-in copy, so an edit to one language that is not mirrored in the
// other fails here instead of shipping two divergent gateways.
func TestSkillMdMatchesSwiftTemplate(t *testing.T) {
	const repo = "alice/registry"
	swift := swiftSkillMd(t, repo)
	got := SkillMd(repo)
	if got == swift {
		return
	}
	if len(got) != len(swift) {
		t.Errorf("template byte lengths differ: Go %d, Swift %d", len(got), len(swift))
	}
	goLines := strings.Split(got, "\n")
	swiftLines := strings.Split(swift, "\n")
	for i := 0; i < len(goLines) && i < len(swiftLines); i++ {
		if goLines[i] != swiftLines[i] {
			t.Fatalf("first divergence at line %d:\n  Go:    %q\n  Swift: %q", i+1, goLines[i], swiftLines[i])
		}
	}
	t.Fatalf("templates differ in length only: Go %d lines, Swift %d lines", len(goLines), len(swiftLines))
}

// swiftSkillMd renders SkillMdTemplate.swift's multi-line literal for repo.
// The literal is plain text apart from the `\(registryRepo)` interpolations,
// so substituting those reproduces exactly what the app writes.
//
// Swift drops the newline that precedes the closing delimiter, and the
// delimiter here sits at column zero so no indentation is stripped; trimming
// that one newline is the whole of the transformation.
func swiftSkillMd(t *testing.T, repo string) string {
	t.Helper()
	path := filepath.Join("..", "..", "..", "mac-app", "Sources", "SkillsRegistryCore", "SkillMdTemplate.swift")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the Swift template: %v", err)
	}
	src := string(raw)
	const open = "return \"\"\"\n"
	start := strings.Index(src, open)
	if start < 0 {
		t.Fatalf("%s no longer opens its literal with %q", path, open)
	}
	body := src[start+len(open):]
	end := strings.Index(body, "\n\"\"\"")
	if end < 0 {
		t.Fatalf("%s has an unterminated multi-line literal, or its closing delimiter is indented", path)
	}
	return strings.ReplaceAll(body[:end], `\(registryRepo)`, repo)
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
	if !strings.Contains(body, "raw.githubusercontent.com/nikships/skills-registry") {
		t.Fatalf("SKILL.md should reference the canonical install.sh URL")
	}
}
