package scan

import (
	"os"
	"path/filepath"
	"testing"
)

func writeSkill(t *testing.T, root, name, body string) string {
	t.Helper()
	folder := filepath.Join(root, name)
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(folder, MainFileName), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return folder
}

func TestSlugify(t *testing.T) {
	cases := map[string]string{
		"Code Review":         "code_review",
		"  Trim Whitespace  ": "trim_whitespace",
		"!!!":                 "skill",
		"hello-world":         "hello_world",
	}
	for in, want := range cases {
		if got := Slugify(in); got != want {
			t.Errorf("Slugify(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDiscoverParsesFrontmatter(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "code-review",
		"---\nname: Code Review\ndescription: review code\n---\nBody.\n")
	writeSkill(t, root, "trim",
		"# Just a header\n\nFirst paragraph here.\n\nSecond paragraph.\n")

	out, err := Discover([]Source{{Path: root, Label: "test"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 {
		t.Fatalf("expected 2 skills, got %d (%+v)", len(out), out)
	}
	bySlug := map[string]Skill{}
	for _, s := range out {
		bySlug[s.Slug] = s
	}
	cr, ok := bySlug["code_review"]
	if !ok {
		t.Fatalf("missing code_review; got %v", bySlug)
	}
	if cr.Name != "Code Review" || cr.Description != "review code" {
		t.Fatalf("wrong frontmatter parse: %+v", cr)
	}
	tr := bySlug["trim"]
	if tr.Description != "First paragraph here." {
		t.Fatalf("description fallback wrong: %q", tr.Description)
	}
}

func TestDiscoverSourcesScansDotDirs(t *testing.T) {
	home := t.TempDir()
	cwd := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".claude", "skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(cwd, ".agents", "skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	sources := DiscoverSources(home, cwd, nil, []string{".claude", ".agents"})
	if len(sources) != 2 {
		t.Fatalf("expected 2 sources, got %d (%+v)", len(sources), sources)
	}
}

func TestDedupeAgainstFiltersByRemoteSlugs(t *testing.T) {
	local := []Skill{
		{Slug: "alpha"},
		{Slug: "beta"},
		{Slug: "gamma"},
	}
	remote := map[string]struct{}{"beta": {}}
	out := DedupeAgainst(local, remote)
	if len(out) != 2 || out[0].Slug != "alpha" || out[1].Slug != "gamma" {
		t.Fatalf("dedupe wrong: %+v", out)
	}
}

func TestEntriesForCleanup_FindsEveryDotFolderCopy(t *testing.T) {
	// The bug we're guarding against: scan.Discover slug-dedupes across
	// sources, so the old cleanup deleted only ONE copy per init run. With
	// the same slug in five dot-folders, the user had to re-run init five
	// times. EntriesForCleanup must return every source's copy in one shot.
	tmp := t.TempDir()
	dotDirs := []string{".codex", ".copilot", ".cursor", ".factory", ".gemini"}
	var sources []Source
	for _, dot := range dotDirs {
		skillsDir := filepath.Join(tmp, dot, "skills")
		writeSkill(t, skillsDir, "adaptive", "---\nname: Adaptive\n---\nbody\n")
		sources = append(sources, Source{Path: skillsDir, Label: "~/" + dot + "/skills"})
	}
	entries := EntriesForCleanup(sources, map[string]struct{}{"adaptive": {}})
	if len(entries) != 5 {
		t.Fatalf("expected 5 entries (one per dot-folder), got %d (%+v)", len(entries), entries)
	}
	for _, en := range entries {
		if en.IsSymlink {
			t.Fatalf("entry should be a real folder, not symlink: %+v", en)
		}
		if _, err := os.Stat(filepath.Join(en.Path, MainFileName)); err != nil {
			t.Fatalf("expected SKILL.md to exist under %s: %v", en.Path, err)
		}
	}
}

func TestEntriesForCleanup_IncludesSymlinks(t *testing.T) {
	// Symlinks under <dot>/skills/ never showed up in Discover (filepath.WalkDir
	// doesn't descend symlinks looking for SKILL.md), so the previous cleanup
	// never unlinked them — even when their targets were already gone.
	tmp := t.TempDir()
	target := filepath.Join(tmp, ".agents", "skills", "animejs")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	claudeSkills := filepath.Join(tmp, ".claude", "skills")
	if err := os.MkdirAll(claudeSkills, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(claudeSkills, "animejs")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	// Even a *dangling* symlink should still match by name — the user wants
	// it gone regardless of whether the target still exists.
	dangling := filepath.Join(claudeSkills, "ghost-skill")
	if err := os.Symlink(filepath.Join(tmp, "does-not-exist"), dangling); err != nil {
		t.Fatal(err)
	}
	sources := []Source{{Path: claudeSkills, Label: "~/.claude/skills"}}
	entries := EntriesForCleanup(sources, map[string]struct{}{"animejs": {}, "ghost-skill": {}})
	if len(entries) != 2 {
		t.Fatalf("expected 2 symlink entries, got %d (%+v)", len(entries), entries)
	}
	for _, en := range entries {
		if !en.IsSymlink {
			t.Fatalf("symlink not flagged: %+v", en)
		}
	}
}

func TestEntriesForCleanup_ProtectsSkillRegistryInstall(t *testing.T) {
	// bootstrap.InstallSkillMd writes our own SKILL.md into
	// <source>/skills-registry/SKILL.md. If the registry happened to have a
	// slug named "skills-registry" (it usually does — this very project), we
	// MUST NOT delete it.
	tmp := t.TempDir()
	writeSkill(t, filepath.Join(tmp, ".factory", "skills"), "skills-registry", "---\nname: x\n---\n")
	sources := []Source{{Path: filepath.Join(tmp, ".factory", "skills"), Label: "~/.factory/skills"}}
	entries := EntriesForCleanup(sources, map[string]struct{}{"skills-registry": {}})
	if len(entries) != 0 {
		t.Fatalf("skills-registry install target must be protected, got %+v", entries)
	}
}

func TestEntriesForCleanup_SkipsUnknownSlugs(t *testing.T) {
	// Only delete things we know are in the registry. A random folder
	// named like a real skill but absent from the registry stays.
	tmp := t.TempDir()
	writeSkill(t, filepath.Join(tmp, ".factory", "skills"), "my-private-skill",
		"---\nname: private\n---\n")
	sources := []Source{{Path: filepath.Join(tmp, ".factory", "skills"), Label: "~/.factory/skills"}}
	entries := EntriesForCleanup(sources, map[string]struct{}{"adaptive": {}, "styles": {}})
	if len(entries) != 0 {
		t.Fatalf("unknown-slug folders must be preserved, got %+v", entries)
	}
}

func TestEntriesForCleanup_RealDirRequiresSkillMd(t *testing.T) {
	// Defensive: if a real directory happens to share a name with a slug
	// but contains no SKILL.md, it's probably unrelated user content and
	// must be left alone.
	tmp := t.TempDir()
	skillsDir := filepath.Join(tmp, ".factory", "skills")
	noMain := filepath.Join(skillsDir, "styles", "unrelated")
	if err := os.MkdirAll(noMain, 0o755); err != nil {
		t.Fatal(err)
	}
	sources := []Source{{Path: skillsDir, Label: "~/.factory/skills"}}
	entries := EntriesForCleanup(sources, map[string]struct{}{"styles": {}})
	if len(entries) != 0 {
		t.Fatalf("dir without SKILL.md should be preserved, got %+v", entries)
	}
}

func TestEntriesForCleanup_MatchesSlugifiedFolderName(t *testing.T) {
	// Slugify normalizes hyphens (and any non-[a-z0-9]) to underscores, so
	// the on-disk folder name and the registry slug usually differ for any
	// skill with a hyphenated name. A literal-name lookup against the
	// registry's slug set would miss every such skill — i.e. the cleanup
	// would do nothing for the most common naming convention.
	tmp := t.TempDir()
	skillsDir := filepath.Join(tmp, ".factory", "skills")
	writeSkill(t, skillsDir, "agp-9-upgrade",
		"---\nname: agp-9-upgrade\n---\n")
	writeSkill(t, skillsDir, "perfetto-sql",
		"---\nname: perfetto-sql\n---\n")
	sources := []Source{{Path: skillsDir, Label: "~/.factory/skills"}}
	registrySlugs := map[string]struct{}{
		"agp_9_upgrade": {},
		"perfetto_sql":  {},
	}
	entries := EntriesForCleanup(sources, registrySlugs)
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries (slugified match), got %d: %+v", len(entries), entries)
	}
}

func TestEntriesForCleanup_SkipsDotfiles(t *testing.T) {
	tmp := t.TempDir()
	skillsDir := filepath.Join(tmp, ".factory", "skills")
	if err := os.MkdirAll(skillsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// macOS sprinkles .DS_Store everywhere; even if a slug were named
	// ".DS_Store" we'd skip it (it can't be a real skill anyway).
	if err := os.WriteFile(filepath.Join(skillsDir, ".DS_Store"), []byte("junk"), 0o644); err != nil {
		t.Fatal(err)
	}
	sources := []Source{{Path: skillsDir, Label: "~/.factory/skills"}}
	entries := EntriesForCleanup(sources, map[string]struct{}{".DS_Store": {}})
	if len(entries) != 0 {
		t.Fatalf("dotfiles must be skipped, got %+v", entries)
	}
}

func TestHashIsDeterministic(t *testing.T) {
	root := t.TempDir()
	folder := writeSkill(t, root, "alpha", "hello")
	s := Skill{Folder: folder}
	h1, err := s.Hash()
	if err != nil {
		t.Fatal(err)
	}
	h2, err := s.Hash()
	if err != nil {
		t.Fatal(err)
	}
	if h1 != h2 || h1 == "" {
		t.Fatalf("non-deterministic hash: %q vs %q", h1, h2)
	}
}
