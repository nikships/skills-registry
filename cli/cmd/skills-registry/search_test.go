package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/anand-92/skills-registry/cli/internal/jsonout"
	"github.com/anand-92/skills-registry/cli/internal/registry"
)

func TestFuzzyScoreOrderMatters(t *testing.T) {
	// Out-of-order or missing query chars must produce a no-match.
	if got := fuzzyScore("abc", "xabcx"); got <= 0 {
		t.Fatalf("ordered subsequence should score, got %d", got)
	}
	if got := fuzzyScore("cba", "abc"); got != 0 {
		t.Fatalf("reversed query should not match, got %d", got)
	}
	if got := fuzzyScore("abz", "abc"); got != 0 {
		t.Fatalf("missing char should not match, got %d", got)
	}
	if got := fuzzyScore("abcdef", "abc"); got != 0 {
		t.Fatalf("query longer than text should not match, got %d", got)
	}
}

func TestFuzzyScoreRewardsTightAlignment(t *testing.T) {
	contiguous := fuzzyScore("git", "git_tools")
	scattered := fuzzyScore("git", "g_blah_i_blah_t")
	if !(contiguous > scattered && scattered > 0) {
		t.Fatalf("expected contiguous(%d) > scattered(%d) > 0", contiguous, scattered)
	}
}

func TestFuzzyScoreCaseBonusBreaksTie(t *testing.T) {
	exactCase := fuzzyScore("Git", "Git Tools")
	wrongCase := fuzzyScore("Git", "git tools")
	if !(exactCase > wrongCase && wrongCase > 0) {
		t.Fatalf("expected exactCase(%d) > wrongCase(%d) > 0", exactCase, wrongCase)
	}
}

func TestScoreAndSortEmptyQueryReturnsNil(t *testing.T) {
	summaries := []registry.Summary{
		{Slug: "alpha", Name: "Alpha", Description: "x"},
		{Slug: "beta", Name: "Beta", Description: "y"},
	}
	if got := scoreAndSort(summaries, ""); len(got) != 0 {
		t.Fatalf("empty query should return no results, got %d", len(got))
	}
	if got := scoreAndSort(summaries, "   "); len(got) != 0 {
		t.Fatalf("whitespace-only query should return no results, got %d", len(got))
	}
}

func TestScoreAndSortRanksByScoreAndSlug(t *testing.T) {
	summaries := []registry.Summary{
		{Slug: "git_tool", Name: "Git Helper", Description: "Git helper commands"},
		{Slug: "js_lint", Name: "JS Linter", Description: "Ruff for JS"},
		{Slug: "py_format", Name: "Python Formatter", Description: "Beautiful python formatting"},
	}
	got := scoreAndSort(summaries, "git")
	if len(got) != 1 {
		t.Fatalf("expected 1 git match, got %d", len(got))
	}
	if got[0].Slug != "git_tool" {
		t.Fatalf("expected git_tool, got %s", got[0].Slug)
	}
}

// TestScoreAndSortCrossLanguageCorpus mirrors
// “test_search_skills_cross_language_corpus“ in
// “infa-not-for-users/tests/test_github_api.py“ — same summaries,
// same queries, same expected ordering. The two scorers (CLI + MCP)
// must produce identical rankings or this test (and its Python twin)
// will diverge.
func TestScoreAndSortCrossLanguageCorpus(t *testing.T) {
	summaries := []registry.Summary{
		{Slug: "alpha_git", Name: "Alpha Git", Description: "Git helpers"},
		{Slug: "beta_python", Name: "Beta Python", Description: "Python tooling"},
		{Slug: "gamma_js", Name: "Gamma JS", Description: "JavaScript tooling"},
	}

	gitSlugs := slugsOf(scoreAndSort(summaries, "git"))
	if !slicesEqual(gitSlugs, []string{"alpha_git"}) {
		t.Fatalf("query=git: want [alpha_git], got %v", gitSlugs)
	}

	toolSlugs := slugsOf(scoreAndSort(summaries, "tool"))
	if !slicesEqual(toolSlugs, []string{"beta_python", "gamma_js"}) {
		t.Fatalf("query=tool: want [beta_python gamma_js], got %v", toolSlugs)
	}
}

func slugsOf(summaries []registry.Summary) []string {
	out := make([]string, 0, len(summaries))
	for _, s := range summaries {
		out = append(out, s.Slug)
	}
	return out
}

func slicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestSearchJSON(t *testing.T) {
	prev := jsonout.Enabled()
	t.Cleanup(func() { jsonout.SetEnabled(prev) })
	jsonout.SetEnabled(true)

	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	writeRegistryConfig(t, "x/y")

	fm := "---\nname: Git Helper\ndescription: Git helper commands\n---\nBody."
	enc := base64.StdEncoding.EncodeToString([]byte(fm))
	fm2 := "---\nname: Other Skill\ndescription: Second skill\n---\nBody."
	enc2 := base64.StdEncoding.EncodeToString([]byte(fm2))

	entries := []map[string]any{
		{
			"key": "GET repos/x/y/contents/",
			"body": []map[string]any{
				{"name": "git_tool", "type": "dir", "sha": "tree-git"},
				{"name": "other", "type": "dir", "sha": "tree-other"},
			},
		},
		{
			"key":  "GET repos/x/y/contents/git_tool/SKILL.md",
			"body": map[string]any{"encoding": "base64", "content": enc},
		},
		{
			"key":  "GET repos/x/y/contents/other/SKILL.md",
			"body": map[string]any{"encoding": "base64", "content": enc2},
		},
	}
	bin := stubGHForRemove(t, entries)
	installGHEnv(t, bin)

	buf := captureJSONOut(t)

	root := newRootCmd()
	root.SetArgs([]string{"search", "git", "--json"})

	var stderr bytes.Buffer
	root.SetErr(&stderr)

	ctx := context.Background()
	err := root.ExecuteContext(ctx)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}

	got := strings.TrimSpace(buf.String())
	var results []searchJSONRow
	if err := json.Unmarshal([]byte(got), &results); err != nil {
		t.Fatalf("invalid JSON output: %q (%v)", got, err)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Slug != "git_tool" {
		t.Errorf("expected slug 'git_tool', got %q", results[0].Slug)
	}
	if results[0].Name != "Git Helper" {
		t.Errorf("expected name 'Git Helper', got %q", results[0].Name)
	}
}
