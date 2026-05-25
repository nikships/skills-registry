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

func TestScoreAndSort(t *testing.T) {
	summaries := []registry.Summary{
		{Slug: "git_tool", Name: "Git Helper", Description: "Git helper commands"},
		{Slug: "js_lint", Name: "JS Linter", Description: "Ruff for JS"},
		{Slug: "py_format", Name: "Python Formatter", Description: "Beautiful python formatting"},
	}

	// Empty query returns all summaries unchanged.
	resEmpty := scoreAndSort(summaries, "")
	if len(resEmpty) != 3 {
		t.Fatalf("expected 3 results, got %d", len(resEmpty))
	}

	// Relevant query
	resGit := scoreAndSort(summaries, "git")
	if len(resGit) != 1 {
		t.Fatalf("expected 1 git match, got %d", len(resGit))
	}
	if resGit[0].Slug != "git_tool" {
		t.Fatalf("expected git_tool, got %s", resGit[0].Slug)
	}
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
