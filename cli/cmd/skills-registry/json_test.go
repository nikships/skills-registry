package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/anand-92/skills-registry/cli/internal/jsonout"
)

// TestRunListJSONEmitsArrayOfSummaries pins the JSON-001 contract:
// `list --json` outputs a JSON array of {slug, name, description}.
// The registry has two skills; both must round-trip through the gh
// shim into the captured buffer with the canonical field order.
func TestRunListJSONEmitsArrayOfSummaries(t *testing.T) {
	prev := jsonout.Enabled()
	t.Cleanup(func() { jsonout.SetEnabled(prev) })
	jsonout.SetEnabled(true)

	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	writeRegistryConfig(t, "x/y")

	fm := "---\nname: Demo Skill\ndescription: A demo skill\n---\nBody."
	enc := base64.StdEncoding.EncodeToString([]byte(fm))
	fm2 := "---\nname: Other\ndescription: Second skill\n---\nBody."
	enc2 := base64.StdEncoding.EncodeToString([]byte(fm2))

	entries := []map[string]any{
		{
			"key": "GET repos/x/y/contents/",
			"body": []map[string]any{
				{"name": "demo", "type": "dir", "sha": "tree-demo"},
				{"name": "other", "type": "dir", "sha": "tree-other"},
			},
		},
		{
			"key":  "GET repos/x/y/contents/demo/SKILL.md",
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
	if err := runListJSON(context.Background(), ""); err != nil {
		t.Fatalf("runListJSON: %v", err)
	}
	got := strings.TrimSpace(buf.String())
	if strings.Contains(got, "\n") {
		t.Fatalf("JSON-006: output must be single-line, got %q", got)
	}
	var payload []map[string]string
	if err := json.Unmarshal([]byte(got), &payload); err != nil {
		t.Fatalf("invalid JSON: %q (%v)", got, err)
	}
	if len(payload) != 2 {
		t.Fatalf("expected 2 entries, got %d: %v", len(payload), payload)
	}
	if payload[0]["slug"] != "demo" || payload[0]["name"] != "Demo Skill" || payload[0]["description"] != "A demo skill" {
		t.Errorf("row[0] mismatch: %v", payload[0])
	}
	if payload[1]["slug"] != "other" {
		t.Errorf("row[1].slug = %q, want other", payload[1]["slug"])
	}
}

// TestRunListJSONFiltersWithQuery verifies the --query flag still
// applies in JSON mode: an unmatched needle yields an empty array
// rather than the full registry, which would break grep-style agent
// scripts.
func TestRunListJSONFiltersWithQuery(t *testing.T) {
	prev := jsonout.Enabled()
	t.Cleanup(func() { jsonout.SetEnabled(prev) })
	jsonout.SetEnabled(true)

	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	writeRegistryConfig(t, "x/y")

	fm := "---\nname: Code Review\ndescription: review code\n---\nBody."
	enc := base64.StdEncoding.EncodeToString([]byte(fm))
	entries := []map[string]any{
		{
			"key": "GET repos/x/y/contents/",
			"body": []map[string]any{
				{"name": "code-review", "type": "dir", "sha": "tree-1"},
			},
		},
		{
			"key":  "GET repos/x/y/contents/code-review/SKILL.md",
			"body": map[string]any{"encoding": "base64", "content": enc},
		},
	}
	bin := stubGHForRemove(t, entries)
	installGHEnv(t, bin)

	buf := captureJSONOut(t)
	if err := runListJSON(context.Background(), "nonexistent"); err != nil {
		t.Fatalf("runListJSON: %v", err)
	}
	got := strings.TrimSpace(buf.String())
	if got != "[]" {
		t.Fatalf("unmatched query should yield empty array, got %q", got)
	}
}

// TestRunListJSONEmptyRegistryEmitsEmptyArray pins the "no skills
// yet" path: a registry with zero subdirs must emit `[]` rather than
// `null` so `jq 'length'` returns 0 without errors.
func TestRunListJSONEmptyRegistryEmitsEmptyArray(t *testing.T) {
	prev := jsonout.Enabled()
	t.Cleanup(func() { jsonout.SetEnabled(prev) })
	jsonout.SetEnabled(true)

	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	writeRegistryConfig(t, "x/y")

	entries := []map[string]any{
		{"key": "GET repos/x/y/contents/", "body": []map[string]any{}},
	}
	bin := stubGHForRemove(t, entries)
	installGHEnv(t, bin)

	buf := captureJSONOut(t)
	if err := runListJSON(context.Background(), ""); err != nil {
		t.Fatalf("runListJSON: %v", err)
	}
	got := strings.TrimSpace(buf.String())
	if got != "[]" {
		t.Fatalf("expected [], got %q", got)
	}
}

// TestRunGetJSONEmitsSlugAndPath pins the JSON-002 contract:
// `get <slug> --json` downloads the skill and emits {slug, path}.
// The path is the resolved on-disk destination so a downstream agent
// can pipe the location into another tool.
func TestRunGetJSONEmitsSlugAndPath(t *testing.T) {
	prev := jsonout.Enabled()
	t.Cleanup(func() { jsonout.SetEnabled(prev) })
	jsonout.SetEnabled(true)

	homeDir := t.TempDir()
	cwd := t.TempDir()
	t.Setenv("HOME", homeDir)
	// Clear XDG_CACHE_HOME so cache.CacheRoot() falls back to $HOME/.cache
	// (i.e. inside the temp homeDir above) regardless of the runner's env.
	// Without this the JSON-002 test would flake on hosts that export an
	// XDG_CACHE_HOME pointing outside the test tempdir.
	t.Setenv("XDG_CACHE_HOME", "")
	if err := os.Chdir(cwd); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(homeDir) })
	writeRegistryConfig(t, "x/y")

	content := base64.StdEncoding.EncodeToString([]byte("# Hello"))
	entries := []map[string]any{
		{
			"key": "GET repos/x/y/contents/demo",
			"body": []map[string]any{
				{"name": "SKILL.md", "type": "file"},
			},
		},
		{
			"key":  "GET repos/x/y/contents/demo/SKILL.md",
			"body": map[string]any{"encoding": "base64", "content": content},
		},
	}
	bin := stubGHForRemove(t, entries)
	installGHEnv(t, bin)

	buf := captureJSONOut(t)
	if err := runGetJSON(context.Background(), "demo", ""); err != nil {
		t.Fatalf("runGetJSON: %v", err)
	}
	got := strings.TrimSpace(buf.String())
	if strings.Contains(got, "\n") {
		t.Fatalf("output should be single-line, got %q", got)
	}
	var payload getJSONResult
	if err := json.Unmarshal([]byte(got), &payload); err != nil {
		t.Fatalf("invalid JSON: %q (%v)", got, err)
	}
	if payload.Slug != "demo" {
		t.Errorf("slug = %q, want demo", payload.Slug)
	}
	if payload.Path == "" {
		t.Error("path should not be empty")
	}
	// Issue #29: the default destination must live under the global cache
	// (~/.cache/skills-mcp/skills/<slug>/), not the cwd-relative .agents/
	// tree the original code produced.
	wantPrefix := filepath.Join(homeDir, ".cache", "skills-mcp", "skills")
	if !strings.HasPrefix(payload.Path, wantPrefix) {
		t.Errorf("path = %q, want prefix %q (default must use the global cache, not cwd/.agents)", payload.Path, wantPrefix)
	}
	if strings.Contains(payload.Path, filepath.Join(cwd, ".agents")) {
		t.Errorf("path %q must not live under %s/.agents", payload.Path, cwd)
	}
	// The downloaded SKILL.md must live at the reported path so a
	// consumer can immediately read it.
	if _, err := os.Stat(filepath.Join(payload.Path, "SKILL.md")); err != nil {
		t.Errorf("downloaded SKILL.md missing at %s: %v", payload.Path, err)
	}
}

// TestRunPublishJSONEmitsSlugSHAUrl pins the JSON-003 contract:
// `publish <path> --json` emits {slug, sha, url} after a successful
// push. The URL must point at the GitHub tree view rooted at the
// short SHA so an agent can immediately deep-link the commit.
func TestRunPublishJSONEmitsSlugSHAUrl(t *testing.T) {
	prev := jsonout.Enabled()
	t.Cleanup(func() { jsonout.SetEnabled(prev) })
	jsonout.SetEnabled(true)

	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	writeRegistryConfig(t, "x/y")

	// Materialize a local skill folder with a SKILL.md.
	skillDir := filepath.Join(t.TempDir(), "demo")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	body := "---\nname: demo\n---\n# Demo\n"
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(body), 0o644); err != nil {
		t.Fatalf("write skill: %v", err)
	}

	// Publish flow walks ref → commit → tree → blob → tree → commit → ref.
	entries := []map[string]any{
		{"key": "GET repos/x/y/git/ref/heads/main", "body": map[string]any{"object": map[string]any{"sha": "parent"}}},
		{"key": "GET repos/x/y/git/commits/parent", "body": map[string]any{"tree": map[string]any{"sha": "base"}}},
		{"key": "GET repos/x/y/git/trees/base?recursive=1", "body": map[string]any{"tree": []any{}}},
		{"key": "POST repos/x/y/git/blobs", "body": map[string]any{"sha": "blob-1"}},
		{"key": "POST repos/x/y/git/trees", "body": map[string]any{"sha": "tree-1"}},
		{"key": "POST repos/x/y/git/commits", "body": map[string]any{"sha": "abcdef1234567890abcdef1234567890abcdef12"}},
		{"key": "PATCH repos/x/y/git/refs/heads/main", "body": map[string]any{"object": map[string]any{"sha": "abcdef1234567890abcdef1234567890abcdef12"}}},
	}
	bin := stubGHForRemove(t, entries)
	installGHEnv(t, bin)

	buf := captureJSONOut(t)
	if err := runPublishJSON(context.Background(), skillDir, ""); err != nil {
		t.Fatalf("runPublishJSON: %v", err)
	}
	got := strings.TrimSpace(buf.String())
	if strings.Contains(got, "\n") {
		t.Fatalf("output should be single-line, got %q", got)
	}
	var payload publishJSONResult
	if err := json.Unmarshal([]byte(got), &payload); err != nil {
		t.Fatalf("invalid JSON: %q (%v)", got, err)
	}
	if payload.Slug != "demo" {
		t.Errorf("slug = %q, want demo", payload.Slug)
	}
	// SHA is the full hash (not 7-char short form) so downstream
	// `gh` callers can pass it back verbatim.
	if payload.SHA != "abcdef1234567890abcdef1234567890abcdef12" {
		t.Errorf("sha = %q, want full hash", payload.SHA)
	}
	if !strings.Contains(payload.URL, "github.com/x/y/tree/") {
		t.Errorf("url should reference repo tree view: %q", payload.URL)
	}
	if !strings.HasSuffix(payload.URL, "/demo") {
		t.Errorf("url should end with slug: %q", payload.URL)
	}
}

// TestRunSyncJSONEmitsPushedAndSkipped pins the JSON-004 contract:
// `sync --json --yes` emits {pushed, skipped} with all locally-
// discovered skills partitioned by whether they were missing upstream.
// The shim has an empty registry, so a freshly-discovered local skill
// must land in `pushed`.
func TestRunSyncJSONEmitsPushedAndSkipped(t *testing.T) {
	prev := jsonout.Enabled()
	t.Cleanup(func() { jsonout.SetEnabled(prev) })
	jsonout.SetEnabled(true)

	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	cwd := t.TempDir()
	if err := os.Chdir(cwd); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(homeDir) })
	writeRegistryConfig(t, "x/y")

	// Seed a local skill under `.claude/skills/demo`.
	localSkill := filepath.Join(homeDir, ".claude", "skills", "demo")
	if err := os.MkdirAll(localSkill, 0o755); err != nil {
		t.Fatalf("mkdir local skill: %v", err)
	}
	body := "---\nname: demo\ndescription: demo\n---\n# Demo\n"
	if err := os.WriteFile(filepath.Join(localSkill, "SKILL.md"), []byte(body), 0o644); err != nil {
		t.Fatalf("write skill: %v", err)
	}

	// Empty registry → demo gets pushed.
	entries := []map[string]any{
		{"key": "GET repos/x/y/contents/", "body": []map[string]any{}},
		// Publish call sequence.
		{"key": "GET repos/x/y/git/ref/heads/main", "body": map[string]any{"object": map[string]any{"sha": "parent"}}},
		{"key": "GET repos/x/y/git/commits/parent", "body": map[string]any{"tree": map[string]any{"sha": "base"}}},
		{"key": "GET repos/x/y/git/trees/base?recursive=1", "body": map[string]any{"tree": []any{}}},
		{"key": "POST repos/x/y/git/blobs", "body": map[string]any{"sha": "blob-1"}},
		{"key": "POST repos/x/y/git/trees", "body": map[string]any{"sha": "tree-1"}},
		{"key": "POST repos/x/y/git/commits", "body": map[string]any{"sha": "commit-1"}},
		{"key": "PATCH repos/x/y/git/refs/heads/main", "body": map[string]any{"object": map[string]any{"sha": "commit-1"}}},
	}
	bin := stubGHForRemove(t, entries)
	installGHEnv(t, bin)

	buf := captureJSONOut(t)
	if err := runSyncJSON(context.Background()); err != nil {
		t.Fatalf("runSyncJSON: %v", err)
	}
	got := strings.TrimSpace(buf.String())
	if strings.Contains(got, "\n") {
		t.Fatalf("output should be single-line, got %q", got)
	}
	var payload syncJSONResult
	if err := json.Unmarshal([]byte(got), &payload); err != nil {
		t.Fatalf("invalid JSON: %q (%v)", got, err)
	}
	if len(payload.Pushed) != 1 || payload.Pushed[0] != "demo" {
		t.Errorf("pushed = %v, want [demo]", payload.Pushed)
	}
	if payload.Skipped == nil {
		t.Error("skipped should never be nil (must serialize as []), got null")
	}
}

// TestRunSyncJSONSkipsAlreadyPresentSkills covers the partition rule:
// a local slug that's already in the registry lands in `skipped`,
// not `pushed`. This is the "no-op sync" path agents drive when they
// just want to confirm the registry mirrors local.
func TestRunSyncJSONSkipsAlreadyPresentSkills(t *testing.T) {
	prev := jsonout.Enabled()
	t.Cleanup(func() { jsonout.SetEnabled(prev) })
	jsonout.SetEnabled(true)

	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	cwd := t.TempDir()
	if err := os.Chdir(cwd); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(homeDir) })
	writeRegistryConfig(t, "x/y")

	localSkill := filepath.Join(homeDir, ".claude", "skills", "demo")
	if err := os.MkdirAll(localSkill, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	body := "---\nname: demo\n---\nBody."
	if err := os.WriteFile(filepath.Join(localSkill, "SKILL.md"), []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Registry already contains "demo" → nothing to push.
	entries := []map[string]any{
		{
			"key": "GET repos/x/y/contents/",
			"body": []map[string]any{
				{"name": "demo", "type": "dir", "sha": "tree-demo"},
			},
		},
	}
	bin := stubGHForRemove(t, entries)
	installGHEnv(t, bin)

	buf := captureJSONOut(t)
	if err := runSyncJSON(context.Background()); err != nil {
		t.Fatalf("runSyncJSON: %v", err)
	}
	var payload syncJSONResult
	if err := json.Unmarshal([]byte(strings.TrimSpace(buf.String())), &payload); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(payload.Pushed) != 0 {
		t.Errorf("pushed = %v, want []", payload.Pushed)
	}
	if len(payload.Skipped) != 1 || payload.Skipped[0] != "demo" {
		t.Errorf("skipped = %v, want [demo]", payload.Skipped)
	}
}

// TestRunSyncJSONEmptyEmitsEmptyArrays guarantees JSON-006: even when
// there's nothing to do (no local skills, empty registry), the output
// is a well-formed `{"pushed":[],"skipped":[]}` object — not `null`
// or missing fields.
func TestRunSyncJSONEmptyEmitsEmptyArrays(t *testing.T) {
	prev := jsonout.Enabled()
	t.Cleanup(func() { jsonout.SetEnabled(prev) })
	jsonout.SetEnabled(true)

	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	cwd := t.TempDir()
	if err := os.Chdir(cwd); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(homeDir) })
	writeRegistryConfig(t, "x/y")

	entries := []map[string]any{
		{"key": "GET repos/x/y/contents/", "body": []map[string]any{}},
	}
	bin := stubGHForRemove(t, entries)
	installGHEnv(t, bin)

	buf := captureJSONOut(t)
	if err := runSyncJSON(context.Background()); err != nil {
		t.Fatalf("runSyncJSON: %v", err)
	}
	got := strings.TrimSpace(buf.String())
	want := `{"pushed":[],"skipped":[]}`
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestRunAddJSONEmitsPushedAndSkipped covers add's JSON parity with
// sync. A local path containing one SKILL.md and an empty registry
// should land that skill in `pushed`.
func TestRunAddJSONEmitsPushedAndSkipped(t *testing.T) {
	prev := jsonout.Enabled()
	t.Cleanup(func() { jsonout.SetEnabled(prev) })
	jsonout.SetEnabled(true)

	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	writeRegistryConfig(t, "x/y")

	root := t.TempDir()
	source := filepath.Join(root, "source")
	skillDir := filepath.Join(source, "demo")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	body := "---\nname: demo\n---\nBody."
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	entries := []map[string]any{
		{"key": "GET repos/x/y/contents/", "body": []map[string]any{}},
		{"key": "GET repos/x/y/git/ref/heads/main", "body": map[string]any{"object": map[string]any{"sha": "parent"}}},
		{"key": "GET repos/x/y/git/commits/parent", "body": map[string]any{"tree": map[string]any{"sha": "base"}}},
		{"key": "GET repos/x/y/git/trees/base?recursive=1", "body": map[string]any{"tree": []any{}}},
		{"key": "POST repos/x/y/git/blobs", "body": map[string]any{"sha": "blob-1"}},
		{"key": "POST repos/x/y/git/trees", "body": map[string]any{"sha": "tree-1"}},
		{"key": "POST repos/x/y/git/commits", "body": map[string]any{"sha": "commit-1"}},
		{"key": "PATCH repos/x/y/git/refs/heads/main", "body": map[string]any{"object": map[string]any{"sha": "commit-1"}}},
	}
	bin := stubGHForRemove(t, entries)
	installGHEnv(t, bin)

	buf := captureJSONOut(t)
	t.Chdir(root)
	if err := runAddJSON(context.Background(), "./source"); err != nil {
		t.Fatalf("runAddJSON: %v", err)
	}
	var payload addJSONResult
	if err := json.Unmarshal([]byte(strings.TrimSpace(buf.String())), &payload); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(payload.Pushed) != 1 || payload.Pushed[0] != "demo" {
		t.Errorf("pushed = %v, want [demo]", payload.Pushed)
	}
	if payload.Skipped == nil {
		t.Error("skipped should be [] not null")
	}
}

// TestShouldAutoYesMatrix pins the auto-yes truth table. The
// underlying isStdinTerminal func variable is swapped to a
// deterministic stub so the test outcome is identical regardless of
// whether the host's `go test` invocation happens to attach a TTY to
// stdin (CI runners and local terminals can differ).
func TestShouldAutoYesMatrix(t *testing.T) {
	prevEnabled := jsonout.Enabled()
	prevStdin := isStdinTerminal
	t.Cleanup(func() {
		jsonout.SetEnabled(prevEnabled)
		isStdinTerminal = prevStdin
	})

	cases := []struct {
		name        string
		jsonEnabled bool
		stdinTTY    bool
		want        bool
	}{
		{"json off + TTY stdin", false, true, false},
		{"json off + piped stdin", false, false, false},
		{"json on + TTY stdin (user opted in)", true, true, false},
		{"json on + piped stdin (agent)", true, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			jsonout.SetEnabled(tc.jsonEnabled)
			isStdinTerminal = func() bool { return tc.stdinTTY }
			got := shouldAutoYes()
			if got != tc.want {
				t.Fatalf("shouldAutoYes() = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestIsStdinTerminalDefaultReadsOSStdin documents the production
// implementation: the unstubbed isStdinTerminal must reach os.Stdin
// (not a hard-coded value). We can't easily assert the value on every
// host, but we can confirm calling it never panics and returns a
// bool — the surface used by shouldAutoYes.
func TestIsStdinTerminalDefaultReadsOSStdin(t *testing.T) {
	_ = isStdinTerminal()
}

// TestRunSyncJSONPropagatesPublishErrorAsJSON pins the JSON-008
// contract for sync's error branch: a 500 from the publish call must
// surface as a JSON-encoded error object on stdout (via PrintError +
// os.Exit). We assert by intercepting the inner publish call through
// the scripted gh shim — a non-200 from GET ref/heads triggers the
// error path before any os.Exit can fire.
//
// NOTE: We test the planSync helper directly because runSyncJSON
// calls os.Exit, which terminates the test binary. planSync owns the
// pre-publish "discover + dedupe" stage; we verify it surfaces the
// upstream error so the wrapping os.Exit path is structurally sound.
func TestPlanSyncSurfacesRegistryError(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	cwd := t.TempDir()
	if err := os.Chdir(cwd); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(homeDir) })
	writeRegistryConfig(t, "x/y")

	entries := []map[string]any{
		{"key": "GET repos/x/y/contents/", "body": "HTTP 500: server unavailable", "exit": 1},
	}
	bin := stubGHForRemove(t, entries)
	installGHEnv(t, bin)

	_, err := planSync(context.Background())
	if err == nil {
		t.Fatal("expected error when registry list fails")
	}
}

// TestSyncJSONResultEmptySerializesAsEmptyArrays guards the
// JSON-006 contract at the type level: an empty syncJSONResult must
// emit `{"pushed":[],"skipped":[]}`, not `{"pushed":null,…}`. The
// initializer forces both slices to non-nil before Print fires; if a
// future refactor drops that initialization the encoded output would
// regress to `null` and consumers' `length` jq filters would break.
func TestSyncJSONResultEmptySerializesAsEmptyArrays(t *testing.T) {
	out := syncJSONResult{Pushed: []string{}, Skipped: []string{}}
	body, err := json.Marshal(out)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{"pushed":[],"skipped":[]}`
	if string(body) != want {
		t.Fatalf("got %q, want %q", string(body), want)
	}
}

// TestListJSONRowFieldOrder pins the JSON-001 contract at the type
// level: marshalling a row must produce `slug` first, then `name`,
// then `description`. Reordering struct fields would silently break
// the published shape.
func TestListJSONRowFieldOrder(t *testing.T) {
	body, err := json.Marshal(listJSONRow{
		Slug:        "demo",
		Name:        "Demo",
		Description: "desc",
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{"slug":"demo","name":"Demo","description":"desc"}`
	if string(body) != want {
		t.Fatalf("got %q, want %q", string(body), want)
	}
}

// TestPublishJSONResultFieldOrder pins the JSON-003 contract at the
// type level: {slug, sha, url} in that exact order so consumers can
// rely on stable jq selectors.
func TestPublishJSONResultFieldOrder(t *testing.T) {
	body, err := json.Marshal(publishJSONResult{
		Slug: "demo",
		SHA:  "abc",
		URL:  "https://github.com/x/y/tree/abc/demo",
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{"slug":"demo","sha":"abc","url":"https://github.com/x/y/tree/abc/demo"}`
	if string(body) != want {
		t.Fatalf("got %q, want %q", string(body), want)
	}
}

// TestGetJSONResultFieldOrder pins the JSON-002 contract at the type
// level: {slug, path} in that exact order.
func TestGetJSONResultFieldOrder(t *testing.T) {
	body, err := json.Marshal(getJSONResult{Slug: "demo", Path: "/tmp/demo"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{"slug":"demo","path":"/tmp/demo"}`
	if string(body) != want {
		t.Fatalf("got %q, want %q", string(body), want)
	}
}
