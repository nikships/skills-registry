package registry

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// stubGHCapture is stubGH with per-entry "capture" support: when an entry
// carries a "capture" path, the JSON body piped to gh's stdin is also
// written to that file so tests can assert on request payloads (ref names,
// base_tree/parents shape). Returns the stub binary path.
func stubGHCapture(t *testing.T, entries []map[string]any) string {
	t.Helper()
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")
	raw, err := json.Marshal(entries)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(statePath, raw, 0o644); err != nil {
		t.Fatalf("write state: %v", err)
	}
	// Capture stdin to a temp file BEFORE invoking python via heredoc —
	// the heredoc itself consumes stdin, so the script can't read the
	// JSON body gh would have forwarded. The captured path travels as an
	// extra argv element python re-reads when an entry has a "capture"
	// target. Mirrors the shim in
	// TestDeleteEmitsSortedNullSHAEntries.
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
py_exit=$?
rm -f "$stdin_file"
exit $py_exit
`, statePath)
	bin := filepath.Join(dir, "gh")
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}
	return bin
}

// TestPublishOnEmptyRepoCreatesRef verifies the first publish to a freshly
// created registry (no HEAD yet) takes the initial-commit path — tree with
// no base, commit with no parents, ref created via POST — instead of
// surfacing the raw 404/409 from the ref read. GitHub answers the missing
// ref with either status depending on the endpoint state, so both are
// pinned here.
func TestPublishOnEmptyRepoCreatesRef(t *testing.T) {
	for _, status := range []string{"HTTP 404: Not Found", "HTTP 409: Git Repository is Empty"} {
		t.Run(status, func(t *testing.T) {
			bin, _ := stubGH(t, []map[string]any{
				{"key": "GET repos/x/y/git/ref/heads/main", "body": status, "exit": 1},
				{"key": "POST repos/x/y/git/blobs", "body": map[string]any{"sha": "blob1"}},
				{"key": "POST repos/x/y/git/trees", "body": map[string]any{"sha": "tree1"}},
				{"key": "POST repos/x/y/git/commits", "body": map[string]any{"sha": "commit1"}},
				// POST (create) — not PATCH (fast-forward) — proves the
				// empty-repo path ran; the two keys can't cross-match.
				{"key": "POST repos/x/y/git/refs", "body": map[string]any{"sha": "commit1"}},
			})
			c := &Client{GH: bin, Repo: "x/y", DefaultBranch: "main", MaxRetries: 3, RetryBaseS: 0}
			sha, err := c.Publish(context.Background(), "demo", map[string][]byte{"SKILL.md": []byte("# Demo")}, "")
			if err != nil {
				t.Fatalf("Publish on empty repo: %v", err)
			}
			if sha != "commit1" {
				t.Fatalf("sha = %q, want commit1", sha)
			}
		})
	}
}

// TestPublishOnEmptyRepoRespectsBranch pins the initial-commit payload
// shape on a non-default branch: no base_tree, empty parents, and the ref
// created under refs/heads/<configured branch>.
func TestPublishOnEmptyRepoRespectsBranch(t *testing.T) {
	dir := t.TempDir()
	treesBody := filepath.Join(dir, "trees.json")
	commitsBody := filepath.Join(dir, "commits.json")
	refsBody := filepath.Join(dir, "refs.json")
	bin := stubGHCapture(t, []map[string]any{
		{"key": "GET repos/x/y/git/ref/heads/develop", "body": "HTTP 404: Not Found", "exit": 1},
		{"key": "POST repos/x/y/git/blobs", "body": map[string]any{"sha": "blob1"}},
		{"key": "POST repos/x/y/git/trees", "body": map[string]any{"sha": "tree1"}, "capture": treesBody},
		{"key": "POST repos/x/y/git/commits", "body": map[string]any{"sha": "commit1"}, "capture": commitsBody},
		{"key": "POST repos/x/y/git/refs", "body": map[string]any{"sha": "commit1"}, "capture": refsBody},
	})
	c := &Client{GH: bin, Repo: "x/y", DefaultBranch: "develop", MaxRetries: 3, RetryBaseS: 0}
	sha, err := c.Publish(context.Background(), "demo", map[string][]byte{"SKILL.md": []byte("# Demo")}, "publish: demo")
	if err != nil {
		t.Fatalf("Publish on empty repo: %v", err)
	}
	if sha != "commit1" {
		t.Fatalf("sha = %q, want commit1", sha)
	}

	var trees struct {
		BaseTree *string `json:"base_tree"`
		Tree     []struct {
			Path string  `json:"path"`
			SHA  *string `json:"sha"`
		} `json:"tree"`
	}
	raw, err := os.ReadFile(treesBody)
	if err != nil {
		t.Fatalf("trees body not captured: %v", err)
	}
	if err := json.Unmarshal(raw, &trees); err != nil {
		t.Fatalf("trees body is not JSON: %v\nbody=%s", err, raw)
	}
	if trees.BaseTree != nil {
		t.Fatalf("base_tree = %q, want absent on the initial commit", *trees.BaseTree)
	}
	if len(trees.Tree) != 1 || trees.Tree[0].Path != "demo/SKILL.md" || trees.Tree[0].SHA == nil {
		t.Fatalf("unexpected tree entries: %+v", trees.Tree)
	}

	var commits struct {
		Message string   `json:"message"`
		Parents []string `json:"parents"`
	}
	raw, err = os.ReadFile(commitsBody)
	if err != nil {
		t.Fatalf("commits body not captured: %v", err)
	}
	if err := json.Unmarshal(raw, &commits); err != nil {
		t.Fatalf("commits body is not JSON: %v\nbody=%s", err, raw)
	}
	if len(commits.Parents) != 0 {
		t.Fatalf("parents = %v, want empty on the initial commit", commits.Parents)
	}

	var refs struct {
		Ref string `json:"ref"`
		SHA string `json:"sha"`
	}
	raw, err = os.ReadFile(refsBody)
	if err != nil {
		t.Fatalf("refs body not captured: %v", err)
	}
	if err := json.Unmarshal(raw, &refs); err != nil {
		t.Fatalf("refs body is not JSON: %v\nbody=%s", err, raw)
	}
	if refs.Ref != "refs/heads/develop" {
		t.Fatalf("ref = %q, want refs/heads/develop", refs.Ref)
	}
	if refs.SHA != "commit1" {
		t.Fatalf("ref sha = %q, want commit1", refs.SHA)
	}
}

// TestDeleteOnEmptyRepoReturnsNotFound verifies deleting from a registry
// with no HEAD reports the slug as missing instead of the raw ref-read
// failure. Only the GET ref call is scripted, so any follow-up write
// would trip the stub's "unexpected gh call" guard and fail the test.
func TestDeleteOnEmptyRepoReturnsNotFound(t *testing.T) {
	for _, tc := range []struct{ name, body string }{
		{"not-found", "HTTP 404: Not Found"},
		{"empty-repo-conflict", "HTTP 409: Git Repository is Empty"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bin, _ := stubGH(t, []map[string]any{
				{"key": "GET repos/x/y/git/ref/heads/main", "body": tc.body, "exit": 1},
			})
			c := &Client{GH: bin, Repo: "x/y", DefaultBranch: "main", MaxRetries: 3, RetryBaseS: 0}
			_, err := c.Delete(context.Background(), "demo")
			if !errors.Is(err, ErrSlugNotFound) {
				t.Fatalf("Delete on empty repo: expected ErrSlugNotFound, got %v", err)
			}
		})
	}
}

// TestPublishOnEmptyRepoSurfacesUnexpectedErrors guards the other side:
// a 500 from the ref read is not an empty repo and must propagate
// instead of falling into the initial-commit path.
func TestPublishOnEmptyRepoSurfacesUnexpectedErrors(t *testing.T) {
	bin, _ := stubGH(t, []map[string]any{
		{"key": "GET repos/x/y/git/ref/heads/main", "body": "HTTP 500: server unavailable", "exit": 1},
	})
	c := &Client{GH: bin, Repo: "x/y", DefaultBranch: "main", MaxRetries: 3, RetryBaseS: 0}
	_, err := c.Publish(context.Background(), "demo", map[string][]byte{"SKILL.md": []byte("hi")}, "")
	if err == nil {
		t.Fatal("expected error on 500, got nil")
	}
}
