package registry

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const (
	// defaultMaxFolderFiles caps how many blobs one folder fetch downloads.
	// A skill folder is a handful of files; anything past this is a sign the
	// URL points at a whole source tree rather than a skill.
	defaultMaxFolderFiles = 1000
	// maxFolderDepth bounds the recursive Contents walk.
	maxFolderDepth = 32
	// maxRefCandidates bounds how many `<ref>/<path>` splits a folder fetch
	// probes when the ref may be a branch name containing slashes.
	maxRefCandidates = 4
)

var fullSHARe = regexp.MustCompile(`^[0-9a-fA-F]{40}$`)

// GitHubTarget is a parsed github.com URL: a repository plus an optional ref
// and folder path. It covers the three shapes `add` accepts as remote GitHub
// sources:
//
//	https://github.com/owner/repo                        → Ref "", Path ""
//	https://github.com/owner/repo/tree/<ref>             → Path ""
//	https://github.com/owner/repo/{tree|blob}/<ref>/<dir> → folder target
//
// `/blob/` and `/tree/` parse identically: the public skill index links skill
// folders with `/blob/`, and GitHub itself serves either form for a directory.
type GitHubTarget struct {
	Owner string
	Repo  string
	Ref   string
	Path  string
}

// ParseGitHubURL parses a github.com repository, tree, or blob URL. The
// boolean is false for any input that is not a github.com URL naming at least
// an owner and repo, and for github.com URLs that are not repository content
// links (`/pulls/1`, `/releases`, …) — callers fall back to their generic
// git-URL handling for those.
func ParseGitHubURL(raw string) (GitHubTarget, bool) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return GitHubTarget{}, false
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
	default:
		return GitHubTarget{}, false
	}
	switch strings.ToLower(u.Hostname()) {
	case "github.com", "www.github.com":
	default:
		return GitHubTarget{}, false
	}
	segs, ok := pathSegments(u.EscapedPath())
	if !ok || len(segs) < 2 {
		return GitHubTarget{}, false
	}
	t := GitHubTarget{Owner: segs[0], Repo: strings.TrimSuffix(segs[1], ".git")}
	if t.Repo == "" {
		return GitHubTarget{}, false
	}
	rest := segs[2:]
	if len(rest) == 0 {
		return t, true
	}
	// Only content links carry a ref. Anything else on github.com (issues,
	// pulls, releases, wiki) is not an importable source.
	if rest[0] != "tree" && rest[0] != "blob" || len(rest) < 2 {
		return GitHubTarget{}, false
	}
	t.Ref = rest[1]
	t.Path = strings.Join(rest[2:], "/")
	return t, true
}

// pathSegments splits an escaped URL path into decoded, non-empty segments.
// Reports false when a segment decodes to something that cannot be a repo
// path component: a separator, a traversal marker, or an empty string. This is
// the first line of defense for the remote paths this file writes to disk.
func pathSegments(escaped string) ([]string, bool) {
	var out []string
	for _, raw := range strings.Split(escaped, "/") {
		if raw == "" {
			continue
		}
		seg, err := url.PathUnescape(raw)
		if err != nil || !safeSegment(seg) {
			return nil, false
		}
		out = append(out, seg)
	}
	return out, true
}

// safeSegment reports whether a single path component is safe to join onto a
// local destination directory.
func safeSegment(seg string) bool {
	if seg == "" || seg == "." || seg == ".." {
		return false
	}
	return !strings.ContainsAny(seg, `/\`)
}

// FullName returns "owner/repo".
func (t GitHubTarget) FullName() string { return t.Owner + "/" + t.Repo }

// CloneURL returns the HTTPS clone URL for the repository.
func (t GitHubTarget) CloneURL() string {
	return "https://github.com/" + t.FullName() + ".git"
}

// WebURL renders the target back as a github.com link. Used in messages so
// the user sees the folder that was actually fetched (which can differ from
// what they pasted when the ref contained slashes).
func (t GitHubTarget) WebURL() string {
	base := "https://github.com/" + t.FullName()
	switch {
	case t.Ref == "":
		return base
	case t.Path == "":
		return base + "/tree/" + t.Ref
	default:
		return base + "/tree/" + t.Ref + "/" + t.Path
	}
}

// IsFolder reports whether the target names a path inside the repository, in
// which case only that subtree needs fetching.
func (t GitHubTarget) IsFolder() bool { return t.Path != "" }

// RefIsSHA reports whether Ref is a full 40-character commit SHA. Those
// cannot be passed to `git clone --branch`, and they make the ref/path split
// unambiguous.
func (t GitHubTarget) RefIsSHA() bool { return fullSHARe.MatchString(t.Ref) }

// Splits returns the candidate ref/path interpretations of a folder target,
// most likely first. A branch name may itself contain slashes
// (`release/2026-01`), and the URL gives no way to tell where the ref ends, so
// a caller that can probe the API tries each split in order. A full commit SHA
// is unambiguous and yields exactly one split.
func (t GitHubTarget) Splits() []GitHubTarget {
	if t.Path == "" || t.RefIsSHA() {
		return []GitHubTarget{t}
	}
	segs := strings.Split(t.Path, "/")
	out := []GitHubTarget{t}
	for i := 0; i < len(segs)-1 && len(out) < maxRefCandidates; i++ {
		out = append(out, GitHubTarget{
			Owner: t.Owner,
			Repo:  t.Repo,
			Ref:   t.Ref + "/" + strings.Join(segs[:i+1], "/"),
			Path:  strings.Join(segs[i+1:], "/"),
		})
	}
	return out
}

// Folder is the result of a subtree fetch.
type Folder struct {
	// Dir is the absolute directory the folder's contents were written into.
	// Its basename is the folder's own name so skill discovery sees the same
	// layout a clone would have produced.
	Dir string
	// Target is the ref/path split that actually resolved.
	Target GitHubTarget
	// Paths are the folder-relative slash-separated paths written, sorted.
	Paths []string
}

// Fetcher downloads a single folder out of any GitHub repository through the
// Contents API, using the authenticated `gh` CLI. It exists so `add` can
// import one skill folder out of a monorepo without cloning the repository.
type Fetcher struct {
	// GH is the path to the gh binary.
	GH string
	// Runner, when set, replaces the `gh` invocation. Tests inject a fake so
	// no process is spawned and no network call is made.
	Runner func(ctx context.Context, args []string) ([]byte, error)
	// MaxFiles caps how many files one fetch downloads. Zero means
	// defaultMaxFolderFiles.
	MaxFiles int
}

// NewFetcher locates `gh` and returns a Fetcher using it.
func NewFetcher() (*Fetcher, error) {
	gh, err := FindGH()
	if err != nil {
		return nil, err
	}
	return &Fetcher{GH: gh}, nil
}

// FetchFolder downloads target's folder (recursively, including nested
// `scripts/`, `references/`, `assets/`, …) into a new directory under
// destRoot named after the folder itself. The parent repository is never
// cloned. A `/blob/` URL that points at a file resolves to that file's
// directory.
func (f *Fetcher) FetchFolder(ctx context.Context, target GitHubTarget, destRoot string) (Folder, error) {
	if !target.IsFolder() {
		return Folder{}, fmt.Errorf("%s names no folder inside the repository", target.WebURL())
	}
	resolved, entries, err := f.resolveFolder(ctx, target)
	if err != nil {
		return Folder{}, err
	}
	dir := filepath.Join(destRoot, path.Base(resolved.Path))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Folder{}, err
	}
	var paths []string
	if err := f.walk(ctx, resolved, "", entries, dir, &paths, 0); err != nil {
		return Folder{}, err
	}
	if len(paths) == 0 {
		return Folder{}, fmt.Errorf("%s is empty", resolved.WebURL())
	}
	sort.Strings(paths)
	return Folder{Dir: dir, Target: resolved, Paths: paths}, nil
}

// resolveFolder finds the ref/path split that exists and returns its listing.
// A candidate whose root listing 404s is a wrong split (or a bad URL); any
// other failure is surfaced immediately.
func (f *Fetcher) resolveFolder(ctx context.Context, target GitHubTarget) (GitHubTarget, []folderEntry, error) {
	var notFound error
	for _, cand := range target.Splits() {
		entries, isFile, err := f.listDir(ctx, cand, cand.Path)
		if err != nil {
			if !isStatus(err, 404) {
				return GitHubTarget{}, nil, err
			}
			// Report the URL as the user wrote it, so the message names the
			// ref/path they typed rather than the last speculative split.
			if notFound == nil {
				notFound = err
			}
			continue
		}
		if !isFile {
			return cand, entries, nil
		}
		// A `/blob/` link to a file (the index links SKILL.md itself this
		// way): import the directory holding it.
		parent := path.Dir(cand.Path)
		if parent == "." || parent == "/" {
			return GitHubTarget{}, nil, fmt.Errorf("%s is a file at the repository root, not a skill folder", cand.WebURL())
		}
		cand.Path = parent
		entries, isFile, err = f.listDir(ctx, cand, cand.Path)
		if err != nil {
			return GitHubTarget{}, nil, err
		}
		if isFile {
			return GitHubTarget{}, nil, fmt.Errorf("%s is not a folder", cand.WebURL())
		}
		return cand, entries, nil
	}
	return GitHubTarget{}, nil, fmt.Errorf(
		"could not find %s on github (check the branch, tag, or commit and the folder path): %w",
		target.WebURL(), notFound)
}

// walk materializes entries under dir, recursing into subdirectories.
func (f *Fetcher) walk(ctx context.Context, t GitHubTarget, rel string, entries []folderEntry, dir string, out *[]string, depth int) error {
	if depth > maxFolderDepth {
		return fmt.Errorf("%s nests deeper than %d levels", t.WebURL(), maxFolderDepth)
	}
	for _, e := range entries {
		if !safeSegment(e.Name) {
			return fmt.Errorf("refusing unsafe path %q in %s", e.Name, t.WebURL())
		}
		childRel := path.Join(rel, e.Name)
		full, err := safeJoin(dir, childRel)
		if err != nil {
			return err
		}
		switch e.Type {
		case "dir":
			if err := f.descend(ctx, t, childRel, full, dir, out, depth); err != nil {
				return err
			}
		case "file":
			if err := f.writeFile(ctx, t, e, childRel, full, out); err != nil {
				return err
			}
		default:
			// "symlink" and "submodule" carry no importable content.
		}
	}
	return nil
}

// descend lists a subdirectory and walks it.
func (f *Fetcher) descend(ctx context.Context, t GitHubTarget, childRel, full, dir string, out *[]string, depth int) error {
	sub, isFile, err := f.listDir(ctx, t, path.Join(t.Path, childRel))
	if err != nil {
		return err
	}
	if isFile {
		return nil
	}
	if err := os.MkdirAll(full, 0o755); err != nil {
		return err
	}
	return f.walk(ctx, t, childRel, sub, dir, out, depth+1)
}

// writeFile downloads one file entry and records its folder-relative path.
func (f *Fetcher) writeFile(ctx context.Context, t GitHubTarget, e folderEntry, childRel, full string, out *[]string) error {
	limit := f.MaxFiles
	if limit <= 0 {
		limit = defaultMaxFolderFiles
	}
	if len(*out) >= limit {
		return fmt.Errorf("%s holds more than %d files; point the URL at a single skill folder", t.WebURL(), limit)
	}
	raw, err := f.fileBytes(ctx, t, e, path.Join(t.Path, childRel))
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(full, raw, 0o644); err != nil {
		return err
	}
	*out = append(*out, childRel)
	return nil
}

// safeJoin joins a folder-relative path onto dir, rejecting anything that
// would escape it. childRel already passed safeSegment per component;
// validateRelPath plus the prefix check are defense in depth against a
// hostile Contents response.
func safeJoin(dir, childRel string) (string, error) {
	clean, err := validateRelPath(childRel)
	if err != nil {
		return "", err
	}
	full := filepath.Join(dir, clean)
	rel, err := filepath.Rel(dir, full)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("rejected path escaping the fetch directory: %q", childRel)
	}
	return full, nil
}

type folderEntry struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Type string `json:"type"`
	SHA  string `json:"sha"`
}

// listDir GETs a Contents path. The API returns an array for a directory and
// an object for a file, so the bool reports which shape came back.
func (f *Fetcher) listDir(ctx context.Context, t GitHubTarget, repoPath string) ([]folderEntry, bool, error) {
	var raw json.RawMessage
	if err := f.getJSON(ctx, contentsEndpoint(t, repoPath), &raw); err != nil {
		return nil, false, err
	}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) > 0 && trimmed[0] == '{' {
		return nil, true, nil
	}
	var entries []folderEntry
	if err := json.Unmarshal(trimmed, &entries); err != nil {
		return nil, false, fmt.Errorf("unexpected contents response for %s: %w", repoPath, err)
	}
	return entries, false, nil
}

// fileBytes returns a file entry's raw content. Files over the Contents API's
// 1 MB inline limit come back with an empty body, so those fall back to the
// blob API.
func (f *Fetcher) fileBytes(ctx context.Context, t GitHubTarget, e folderEntry, repoPath string) ([]byte, error) {
	var blob fileBlob
	if err := f.getJSON(ctx, contentsEndpoint(t, repoPath), &blob); err != nil {
		return nil, err
	}
	if blob.Encoding == "base64" {
		return decodeBlob(blob)
	}
	if e.SHA == "" {
		return nil, fmt.Errorf("%s: unsupported content encoding %q", repoPath, blob.Encoding)
	}
	var large fileBlob
	if err := f.getJSON(ctx, fmt.Sprintf("repos/%s/git/blobs/%s", t.FullName(), e.SHA), &large); err != nil {
		return nil, err
	}
	if large.Encoding != "base64" {
		return nil, fmt.Errorf("%s: unsupported blob encoding %q", repoPath, large.Encoding)
	}
	return decodeBlob(large)
}

// contentsEndpoint builds a `gh api` Contents endpoint, escaping every path
// segment and pinning the ref when the URL carried one.
func contentsEndpoint(t GitHubTarget, repoPath string) string {
	var escaped []string
	for _, seg := range strings.Split(repoPath, "/") {
		if seg == "" {
			continue
		}
		escaped = append(escaped, url.PathEscape(seg))
	}
	endpoint := fmt.Sprintf("repos/%s/contents/%s", t.FullName(), strings.Join(escaped, "/"))
	if t.Ref != "" {
		endpoint += "?ref=" + url.QueryEscape(t.Ref)
	}
	return endpoint
}

func (f *Fetcher) getJSON(ctx context.Context, endpoint string, out any) error {
	body, err := f.run(ctx, []string{"api", "-X", "GET", endpoint, "-H", "Accept: application/vnd.github+json"})
	if err != nil {
		return err
	}
	if out == nil || len(bytes.TrimSpace(body)) == 0 {
		return nil
	}
	return json.Unmarshal(body, out)
}

func (f *Fetcher) run(ctx context.Context, args []string) ([]byte, error) {
	if f.Runner != nil {
		return f.Runner(ctx, args)
	}
	if f.GH == "" {
		return nil, errors.New("no gh binary configured")
	}
	cmd := exec.CommandContext(ctx, f.GH, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = strings.TrimSpace(stdout.String())
		}
		return nil, &apiError{endpoint: strings.Join(args, " "), status: parseStatus(msg), body: msg, raw: err}
	}
	return stdout.Bytes(), nil
}
