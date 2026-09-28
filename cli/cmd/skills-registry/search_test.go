package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/nikships/skills-registry/cli/internal/jsonout"
	"github.com/nikships/skills-registry/cli/internal/registry"
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

// TestScoreAndSortCrossLanguageCorpus is the shared scorer contract.
// Swift testCrossLanguageCorpus runs these cases verbatim: same names,
// inputs, and expected scores. Both scorers normalize to NFC before
// matching, so a precomposed accent and a combining mark score the same.
func TestScoreAndSortCrossLanguageCorpus(t *testing.T) {
	// Exact scores pin each bonus and the gap penalty. Changing a constant
	// without updating both suites fails here.
	scoreCases := []struct {
		name  string
		query string
		text  string
		want  int
	}{
		{"boundary-word-start", "git", "git tools", 69},
		{"buried-midword", "git", "legitimate", 61},
		{"camel-bonus", "ab", "aB", 53},
		{"camel-absent", "ab", "ab", 47},
		{"consecutive-run", "bc", "abc", 39},
		{"consecutive-broken", "bc", "abxc", 32},
		{"exact-case", "Git", "Git Tools", 69},
		{"folded-case", "Git", "git tools", 68},
		{"gap-one", "git", "gXit", 62},
		{"gap-two", "git", "gXXit", 60},
		// Thirty gaps between a and b drive the penalty below zero, which
		// both scorers clamp to a non-match.
		{"gap-floor", "ab", "a" + strings.Repeat("x", 30) + "b", 0},
	}
	for _, tc := range scoreCases {
		t.Run(tc.name, func(t *testing.T) {
			if got := fuzzyScore(tc.query, tc.text); got != tc.want {
				t.Fatalf("fuzzyScore(%q, %q) = %d, want %d", tc.query, tc.text, got, tc.want)
			}
		})
	}

	// U+00E9 is the precomposed é. U+0301 is the combining acute.
	const (
		nfdCafe = "cafe\u0301"
		nfcCafe = "caf\u00e9"
		nfcText = "Caf\u00e9 Tools"
		nfdText = "Cafe\u0301 Tools"
	)
	t.Run("nfc-equals-nfd", func(t *testing.T) {
		const want = 90
		for _, pair := range [][2]string{
			{nfdCafe, nfcText},
			{nfcCafe, nfcText},
			{nfcCafe, nfdText},
			{nfdCafe, nfdText},
		} {
			if got := fuzzyScore(pair[0], pair[1]); got != want {
				t.Fatalf("fuzzyScore(%q, %q) = %d, want %d", pair[0], pair[1], got, want)
			}
		}
		summaries := []registry.Summary{
			{Slug: "cafe", Name: "Caf\u00e9 Helper", Description: "drinks"},
			{Slug: "other", Name: "Other", Description: "unrelated"},
		}
		for _, q := range []string{nfdCafe, nfcCafe} {
			got := slugsOf(scoreAndSort(summaries, q))
			if !slicesEqual(got, []string{"cafe"}) {
				t.Fatalf("query %q: want [cafe], got %v", q, got)
			}
		}
	})

	t.Run("name-outranks-description", func(t *testing.T) {
		// Input order is the reverse of the expected rank, so a scorer
		// that forgets field weights cannot pass by preserving input order.
		summaries := []registry.Summary{
			{Slug: "desc_hit", Name: "unrelated", Description: "git"},
			{Slug: "name_hit", Name: "git", Description: "unrelated"},
		}
		got := slugsOf(scoreAndSort(summaries, "git"))
		want := []string{"name_hit", "desc_hit"}
		if !slicesEqual(got, want) {
			t.Fatalf("query=git: want %v, got %v", want, got)
		}
	})

	t.Run("slug-tiebreak", func(t *testing.T) {
		summaries := []registry.Summary{
			{Slug: "zeta", Name: "Tool", Description: "x"},
			{Slug: "alpha", Name: "Tool", Description: "x"},
		}
		got := slugsOf(scoreAndSort(summaries, "tool"))
		want := []string{"alpha", "zeta"}
		if !slicesEqual(got, want) {
			t.Fatalf("query=tool: want %v, got %v", want, got)
		}
	})

	t.Run("top-10-cutoff", func(t *testing.T) {
		// Inserted high slug first. Equal scores sort by slug, then the
		// eleventh result (s11) is dropped.
		summaries := make([]registry.Summary, 0, 11)
		for i := 11; i >= 1; i-- {
			summaries = append(summaries, registry.Summary{
				Slug:        fmt.Sprintf("s%02d", i),
				Name:        "Match",
				Description: "x",
			})
		}
		got := slugsOf(scoreAndSort(summaries, "match"))
		want := []string{"s01", "s02", "s03", "s04", "s05", "s06", "s07", "s08", "s09", "s10"}
		if !slicesEqual(got, want) {
			t.Fatalf("top-10: want %v, got %v", want, got)
		}
	})

	t.Run("empty-query", func(t *testing.T) {
		summaries := []registry.Summary{
			{Slug: "alpha", Name: "Alpha", Description: "x"},
		}
		for _, q := range []string{"", "   ", " \t\n"} {
			if got := scoreAndSort(summaries, q); len(got) != 0 {
				t.Fatalf("query %q should return no results, got %d", q, len(got))
			}
		}
	})

	t.Run("sample-registry", func(t *testing.T) {
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
	})
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
