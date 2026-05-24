package registry

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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
import fcntl, json, os, sys
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
            f.flush()
            os.fsync(f.fileno())
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

func TestExistsReturnsTrueOn200(t *testing.T) {
	bin, _ := stubGH(t, []map[string]any{
		{"key": "GET repos/x/y", "body": map[string]any{"name": "y", "full_name": "x/y"}},
	})
	c := &Client{GH: bin, Repo: "x/y", DefaultBranch: "main"}
	ok, err := c.Exists(context.Background())
	if err != nil {
		t.Fatalf("Exists: %v", err)
	}
	if !ok {
		t.Fatalf("expected Exists=true")
	}
}

func TestExistsReturnsFalseOn404(t *testing.T) {
	bin, _ := stubGH(t, []map[string]any{
		{"key": "GET repos/x/y", "body": "HTTP 404: Not Found", "exit": 1},
	})
	c := &Client{GH: bin, Repo: "x/y", DefaultBranch: "main"}
	ok, err := c.Exists(context.Background())
	if err != nil {
		t.Fatalf("Exists should swallow 404; got err=%v", err)
	}
	if ok {
		t.Fatalf("expected Exists=false on 404")
	}
}

func TestExistsPropagatesOtherErrors(t *testing.T) {
	bin, _ := stubGH(t, []map[string]any{
		{"key": "GET repos/x/y", "body": "HTTP 500: Internal Server Error", "exit": 1},
	})
	c := &Client{GH: bin, Repo: "x/y", DefaultBranch: "main"}
	ok, err := c.Exists(context.Background())
	if err == nil {
		t.Fatalf("expected error on non-404 failure")
	}
	if ok {
		t.Fatalf("expected Exists=false when an error is returned")
	}
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

// runGitInTest is a tiny helper for tests that exec git directly.
func runGitInTest(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, dir, err, out)
	}
}

// initBareRemote creates an empty bare repo and returns its absolute path
// (suitable for use as a `file://...` URL or directly as a remote).
func initBareRemote(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runGitInTest(t, dir, "init", "--bare", "-b", "main")
	return dir
}

// TestPushTreeViaGitNewRepo verifies the fresh-repo path: no main branch
// upstream yet, PushTreeViaGit must `git init` locally, commit, and push.
func TestPushTreeViaGitNewRepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	// gh stub: refExists 404 (no branch yet) + `gh auth setup-git` no-op +
	// `gh api user` returns identity.
	bin, _ := stubGH(t, []map[string]any{
		// setupGitAuth runs `gh auth setup-git ...`
		{"key": "auth setup-git", "body": ""},
		// gitAuthor runs `gh api -X GET user`
		{"key": "GET user", "body": map[string]any{
			"login": "tester",
			"name":  "Test User",
			"email": "tester@example.com",
		}},
		// refExists runs `gh api -X GET repos/x/y/git/ref/heads/main`
		{"key": "GET repos/x/y/git/ref/heads/main", "body": "HTTP 404: not found", "exit": 1},
	})

	remote := initBareRemote(t)
	var (
		mu       sync.Mutex
		progress [][2]int
	)
	c := &Client{
		GH:            bin,
		Repo:          "x/y",
		DefaultBranch: "main",
		HTTPSURL:      remote,
		OnProgress: func(done, total int) {
			mu.Lock()
			defer mu.Unlock()
			progress = append(progress, [2]int{done, total})
		},
	}

	files := map[string][]byte{
		"code-review/SKILL.md":           []byte("# Code Review"),
		"code-review/resources/extra.md": []byte("extra"),
		"qa/SKILL.md":                    []byte("# QA"),
	}
	if err := c.PushTreeViaGit(context.Background(), files, "init: import 2 skills"); err != nil {
		t.Fatalf("PushTreeViaGit: %v", err)
	}

	// Three progress events, total=3, monotonic.
	if len(progress) != 3 {
		t.Fatalf("expected 3 progress events, got %d: %v", len(progress), progress)
	}
	last := progress[len(progress)-1]
	if last[0] != 3 || last[1] != 3 {
		t.Fatalf("expected final progress (3,3), got %v", progress)
	}

	// Verify pushed contents by cloning the bare repo and inspecting.
	checkout := t.TempDir()
	runGitInTest(t, checkout, "clone", remote, "tree")
	for rel, want := range files {
		got, err := os.ReadFile(filepath.Join(checkout, "tree", rel))
		if err != nil {
			t.Fatalf("missing %s after push: %v", rel, err)
		}
		if string(got) != string(want) {
			t.Fatalf("%s: got %q, want %q", rel, got, want)
		}
	}
}

// TestPushTreeViaGitExistingRepo verifies the existing-branch path:
// PushTreeViaGit clones the bare remote, adds new files on top, and pushes.
func TestPushTreeViaGitExistingRepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	// Seed the bare remote with an initial commit (one file) so the branch
	// exists upstream.
	remote := initBareRemote(t)
	seed := t.TempDir()
	runGitInTest(t, seed, "clone", remote, "seed")
	seedRepo := filepath.Join(seed, "seed")
	runGitInTest(t, seedRepo, "config", "user.name", "seed")
	runGitInTest(t, seedRepo, "config", "user.email", "seed@example.com")
	if err := os.WriteFile(filepath.Join(seedRepo, "README.md"), []byte("seed"), 0o644); err != nil {
		t.Fatalf("write seed: %v", err)
	}
	runGitInTest(t, seedRepo, "add", "README.md")
	runGitInTest(t, seedRepo, "commit", "-m", "seed")
	runGitInTest(t, seedRepo, "push", "-u", "origin", "main")

	// gh stub: refExists returns a SHA (branch exists).
	bin, _ := stubGH(t, []map[string]any{
		{"key": "auth setup-git", "body": ""},
		{"key": "GET user", "body": map[string]any{
			"login": "tester", "name": "", "email": "",
		}},
		{"key": "GET repos/x/y/git/ref/heads/main", "body": map[string]any{
			"object": map[string]any{"sha": "deadbeef"},
		}},
	})
	c := &Client{
		GH:            bin,
		Repo:          "x/y",
		DefaultBranch: "main",
		HTTPSURL:      remote,
	}
	files := map[string][]byte{
		"new-skill/SKILL.md": []byte("# New Skill"),
	}
	if err := c.PushTreeViaGit(context.Background(), files, "publish: new-skill"); err != nil {
		t.Fatalf("PushTreeViaGit: %v", err)
	}

	// Verify both the seed file AND the new file land in the remote.
	checkout := t.TempDir()
	runGitInTest(t, checkout, "clone", remote, "tree")
	for _, rel := range []string{"README.md", "new-skill/SKILL.md"} {
		if _, err := os.ReadFile(filepath.Join(checkout, "tree", rel)); err != nil {
			t.Fatalf("missing %s after push: %v", rel, err)
		}
	}
}

// TestPushTreeViaGitRejectsTraversal makes sure the path validation matches
// the REST blob path and refuses ../-style payloads. Also exercises the
// absolute-path rejection added after CodeRabbit / factory-droid flagged that
// `strings.TrimPrefix(rel, "/")` only strips one leading slash, so
// `//etc/passwd` would survive normalization and trick filepath.Join into
// writing outside the tempdir.
func TestPushTreeViaGitRejectsTraversal(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	cases := []struct {
		name string
		path string
	}{
		{"parent-dir", "../escape.md"},
		{"nested-parent-dir", "skills/../../escape.md"},
		{"single-leading-slash", "/etc/passwd"},
		{"double-leading-slash", "//etc/passwd"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			bin, _ := stubGH(t, []map[string]any{
				{"key": "auth setup-git", "body": ""},
				{"key": "GET user", "body": map[string]any{"login": "tester"}},
				{"key": "GET repos/x/y/git/ref/heads/main", "body": "HTTP 404: not found", "exit": 1},
			})
			c := &Client{
				GH:            bin,
				Repo:          "x/y",
				DefaultBranch: "main",
				HTTPSURL:      initBareRemote(t),
			}
			err := c.PushTreeViaGit(context.Background(),
				map[string][]byte{tc.path: []byte("bad")}, "init")
			if err == nil {
				t.Fatalf("expected rejection for %q", tc.path)
			}
		})
	}
}

// TestPushTreeViaGitNoOpDoesNotEmitPushingStatus verifies that re-running
// PushTreeViaGit against a remote whose tree already matches the local payload
// returns nil without firing OnStatus("pushing to github…"). Without this,
// callers (bootstrap) would print "pushing to github…" even when nothing
// actually went over the wire.
func TestPushTreeViaGitNoOpDoesNotEmitPushingStatus(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	// Seed: bare remote with one file already committed under "noop/SKILL.md".
	remote := initBareRemote(t)
	seed := t.TempDir()
	runGitInTest(t, seed, "clone", remote, "seed")
	seedRepo := filepath.Join(seed, "seed")
	runGitInTest(t, seedRepo, "config", "user.name", "seed")
	runGitInTest(t, seedRepo, "config", "user.email", "seed@example.com")
	if err := os.MkdirAll(filepath.Join(seedRepo, "noop"), 0o755); err != nil {
		t.Fatalf("mkdir noop: %v", err)
	}
	body := []byte("identical")
	if err := os.WriteFile(filepath.Join(seedRepo, "noop", "SKILL.md"), body, 0o644); err != nil {
		t.Fatalf("write seed: %v", err)
	}
	runGitInTest(t, seedRepo, "add", "noop/SKILL.md")
	runGitInTest(t, seedRepo, "commit", "-m", "seed")
	runGitInTest(t, seedRepo, "push", "-u", "origin", "main")

	bin, _ := stubGH(t, []map[string]any{
		{"key": "auth setup-git", "body": ""},
		{"key": "GET user", "body": map[string]any{"login": "tester"}},
		{"key": "GET repos/x/y/git/ref/heads/main", "body": map[string]any{
			"object": map[string]any{"sha": "deadbeef"},
		}},
	})

	var (
		mu     sync.Mutex
		status []string
	)
	c := &Client{
		GH:            bin,
		Repo:          "x/y",
		DefaultBranch: "main",
		HTTPSURL:      remote,
		OnStatus: func(msg string) {
			mu.Lock()
			defer mu.Unlock()
			status = append(status, msg)
		},
	}
	// Push the SAME content again — `git status --porcelain` will be empty,
	// PushTreeViaGit must short-circuit before firing OnStatus.
	err := c.PushTreeViaGit(context.Background(),
		map[string][]byte{"noop/SKILL.md": body}, "noop")
	if err != nil {
		t.Fatalf("PushTreeViaGit: %v", err)
	}
	if len(status) != 0 {
		t.Fatalf("OnStatus fired %d time(s) on a no-op push: %v", len(status), status)
	}
}

// TestPushTreeViaGitEmitsPushingStatus is the counterpoint to the no-op test:
// a real push MUST fire OnStatus("pushing to github…") exactly once.
func TestPushTreeViaGitEmitsPushingStatus(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	bin, _ := stubGH(t, []map[string]any{
		{"key": "auth setup-git", "body": ""},
		{"key": "GET user", "body": map[string]any{"login": "tester"}},
		{"key": "GET repos/x/y/git/ref/heads/main", "body": "HTTP 404: not found", "exit": 1},
	})
	var (
		mu     sync.Mutex
		status []string
	)
	c := &Client{
		GH:            bin,
		Repo:          "x/y",
		DefaultBranch: "main",
		HTTPSURL:      initBareRemote(t),
		OnStatus: func(msg string) {
			mu.Lock()
			defer mu.Unlock()
			status = append(status, msg)
		},
	}
	if err := c.PushTreeViaGit(context.Background(),
		map[string][]byte{"a/SKILL.md": []byte("a")}, "init"); err != nil {
		t.Fatalf("PushTreeViaGit: %v", err)
	}
	if len(status) != 1 || !strings.Contains(status[0], "pushing") {
		t.Fatalf("expected exactly one 'pushing…' status, got: %v", status)
	}
}

// TestParseSummary_FoldedBlockScalarDescription verifies that the SKILL.md
// summarizer reads the common YAML folded-scalar (“>“) and literal (“|“)
// descriptions instead of storing the indicator character verbatim. The
// previous parser silently dropped the multi-line continuation lines and
// surfaced ">" / ">-" in the list TUI.
func TestParseSummary_FoldedBlockScalarDescription(t *testing.T) {
	text := "---\n" +
		"name: my-skill\n" +
		"description: >\n" +
		"  Build terminal UIs with Charmbracelet. Use when:\n" +
		"  Go TUI, shell prompts/spinners.\n" +
		"---\n# body"
	name, desc := parseSummary(text, "my_skill")
	if name != "my-skill" {
		t.Fatalf("name = %q, want my-skill", name)
	}
	want := "Build terminal UIs with Charmbracelet. Use when: Go TUI, shell prompts/spinners."
	if desc != want {
		t.Fatalf("desc = %q, want %q", desc, want)
	}
}

func TestParseSummary_FoldedStripBlockScalar(t *testing.T) {
	text := "---\ndescription: >-\n  Hello world.\n  Second line.\n---\n"
	_, desc := parseSummary(text, "x")
	if desc != "Hello world. Second line." {
		t.Fatalf("desc = %q", desc)
	}
}

func TestParseSummary_LiteralBlockScalar(t *testing.T) {
	text := "---\ndescription: |\n  line one\n  line two\n---\n"
	_, desc := parseSummary(text, "x")
	// _parseSummary collapses whitespace; literal scalars therefore end up
	// space-joined like folded ones for the listing.
	if desc != "line one line two" {
		t.Fatalf("desc = %q", desc)
	}
}

func TestParseSummary_InlineCommentAfterBlockMarker(t *testing.T) {
	// YAML allows a trailing comment on the indicator line itself
	// (``description: > # label``). The previous matcher required an exact
	// match against the marker set and so silently dropped the block.
	text := "---\ndescription: > # quick label\n  Real description here.\n  Spanning two lines.\n---\n"
	_, desc := parseSummary(text, "x")
	if desc != "Real description here. Spanning two lines." {
		t.Fatalf("desc = %q", desc)
	}
}

func TestParseSummary_RegressionMarkerNotStored(t *testing.T) {
	// The old parser stored ">" / ">-" verbatim as the description; this
	// regression test pins the new behavior.
	text := "---\ndescription: >\n  Real text here.\n---\n"
	_, desc := parseSummary(text, "x")
	if desc == ">" || desc == ">-" || desc == "" {
		t.Fatalf("desc unexpectedly = %q (regression)", desc)
	}
	if !strings.Contains(desc, "Real text here.") {
		t.Fatalf("desc lost content: %q", desc)
	}
}

// TestPushTreeViaGitGitMissing simulates a host without a usable git binary
// by pointing GitBin at a non-existent file. The gh stub is fully populated
// so execution reaches the first `git` subprocess; we then expect an error
// from the missing binary itself (not from upstream gh plumbing).
func TestPushTreeViaGitGitMissing(t *testing.T) {
	bin, _ := stubGH(t, []map[string]any{
		{"key": "auth setup-git", "body": ""},
		{"key": "GET user", "body": map[string]any{"login": "tester"}},
		{"key": "GET repos/x/y/git/ref/heads/main", "body": "HTTP 404: not found", "exit": 1},
	})
	c := &Client{
		GH:            bin,
		Repo:          "x/y",
		DefaultBranch: "main",
		GitBin:        "/nonexistent/git-binary",
		HTTPSURL:      initBareRemote(t),
	}
	err := c.PushTreeViaGit(context.Background(),
		map[string][]byte{"a/SKILL.md": []byte("a")}, "init")
	if err == nil {
		t.Fatalf("expected error when git binary doesn't exist")
	}
	// Sanity-check that we got past the gh plumbing (setupGitAuth / gitAuthor
	// / refExists) and tripped on the missing git binary itself.
	msg := err.Error()
	if !strings.Contains(msg, "git ") {
		t.Fatalf("expected error from git subprocess, got: %v", err)
	}
}

// TestDeleteRemovesSubtree exercises the happy path: ref → commit → tree
// lookup discovers two blobs under code-review/, the resulting POST tree
// payload carries null SHAs for both, and the new commit SHA is returned.
func TestDeleteRemovesSubtree(t *testing.T) {
	bin, _ := stubGH(t, []map[string]any{
		{"key": "GET repos/x/y/git/ref/heads/main", "body": map[string]any{"object": map[string]any{"sha": "parent"}}},
		{"key": "GET repos/x/y/git/commits/parent", "body": map[string]any{"tree": map[string]any{"sha": "base"}}},
		{
			"key": "GET repos/x/y/git/trees/base?recursive=1",
			"body": map[string]any{"tree": []any{
				map[string]any{"path": "code-review/SKILL.md", "type": "blob"},
				map[string]any{"path": "code-review/resources/extra.md", "type": "blob"},
				map[string]any{"path": "other-skill/SKILL.md", "type": "blob"},
			}},
		},
		{"key": "POST repos/x/y/git/trees", "body": map[string]any{"sha": "new-tree"}},
		{"key": "POST repos/x/y/git/commits", "body": map[string]any{"sha": "new-commit"}},
		{"key": "PATCH repos/x/y/git/refs/heads/main", "body": map[string]any{"object": map[string]any{"sha": "new-commit"}}},
	})
	c := &Client{GH: bin, Repo: "x/y", DefaultBranch: "main", MaxRetries: 3, RetryBaseS: 0}
	sha, err := c.Delete(context.Background(), "code-review")
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if sha != "new-commit" {
		t.Fatalf("Delete returned sha = %q, want new-commit", sha)
	}
}

// TestDeleteReturnsNotFoundOnMissingSlug verifies the contract that
// callers (notably `skill-registry remove`) use to drive the exit-1
// "slug not found" path: when the recursive tree listing returns no
// blobs under <slug>/, Delete must return ErrSlugNotFound and skip the
// commit/ref-update calls entirely.
func TestDeleteReturnsNotFoundOnMissingSlug(t *testing.T) {
	bin, _ := stubGH(t, []map[string]any{
		{"key": "GET repos/x/y/git/ref/heads/main", "body": map[string]any{"object": map[string]any{"sha": "parent"}}},
		{"key": "GET repos/x/y/git/commits/parent", "body": map[string]any{"tree": map[string]any{"sha": "base"}}},
		{
			"key": "GET repos/x/y/git/trees/base?recursive=1",
			"body": map[string]any{"tree": []any{
				map[string]any{"path": "other-skill/SKILL.md", "type": "blob"},
			}},
		},
	})
	c := &Client{GH: bin, Repo: "x/y", DefaultBranch: "main", MaxRetries: 3, RetryBaseS: 0}
	_, err := c.Delete(context.Background(), "missing-slug")
	if err == nil {
		t.Fatal("expected ErrSlugNotFound, got nil")
	}
	if !errors.Is(err, ErrSlugNotFound) {
		t.Fatalf("expected ErrSlugNotFound, got %v", err)
	}
}

// TestDeleteRetriesOnConflict mirrors TestPublishRetriesOnConflict: a
// 422 from the ref update on the first attempt must trigger a retry
// against freshly-fetched HEAD on the second pass.
func TestDeleteRetriesOnConflict(t *testing.T) {
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
			{
				"key": "GET repos/x/y/git/trees/base?recursive=1",
				"body": map[string]any{"tree": []any{
					map[string]any{"path": "code-review/SKILL.md", "type": "blob"},
				}},
			},
			{"key": "POST repos/x/y/git/trees", "body": map[string]any{"sha": "new-tree"}},
			{"key": "POST repos/x/y/git/commits", "body": map[string]any{"sha": commitSHA}},
			{"key": "PATCH repos/x/y/git/refs/heads/main", "body": bodyValue, "exit": exit},
		}
	}
	entries := append(makeRound("c1", true), makeRound("c2", false)...)
	bin, _ := stubGH(t, entries)
	c := &Client{GH: bin, Repo: "x/y", DefaultBranch: "main", MaxRetries: 3, RetryBaseS: 0}
	start := time.Now()
	sha, err := c.Delete(context.Background(), "code-review")
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if sha != "c2" {
		t.Fatalf("Delete returned sha = %q after retry, want c2", sha)
	}
	if time.Since(start) > 5*time.Second {
		t.Fatalf("retry should be quick when RetryBaseS=0")
	}
}

// TestDeleteSurfacesUnexpectedErrors verifies that non-409/422 failures
// propagate immediately without retries. A 500 from the GET ref call
// has nothing to do with concurrent writers; retrying would just stall
// the user with a stuck progress indicator.
func TestDeleteSurfacesUnexpectedErrors(t *testing.T) {
	bin, _ := stubGH(t, []map[string]any{
		{"key": "GET repos/x/y/git/ref/heads/main", "body": "HTTP 500: server unavailable", "exit": 1},
	})
	c := &Client{GH: bin, Repo: "x/y", DefaultBranch: "main", MaxRetries: 3, RetryBaseS: 0}
	_, err := c.Delete(context.Background(), "code-review")
	if err == nil {
		t.Fatal("expected error on 500, got nil")
	}
	if errors.Is(err, ErrSlugNotFound) {
		t.Fatalf("500 misclassified as ErrSlugNotFound: %v", err)
	}
}

// TestDeleteEmitsSortedNullSHAEntries documents the deterministic-tree
// guarantee: the POST trees payload must list slug paths in lexical
// order so a SHA-of-the-request fingerprint stays stable across runs.
// We capture the JSON sent to `gh` for the POST trees call via a
// dedicated shim that mirrors stubGH but echoes the stdin into a file
// the test can inspect.
func TestDeleteEmitsSortedNullSHAEntries(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")
	capturePath := filepath.Join(dir, "trees-body.json")
	entries := []map[string]any{
		{"key": "GET repos/x/y/git/ref/heads/main", "body": map[string]any{"object": map[string]any{"sha": "parent"}}},
		{"key": "GET repos/x/y/git/commits/parent", "body": map[string]any{"tree": map[string]any{"sha": "base"}}},
		{
			"key": "GET repos/x/y/git/trees/base?recursive=1",
			"body": map[string]any{"tree": []any{
				map[string]any{"path": "demo/zzz.md", "type": "blob"},
				map[string]any{"path": "demo/SKILL.md", "type": "blob"},
				map[string]any{"path": "demo/abc.md", "type": "blob"},
			}},
		},
		{"key": "POST repos/x/y/git/trees", "body": map[string]any{"sha": "new-tree"}, "capture": capturePath},
		{"key": "POST repos/x/y/git/commits", "body": map[string]any{"sha": "new-commit"}},
		{"key": "PATCH repos/x/y/git/refs/heads/main", "body": map[string]any{"object": map[string]any{"sha": "new-commit"}}},
	}
	raw, err := json.Marshal(entries)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(statePath, raw, 0o644); err != nil {
		t.Fatalf("write state: %v", err)
	}
	// Capture stdin to a temp file BEFORE invoking python via heredoc —
	// the heredoc itself consumes stdin, so the script can't read the
	// JSON body gh would have forwarded. We pass the captured path as
	// an extra argv element python re-reads when an entry has a
	// "capture" target.
	script := fmt.Sprintf(`#!/bin/sh
state=%q
stdin_file=$(mktemp)
cat > "$stdin_file"
python3 - "$state" "$stdin_file" "$@" <<'PY'
import fcntl, json, os, sys
state = sys.argv[1]
stdin_path = sys.argv[2]
argv = " ".join(sys.argv[3:])
with open(state, "r+") as f:
    fcntl.flock(f, fcntl.LOCK_EX)
    data = json.load(f)
    for i, entry in enumerate(data):
        if entry["key"] in argv:
            body = entry.get("body", "")
            capture = entry.get("capture")
            exit_code = entry.get("exit", 0)
            if capture:
                with open(stdin_path, "r") as src:
                    payload = src.read()
                with open(capture, "w") as cap:
                    cap.write(payload)
            data.pop(i)
            f.seek(0)
            f.truncate()
            json.dump(data, f)
            f.flush()
            os.fsync(f.fileno())
            fcntl.flock(f, fcntl.LOCK_UN)
            if body:
                sys.stdout.write(body if isinstance(body, str) else json.dumps(body))
            sys.exit(exit_code)
    fcntl.flock(f, fcntl.LOCK_UN)
sys.stderr.write(f"unexpected gh call: {argv}\n")
sys.exit(99)
PY
rm -f "$stdin_file"
`, statePath)
	bin := filepath.Join(dir, "gh")
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}
	c := &Client{GH: bin, Repo: "x/y", DefaultBranch: "main", MaxRetries: 3, RetryBaseS: 0}
	if _, err := c.Delete(context.Background(), "demo"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	body, err := os.ReadFile(capturePath)
	if err != nil {
		t.Fatalf("capture not written: %v", err)
	}
	var payload struct {
		BaseTree string `json:"base_tree"`
		Tree     []struct {
			Path string  `json:"path"`
			SHA  *string `json:"sha"`
		} `json:"tree"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("captured body is not JSON: %v\nbody=%s", err, body)
	}
	if payload.BaseTree != "base" {
		t.Fatalf("base_tree = %q, want base", payload.BaseTree)
	}
	wantPaths := []string{"demo/SKILL.md", "demo/abc.md", "demo/zzz.md"}
	if len(payload.Tree) != len(wantPaths) {
		t.Fatalf("tree entries = %d, want %d (%+v)", len(payload.Tree), len(wantPaths), payload.Tree)
	}
	for i, want := range wantPaths {
		if payload.Tree[i].Path != want {
			t.Fatalf("tree[%d].path = %q, want %q", i, payload.Tree[i].Path, want)
		}
		if payload.Tree[i].SHA != nil {
			t.Fatalf("tree[%d].sha = %v, want nil (deletion)", i, *payload.Tree[i].SHA)
		}
	}
}
