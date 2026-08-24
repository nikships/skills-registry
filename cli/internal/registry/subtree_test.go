package registry

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseGitHubURL(t *testing.T) {
	cases := []struct {
		name string
		in   string
		ok   bool
		want GitHubTarget
	}{
		{
			name: "tree url with subpath",
			in:   "https://github.com/owner/repo/tree/main/skills/pdf",
			ok:   true,
			want: GitHubTarget{Owner: "owner", Repo: "repo", Ref: "main", Path: "skills/pdf"},
		},
		{
			name: "blob url with full sha",
			in:   "https://github.com/openclaw/openclaw/blob/0123456789abcdef0123456789abcdef01234567/skills/summarize",
			ok:   true,
			want: GitHubTarget{
				Owner: "openclaw", Repo: "openclaw",
				Ref:  "0123456789abcdef0123456789abcdef01234567",
				Path: "skills/summarize",
			},
		},
		{
			name: "blob url pointing at SKILL.md",
			in:   "https://github.com/o/r/blob/main/skills/foo/SKILL.md",
			ok:   true,
			want: GitHubTarget{Owner: "o", Repo: "r", Ref: "main", Path: "skills/foo/SKILL.md"},
		},
		{
			name: "tree url without subpath",
			in:   "https://github.com/owner/repo/tree/dev",
			ok:   true,
			want: GitHubTarget{Owner: "owner", Repo: "repo", Ref: "dev"},
		},
		{
			name: "bare repo url",
			in:   "https://github.com/owner/repo",
			ok:   true,
			want: GitHubTarget{Owner: "owner", Repo: "repo"},
		},
		{
			name: "strips .git suffix",
			in:   "https://github.com/owner/repo.git/tree/main/x",
			ok:   true,
			want: GitHubTarget{Owner: "owner", Repo: "repo", Ref: "main", Path: "x"},
		},
		{
			name: "www host and trailing slash",
			in:   "https://www.github.com/owner/repo/tree/main/skills/pdf/",
			ok:   true,
			want: GitHubTarget{Owner: "owner", Repo: "repo", Ref: "main", Path: "skills/pdf"},
		},
		{
			name: "percent-encoded space in path",
			in:   "https://github.com/owner/repo/tree/main/skills/my%20skill",
			ok:   true,
			want: GitHubTarget{Owner: "owner", Repo: "repo", Ref: "main", Path: "skills/my skill"},
		},
		{name: "shorthand is not a url", in: "owner/repo"},
		{name: "other host", in: "https://gitlab.com/owner/repo/tree/main/x"},
		{name: "ssh remote", in: "git@github.com:owner/repo.git"},
		{name: "owner only", in: "https://github.com/owner"},
		{name: "pull request url", in: "https://github.com/owner/repo/pull/12"},
		{name: "tree with no ref", in: "https://github.com/owner/repo/tree"},
		{name: "encoded traversal in path", in: "https://github.com/owner/repo/tree/main/..%2f..%2fetc"},
		{name: "encoded separator in path", in: "https://github.com/owner/repo/tree/main/a%2Fb"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := ParseGitHubURL(tc.in)
			if ok != tc.ok {
				t.Fatalf("ParseGitHubURL(%q) ok = %v, want %v (got %+v)", tc.in, ok, tc.ok, got)
			}
			if ok && got != tc.want {
				t.Fatalf("ParseGitHubURL(%q) = %+v, want %+v", tc.in, got, tc.want)
			}
		})
	}
}

func TestGitHubTargetAccessors(t *testing.T) {
	folder := GitHubTarget{Owner: "o", Repo: "r", Ref: "main", Path: "skills/pdf"}
	if !folder.IsFolder() {
		t.Error("IsFolder() = false for a path target")
	}
	if folder.RefIsSHA() {
		t.Error("RefIsSHA() = true for a branch name")
	}
	if got := folder.CloneURL(); got != "https://github.com/o/r.git" {
		t.Errorf("CloneURL() = %q", got)
	}
	if got := folder.WebURL(); got != "https://github.com/o/r/tree/main/skills/pdf" {
		t.Errorf("WebURL() = %q", got)
	}

	sha := GitHubTarget{Owner: "o", Repo: "r", Ref: strings.Repeat("a", 40), Path: "x"}
	if !sha.RefIsSHA() {
		t.Error("RefIsSHA() = false for a 40-char hex ref")
	}

	repo := GitHubTarget{Owner: "o", Repo: "r"}
	if repo.IsFolder() {
		t.Error("IsFolder() = true for a bare repo target")
	}
	if got := repo.WebURL(); got != "https://github.com/o/r" {
		t.Errorf("WebURL() = %q", got)
	}
	if got := (GitHubTarget{Owner: "o", Repo: "r", Ref: "dev"}).WebURL(); got != "https://github.com/o/r/tree/dev" {
		t.Errorf("WebURL() = %q", got)
	}
}

func TestGitHubTargetSplits(t *testing.T) {
	// A branch name may contain slashes, so a folder URL has several possible
	// ref/path readings. Most-likely (shortest ref) comes first.
	got := (GitHubTarget{Owner: "o", Repo: "r", Ref: "release", Path: "2026-01/skills/pdf"}).Splits()
	want := []GitHubTarget{
		{Owner: "o", Repo: "r", Ref: "release", Path: "2026-01/skills/pdf"},
		{Owner: "o", Repo: "r", Ref: "release/2026-01", Path: "skills/pdf"},
		{Owner: "o", Repo: "r", Ref: "release/2026-01/skills", Path: "pdf"},
	}
	if len(got) != len(want) {
		t.Fatalf("Splits() = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Splits()[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}

	// A full SHA is unambiguous — exactly one reading.
	sha := GitHubTarget{Owner: "o", Repo: "r", Ref: strings.Repeat("b", 40), Path: "a/b/c"}
	if splits := sha.Splits(); len(splits) != 1 || splits[0] != sha {
		t.Fatalf("Splits() for a SHA ref = %+v, want [%+v]", splits, sha)
	}
}

// fakeContents scripts the Contents API. Keys are the repo-relative paths the
// fetcher requests (with the `?ref=` suffix stripped); values are the JSON
// bodies to reply with. A missing key answers 404, so an unexpected ref/path
// split behaves exactly like the real API.
type fakeContents struct {
	dirs  map[string][]folderEntry
	files map[string]string // path → raw content
	blobs map[string]string // blob SHA → raw content
	// calls records every endpoint requested so tests can assert no extra
	// traffic (and that no clone-shaped call happened).
	calls []string
}

func (f *fakeContents) runner() func(context.Context, []string) ([]byte, error) {
	return func(_ context.Context, args []string) ([]byte, error) {
		endpoint := ""
		for i, a := range args {
			if a == "-X" && i+2 < len(args) {
				endpoint = args[i+2]
			}
		}
		f.calls = append(f.calls, endpoint)
		bare := strings.SplitN(endpoint, "?", 2)[0]
		if sha, ok := strings.CutPrefix(bare, "repos/o/r/git/blobs/"); ok {
			content, ok := f.blobs[sha]
			if !ok {
				return nil, &apiError{endpoint: endpoint, status: 404, body: "HTTP 404: Not Found"}
			}
			return mustJSON(fileBlob{
				Encoding: "base64",
				Content:  base64.StdEncoding.EncodeToString([]byte(content)),
			}), nil
		}
		repoPath, ok := strings.CutPrefix(bare, "repos/o/r/contents/")
		if !ok {
			return nil, &apiError{endpoint: endpoint, status: 404, body: "HTTP 404: Not Found"}
		}
		if entries, ok := f.dirs[repoPath]; ok {
			return mustJSON(entries), nil
		}
		if content, ok := f.files[repoPath]; ok {
			return mustJSON(fileBlob{
				Encoding: "base64",
				Content:  base64.StdEncoding.EncodeToString([]byte(content)),
			}), nil
		}
		return nil, &apiError{endpoint: endpoint, status: 404, body: "HTTP 404: Not Found"}
	}
}

func mustJSON(v any) []byte {
	raw, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return raw
}

// skillFolder is the shared happy-path fixture: skills/pdf holding SKILL.md
// plus a nested scripts/ dir, sitting next to an unrelated sibling folder that
// must never be fetched.
func skillFolder() *fakeContents {
	return &fakeContents{
		dirs: map[string][]folderEntry{
			"skills/pdf": {
				{Name: "SKILL.md", Type: "file", SHA: "sha-skill"},
				{Name: "scripts", Type: "dir"},
				{Name: "link", Type: "symlink"},
			},
			"skills/pdf/scripts": {
				{Name: "run.sh", Type: "file", SHA: "sha-run"},
			},
			"skills": {
				{Name: "pdf", Type: "dir"},
				{Name: "other", Type: "dir"},
			},
		},
		files: map[string]string{
			"skills/pdf/SKILL.md":       "---\nname: pdf\n---\nBody.",
			"skills/pdf/scripts/run.sh": "#!/bin/sh\necho hi\n",
		},
	}
}

func fetchInto(t *testing.T, fake *fakeContents, rawURL string) (Folder, error) {
	t.Helper()
	target, ok := ParseGitHubURL(rawURL)
	if !ok {
		t.Fatalf("ParseGitHubURL(%q) rejected the URL", rawURL)
	}
	f := &Fetcher{Runner: fake.runner()}
	return f.FetchFolder(context.Background(), target, t.TempDir())
}

func TestFetchFolderTreeURL(t *testing.T) {
	fake := skillFolder()
	got, err := fetchInto(t, fake, "https://github.com/o/r/tree/main/skills/pdf")
	if err != nil {
		t.Fatalf("FetchFolder: %v", err)
	}
	if filepath.Base(got.Dir) != "pdf" {
		t.Errorf("Dir basename = %q, want pdf", filepath.Base(got.Dir))
	}
	wantPaths := []string{"SKILL.md", "scripts/run.sh"}
	if strings.Join(got.Paths, ",") != strings.Join(wantPaths, ",") {
		t.Errorf("Paths = %v, want %v", got.Paths, wantPaths)
	}
	body, err := os.ReadFile(filepath.Join(got.Dir, "SKILL.md"))
	if err != nil {
		t.Fatalf("read SKILL.md: %v", err)
	}
	if !strings.Contains(string(body), "name: pdf") {
		t.Errorf("SKILL.md = %q", body)
	}
	script, err := os.ReadFile(filepath.Join(got.Dir, "scripts", "run.sh"))
	if err != nil {
		t.Fatalf("read scripts/run.sh: %v", err)
	}
	if !strings.Contains(string(script), "echo hi") {
		t.Errorf("run.sh = %q", script)
	}
	// Only the requested folder is touched — the sibling is never listed.
	for _, call := range fake.calls {
		if strings.Contains(call, "skills/other") {
			t.Errorf("fetched outside the target folder: %q", call)
		}
	}
	// Every call must pin the ref so a moving branch can't mix revisions.
	for _, call := range fake.calls {
		if !strings.Contains(call, "?ref=main") {
			t.Errorf("call %q is not ref-pinned", call)
		}
	}
}

func TestFetchFolderBlobURLWithCommitSHA(t *testing.T) {
	sha := "0123456789abcdef0123456789abcdef01234567"
	fake := skillFolder()
	got, err := fetchInto(t, fake, "https://github.com/o/r/blob/"+sha+"/skills/pdf")
	if err != nil {
		t.Fatalf("FetchFolder: %v", err)
	}
	if got.Target.Ref != sha || got.Target.Path != "skills/pdf" {
		t.Errorf("Target = %+v, want ref %s path skills/pdf", got.Target, sha)
	}
	if len(got.Paths) != 2 {
		t.Errorf("Paths = %v, want 2 entries", got.Paths)
	}
	// A SHA ref is unambiguous, so no alternate ref/path split is probed.
	if n := len(fake.calls); n != 4 {
		t.Errorf("made %d calls (%v), want 4", n, fake.calls)
	}
}

func TestFetchFolderBlobURLPointingAtFile(t *testing.T) {
	// The public index links SKILL.md itself; import the folder holding it.
	fake := skillFolder()
	got, err := fetchInto(t, fake, "https://github.com/o/r/blob/main/skills/pdf/SKILL.md")
	if err != nil {
		t.Fatalf("FetchFolder: %v", err)
	}
	if got.Target.Path != "skills/pdf" {
		t.Errorf("Target.Path = %q, want skills/pdf", got.Target.Path)
	}
	if filepath.Base(got.Dir) != "pdf" {
		t.Errorf("Dir basename = %q, want pdf", filepath.Base(got.Dir))
	}
}

func TestFetchFolderResolvesSlashedBranchName(t *testing.T) {
	// `release/2026-01` is a branch, so the first split (ref "release") 404s
	// and the fetcher falls through to the next reading.
	fake := skillFolder()
	f := &Fetcher{Runner: fake.runner()}
	target := GitHubTarget{Owner: "o", Repo: "r", Ref: "release", Path: "2026-01/skills/pdf"}
	got, err := f.FetchFolder(context.Background(), target, t.TempDir())
	if err != nil {
		t.Fatalf("FetchFolder: %v", err)
	}
	if got.Target.Ref != "release/2026-01" || got.Target.Path != "skills/pdf" {
		t.Errorf("Target = %+v, want ref release/2026-01 path skills/pdf", got.Target)
	}
}

func TestFetchFolderRejectsTraversalInAPIResponse(t *testing.T) {
	// A hostile Contents response naming ../../ must not write outside the
	// fetch directory.
	for _, name := range []string{"..", "../escape.md", "../../etc/passwd", "a/b", `a\b`} {
		t.Run(name, func(t *testing.T) {
			fake := &fakeContents{
				dirs: map[string][]folderEntry{
					"skills/pdf": {{Name: name, Type: "file", SHA: "sha-x"}},
				},
				files: map[string]string{"skills/pdf/" + name: "pwned"},
			}
			dest := t.TempDir()
			target := GitHubTarget{Owner: "o", Repo: "r", Ref: "main", Path: "skills/pdf"}
			f := &Fetcher{Runner: fake.runner()}
			if _, err := f.FetchFolder(context.Background(), target, dest); err == nil {
				t.Fatalf("FetchFolder accepted unsafe entry %q", name)
			} else if !strings.Contains(err.Error(), "unsafe path") {
				t.Fatalf("error = %v, want an unsafe-path rejection", err)
			}
			// Nothing may exist above the fetch dir.
			if _, err := os.Stat(filepath.Join(dest, "escape.md")); err == nil {
				t.Fatal("traversal wrote a file above the folder dir")
			}
		})
	}
}

func TestFetchFolderRejectsTraversalInNestedDir(t *testing.T) {
	fake := &fakeContents{
		dirs: map[string][]folderEntry{
			"skills/pdf":         {{Name: "scripts", Type: "dir"}},
			"skills/pdf/scripts": {{Name: "../../pwned", Type: "file", SHA: "s"}},
		},
	}
	target := GitHubTarget{Owner: "o", Repo: "r", Ref: "main", Path: "skills/pdf"}
	f := &Fetcher{Runner: fake.runner()}
	if _, err := f.FetchFolder(context.Background(), target, t.TempDir()); err == nil {
		t.Fatal("nested traversal was accepted")
	}
}

func TestFetchFolderEmptyFolderErrors(t *testing.T) {
	fake := &fakeContents{dirs: map[string][]folderEntry{"skills/pdf": {}}}
	_, err := fetchInto(t, fake, "https://github.com/o/r/tree/main/skills/pdf")
	if err == nil {
		t.Fatal("expected an error for an empty folder")
	}
	if !strings.Contains(err.Error(), "is empty") {
		t.Fatalf("error = %v, want an 'is empty' message", err)
	}
}

func TestFetchFolderMissingFolderErrorNamesTheURL(t *testing.T) {
	fake := &fakeContents{}
	_, err := fetchInto(t, fake, "https://github.com/o/r/tree/main/skills/nope")
	if err == nil {
		t.Fatal("expected an error for a missing folder")
	}
	msg := err.Error()
	if !strings.Contains(msg, "https://github.com/o/r/tree/main/skills/nope") {
		t.Errorf("error %q does not name the requested URL", msg)
	}
	if !strings.Contains(msg, "branch, tag, or commit") {
		t.Errorf("error %q lacks actionable guidance", msg)
	}
}

func TestFetchFolderPropagatesNon404(t *testing.T) {
	f := &Fetcher{Runner: func(context.Context, []string) ([]byte, error) {
		return nil, &apiError{endpoint: "contents", status: 500, body: "HTTP 500: boom"}
	}}
	target := GitHubTarget{Owner: "o", Repo: "r", Ref: "main", Path: "skills/pdf"}
	_, err := f.FetchFolder(context.Background(), target, t.TempDir())
	var apiErr *apiError
	if !errors.As(err, &apiErr) || apiErr.status != 500 {
		t.Fatalf("err = %v, want the underlying 500 to surface", err)
	}
}

func TestFetchFolderRejectsNonFolderTarget(t *testing.T) {
	f := &Fetcher{Runner: func(context.Context, []string) ([]byte, error) {
		t.Fatal("no API call expected for a repo-level target")
		return nil, nil
	}}
	_, err := f.FetchFolder(context.Background(), GitHubTarget{Owner: "o", Repo: "r"}, t.TempDir())
	if err == nil {
		t.Fatal("expected an error for a target with no folder path")
	}
}

func TestFetchFolderHonorsMaxFiles(t *testing.T) {
	entries := make([]folderEntry, 0, 5)
	files := map[string]string{}
	for i := 0; i < 5; i++ {
		name := fmt.Sprintf("f%d.md", i)
		entries = append(entries, folderEntry{Name: name, Type: "file", SHA: name})
		files["skills/pdf/"+name] = "x"
	}
	fake := &fakeContents{dirs: map[string][]folderEntry{"skills/pdf": entries}, files: files}
	f := &Fetcher{Runner: fake.runner(), MaxFiles: 2}
	target := GitHubTarget{Owner: "o", Repo: "r", Ref: "main", Path: "skills/pdf"}
	_, err := f.FetchFolder(context.Background(), target, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "more than 2 files") {
		t.Fatalf("err = %v, want a MaxFiles rejection", err)
	}
}

func TestFetchFolderFallsBackToBlobAPIForLargeFile(t *testing.T) {
	// Files over the Contents API's inline limit come back with no content;
	// the blob API serves them.
	fake := skillFolder()
	fake.files["skills/pdf/SKILL.md"] = ""
	delete(fake.files, "skills/pdf/SKILL.md")
	fake.dirs["skills/pdf"] = []folderEntry{{Name: "SKILL.md", Type: "file", SHA: "sha-skill"}}
	fake.blobs = map[string]string{"sha-skill": "large body"}
	// Contents returns a non-base64 (empty-content) response for the file.
	prev := fake.runner()
	f := &Fetcher{Runner: func(ctx context.Context, args []string) ([]byte, error) {
		for _, a := range args {
			if strings.HasPrefix(a, "repos/o/r/contents/skills/pdf/SKILL.md") {
				return mustJSON(fileBlob{Encoding: "none", Content: ""}), nil
			}
		}
		return prev(ctx, args)
	}}
	target := GitHubTarget{Owner: "o", Repo: "r", Ref: "main", Path: "skills/pdf"}
	got, err := f.FetchFolder(context.Background(), target, t.TempDir())
	if err != nil {
		t.Fatalf("FetchFolder: %v", err)
	}
	body, err := os.ReadFile(filepath.Join(got.Dir, "SKILL.md"))
	if err != nil {
		t.Fatalf("read SKILL.md: %v", err)
	}
	if string(body) != "large body" {
		t.Errorf("SKILL.md = %q, want the blob-API content", body)
	}
}

func TestContentsEndpointEscapesSegmentsAndPinsRef(t *testing.T) {
	got := contentsEndpoint(GitHubTarget{Owner: "o", Repo: "r", Ref: "release/1 2"}, "skills/my skill")
	want := "repos/o/r/contents/skills/my%20skill?ref=release%2F1+2"
	if got != want {
		t.Errorf("contentsEndpoint = %q, want %q", got, want)
	}
	if got := contentsEndpoint(GitHubTarget{Owner: "o", Repo: "r"}, "a"); got != "repos/o/r/contents/a" {
		t.Errorf("contentsEndpoint without a ref = %q", got)
	}
}
