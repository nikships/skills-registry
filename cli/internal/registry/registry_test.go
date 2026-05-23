package registry

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// stubGH writes a small shell script that replays scripted JSON responses
// based on substring matches against argv. Each match is consumed in order.
//
// Tests load a JSON file in the form
//
//	[{"key": "GET repos/x/y/...", "body": <any>, "exit": 0}]
//
// where "body" can be a string (echoed verbatim) or any JSON value (re-encoded).
func stubGH(t *testing.T, entries []map[string]any) (string, string) {
	t.Helper()
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")
	raw, err := json.Marshal(entries)
	if err != nil {
		t.Fatalf("marshal stub entries: %v", err)
	}
	if err := os.WriteFile(statePath, raw, 0o644); err != nil {
		t.Fatalf("write state: %v", err)
	}
	script := fmt.Sprintf(`#!/bin/sh
state=%q
python3 - <<'PY' "$state" "$@"
import fcntl, json, sys
state = sys.argv[1]
argv = " ".join(sys.argv[2:])
with open(state, "r+") as f:
    fcntl.flock(f, fcntl.LOCK_EX)
    data = json.load(f)
    for i, entry in enumerate(data):
        if entry["key"] in argv:
            body = entry.get("body", "")
            exit_code = entry.get("exit", 0)
            data.pop(i)
            f.seek(0)
            f.truncate()
            json.dump(data, f)
            fcntl.flock(f, fcntl.LOCK_UN)
            if body:
                sys.stdout.write(body if isinstance(body, str) else json.dumps(body))
            sys.exit(exit_code)
    fcntl.flock(f, fcntl.LOCK_UN)
sys.stderr.write(f"unexpected gh call: {argv}\n")
sys.exit(99)
PY
`, statePath)
	binary := filepath.Join(dir, "gh")
	if err := os.WriteFile(binary, []byte(script), 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}
	return binary, statePath
}

func TestListReturnsSummaries(t *testing.T) {
	frontmatter := "---\nname: Code Review\ndescription: review code\n---\nBody.\n"
	encoded := base64.StdEncoding.EncodeToString([]byte(frontmatter))
	bin, _ := stubGH(t, []map[string]any{
		{
			"key": "GET repos/x/y/contents/",
			"body": []map[string]any{
				{"name": "code-review", "type": "dir", "sha": "tree-1"},
				{"name": "readme.md", "type": "file"},
				{"name": ".github", "type": "dir", "sha": "ignore"},
			},
		},
		{
			"key":  "GET repos/x/y/contents/code-review/SKILL.md",
			"body": map[string]any{"encoding": "base64", "content": encoded},
		},
	})
	c := &Client{GH: bin, Repo: "x/y", DefaultBranch: "main"}
	summaries, err := c.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(summaries) != 1 || summaries[0].Slug != "code-review" {
		t.Fatalf("unexpected summaries: %+v", summaries)
	}
	if summaries[0].Name != "Code Review" {
		t.Fatalf("expected name parsed from frontmatter; got %q", summaries[0].Name)
	}
}

func TestPublishRetriesOnConflict(t *testing.T) {
	makeRound := func(commitSHA string, conflict bool) []map[string]any {
		patchBody := map[string]any{"object": map[string]any{"sha": commitSHA}}
		exit := 0
		var bodyValue any = patchBody
		if conflict {
			bodyValue = "HTTP 422: non-fast-forward"
			exit = 1
		}
		return []map[string]any{
			{"key": "GET repos/x/y/git/ref/heads/main", "body": map[string]any{"object": map[string]any{"sha": "parent"}}},
			{"key": "GET repos/x/y/git/commits/parent", "body": map[string]any{"tree": map[string]any{"sha": "base"}}},
			{"key": "GET repos/x/y/git/trees/base?recursive=1", "body": map[string]any{"tree": []any{}}},
			{"key": "POST repos/x/y/git/blobs", "body": map[string]any{"sha": "blob"}},
			{"key": "POST repos/x/y/git/trees", "body": map[string]any{"sha": "tree"}},
			{"key": "POST repos/x/y/git/commits", "body": map[string]any{"sha": commitSHA}},
			{"key": "PATCH repos/x/y/git/refs/heads/main", "body": bodyValue, "exit": exit},
		}
	}
	entries := append(makeRound("c1", true), makeRound("c2", false)...)
	bin, _ := stubGH(t, entries)
	c := &Client{GH: bin, Repo: "x/y", DefaultBranch: "main", MaxRetries: 3, RetryBaseS: 0}
	start := time.Now()
	sha, err := c.Publish(context.Background(), "code-review", map[string][]byte{"SKILL.md": []byte("hi")}, "")
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if sha != "c2" {
		t.Fatalf("expected c2, got %q", sha)
	}
	if time.Since(start) > 5*time.Second {
		t.Fatalf("retry should be quick when RetryBaseS=0")
	}
}

func TestGetDownloadsRecursively(t *testing.T) {
	mdContent := base64.StdEncoding.EncodeToString([]byte("# SKILL"))
	extraContent := base64.StdEncoding.EncodeToString([]byte("data"))
	bin, _ := stubGH(t, []map[string]any{
		{
			"key": "GET repos/x/y/contents/code-review",
			"body": []map[string]any{
				{"name": "SKILL.md", "type": "file"},
				{"name": "resources", "type": "dir"},
			},
		},
		{
			"key":  "GET repos/x/y/contents/code-review/SKILL.md",
			"body": map[string]any{"encoding": "base64", "content": mdContent},
		},
		{
			"key": "GET repos/x/y/contents/code-review/resources",
			"body": []map[string]any{
				{"name": "extra.md", "type": "file"},
			},
		},
		{
			"key":  "GET repos/x/y/contents/code-review/resources/extra.md",
			"body": map[string]any{"encoding": "base64", "content": extraContent},
		},
	})
	c := &Client{GH: bin, Repo: "x/y", DefaultBranch: "main"}
	dest := t.TempDir()
	if err := c.Get(context.Background(), "code-review", dest); err != nil {
		t.Fatalf("Get: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dest, "SKILL.md"))
	if err != nil || string(got) != "# SKILL" {
		t.Fatalf("SKILL.md missing or wrong content: %q %v", got, err)
	}
	extra, err := os.ReadFile(filepath.Join(dest, "resources", "extra.md"))
	if err != nil || string(extra) != "data" {
		t.Fatalf("resources/extra.md missing: %q %v", extra, err)
	}
}

// TestPushTreeReportsProgress verifies the OnProgress callback fires once per
// uploaded file with monotonically increasing `done` counts, and that the
// parallel blob path (Workers > 1) completes correctly.
func TestPushTreeReportsProgress(t *testing.T) {
	entries := []map[string]any{
		{"key": "GET repos/x/y/git/ref/heads/main", "body": map[string]any{"object": map[string]any{"sha": "parent"}}},
		{"key": "GET repos/x/y/git/commits/parent", "body": map[string]any{"tree": map[string]any{"sha": "base"}}},
		{"key": "POST repos/x/y/git/blobs", "body": map[string]any{"sha": "b1"}},
		{"key": "POST repos/x/y/git/blobs", "body": map[string]any{"sha": "b2"}},
		{"key": "POST repos/x/y/git/blobs", "body": map[string]any{"sha": "b3"}},
		{"key": "POST repos/x/y/git/trees", "body": map[string]any{"sha": "tree"}},
		{"key": "POST repos/x/y/git/commits", "body": map[string]any{"sha": "commit"}},
		{"key": "PATCH repos/x/y/git/refs/heads/main", "body": map[string]any{"object": map[string]any{"sha": "commit"}}},
	}
	bin, _ := stubGH(t, entries)
	var (
		mu       sync.Mutex
		progress [][2]int
	)
	c := &Client{
		GH: bin, Repo: "x/y", DefaultBranch: "main", Workers: 4,
		OnProgress: func(done, total int) {
			mu.Lock()
			defer mu.Unlock()
			progress = append(progress, [2]int{done, total})
		},
	}
	sha, err := c.PushTree(context.Background(),
		map[string][]byte{"a/SKILL.md": []byte("a"), "b/SKILL.md": []byte("b"), "c/SKILL.md": []byte("c")},
		"init")
	if err != nil {
		t.Fatalf("PushTree: %v", err)
	}
	if sha != "commit" {
		t.Fatalf("expected commit sha, got %q", sha)
	}
	if len(progress) != 3 {
		t.Fatalf("expected 3 progress events, got %d: %v", len(progress), progress)
	}
	for i, p := range progress {
		if p[1] != 3 {
			t.Fatalf("progress[%d].total = %d, want 3", i, p[1])
		}
	}
	last := progress[len(progress)-1]
	if last[0] != 3 || last[1] != 3 {
		t.Fatalf("expected final progress (3,3), got (%d,%d)", last[0], last[1])
	}
}

// TestBootstrapInitialCommitProgressTotal makes sure progress reporting from
// the PUT seed + recursive pushTree path stays consistent with the original
// total file count (not the post-PUT remainder).
func TestBootstrapInitialCommitProgressTotal(t *testing.T) {
	entries := []map[string]any{
		// First PushTree call: ref doesn't exist yet → fall through to bootstrapInitialCommit.
		{"key": "GET repos/x/y/git/ref/heads/main", "body": "HTTP 404: not found", "exit": 1},
		// PUT seeds the first file.
		{"key": "PUT repos/x/y/contents/a/SKILL.md", "body": map[string]any{"commit": map[string]any{"sha": "c1"}}},
		// Recursive pushTree fetches ref (now exists) + commit + base tree.
		{"key": "GET repos/x/y/git/ref/heads/main", "body": map[string]any{"object": map[string]any{"sha": "c1"}}},
		{"key": "GET repos/x/y/git/commits/c1", "body": map[string]any{"tree": map[string]any{"sha": "t1"}}},
		// Two remaining blobs.
		{"key": "POST repos/x/y/git/blobs", "body": map[string]any{"sha": "b2"}},
		{"key": "POST repos/x/y/git/blobs", "body": map[string]any{"sha": "b3"}},
		{"key": "POST repos/x/y/git/trees", "body": map[string]any{"sha": "tree"}},
		{"key": "POST repos/x/y/git/commits", "body": map[string]any{"sha": "final"}},
		{"key": "PATCH repos/x/y/git/refs/heads/main", "body": map[string]any{"object": map[string]any{"sha": "final"}}},
	}
	bin, _ := stubGH(t, entries)
	var (
		mu       sync.Mutex
		progress [][2]int
	)
	c := &Client{
		GH: bin, Repo: "x/y", DefaultBranch: "main", Workers: 2,
		OnProgress: func(done, total int) {
			mu.Lock()
			defer mu.Unlock()
			progress = append(progress, [2]int{done, total})
		},
	}
	sha, err := c.PushTree(context.Background(),
		map[string][]byte{"a/SKILL.md": []byte("a"), "b/SKILL.md": []byte("b"), "c/SKILL.md": []byte("c")},
		"init")
	if err != nil {
		t.Fatalf("PushTree: %v", err)
	}
	if sha != "final" {
		t.Fatalf("expected final sha, got %q", sha)
	}
	if len(progress) != 3 {
		t.Fatalf("expected 3 progress events (1 PUT + 2 blobs), got %d: %v", len(progress), progress)
	}
	// All events should report total=3 (NOT 2 — the recursive pushTree must
	// preserve the original total).
	for i, p := range progress {
		if p[1] != 3 {
			t.Fatalf("progress[%d].total = %d, want 3 (got events: %v)", i, p[1], progress)
		}
	}
	// Final event must be (3,3).
	last := progress[len(progress)-1]
	if last[0] != 3 || last[1] != 3 {
		t.Fatalf("expected final progress (3,3), got (%d,%d) — events: %v", last[0], last[1], progress)
	}
}
