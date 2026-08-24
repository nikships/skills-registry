package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nikships/skills-registry/cli/internal/jsonout"
	"github.com/nikships/skills-registry/cli/internal/registry"
	"github.com/nikships/skills-registry/cli/internal/scan"
)

func TestRedactSourceUserInfo(t *testing.T) {
	got := redactSourceUserInfo("https://user@example.com/org/repo.git")
	if got != "https://example.com/org/repo.git" {
		t.Fatalf("redactSourceUserInfo() = %q", got)
	}
}

func TestRedactSourceUserInfoLeavesNonURL(t *testing.T) {
	got := redactSourceUserInfo("owner/repo")
	if got != "owner/repo" {
		t.Fatalf("redactSourceUserInfo(owner/repo) = %q", got)
	}
}

// fakeGitHubFolder installs a folder fetcher whose Contents API responses are
// scripted from `files` (folder-relative path → content). Nested directories
// are synthesized from the paths. Any request outside the folder answers 404,
// so a fetch that wanders is a test failure by construction.
func fakeGitHubFolder(t *testing.T, repo, ref, folder string, files map[string]string) {
	t.Helper()
	type entry struct {
		Name string `json:"name"`
		Type string `json:"type"`
		SHA  string `json:"sha"`
	}
	dirs := map[string]map[string]entry{}
	touchDir := func(p string) {
		if dirs[p] == nil {
			dirs[p] = map[string]entry{}
		}
	}
	touchDir(folder)
	for rel := range files {
		dir := folder
		segs := strings.Split(rel, "/")
		for _, seg := range segs[:len(segs)-1] {
			touchDir(dir)
			dirs[dir][seg] = entry{Name: seg, Type: "dir"}
			dir += "/" + seg
			touchDir(dir)
		}
		leaf := segs[len(segs)-1]
		dirs[dir][leaf] = entry{Name: leaf, Type: "file", SHA: "sha-" + rel}
	}

	prev := newFolderFetcher
	t.Cleanup(func() { newFolderFetcher = prev })
	newFolderFetcher = func() (*registry.Fetcher, error) {
		return &registry.Fetcher{Runner: func(_ context.Context, args []string) ([]byte, error) {
			endpoint := args[len(args)-3]
			bare := strings.SplitN(endpoint, "?", 2)[0]
			if !strings.Contains(endpoint, "ref="+ref) {
				t.Errorf("call %q is not pinned to ref %q", endpoint, ref)
			}
			repoPath, ok := strings.CutPrefix(bare, "repos/"+repo+"/contents/")
			if !ok {
				t.Fatalf("unexpected endpoint %q", endpoint)
			}
			if listing, ok := dirs[repoPath]; ok {
				out := make([]entry, 0, len(listing))
				for _, e := range listing {
					out = append(out, e)
				}
				return json.Marshal(out)
			}
			rel, ok := strings.CutPrefix(repoPath, folder+"/")
			if content, isFile := files[rel]; ok && isFile {
				return json.Marshal(map[string]string{
					"encoding": "base64",
					"content":  base64.StdEncoding.EncodeToString([]byte(content)),
				})
			}
			return nil, &notFoundError{endpoint}
		}}, nil
	}
}

// notFoundError mimics the shape `registry` derives a 404 from: the status is
// parsed out of the message body.
type notFoundError struct{ endpoint string }

func (e *notFoundError) Error() string { return "gh " + e.endpoint + " failed (status 404): HTTP 404" }

// fakeGit puts a `git` shim first on PATH. The shim records its argv and
// materializes a two-skill tree in the clone destination, so a test can prove
// the clone path still walks every nested SKILL.md. Returns the argv file.
func fakeGit(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	argvPath := filepath.Join(dir, "argv")
	script := `#!/bin/sh
PATH=/usr/bin:/bin
echo "$@" >> ` + argvPath + `
for a in "$@"; do dest="$a"; done
mkdir -p "$dest/top" "$dest/nested/deep"
cat > "$dest/top/SKILL.md" <<'EOF'
---
name: top
---
Top body.
EOF
cat > "$dest/nested/deep/SKILL.md" <<'EOF'
---
name: deep
---
Deep body.
EOF
`
	if err := os.WriteFile(filepath.Join(dir, "git"), []byte(script), 0o755); err != nil {
		t.Fatalf("write git shim: %v", err)
	}
	shimPath(t, dir)
	return argvPath
}

// shimPath puts dir first on PATH so a shim there shadows the real binary,
// keeping the system directories available for anything else the test needs.
func shimPath(t *testing.T, dir string) {
	t.Helper()
	t.Setenv("PATH", dir+string(os.PathListSeparator)+"/usr/bin"+string(os.PathListSeparator)+"/bin")
}

// failingGit puts a `git` shim on PATH that fails the test if it runs. Folder
// URLs must never reach the clone path.
func failingGit(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	marker := filepath.Join(dir, "invoked")
	script := "#!/bin/sh\nPATH=/usr/bin:/bin\ntouch " + marker + "\nexit 1\n"
	if err := os.WriteFile(filepath.Join(dir, "git"), []byte(script), 0o755); err != nil {
		t.Fatalf("write git shim: %v", err)
	}
	shimPath(t, dir)
	t.Cleanup(func() {
		if _, err := os.Stat(marker); err == nil {
			t.Error("git clone ran for a source that must not clone")
		}
	})
}

func discoverSlugs(t *testing.T, dir string) []string {
	t.Helper()
	skills, err := scan.Discover([]scan.Source{{Path: dir, Label: "test"}})
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	slugs := make([]string, 0, len(skills))
	for _, s := range skills {
		slugs = append(slugs, s.Slug)
	}
	return slugs
}

func TestResolveSourceFetchesGitHubFolderWithoutCloning(t *testing.T) {
	failingGit(t)
	sha := "0123456789abcdef0123456789abcdef01234567"
	fakeGitHubFolder(t, "openclaw/openclaw", sha, "skills/summarize", map[string]string{
		"SKILL.md":         "---\nname: summarize\n---\nBody.",
		"scripts/run.sh":   "#!/bin/sh\n",
		"references/x.md":  "ref",
		"assets/logo.txt":  "logo",
		"nested/deep/a.md": "deep",
	})

	dir, cleanup, err := resolveSourceQuiet(context.Background(),
		"https://github.com/openclaw/openclaw/blob/"+sha+"/skills/summarize")
	if err != nil {
		t.Fatalf("resolveSource: %v", err)
	}
	defer cleanup()

	// The temp dir holds exactly the requested folder, nothing else.
	top, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	if len(top) != 1 || top[0].Name() != "summarize" {
		names := []string{}
		for _, e := range top {
			names = append(names, e.Name())
		}
		t.Fatalf("temp dir holds %v, want just [summarize]", names)
	}
	for _, rel := range []string{
		"summarize/SKILL.md", "summarize/scripts/run.sh",
		"summarize/references/x.md", "summarize/assets/logo.txt",
		"summarize/nested/deep/a.md",
	} {
		if _, err := os.Stat(filepath.Join(dir, rel)); err != nil {
			t.Errorf("missing fetched file %s: %v", rel, err)
		}
	}
	if got := discoverSlugs(t, dir); len(got) != 1 || got[0] != "summarize" {
		t.Errorf("discovered %v, want [summarize]", got)
	}
}

func TestResolveSourceFetchesTreeFolderURL(t *testing.T) {
	failingGit(t)
	fakeGitHubFolder(t, "owner/repo", "main", "skills/pdf", map[string]string{
		"SKILL.md": "---\nname: pdf\n---\nBody.",
	})
	dir, cleanup, err := resolveSourceQuiet(context.Background(),
		"https://github.com/owner/repo/tree/main/skills/pdf")
	if err != nil {
		t.Fatalf("resolveSource: %v", err)
	}
	defer cleanup()
	if got := discoverSlugs(t, dir); len(got) != 1 || got[0] != "pdf" {
		t.Errorf("discovered %v, want [pdf]", got)
	}
}

func TestResolveSourceFolderWithoutSkillMdErrorsCleanly(t *testing.T) {
	failingGit(t)
	fakeGitHubFolder(t, "owner/repo", "main", "src/utils", map[string]string{
		"helper.go": "package utils",
	})
	_, cleanup, err := resolveSourceQuiet(context.Background(),
		"https://github.com/owner/repo/tree/main/src/utils")
	defer cleanup()
	if err == nil {
		t.Fatal("expected an error for a folder with no SKILL.md")
	}
	msg := err.Error()
	if !strings.Contains(msg, scan.MainFileName) || !strings.Contains(msg, "src/utils") {
		t.Errorf("error %q should name the missing SKILL.md and the folder", msg)
	}
}

func TestResolveSourceEmptyFolderErrorsCleanly(t *testing.T) {
	failingGit(t)
	fakeGitHubFolder(t, "owner/repo", "main", "skills/empty", map[string]string{})
	_, cleanup, err := resolveSourceQuiet(context.Background(),
		"https://github.com/owner/repo/tree/main/skills/empty")
	defer cleanup()
	if err == nil {
		t.Fatal("expected an error for an empty folder")
	}
	if !strings.Contains(err.Error(), "is empty") {
		t.Errorf("error = %v, want an 'is empty' message", err)
	}
}

func TestResolveSourceShorthandShallowClonesAndWalksEverySkill(t *testing.T) {
	argvPath := fakeGit(t)
	prev := newFolderFetcher
	t.Cleanup(func() { newFolderFetcher = prev })
	newFolderFetcher = func() (*registry.Fetcher, error) {
		t.Error("shorthand must not use the folder fetcher")
		return nil, nil
	}

	dir, cleanup, err := resolveSourceQuiet(context.Background(), "owner/repo")
	if err != nil {
		t.Fatalf("resolveSource: %v", err)
	}
	defer cleanup()

	argv, err := os.ReadFile(argvPath)
	if err != nil {
		t.Fatalf("read argv: %v", err)
	}
	line := strings.TrimSpace(string(argv))
	for _, want := range []string{"clone", "--depth 1", "--single-branch", "https://github.com/owner/repo.git"} {
		if !strings.Contains(line, want) {
			t.Errorf("clone argv %q missing %q", line, want)
		}
	}
	if strings.Contains(line, "--branch") {
		t.Errorf("clone argv %q should not pin a branch for shorthand", line)
	}
	got := discoverSlugs(t, dir)
	if len(got) != 2 || got[0] != "deep" || got[1] != "top" {
		t.Errorf("discovered %v, want every nested SKILL.md ([deep top])", got)
	}
}

func TestResolveSourceRepoAndBranchURLsClone(t *testing.T) {
	cases := []struct {
		name       string
		source     string
		wantArgs   []string
		unwantArgs []string
	}{
		{
			name:       "bare repo url",
			source:     "https://github.com/owner/repo",
			wantArgs:   []string{"https://github.com/owner/repo.git"},
			unwantArgs: []string{"--branch"},
		},
		{
			name:     "tree url without a folder pins the branch",
			source:   "https://github.com/owner/repo/tree/dev",
			wantArgs: []string{"--branch dev", "https://github.com/owner/repo.git"},
		},
		{
			name:       "non-github git url is cloned verbatim",
			source:     "https://gitlab.com/owner/repo.git",
			wantArgs:   []string{"https://gitlab.com/owner/repo.git"},
			unwantArgs: []string{"--branch"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			argvPath := fakeGit(t)
			_, cleanup, err := resolveSourceQuiet(context.Background(), tc.source)
			if err != nil {
				t.Fatalf("resolveSource: %v", err)
			}
			defer cleanup()
			argv, err := os.ReadFile(argvPath)
			if err != nil {
				t.Fatalf("read argv: %v", err)
			}
			line := strings.TrimSpace(string(argv))
			for _, want := range tc.wantArgs {
				if !strings.Contains(line, want) {
					t.Errorf("clone argv %q missing %q", line, want)
				}
			}
			for _, unwanted := range tc.unwantArgs {
				if strings.Contains(line, unwanted) {
					t.Errorf("clone argv %q should not contain %q", line, unwanted)
				}
			}
		})
	}
}

// TestRunAddJSONFolderURL proves the --json code path works for a folder URL:
// the folder is fetched over the Contents API, published, and installed, with
// no clone of the parent repository.
func TestRunAddJSONFolderURL(t *testing.T) {
	prev := jsonout.Enabled()
	t.Cleanup(func() { jsonout.SetEnabled(prev) })
	jsonout.SetEnabled(true)
	t.Setenv("SKILLS_MIRROR_DISABLE", "1")

	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	writeRegistryConfig(t, "x/y")

	entries := []map[string]any{
		{"key": "GET repos/x/y/contents/", "body": []map[string]any{}},
		{"key": "GET repos/x/y/git/ref/heads/main", "body": map[string]any{"object": map[string]any{"sha": "parent"}}},
		{"key": "GET repos/x/y/git/commits/parent", "body": map[string]any{"tree": map[string]any{"sha": "base"}}},
		{"key": "GET repos/x/y/git/trees/base?recursive=1", "body": map[string]any{"tree": []any{}}},
		{"key": "POST repos/x/y/git/blobs", "body": map[string]any{"sha": "blob-1"}},
		{"key": "POST repos/x/y/git/blobs", "body": map[string]any{"sha": "blob-2"}},
		{"key": "POST repos/x/y/git/trees", "body": map[string]any{"sha": "tree-1"}},
		{"key": "POST repos/x/y/git/commits", "body": map[string]any{"sha": "commit-1"}},
		{"key": "PATCH repos/x/y/git/refs/heads/main", "body": map[string]any{"object": map[string]any{"sha": "commit-1"}}},
		{"key": "GET repos/x/y/contents/summarize", "body": []map[string]any{
			{"type": "file", "name": "SKILL.md", "sha": "skill-md-sha"},
		}},
		{"key": "GET repos/x/y/contents/summarize/SKILL.md", "body": map[string]any{
			"encoding": "base64",
			"content":  "Ym9keQ==",
		}},
	}
	bin := stubGHForRemove(t, entries)
	installGHEnv(t, bin)

	// installGHEnv/stubGH points the registry client at the shim; the folder
	// fetcher is faked separately, and git must never run.
	sha := "0123456789abcdef0123456789abcdef01234567"
	fakeGitHubFolder(t, "openclaw/openclaw", sha, "skills/summarize", map[string]string{
		"SKILL.md":       "---\nname: summarize\n---\nBody.",
		"scripts/run.sh": "#!/bin/sh\n",
	})

	buf := captureJSONOut(t)
	t.Chdir(t.TempDir())
	err := runAddJSON(context.Background(),
		"https://github.com/openclaw/openclaw/blob/"+sha+"/skills/summarize")
	if err != nil {
		t.Fatalf("runAddJSON: %v (output %s)", err, buf.String())
	}
	var payload addJSONResult
	if err := json.Unmarshal([]byte(strings.TrimSpace(buf.String())), &payload); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(payload.Pushed) != 1 || payload.Pushed[0] != "summarize" {
		t.Errorf("pushed = %v, want [summarize]", payload.Pushed)
	}
	if payload.Skipped == nil {
		t.Error("skipped should be [] not null")
	}
	if paths := payload.Installed["summarize"]; len(paths) == 0 {
		t.Errorf("installed = %+v, want at least one path for summarize", payload.Installed)
	}
}

func TestContainsSkillFile(t *testing.T) {
	if !containsSkillFile([]string{"scripts/run.sh", scan.MainFileName}) {
		t.Error("containsSkillFile missed a root SKILL.md")
	}
	if !containsSkillFile([]string{"pdf/" + scan.MainFileName}) {
		t.Error("containsSkillFile missed a nested SKILL.md")
	}
	if containsSkillFile([]string{"README.md", "src/main.go"}) {
		t.Error("containsSkillFile matched a folder with no SKILL.md")
	}
}
