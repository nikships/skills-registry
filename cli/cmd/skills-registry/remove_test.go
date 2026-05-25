package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/anand-92/skills-registry/cli/internal/agents"
	"github.com/anand-92/skills-registry/cli/internal/cache"
	"github.com/anand-92/skills-registry/cli/internal/jsonout"
	"github.com/anand-92/skills-registry/cli/internal/registry"
)

// stubGHForRemove writes a minimal gh shim that replays scripted
// responses keyed by argv substring. Mirrors the registry package's
// internal stubGH helper (we can't reuse it directly without exporting
// it) so the remove subcommand can be exercised end-to-end against a
// fake registry without making real GitHub calls. The shared helper
// also captures stdin to a temp file so POST/PATCH bodies are
// available — useful when a future test wants to assert on payload
// shape.
func stubGHForRemove(t *testing.T, entries []map[string]any) string {
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
	script := `#!/bin/sh
state=` + filepath.Clean(statePath) + `
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
rm -f "$stdin_file"
`
	bin := filepath.Join(dir, "gh")
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}
	return bin
}

// writeRegistryConfig drops a one-line registry.toml under
// XDG_CONFIG_HOME so config.Load() resolves to the supplied repo.
// Returns the resolved path purely for assertion fodder.
func writeRegistryConfig(t *testing.T, repo string) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("SKILLS_REGISTRY", "")
	cfgDir := filepath.Join(dir, "skills-mcp")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatalf("mkdir config: %v", err)
	}
	body := `[registry]
repo = "` + repo + `"
default_branch = "main"
`
	cfgPath := filepath.Join(cfgDir, "registry.toml")
	if err := os.WriteFile(cfgPath, []byte(body), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return cfgPath
}

// successEntries returns the scripted gh responses for a happy-path
// remove of slug "demo": Slugs() → demo + other, then Delete (ref,
// commit, tree, POST tree, POST commit, PATCH ref).
func successEntries() []map[string]any {
	return []map[string]any{
		{
			"key": "GET repos/x/y/contents/",
			"body": []map[string]any{
				{"name": "demo", "type": "dir", "sha": "tree-demo"},
				{"name": "other", "type": "dir", "sha": "tree-other"},
			},
		},
		{"key": "GET repos/x/y/git/ref/heads/main", "body": map[string]any{"object": map[string]any{"sha": "parent"}}},
		{"key": "GET repos/x/y/git/commits/parent", "body": map[string]any{"tree": map[string]any{"sha": "base"}}},
		{
			"key": "GET repos/x/y/git/trees/base?recursive=1",
			"body": map[string]any{"tree": []any{
				map[string]any{"path": "demo/SKILL.md", "type": "blob"},
			}},
		},
		{"key": "POST repos/x/y/git/trees", "body": map[string]any{"sha": "new-tree"}},
		{"key": "POST repos/x/y/git/commits", "body": map[string]any{"sha": "new-commit"}},
		{"key": "PATCH repos/x/y/git/refs/heads/main", "body": map[string]any{"object": map[string]any{"sha": "new-commit"}}},
	}
}

// captureJSONOut swaps jsonout's internal writer for a buffer so the
// test can read what `jsonout.Print` / `jsonout.PrintError` wrote. The
// jsonout package captures os.Stdout at init time, so naively
// re-pointing os.Stdout here would leak everything to the test
// runner's stdout. SwapWriter exists for exactly this case.
func captureJSONOut(t *testing.T) *bytes.Buffer {
	t.Helper()
	buf := &bytes.Buffer{}
	prev := jsonout.SwapWriter(buf)
	t.Cleanup(func() { jsonout.SwapWriter(prev) })
	return buf
}

// installGHEnv points registry.FindGH at the supplied shim by setting
// GH_BIN (the same env knob FindGH reads for tests) for the duration
// of the test.
func installGHEnv(t *testing.T, bin string) {
	t.Helper()
	t.Setenv("GH_BIN", bin)
}

// TestRunRemoveDeletesRegistryAndLocalArtifacts is the end-to-end
// success test: a cached skill folder + meta file + agent dot-folder
// all exist on disk, the registry contains the slug, and runRemove
// wipes everything in one shot.
func TestRunRemoveDeletesRegistryAndLocalArtifacts(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(homeDir, ".cache"))
	t.Chdir(homeDir) // ensure os.Getwd() succeeds for dot-folder sweep
	writeRegistryConfig(t, "x/y")
	bin := stubGHForRemove(t, successEntries())
	installGHEnv(t, bin)

	// Seed the Python MCP cache.
	cacheRoot := cache.CacheRoot()
	cacheSlugDir := filepath.Join(cacheRoot, "demo")
	if err := os.MkdirAll(cacheSlugDir, 0o755); err != nil {
		t.Fatalf("mkdir cache: %v", err)
	}
	if err := os.WriteFile(filepath.Join(cacheSlugDir, "SKILL.md"), []byte("cached"), 0o644); err != nil {
		t.Fatalf("write cache slug: %v", err)
	}
	metaPath := filepath.Join(cacheRoot, "demo.meta.json")
	if err := os.WriteFile(metaPath, []byte(`{"tree_sha":"x"}`), 0o644); err != nil {
		t.Fatalf("write meta: %v", err)
	}

	// Seed one agent dot-folder under HOME so removeFromDotFolders
	// finds a matching subdir. We pick `.claude` because agents.All()
	// always includes it.
	agentDir := filepath.Join(homeDir, ".claude", "skills", "demo")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatalf("mkdir agent: %v", err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "SKILL.md"), []byte("agent"), 0o644); err != nil {
		t.Fatalf("write agent skill: %v", err)
	}

	report, err := runRemove(context.Background(), "demo", true /*yes*/, true /*quiet*/)
	if err != nil {
		t.Fatalf("runRemove: %v", err)
	}
	if report == nil {
		t.Fatal("runRemove returned nil report on success")
	}
	if report.Slug != "demo" {
		t.Errorf("slug = %q, want demo", report.Slug)
	}
	if report.CommitSHA != "new-commit" {
		t.Errorf("commit = %q, want new-commit", report.CommitSHA)
	}
	if !report.CacheCleared {
		t.Error("cache should have been cleared")
	}
	if report.DotFoldersCleared != 1 {
		t.Errorf("DotFoldersCleared = %d, want 1", report.DotFoldersCleared)
	}

	// Filesystem assertions.
	if _, err := os.Stat(cacheSlugDir); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("cache slug dir still exists: %v", err)
	}
	if _, err := os.Stat(metaPath); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("cache meta file still exists: %v", err)
	}
	if _, err := os.Stat(agentDir); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("agent dir still exists: %v", err)
	}
}

// TestRunRemoveReturnsErrorWhenSlugMissing pins down the exit-1 contract:
// the registry's slug set doesn't contain the requested name, so we
// surface an explicit "not found" error and skip the Delete call.
func TestRunRemoveReturnsErrorWhenSlugMissing(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	writeRegistryConfig(t, "x/y")
	bin := stubGHForRemove(t, []map[string]any{
		{
			"key": "GET repos/x/y/contents/",
			"body": []map[string]any{
				{"name": "other", "type": "dir", "sha": "tree-other"},
			},
		},
	})
	installGHEnv(t, bin)
	_, err := runRemove(context.Background(), "demo", true, true)
	if err == nil {
		t.Fatal("expected error for missing slug")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error should mention 'not found': %v", err)
	}
	if !strings.Contains(err.Error(), "demo") {
		t.Errorf("error should mention slug: %v", err)
	}
}

// TestRunRemoveSlugifiesUserInput verifies the friendly-input
// behavior: a user types "Demo Skill" or "demo-skill" and the canonical
// "demo_skill" slug is looked up in the registry. Matches the
// Slugify pattern publish + sync already use.
func TestRunRemoveSlugifiesUserInput(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(homeDir, ".cache"))
	writeRegistryConfig(t, "x/y")
	entries := []map[string]any{
		{
			"key": "GET repos/x/y/contents/",
			"body": []map[string]any{
				{"name": "demo_skill", "type": "dir", "sha": "tree-1"},
			},
		},
		{"key": "GET repos/x/y/git/ref/heads/main", "body": map[string]any{"object": map[string]any{"sha": "parent"}}},
		{"key": "GET repos/x/y/git/commits/parent", "body": map[string]any{"tree": map[string]any{"sha": "base"}}},
		{
			"key": "GET repos/x/y/git/trees/base?recursive=1",
			"body": map[string]any{"tree": []any{
				map[string]any{"path": "demo_skill/SKILL.md", "type": "blob"},
			}},
		},
		{"key": "POST repos/x/y/git/trees", "body": map[string]any{"sha": "new-tree"}},
		{"key": "POST repos/x/y/git/commits", "body": map[string]any{"sha": "new-commit"}},
		{"key": "PATCH repos/x/y/git/refs/heads/main", "body": map[string]any{"object": map[string]any{"sha": "new-commit"}}},
	}
	bin := stubGHForRemove(t, entries)
	installGHEnv(t, bin)

	report, err := runRemove(context.Background(), "Demo Skill", true, true)
	if err != nil {
		t.Fatalf("runRemove: %v", err)
	}
	if report == nil || report.Slug != "demo_skill" {
		t.Errorf("expected slug demo_skill, got %+v", report)
	}
}

// TestRunRemoveSkipsConfirmWhenQuietMode verifies the JSON / non-TTY
// contract: in quietMode=true the prompt is not raised even when yes
// is false. The test trips an unscripted gh call iff the prompt
// short-circuits — which would happen if confirmRemove ran (it'd hang
// on tea.NewProgram in a non-TTY harness instead of returning the
// expected report).
func TestRunRemoveSkipsConfirmWhenQuietMode(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	writeRegistryConfig(t, "x/y")
	bin := stubGHForRemove(t, successEntries())
	installGHEnv(t, bin)

	report, err := runRemove(context.Background(), "demo", false /*yes*/, true /*quiet*/)
	if err != nil {
		t.Fatalf("runRemove: %v", err)
	}
	if report == nil {
		t.Fatal("expected a report")
	}
}

// TestReportToJSONIncludesRegistryAlways pins the contract that the
// `removed_from` array always carries the canonical registry token —
// even when cache / dotfolder cleanup didn't fire. Without this, a
// `jq '.removed_from | index("registry")'` consumer would have to
// special-case the empty path.
func TestReportToJSONIncludesRegistryAlways(t *testing.T) {
	out := reportToJSON(removeReport{Slug: "demo", Repo: "x/y", CommitSHA: "abc"})
	if len(out.RemovedFrom) != 1 || out.RemovedFrom[0] != removeLocationRegistry {
		t.Fatalf("removed_from = %v, want [%q]", out.RemovedFrom, removeLocationRegistry)
	}
	if out.Slug != "demo" || out.Repo != "x/y" || out.SHA != "abc" {
		t.Fatalf("payload mismatch: %+v", out)
	}
}

// TestReportToJSONAddsCacheAndDotfoldersWhenCleared verifies the
// additive ordering: cache before dotfolders so consumers don't have
// to re-sort. Mirrors JSON_005's example in validation-contract.md.
func TestReportToJSONAddsCacheAndDotfoldersWhenCleared(t *testing.T) {
	out := reportToJSON(removeReport{
		Slug:              "demo",
		CommitSHA:         "abc",
		CacheCleared:      true,
		DotFoldersCleared: 2,
	})
	want := []string{removeLocationRegistry, removeLocationCache, removeLocationDotFolder}
	if len(out.RemovedFrom) != len(want) {
		t.Fatalf("removed_from = %v, want %v", out.RemovedFrom, want)
	}
	for i, v := range want {
		if out.RemovedFrom[i] != v {
			t.Errorf("removed_from[%d] = %q, want %q", i, out.RemovedFrom[i], v)
		}
	}
}

// TestReportToJSONOmitsCacheIfNotCleared documents the "did not exist"
// branch: when removeFromCache reports false, the JSON payload does
// not include the cache token so downstream consumers know nothing
// was wiped locally.
func TestReportToJSONOmitsCacheIfNotCleared(t *testing.T) {
	out := reportToJSON(removeReport{Slug: "demo", DotFoldersCleared: 1})
	for _, v := range out.RemovedFrom {
		if v == removeLocationCache {
			t.Fatalf("removed_from unexpectedly contains cache: %v", out.RemovedFrom)
		}
	}
}

// TestRemoveSummaryLineCondensesReport renders the toast caption and
// verifies it stays single-line + readable for the hub's toast row.
func TestRemoveSummaryLineCondensesReport(t *testing.T) {
	line := removeSummaryLine(removeReport{
		Slug:              "demo",
		CacheCleared:      true,
		DotFoldersCleared: 3,
	})
	if strings.Contains(line, "\n") {
		t.Fatalf("toast line must be single-line: %q", line)
	}
	if !strings.Contains(line, "demo") {
		t.Errorf("toast should name slug: %q", line)
	}
	if !strings.Contains(line, "registry") {
		t.Errorf("toast should mention registry: %q", line)
	}
	if !strings.Contains(line, "cache") {
		t.Errorf("toast should mention cache: %q", line)
	}
	if !strings.Contains(line, "3 dotfolders") {
		t.Errorf("toast should mention 3 dotfolders: %q", line)
	}
}

// TestMatchSlugChildrenLiteralAndSlugified covers both name-equality
// and Slugify-fallback so hyphenated folders on disk
// ("agp-9-upgrade") match canonical slugs ("agp_9_upgrade") — the
// same rule scan.EntriesForCleanup uses.
func TestMatchSlugChildrenLiteralAndSlugified(t *testing.T) {
	parent := t.TempDir()
	for _, name := range []string{"agp_9_upgrade", "agp-9-upgrade", "unrelated"} {
		if err := os.MkdirAll(filepath.Join(parent, name), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", name, err)
		}
	}
	got := matchSlugChildren(parent, "agp_9_upgrade")
	if len(got) != 2 {
		t.Fatalf("expected 2 matches (literal + slugified), got %d: %v", len(got), got)
	}
	for _, p := range got {
		if strings.HasSuffix(p, "unrelated") {
			t.Errorf("unrelated dir matched: %q", p)
		}
	}
}

// TestMatchSlugChildrenMissingParent covers the "parent doesn't exist"
// short-circuit: dot-folders are usually absent on a fresh install, so
// the helper has to tolerate missing parents without erroring out.
func TestMatchSlugChildrenMissingParent(t *testing.T) {
	got := matchSlugChildren(filepath.Join(t.TempDir(), "nope"), "demo")
	if len(got) != 0 {
		t.Fatalf("expected no matches for missing parent, got %v", got)
	}
}

// TestRemoveFromCacheRemovesBothArtifacts seeds the slug dir + meta
// file under CacheRoot() and checks both are gone after the call.
func TestRemoveFromCacheRemovesBothArtifacts(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(homeDir, ".cache"))
	root := cache.CacheRoot()
	skillDir := filepath.Join(root, "demo")
	metaPath := filepath.Join(root, "demo.meta.json")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("mkdir slug dir: %v", err)
	}
	if err := os.WriteFile(metaPath, []byte("{}"), 0o644); err != nil {
		t.Fatalf("write meta: %v", err)
	}
	if !removeFromCache("demo") {
		t.Fatal("removeFromCache should return true when artifacts existed")
	}
	if _, err := os.Stat(skillDir); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("slug dir still exists: %v", err)
	}
	if _, err := os.Stat(metaPath); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("meta file still exists: %v", err)
	}
}

// TestRemoveFromCacheReturnsFalseWhenAbsent documents that a missing
// cache entry is not an error — get_skill simply hasn't run for this
// slug yet, and the wider remove flow should still report success.
func TestRemoveFromCacheReturnsFalseWhenAbsent(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(homeDir, ".cache"))
	if removeFromCache("never-cached") {
		t.Fatal("removeFromCache should return false when nothing existed")
	}
}

// TestRemoveFromDotFoldersScansAllAgents verifies the agents.All()
// integration: at least one configured agent dot-folder containing the
// slug must be picked up and deleted, even though most others are
// absent on a fresh checkout.
func TestRemoveFromDotFoldersScansAllAgents(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	var underHome agents.Target
	for _, a := range agents.All() {
		if a.UnderHome {
			underHome = a
			break
		}
	}
	if underHome.DotDir == "" {
		t.Fatal("no UnderHome agent registered")
	}

	target := filepath.Join(homeDir, underHome.DotDir, "skills", "demo")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatalf("mkdir target: %v", err)
	}
	deleted := removeFromDotFoldersAt("demo", homeDir, homeDir)
	if len(deleted) != 1 {
		t.Fatalf("expected 1 deletion, got %d: %v", len(deleted), deleted)
	}
	if _, err := os.Stat(target); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("target still exists: %v", err)
	}
}

// TestRemoveCmdRegisteredOnRoot confirms the command is reachable via
// cobra. Without this, --json + integration tests would mask a typo in
// main.go's AddCommand call.
func TestRemoveCmdRegisteredOnRoot(t *testing.T) {
	root := newRootCmd()
	cmd, _, err := root.Find([]string{"remove"})
	if err != nil {
		t.Fatalf("remove subcommand not registered: %v", err)
	}
	if cmd == root {
		t.Fatal("remove subcommand lookup returned root command")
	}
	if !cmd.Flags().HasAvailableFlags() {
		t.Fatal("remove command should declare flags (--yes)")
	}
	if cmd.Flag("yes") == nil {
		t.Fatal("remove command missing --yes flag")
	}
}

// TestRunRemoveCmdJSONSuccess verifies the JSON success branch
// end-to-end: jsonout.Enabled() is true → quietMode propagates → the
// payload is well-formed JSON containing {slug, removed_from, sha,
// repo}. We also capture stdout to confirm there's no human-readable
// chatter mixed in.
func TestRunRemoveCmdJSONSuccess(t *testing.T) {
	prev := jsonout.Enabled()
	t.Cleanup(func() { jsonout.SetEnabled(prev) })
	jsonout.SetEnabled(true)

	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(homeDir, ".cache"))
	writeRegistryConfig(t, "x/y")
	bin := stubGHForRemove(t, successEntries())
	installGHEnv(t, bin)

	buf := captureJSONOut(t)
	if err := runRemoveCmd(context.Background(), "demo", true); err != nil {
		t.Fatalf("runRemoveCmd: %v", err)
	}
	got := strings.TrimSpace(buf.String())
	var payload struct {
		Slug        string   `json:"slug"`
		Repo        string   `json:"repo"`
		SHA         string   `json:"sha"`
		RemovedFrom []string `json:"removed_from"`
	}
	if err := json.Unmarshal([]byte(got), &payload); err != nil {
		t.Fatalf("output is not valid JSON: %q (%v)", got, err)
	}
	if payload.Slug != "demo" {
		t.Errorf("slug = %q, want demo", payload.Slug)
	}
	if payload.Repo != "x/y" {
		t.Errorf("repo = %q, want x/y", payload.Repo)
	}
	if payload.SHA != "new-commit" {
		t.Errorf("sha = %q, want new-commit", payload.SHA)
	}
	if len(payload.RemovedFrom) == 0 || payload.RemovedFrom[0] != removeLocationRegistry {
		t.Errorf("removed_from missing registry: %v", payload.RemovedFrom)
	}
	// JSON_006: must round-trip cleanly through encoding/json (already
	// asserted above) AND have no trailing text.
	if strings.Contains(got, "\n") {
		t.Errorf("JSON output spans multiple lines: %q", got)
	}
}

// TestErrSlugNotFoundIsExported guards against future refactors
// hiding the sentinel — the remove command relies on importing it via
// registry.ErrSlugNotFound rather than re-defining a string check.
func TestErrSlugNotFoundIsExported(t *testing.T) {
	if registry.ErrSlugNotFound == nil {
		t.Fatal("registry.ErrSlugNotFound should be a non-nil sentinel")
	}
	wrapped := errors.Join(errors.New("delete demo"), registry.ErrSlugNotFound)
	if !errors.Is(wrapped, registry.ErrSlugNotFound) {
		t.Fatal("errors.Is should unwrap registry.ErrSlugNotFound")
	}
}
