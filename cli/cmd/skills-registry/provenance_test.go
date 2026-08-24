package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nikships/skills-registry/cli/internal/scan"
)

// readSkillMd returns the SKILL.md written into a skill folder.
func readSkillMd(t *testing.T, folder string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(folder, scan.MainFileName))
	if err != nil {
		t.Fatalf("read SKILL.md: %v", err)
	}
	return string(raw)
}

// stampedFolder materializes one skill folder under a fetch root, stamps it as
// an untrusted import, and returns the resulting SKILL.md.
func stampedFolder(t *testing.T, source, category, body string) string {
	t.Helper()
	root := t.TempDir()
	folder := filepath.Join(root, "summarize")
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(folder, scan.MainFileName), []byte(body), 0o644); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}
	skills := []scan.Skill{{Slug: "summarize", Folder: folder}}
	if err := stampProvenance(true, source, root, category, skills); err != nil {
		t.Fatalf("stampProvenance: %v", err)
	}
	return readSkillMd(t, folder)
}

// ────────────────────────────────────────────────────────────────────────────
// mergeFrontmatter
// ────────────────────────────────────────────────────────────────────────────

// TestMergeFrontmatterAppendsMissingKeys is the core case: both keys land
// inside the existing block, and every upstream line survives verbatim in its
// original order.
func TestMergeFrontmatterAppendsMissingKeys(t *testing.T) {
	text := "---\nname: summarize\ndescription: Summarize URLs and PDFs.\n---\n# Body\n\nUpstream text.\n"
	got, changed := mergeFrontmatter(text, []frontmatterKey{
		{key: provenanceCategoryKey, value: "AIGC"},
		{key: provenanceSourceKey, value: "https://github.com/o/r/tree/abc/skills/summarize"},
	})
	if !changed {
		t.Fatal("changed = false; both keys were missing")
	}
	want := "---\nname: summarize\ndescription: Summarize URLs and PDFs.\n" +
		"category: AIGC\nsource_url: https://github.com/o/r/tree/abc/skills/summarize\n" +
		"---\n# Body\n\nUpstream text.\n"
	if got != want {
		t.Fatalf("merged document:\n%q\nwant:\n%q", got, want)
	}
}

// TestMergeFrontmatterKeepsExistingValues is the "do not overwrite" rule: an
// upstream file that already declares either key keeps its own value.
func TestMergeFrontmatterKeepsExistingValues(t *testing.T) {
	text := "---\nname: summarize\ncategory: Upstream Category\nsource_url: https://upstream.example/skill\n---\nBody\n"
	got, changed := mergeFrontmatter(text, []frontmatterKey{
		{key: provenanceCategoryKey, value: "AIGC"},
		{key: provenanceSourceKey, value: "https://github.com/o/r/tree/abc/skills/summarize"},
	})
	if changed {
		t.Errorf("changed = true; both keys were already present:\n%s", got)
	}
	if got != text {
		t.Errorf("document changed:\n%q\nwant unchanged:\n%q", got, text)
	}
}

// TestMergeFrontmatterFillsEmptyValues is the other half of that rule: an
// empty value is not a value, so it gets filled.
func TestMergeFrontmatterFillsEmptyValues(t *testing.T) {
	cases := map[string]string{
		"bare":           "---\nname: x\ncategory:\n---\nBody\n",
		"empty quotes":   "---\nname: x\ncategory: \"\"\n---\nBody\n",
		"trailing space": "---\nname: x\ncategory:   \n---\nBody\n",
	}
	for name, text := range cases {
		t.Run(name, func(t *testing.T) {
			got, changed := mergeFrontmatter(text, []frontmatterKey{
				{key: provenanceCategoryKey, value: "AIGC"},
			})
			if !changed {
				t.Fatalf("changed = false for an empty category:\n%s", got)
			}
			if !strings.Contains(got, "category: AIGC") {
				t.Errorf("merged document should carry the filled category:\n%s", got)
			}
			if strings.Count(got, "category:") != 1 {
				t.Errorf("category must be replaced in place, not duplicated:\n%s", got)
			}
		})
	}
}

// TestMergeFrontmatterIgnoresIndentedKeys proves an indented `category:` inside
// a block scalar is text, not a top-level key, so the real key is still added.
func TestMergeFrontmatterIgnoresIndentedKeys(t *testing.T) {
	text := "---\nname: x\ndescription: |\n  category: not a key\n---\nBody\n"
	got, changed := mergeFrontmatter(text, []frontmatterKey{
		{key: provenanceCategoryKey, value: "AIGC"},
	})
	if !changed {
		t.Fatalf("changed = false; the indented line is a block scalar's text:\n%s", got)
	}
	if !strings.Contains(got, "  category: not a key") {
		t.Errorf("the block scalar's text was rewritten:\n%s", got)
	}
	if !strings.Contains(got, "\ncategory: AIGC\n") {
		t.Errorf("the top-level category was not added:\n%s", got)
	}
}

func TestMergeFrontmatterAddsBlockWhenAbsent(t *testing.T) {
	got, changed := mergeFrontmatter("# Just a body\n", []frontmatterKey{
		{key: provenanceSourceKey, value: "https://github.com/o/r/tree/abc/skills/x"},
	})
	if !changed {
		t.Fatal("changed = false for a document with no frontmatter")
	}
	want := "---\nsource_url: https://github.com/o/r/tree/abc/skills/x\n---\n# Just a body\n"
	if got != want {
		t.Fatalf("merged document:\n%q\nwant:\n%q", got, want)
	}
}

// TestMergeFrontmatterLeavesUnterminatedBlockAlone: an opening `---` with no
// closing one means the document's metadata has no known end, so guessing where
// to insert would risk rewriting the body.
func TestMergeFrontmatterLeavesUnterminatedBlockAlone(t *testing.T) {
	text := "---\nname: x\nno closing fence here\n"
	got, changed := mergeFrontmatter(text, []frontmatterKey{{key: provenanceCategoryKey, value: "AIGC"}})
	if changed || got != text {
		t.Fatalf("merged %q, want the document untouched", got)
	}
}

func TestMergeFrontmatterNoKeysIsNoOp(t *testing.T) {
	text := "---\nname: x\n---\nBody\n"
	if got, changed := mergeFrontmatter(text, nil); changed || got != text {
		t.Fatalf("merged %q (changed %v), want a no-op", got, changed)
	}
}

// TestYAMLScalarQuotesOnlyWhenNeeded pins the formatting convention: a URL and
// an ordinary category stay plain, and a value that would break the document
// (or smuggle a second line into it) is quoted.
func TestYAMLScalarQuotesOnlyWhenNeeded(t *testing.T) {
	plain := []string{
		"https://github.com/o/r/tree/abc/skills/x",
		"AIGC",
		"Developer Tools",
		"a:b",
	}
	for _, v := range plain {
		if got := yamlScalar(v); got != v {
			t.Errorf("yamlScalar(%q) = %q, want it left plain", v, got)
		}
	}
	quoted := []string{
		"",
		"AIGC\nname: hijacked",
		`has "quotes"`,
		"trailing: ",
		"  leading space",
		"# comment-looking",
		"- listish",
	}
	for _, v := range quoted {
		got := yamlScalar(v)
		if !strings.HasPrefix(got, `"`) {
			t.Errorf("yamlScalar(%q) = %q, want it quoted", v, got)
		}
		if strings.Contains(got[1:len(got)-1], "\n") {
			t.Errorf("yamlScalar(%q) = %q, a newline must not survive unescaped", v, got)
		}
	}
}

// ────────────────────────────────────────────────────────────────────────────
// source_url derivation
// ────────────────────────────────────────────────────────────────────────────

// TestSourceURLForNamesTheSkillFolder covers every source shape `add` accepts.
// The URL names the folder, so it ends in the skill's own directory — including
// the blob-to-SKILL.md case the public index links.
func TestSourceURLForNamesTheSkillFolder(t *testing.T) {
	const sha = "0123456789abcdef0123456789abcdef01234567"
	cases := []struct {
		name           string
		source         string
		relFolder      string
		want           string
		wantFolderBase string
	}{
		{
			name:      "blob URL naming the folder",
			source:    "https://github.com/openclaw/openclaw/blob/" + sha + "/skills/summarize",
			relFolder: "summarize",
			want:      "https://github.com/openclaw/openclaw/tree/" + sha + "/skills/summarize",
		},
		{
			name:      "blob URL naming SKILL.md itself",
			source:    "https://github.com/openclaw/openclaw/blob/" + sha + "/skills/summarize/SKILL.md",
			relFolder: "summarize",
			want:      "https://github.com/openclaw/openclaw/tree/" + sha + "/skills/summarize",
		},
		{
			name:      "tree URL with a branch ref",
			source:    "https://github.com/openclaw/openclaw/tree/main/skills/summarize",
			relFolder: "summarize",
			want:      "https://github.com/openclaw/openclaw/tree/main/skills/summarize",
		},
		{
			name:      "folder of skills gets a per-skill URL",
			source:    "https://github.com/openclaw/openclaw/tree/main/skills",
			relFolder: "skills/summarize",
			want:      "https://github.com/openclaw/openclaw/tree/main/skills/summarize",
		},
		{
			name:      "owner/repo shorthand pins HEAD",
			source:    "openclaw/openclaw",
			relFolder: "skills/summarize",
			want:      "https://github.com/openclaw/openclaw/tree/HEAD/skills/summarize",
		},
		{
			name:      "repository URL with no ref pins HEAD",
			source:    "https://github.com/openclaw/openclaw",
			relFolder: "skills/summarize",
			want:      "https://github.com/openclaw/openclaw/tree/HEAD/skills/summarize",
		},
		{
			name:      "non-github remote is recorded as given",
			source:    "https://gitlab.com/owner/repo.git",
			relFolder: "summarize",
			want:      "https://gitlab.com/owner/repo.git",
		},
		{
			name:      "credentials in a remote are redacted",
			source:    "https://user:token@gitlab.com/owner/repo.git",
			relFolder: "summarize",
			want:      "https://gitlab.com/owner/repo.git",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			folder := filepath.Join(root, filepath.FromSlash(tc.relFolder))
			got := sourceURLFor(tc.source, root, folder)
			if got != tc.want {
				t.Fatalf("sourceURLFor(%q, %q) = %q, want %q", tc.source, tc.relFolder, got, tc.want)
			}
		})
	}
}

// TestSourceURLEndsInTheSkillFolderName states the property explicitly, so a
// later reader does not mistake the trailing skill directory for a bug.
func TestSourceURLEndsInTheSkillFolderName(t *testing.T) {
	root := t.TempDir()
	got := sourceURLFor(
		"https://github.com/openclaw/openclaw/tree/main/skills/summarize",
		root, filepath.Join(root, "summarize"))
	if !strings.HasSuffix(got, "/summarize") {
		t.Fatalf("source_url = %q; it names the folder, so it must end in the skill directory", got)
	}
}

func TestBoundedCategoryCollapsesAndClips(t *testing.T) {
	if got := boundedCategory("  AIGC \n injected: line "); got != "AIGC injected: line" {
		t.Errorf("boundedCategory collapsed to %q", got)
	}
	long := strings.Repeat("x", maxCategoryLen*2)
	if got := boundedCategory(long); len([]rune(got)) != maxCategoryLen {
		t.Errorf("boundedCategory kept %d runes, want %d", len([]rune(got)), maxCategoryLen)
	}
	if got := boundedCategory("   "); got != "" {
		t.Errorf("boundedCategory(whitespace) = %q, want empty", got)
	}
}

// ────────────────────────────────────────────────────────────────────────────
// stampProvenance
// ────────────────────────────────────────────────────────────────────────────

// TestStampProvenanceWritesBothKeys is the acceptance case at the helper level.
func TestStampProvenanceWritesBothKeys(t *testing.T) {
	const sha = "0123456789abcdef0123456789abcdef01234567"
	got := stampedFolder(t,
		"https://github.com/openclaw/openclaw/blob/"+sha+"/skills/summarize",
		"AIGC", cleanSkillMd)
	for _, want := range []string{
		"category: AIGC",
		"source_url: https://github.com/openclaw/openclaw/tree/" + sha + "/skills/summarize",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("stamped SKILL.md missing %q:\n%s", want, got)
		}
	}
	// The upstream content is untouched.
	if !strings.Contains(got, "name: summarize") || !strings.Contains(got, "Summarize the input.") {
		t.Errorf("stamping altered the upstream skill:\n%s", got)
	}
}

// TestStampProvenanceOmitsAbsentCategory: an index miss must not invent one.
func TestStampProvenanceOmitsAbsentCategory(t *testing.T) {
	got := stampedFolder(t, "https://github.com/o/r/tree/main/skills/summarize", "", cleanSkillMd)
	if strings.Contains(got, "category:") {
		t.Errorf("an ungraded import invented a category:\n%s", got)
	}
	if !strings.Contains(got, "source_url: https://github.com/o/r/tree/main/skills/summarize") {
		t.Errorf("source_url is still required without a category:\n%s", got)
	}
}

// TestStampProvenanceIsNoOpForTrustedSource: the stamp is an import annotation,
// so a trusted source's bytes are published exactly as they are on disk.
func TestStampProvenanceIsNoOpForTrustedSource(t *testing.T) {
	root := writeSkillFolder(t, "summarize", cleanSkillMd)
	folder := filepath.Join(root, "summarize")
	if err := stampProvenance(false, "./skills", root, "AIGC", []scan.Skill{{Slug: "summarize", Folder: folder}}); err != nil {
		t.Fatalf("stampProvenance: %v", err)
	}
	if got := readSkillMd(t, folder); got != cleanSkillMd {
		t.Fatalf("a trusted source was stamped:\n%s", got)
	}
}

// TestStampProvenanceRoundTripsThroughScan is the parse round-trip: the extra
// keys must not disturb the name and description the registry listing shows.
func TestStampProvenanceRoundTripsThroughScan(t *testing.T) {
	const sha = "0123456789abcdef0123456789abcdef01234567"
	root := t.TempDir()
	folder := filepath.Join(root, "summarize")
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(folder, scan.MainFileName), []byte(cleanSkillMd), 0o644); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}
	source := "https://github.com/openclaw/openclaw/blob/" + sha + "/skills/summarize"
	if err := stampProvenance(true, source, root, "AIGC",
		[]scan.Skill{{Slug: "summarize", Folder: folder}}); err != nil {
		t.Fatalf("stampProvenance: %v", err)
	}
	skills, err := scan.Discover([]scan.Source{{Path: root, Label: "stamped"}})
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if len(skills) != 1 {
		t.Fatalf("discovered %d skills, want 1", len(skills))
	}
	if skills[0].Name != "summarize" {
		t.Errorf("name = %q, want summarize", skills[0].Name)
	}
	if skills[0].Description != "Summarizes a document." {
		t.Errorf("description = %q, want the upstream one", skills[0].Description)
	}
	if skills[0].Category != "AIGC" {
		t.Errorf("category = %q, want AIGC", skills[0].Category)
	}
	if skills[0].SourceURL != "https://github.com/openclaw/openclaw/tree/"+sha+"/skills/summarize" {
		t.Errorf("source_url = %q, want the folder URL", skills[0].SourceURL)
	}
}

// TestStampProvenancePreservesFileMode keeps the fetched file's permissions,
// because the stamp rewrites the file rather than editing it in place.
func TestStampProvenancePreservesFileMode(t *testing.T) {
	root := t.TempDir()
	folder := filepath.Join(root, "summarize")
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	main := filepath.Join(folder, scan.MainFileName)
	if err := os.WriteFile(main, []byte(cleanSkillMd), 0o600); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}
	if err := stampProvenance(true, "https://github.com/o/r/tree/main/skills/summarize", root, "",
		[]scan.Skill{{Slug: "summarize", Folder: folder}}); err != nil {
		t.Fatalf("stampProvenance: %v", err)
	}
	info, err := os.Stat(main)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("mode = %v, want the original 0600", got)
	}
}
