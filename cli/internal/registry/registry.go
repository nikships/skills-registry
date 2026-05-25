// Package registry talks to a GitHub-backed skill registry via `gh api`.
// Mirrors src/skills_mcp/registry_api.py so the Python MCP server and the
// Go CLI behave identically.
package registry

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// FallbackPaths mirror the Python `gh.find_gh()` list so the CLI keeps
// working in GUI-launched contexts (where PATH may be minimal).
var FallbackPaths = []string{
	"~/.local/bin/gh",
	"/opt/homebrew/bin/gh",
	"/usr/local/bin/gh",
	"/usr/bin/gh",
}

// FindGH locates a usable `gh` binary. Honors GH_BIN for tests.
func FindGH() (string, error) {
	if override := os.Getenv("GH_BIN"); override != "" {
		if _, err := os.Stat(override); err == nil {
			return override, nil
		}
	}
	if p, err := exec.LookPath("gh"); err == nil {
		return p, nil
	}
	home, _ := os.UserHomeDir()
	for _, raw := range FallbackPaths {
		path := strings.Replace(raw, "~", home, 1)
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path, nil
		}
	}
	return "", errors.New(
		"GitHub CLI (`gh`) not found on PATH or in common install locations.\n" +
			"Install from https://cli.github.com/ and run `gh auth login`.",
	)
}

// EnsureAuthed verifies `gh auth status` succeeds.
func EnsureAuthed(ctx context.Context, gh string) error {
	cmd := exec.CommandContext(ctx, gh, "auth", "status")
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("`gh` is not authenticated; run `gh auth login`")
	}
	return nil
}

// Client wraps a single registry repo.
type Client struct {
	GH            string // path to gh binary
	Repo          string // "owner/repo"
	DefaultBranch string
	MaxRetries    int
	RetryBaseS    float64
	// Workers caps the concurrent blob uploads in PushTree / Publish.
	// Defaults to 8 when zero.
	Workers int
	// OnProgress, if set, is called after each file is uploaded during a
	// PushTree or Publish / PushTreeViaGit operation. `done` is the cumulative
	// count, `total` is the operation's total file count. Useful for progress
	// bars.
	OnProgress func(done, total int)
	// OnStatus, if set, is called by long-running operations to surface a
	// short human-readable status string (e.g. "pushing to github…").
	// Distinct from OnProgress so the caller only emits the message when work
	// is actually about to happen (a `git push` won't fire on a no-op
	// PushTreeViaGit call where the working tree matches the remote).
	OnStatus func(msg string)
	// HTTPSURL, if set, overrides the default `https://github.com/<repo>.git`
	// remote URL used by PushTreeViaGit and the read-side mirror clone.
	// Tests set this to a local bare repo path; production callers leave it
	// empty.
	HTTPSURL string
	// GitBin, if set, overrides exec.LookPath("git"). Tests inject this; in
	// production callers leave it empty.
	GitBin string
	// MirrorRoot, if set, overrides the default
	// ~/.cache/skills-mcp/mirror/<owner>/<repo> directory used by the
	// git-backed read mirror. Tests inject a t.TempDir() so suites never
	// touch the user's real cache; production callers leave it empty.
	MirrorRoot string
}

// New constructs a client with sensible defaults. Looks up `gh` if not set.
func New(repo, branch string) (*Client, error) {
	gh, err := FindGH()
	if err != nil {
		return nil, err
	}
	if branch == "" {
		branch = "main"
	}
	return &Client{
		GH:            gh,
		Repo:          repo,
		DefaultBranch: branch,
		MaxRetries:    3,
		RetryBaseS:    0.5,
		Workers:       8,
	}, nil
}

// Summary is one row in the listing.
type Summary struct {
	Slug        string
	Name        string
	Description string
	TreeSHA     string
}

// Slugs returns a set of every top-level slug in the registry. Reads
// from the local git mirror when available (see mirror.go); falls back
// to a single `gh api repos/<r>/contents/` call when the mirror is
// disabled or the clone fails.
func (c *Client) Slugs(ctx context.Context) (map[string]struct{}, error) {
	if c.mirrorEnabled() {
		if out, err := c.slugsViaMirror(ctx); err == nil {
			return out, nil
		}
	}
	return c.slugsViaAPI(ctx)
}

func (c *Client) slugsViaAPI(ctx context.Context) (map[string]struct{}, error) {
	entries, err := c.contents(ctx, "")
	if err != nil {
		return nil, err
	}
	out := map[string]struct{}{}
	for _, e := range entries {
		if e.Type == "dir" && !strings.HasPrefix(e.Name, ".") {
			out[e.Name] = struct{}{}
		}
	}
	return out, nil
}

// List enumerates registry skills with their summaries. Reads from the
// local git mirror when available; falls back to the 1 + N gh-api walk
// when the mirror is disabled (SKILLS_MIRROR_DISABLE) or `git` is
// unavailable. The fallback exists so the CLI keeps working on hosts
// without a usable `git` binary — at the cost of being slow.
func (c *Client) List(ctx context.Context) ([]Summary, error) {
	if c.mirrorEnabled() {
		if out, err := c.listViaMirror(ctx); err == nil {
			return out, nil
		}
	}
	return c.listViaAPI(ctx)
}

func (c *Client) listViaAPI(ctx context.Context) ([]Summary, error) {
	entries, err := c.contents(ctx, "")
	if err != nil {
		return nil, err
	}
	var out []Summary
	for _, e := range entries {
		if e.Type != "dir" || strings.HasPrefix(e.Name, ".") {
			continue
		}
		s, ok, err := c.summarize(ctx, e.Name, e.SHA)
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, s)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Slug < out[j].Slug })
	return out, nil
}

func (c *Client) summarize(ctx context.Context, slug, treeSHA string) (Summary, bool, error) {
	var blob fileBlob
	if err := c.getJSON(ctx, fmt.Sprintf("repos/%s/contents/%s/SKILL.md", c.Repo, slug), &blob); err != nil {
		if isStatus(err, 404) {
			return Summary{}, false, nil
		}
		return Summary{}, false, err
	}
	if blob.Encoding != "base64" {
		return Summary{}, false, nil
	}
	raw, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(blob.Content, "\n", ""))
	if err != nil {
		return Summary{}, false, err
	}
	name, desc := parseSummary(string(raw), slug)
	return Summary{
		Slug:        slug,
		Name:        name,
		Description: desc,
		TreeSHA:     treeSHA,
	}, true, nil
}

// Get downloads the full <slug>/ folder into dest. Existing files are
// overwritten. Reads from the local git mirror when available; falls
// back to the recursive gh-api walk otherwise.
func (c *Client) Get(ctx context.Context, slug, dest string) error {
	if c.mirrorEnabled() {
		if err := c.getViaMirror(ctx, slug, dest); err == nil {
			return nil
		}
	}
	return c.downloadRecursive(ctx, slug, dest)
}

func (c *Client) downloadRecursive(ctx context.Context, repoPath, destDir string) error {
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return err
	}
	entries, err := c.contents(ctx, repoPath)
	if err != nil {
		return err
	}
	for _, e := range entries {
		switch e.Type {
		case "dir":
			sub := filepath.Join(destDir, e.Name)
			if err := c.downloadRecursive(ctx, repoPath+"/"+e.Name, sub); err != nil {
				return err
			}
		case "file":
			var blob fileBlob
			if err := c.getJSON(ctx, fmt.Sprintf("repos/%s/contents/%s/%s", c.Repo, repoPath, e.Name), &blob); err != nil {
				return err
			}
			if blob.Encoding != "base64" {
				continue
			}
			raw, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(blob.Content, "\n", ""))
			if err != nil {
				return err
			}
			if err := os.WriteFile(filepath.Join(destDir, e.Name), raw, 0o644); err != nil {
				return err
			}
		}
	}
	return nil
}

// ErrSlugNotFound is returned by Delete when the requested slug doesn't
// exist in the registry. Callers (e.g. the `remove` subcommand) treat
// this as a clean exit-1 condition rather than a generic API failure.
var ErrSlugNotFound = errors.New("slug not found in registry")

// Delete atomically removes the entire <slug>/ subtree from the
// registry. Returns the new commit SHA, or ErrSlugNotFound if the slug
// has no files in the current tree. Retries on 409/422 with the same
// exponential backoff Publish uses.
//
// The Git Data API call sequence mirrors Publish (so writers and
// deleters can't race their way into a corrupt tree):
//
//	GET  refs/heads/<branch>             → parent SHA
//	GET  commits/<parent>                → base tree SHA
//	GET  trees/<base>?recursive=1        → list blobs under <slug>/
//	POST trees (base_tree + null SHAs)   → new tree without those blobs
//	POST commits (msg, tree, parents)    → new commit
//	PATCH refs/heads/<branch>            → fast-forward ref
func (c *Client) Delete(ctx context.Context, slug string) (string, error) {
	var lastErr error
	for attempt := 0; attempt < c.MaxRetries; attempt++ {
		sha, err := c.deleteOnce(ctx, slug)
		if err == nil {
			return sha, nil
		}
		if errors.Is(err, ErrSlugNotFound) {
			return "", err
		}
		if !isStatus(err, 409) && !isStatus(err, 422) {
			return "", err
		}
		lastErr = err
		delay := time.Duration(c.RetryBaseS*float64(int(1)<<attempt)*1000) * time.Millisecond
		time.Sleep(delay)
	}
	return "", fmt.Errorf("delete %q: conflict after %d retries: %w", slug, c.MaxRetries, lastErr)
}

func (c *Client) deleteOnce(ctx context.Context, slug string) (string, error) {
	var ref struct {
		Object struct {
			SHA string `json:"sha"`
		} `json:"object"`
	}
	if err := c.getJSON(ctx, fmt.Sprintf("repos/%s/git/ref/heads/%s", c.Repo, c.DefaultBranch), &ref); err != nil {
		return "", err
	}
	parentSHA := ref.Object.SHA

	var commit struct {
		Tree struct {
			SHA string `json:"sha"`
		} `json:"tree"`
	}
	if err := c.getJSON(ctx, fmt.Sprintf("repos/%s/git/commits/%s", c.Repo, parentSHA), &commit); err != nil {
		return "", err
	}
	baseTreeSHA := commit.Tree.SHA

	previous, err := c.listTreePaths(ctx, baseTreeSHA, slug)
	if err != nil {
		return "", err
	}
	if len(previous) == 0 {
		return "", ErrSlugNotFound
	}

	// Deterministic ordering keeps the resulting tree payload identical
	// across runs — matters for testability and for any caller that
	// hashes the request body.
	staleKeys := make([]string, 0, len(previous))
	for k := range previous {
		staleKeys = append(staleKeys, k)
	}
	sort.Strings(staleKeys)
	entries := make([]treeEntry, 0, len(staleKeys))
	for _, stale := range staleKeys {
		entries = append(entries, treeEntry{
			Path: slug + "/" + stale,
			Mode: "100644",
			Type: "blob",
			SHA:  nil,
		})
	}

	treePayload, _ := json.Marshal(map[string]any{
		"base_tree": baseTreeSHA,
		"tree":      entries,
	})
	var newTree struct {
		SHA string `json:"sha"`
	}
	if err := c.postJSON(ctx, fmt.Sprintf("repos/%s/git/trees", c.Repo), treePayload, &newTree); err != nil {
		return "", err
	}

	commitPayload, _ := json.Marshal(map[string]any{
		"message": "remove: " + slug,
		"tree":    newTree.SHA,
		"parents": []string{parentSHA},
	})
	var newCommit struct {
		SHA string `json:"sha"`
	}
	if err := c.postJSON(ctx, fmt.Sprintf("repos/%s/git/commits", c.Repo), commitPayload, &newCommit); err != nil {
		return "", err
	}

	refPayload, _ := json.Marshal(map[string]any{
		"sha":   newCommit.SHA,
		"force": false,
	})
	if err := c.patchJSON(ctx, fmt.Sprintf("repos/%s/git/refs/heads/%s", c.Repo, c.DefaultBranch), refPayload, nil); err != nil {
		return "", err
	}
	return newCommit.SHA, nil
}

// Publish atomically replaces <slug>/ with files (path → bytes).
// Returns the new commit SHA. Retries on 409/422 (non-fast-forward).
func (c *Client) Publish(ctx context.Context, slug string, files map[string][]byte, message string) (string, error) {
	if message == "" {
		message = "publish: " + slug
	}
	var lastErr error
	for attempt := 0; attempt < c.MaxRetries; attempt++ {
		sha, err := c.publishOnce(ctx, slug, files, message)
		if err == nil {
			return sha, nil
		}
		if !isStatus(err, 409) && !isStatus(err, 422) {
			return "", err
		}
		lastErr = err
		delay := time.Duration(c.RetryBaseS*float64(int(1)<<attempt)*1000) * time.Millisecond
		time.Sleep(delay)
	}
	return "", fmt.Errorf("publish %q: conflict after %d retries: %w", slug, c.MaxRetries, lastErr)
}

func (c *Client) publishOnce(ctx context.Context, slug string, files map[string][]byte, message string) (string, error) {
	var ref struct {
		Object struct {
			SHA string `json:"sha"`
		} `json:"object"`
	}
	if err := c.getJSON(ctx, fmt.Sprintf("repos/%s/git/ref/heads/%s", c.Repo, c.DefaultBranch), &ref); err != nil {
		return "", err
	}
	parentSHA := ref.Object.SHA

	var commit struct {
		Tree struct {
			SHA string `json:"sha"`
		} `json:"tree"`
	}
	if err := c.getJSON(ctx, fmt.Sprintf("repos/%s/git/commits/%s", c.Repo, parentSHA), &commit); err != nil {
		return "", err
	}
	baseTreeSHA := commit.Tree.SHA

	previous, err := c.listTreePaths(ctx, baseTreeSHA, slug)
	if err != nil {
		return "", err
	}

	normalized := make(map[string][]byte, len(files))
	incoming := make(map[string]struct{}, len(files))
	for rel, content := range files {
		rel = strings.ReplaceAll(rel, string(filepath.Separator), "/")
		rel = strings.TrimPrefix(rel, "/")
		normalized[rel] = content
		incoming[rel] = struct{}{}
	}
	blobs, err := c.uploadBlobs(ctx, normalized, 0, len(normalized))
	if err != nil {
		return "", err
	}
	entries := make([]treeEntry, 0, len(normalized)+len(previous))
	for rel, sha := range blobs {
		sha := sha
		entries = append(entries, treeEntry{
			Path: slug + "/" + rel,
			Mode: "100644",
			Type: "blob",
			SHA:  &sha,
		})
	}
	staleKeys := make([]string, 0, len(previous))
	for k := range previous {
		if _, ok := incoming[k]; !ok {
			staleKeys = append(staleKeys, k)
		}
	}
	sort.Strings(staleKeys)
	for _, stale := range staleKeys {
		entries = append(entries, treeEntry{
			Path: slug + "/" + stale,
			Mode: "100644",
			Type: "blob",
			SHA:  nil,
		})
	}

	treePayload, _ := json.Marshal(map[string]any{
		"base_tree": baseTreeSHA,
		"tree":      entries,
	})
	var newTree struct {
		SHA string `json:"sha"`
	}
	if err := c.postJSON(ctx, fmt.Sprintf("repos/%s/git/trees", c.Repo), treePayload, &newTree); err != nil {
		return "", err
	}

	commitPayload, _ := json.Marshal(map[string]any{
		"message": message,
		"tree":    newTree.SHA,
		"parents": []string{parentSHA},
	})
	var newCommit struct {
		SHA string `json:"sha"`
	}
	if err := c.postJSON(ctx, fmt.Sprintf("repos/%s/git/commits", c.Repo), commitPayload, &newCommit); err != nil {
		return "", err
	}

	refPayload, _ := json.Marshal(map[string]any{
		"sha":   newCommit.SHA,
		"force": false,
	})
	if err := c.patchJSON(ctx, fmt.Sprintf("repos/%s/git/refs/heads/%s", c.Repo, c.DefaultBranch), refPayload, nil); err != nil {
		return "", err
	}
	return newCommit.SHA, nil
}

func (c *Client) listTreePaths(ctx context.Context, rootSHA, subPath string) (map[string]struct{}, error) {
	var resp struct {
		Tree []struct {
			Path string `json:"path"`
			Type string `json:"type"`
		} `json:"tree"`
	}
	endpoint := fmt.Sprintf("repos/%s/git/trees/%s?recursive=1", c.Repo, rootSHA)
	if err := c.getJSON(ctx, endpoint, &resp); err != nil {
		if isStatus(err, 404) {
			return map[string]struct{}{}, nil
		}
		return nil, err
	}
	prefix := subPath + "/"
	out := map[string]struct{}{}
	for _, e := range resp.Tree {
		if e.Type != "blob" || !strings.HasPrefix(e.Path, prefix) {
			continue
		}
		out[e.Path[len(prefix):]] = struct{}{}
	}
	return out, nil
}

// Exists reports whether the configured repo is visible to the authenticated
// user. A 404 maps to (false, nil) so callers can distinguish "deleted /
// renamed / never created" from real network or auth failures.
func (c *Client) Exists(ctx context.Context) (bool, error) {
	var resp struct {
		Name string `json:"name"`
	}
	if err := c.getJSON(ctx, "repos/"+c.Repo, &resp); err != nil {
		if isStatus(err, 404) {
			return false, nil
		}
		return false, err
	}
	return resp.Name != "", nil
}

// CreateRepo creates a new repo on the authenticated user's account.
// Returns "owner/name". Honors visibility ("public" or "private").
func (c *Client) CreateRepo(ctx context.Context, name, visibility, description string) (string, error) {
	if visibility != "public" && visibility != "private" {
		return "", fmt.Errorf("visibility must be public or private, got %q", visibility)
	}
	args := []string{"repo", "create", name, "--" + visibility, "--description", description}
	out, err := c.runGH(ctx, args, nil)
	if err != nil {
		return "", err
	}
	// `gh repo create` prints the new owner/name URL on stdout.
	line := strings.TrimSpace(out)
	// Strip leading scheme/path so we land at owner/name.
	if u, perr := url.Parse(line); perr == nil && u.Host != "" {
		return strings.TrimPrefix(u.Path, "/"), nil
	}
	return line, nil
}

// PushTreeViaGit commits and pushes a tree of files using the local `git`
// binary over HTTPS, authenticated by the user's `gh` token.
//
// This is the preferred bulk-upload path (used by `bootstrap`) because it
// sends every file in a single `git push` instead of N blob POSTs through the
// REST API. The REST blob path trips GitHub's secondary rate limit at ~80
// POSTs/minute, which is easy to hit on first-time registries with dozens of
// skills.
//
// Requirements:
//   - `git` available on PATH (or `Client.GitBin` set).
//   - `gh auth status` already verified (the caller's responsibility).
//   - Network access to https://github.com.
//
// Behavior:
//   - If the remote branch already exists, the repo is shallow-cloned and the
//     supplied files are written on top of the existing tree (additions or
//     overwrites only — this method does not delete files).
//   - If the remote branch is missing (brand-new repo from `gh repo create`),
//     a fresh git workdir is initialized and pushed as the initial commit.
//   - When the resulting working tree is identical to the remote, no commit
//     is created and PushTreeViaGit returns (nil, "no-op").
//
// Files are accepted as repo-relative path → content. Paths containing `..`
// are rejected to match the validation applied to the REST blob path.
//
// Progress reporting: c.OnProgress fires once per file during the local-write
// phase (`done` = number of files written, `total` = len(files)). The final
// `git push` itself is opaque.
func (c *Client) PushTreeViaGit(ctx context.Context, files map[string][]byte, message string) error {
	if len(files) == 0 {
		return errors.New("nothing to push")
	}
	gitBin, err := c.resolveGitBin()
	if err != nil {
		return err
	}
	if err := c.setupGitAuth(ctx); err != nil {
		return fmt.Errorf("configure git credentials via gh: %w", err)
	}
	name, email, err := c.gitAuthor(ctx)
	if err != nil {
		return fmt.Errorf("resolve git author: %w", err)
	}
	work, err := os.MkdirTemp("", "skills-registry-push-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(work)
	if err := c.initWorkdir(ctx, gitBin, work); err != nil {
		return err
	}
	if err := configureGitRepo(ctx, gitBin, work, name, email); err != nil {
		return err
	}
	if err := c.writeFilesToWorkdir(work, files); err != nil {
		return err
	}
	return c.commitAndPushIfChanged(ctx, gitBin, work, message)
}

// resolveGitBin returns the git binary path from Client.GitBin or PATH.
func (c *Client) resolveGitBin() (string, error) {
	if c.GitBin != "" {
		return c.GitBin, nil
	}
	found, err := exec.LookPath("git")
	if err != nil {
		return "", errors.New(
			"git not found on PATH. install git from https://git-scm.com/downloads " +
				"(macOS: `brew install git`, Linux: `apt install git` / `dnf install git`) and re-run.")
	}
	return found, nil
}

// runGitIn executes a git command in the given directory, returning a
// formatted error on failure. Replaces the closure that PushTreeViaGit
// previously defined inline.
func runGitIn(ctx context.Context, gitBin, dir string, args ...string) error {
	cmd := exec.CommandContext(ctx, gitBin, args...)
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	cmd.Stdout = io.Discard
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git %s: %s", strings.Join(args, " "), strings.TrimSpace(stderr.String()))
	}
	return nil
}

// initWorkdir either shallow-clones an existing remote branch or initializes
// a fresh git workdir in `work`.
func (c *Client) initWorkdir(ctx context.Context, gitBin, work string) error {
	remoteURL := c.HTTPSURL
	if remoteURL == "" {
		remoteURL = "https://github.com/" + c.Repo + ".git"
	}
	branchExists, err := c.refExists(ctx)
	if err != nil {
		return err
	}
	if branchExists {
		clone := exec.CommandContext(ctx, gitBin,
			"clone", "--depth", "1", "--branch", c.DefaultBranch, remoteURL, work)
		var stderr bytes.Buffer
		clone.Stderr = &stderr
		clone.Stdout = io.Discard
		if err := clone.Run(); err != nil {
			return fmt.Errorf("git clone %s: %s", c.Repo, strings.TrimSpace(stderr.String()))
		}
		return nil
	}
	if err := runGitIn(ctx, gitBin, work, "init", "-b", c.DefaultBranch); err != nil {
		return err
	}
	return runGitIn(ctx, gitBin, work, "remote", "add", "origin", remoteURL)
}

// configureGitRepo sets user.name, user.email, and disables GPG signing in
// the given workdir.
func configureGitRepo(ctx context.Context, gitBin, work, name, email string) error {
	if err := runGitIn(ctx, gitBin, work, "config", "user.name", name); err != nil {
		return err
	}
	if err := runGitIn(ctx, gitBin, work, "config", "user.email", email); err != nil {
		return err
	}
	return runGitIn(ctx, gitBin, work, "config", "commit.gpgsign", "false")
}

// validateRelPath normalizes a repo-relative path and rejects traversal
// attacks, absolute paths, and empty strings. Mirrors
// registry_api._normalize_rel_path.
func validateRelPath(rel string) (string, error) {
	rel = strings.ReplaceAll(rel, string(filepath.Separator), "/")
	if rel == "" {
		return "", errors.New("rejected empty relative path")
	}
	if strings.HasPrefix(rel, "/") {
		return "", fmt.Errorf("rejected absolute path: %q", rel)
	}
	clean := filepath.ToSlash(filepath.Clean(rel))
	if clean == ".." || strings.HasPrefix(clean, "../") || strings.Contains(clean, "/../") {
		return "", fmt.Errorf("rejected path containing ..: %q", rel)
	}
	osClean := filepath.FromSlash(clean)
	if filepath.IsAbs(osClean) || filepath.VolumeName(osClean) != "" {
		return "", fmt.Errorf("rejected absolute path: %q", rel)
	}
	return osClean, nil
}

// writeFilesToWorkdir validates and materializes every file under the working
// tree, reporting progress via c.OnProgress.
func (c *Client) writeFilesToWorkdir(work string, files map[string][]byte) error {
	written := 0
	total := len(files)
	for rel, content := range files {
		osClean, err := validateRelPath(rel)
		if err != nil {
			return err
		}
		full := filepath.Join(work, osClean)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(full, content, 0o644); err != nil {
			return err
		}
		written++
		if c.OnProgress != nil {
			c.OnProgress(written, total)
		}
	}
	return nil
}

// commitAndPushIfChanged stages all changes, skips if nothing changed,
// otherwise commits and pushes to the remote.
func (c *Client) commitAndPushIfChanged(ctx context.Context, gitBin, work, message string) error {
	if err := runGitIn(ctx, gitBin, work, "add", "-A"); err != nil {
		return err
	}
	statusCmd := exec.CommandContext(ctx, gitBin, "status", "--porcelain")
	statusCmd.Dir = work
	statusOut, err := statusCmd.Output()
	if err != nil {
		return fmt.Errorf("git status: %w", err)
	}
	if len(strings.TrimSpace(string(statusOut))) == 0 {
		return nil
	}
	if err := runGitIn(ctx, gitBin, work, "commit", "-m", message); err != nil {
		return err
	}
	if c.OnStatus != nil {
		c.OnStatus("pushing to github…")
	}
	return runGitIn(ctx, gitBin, work, "push", "-u", "origin", c.DefaultBranch)
}

// setupGitAuth wires `gh` in as git's HTTPS credential helper for github.com.
// Idempotent.
func (c *Client) setupGitAuth(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, c.GH, "auth", "setup-git", "--hostname", "github.com")
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	return cmd.Run()
}

// gitAuthor resolves (name, email) for the commit author from the
// authenticated GitHub identity. Falls back to `login` and the GitHub
// no-reply email pattern when the user hasn't exposed a name/email.
func (c *Client) gitAuthor(ctx context.Context) (string, string, error) {
	var u struct {
		Login string `json:"login"`
		Name  string `json:"name"`
		Email string `json:"email"`
	}
	if err := c.GetJSON(ctx, "user", &u); err != nil {
		return "", "", err
	}
	if u.Login == "" {
		return "", "", errors.New("could not determine GitHub login")
	}
	name := u.Name
	if name == "" {
		name = u.Login
	}
	email := u.Email
	if email == "" {
		email = u.Login + "@users.noreply.github.com"
	}
	return name, email, nil
}

// refExists reports whether `<DefaultBranch>` exists on the remote. New repos
// created by `gh repo create` (without --add-readme) have no branches at all.
func (c *Client) refExists(ctx context.Context) (bool, error) {
	var resp struct {
		Object struct {
			SHA string `json:"sha"`
		} `json:"object"`
	}
	err := c.getJSON(ctx, fmt.Sprintf("repos/%s/git/ref/heads/%s", c.Repo, c.DefaultBranch), &resp)
	if err == nil {
		return resp.Object.SHA != "", nil
	}
	if isStatus(err, 404) || isStatus(err, 409) {
		return false, nil
	}
	return false, err
}

// PushTree commits a set of files to the repo using the atomic Git Data API
// path (per-file blob POSTs + tree + commit + ref update). Retained for the
// callers that already worked with it; `bootstrap` now prefers PushTreeViaGit
// to avoid GitHub's secondary rate limit on large initial imports.
// Blob uploads run in parallel (see Client.Workers); progress is reported via
// Client.OnProgress.
func (c *Client) PushTree(ctx context.Context, files map[string][]byte, message string) (string, error) {
	normalized := make(map[string][]byte, len(files))
	for rel, content := range files {
		rel = strings.ReplaceAll(rel, string(filepath.Separator), "/")
		rel = strings.TrimPrefix(rel, "/")
		normalized[rel] = content
	}
	total := len(normalized)
	return c.pushTree(ctx, normalized, message, 0, total)
}

// pushTree is the internal worker. `alreadyDone` lets a caller (typically
// bootstrapInitialCommit) start counting progress from a non-zero base so the
// "X/total" display stays monotonic across the PUT + Git-Data-API steps.
func (c *Client) pushTree(ctx context.Context, files map[string][]byte, message string, alreadyDone, total int) (string, error) {
	var ref struct {
		Object struct {
			SHA string `json:"sha"`
		} `json:"object"`
	}
	if err := c.getJSON(ctx, fmt.Sprintf("repos/%s/git/ref/heads/%s", c.Repo, c.DefaultBranch), &ref); err != nil {
		if !isStatus(err, 404) && !isStatus(err, 409) {
			return "", err
		}
		return c.bootstrapInitialCommit(ctx, files, message, total)
	}
	parentSHA := ref.Object.SHA

	var commit struct {
		Tree struct {
			SHA string `json:"sha"`
		} `json:"tree"`
	}
	if err := c.getJSON(ctx, fmt.Sprintf("repos/%s/git/commits/%s", c.Repo, parentSHA), &commit); err != nil {
		return "", err
	}
	baseTreeSHA := commit.Tree.SHA

	blobs, err := c.uploadBlobs(ctx, files, alreadyDone, total)
	if err != nil {
		return "", err
	}
	entries := make([]treeEntry, 0, len(files))
	for rel, sha := range blobs {
		sha := sha
		entries = append(entries, treeEntry{
			Path: rel,
			Mode: "100644",
			Type: "blob",
			SHA:  &sha,
		})
	}

	treePayload, _ := json.Marshal(map[string]any{
		"base_tree": baseTreeSHA,
		"tree":      entries,
	})
	var newTree struct {
		SHA string `json:"sha"`
	}
	if err := c.postJSON(ctx, fmt.Sprintf("repos/%s/git/trees", c.Repo), treePayload, &newTree); err != nil {
		return "", err
	}

	commitPayload, _ := json.Marshal(map[string]any{
		"message": message,
		"tree":    newTree.SHA,
		"parents": []string{parentSHA},
	})
	var newCommit struct {
		SHA string `json:"sha"`
	}
	if err := c.postJSON(ctx, fmt.Sprintf("repos/%s/git/commits", c.Repo), commitPayload, &newCommit); err != nil {
		return "", err
	}

	refPayload, _ := json.Marshal(map[string]any{
		"sha":   newCommit.SHA,
		"force": false,
	})
	if err := c.patchJSON(ctx, fmt.Sprintf("repos/%s/git/refs/heads/%s", c.Repo, c.DefaultBranch), refPayload, nil); err != nil {
		return "", err
	}
	return newCommit.SHA, nil
}

// bootstrapInitialCommit handles repos with no commits yet (no main ref).
// `gh repo create` produces an empty repo, so we create the initial commit
// via "create or update file" which auto-creates the branch.
func (c *Client) bootstrapInitialCommit(ctx context.Context, files map[string][]byte, message string, total int) (string, error) {
	// For a brand-new repo, we need at least one PUT to seed the ref. Use the
	// first file alphabetically; the rest go via pushTree once main exists.
	var keys []string
	for k := range files {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if len(keys) == 0 {
		return "", errors.New("nothing to push")
	}
	first := keys[0]
	body, _ := json.Marshal(map[string]any{
		"message": message,
		"content": base64.StdEncoding.EncodeToString(files[first]),
		"branch":  c.DefaultBranch,
	})
	var resp struct {
		Commit struct {
			SHA string `json:"sha"`
		} `json:"commit"`
	}
	if err := c.putJSON(ctx, fmt.Sprintf("repos/%s/contents/%s", c.Repo, first), body, &resp); err != nil {
		return "", err
	}
	if c.OnProgress != nil {
		c.OnProgress(1, total)
	}
	if len(keys) == 1 {
		return resp.Commit.SHA, nil
	}
	remaining := make(map[string][]byte, len(keys)-1)
	for _, k := range keys[1:] {
		remaining[k] = files[k]
	}
	return c.pushTree(ctx, remaining, message, 1, total)
}

// uploadBlobs uploads each (rel, content) pair as a Git blob in parallel and
// returns rel→sha. Concurrency is bounded by c.Workers (default 8). Progress
// is reported via c.OnProgress after each successful upload, with `done`
// starting at `alreadyDone+1` and increasing to `alreadyDone+len(files)`.
func (c *Client) uploadBlobs(ctx context.Context, files map[string][]byte, alreadyDone, total int) (map[string]string, error) {
	if len(files) == 0 {
		return map[string]string{}, nil
	}
	workers := c.Workers
	if workers <= 0 {
		workers = 8
	}
	if workers > len(files) {
		workers = len(files)
	}

	type job struct {
		rel     string
		content []byte
	}
	type result struct {
		rel string
		sha string
		err error
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	jobs := make(chan job)
	results := make(chan result, len(files))

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				body, _ := json.Marshal(map[string]string{
					"content":  base64.StdEncoding.EncodeToString(j.content),
					"encoding": "base64",
				})
				var blob struct {
					SHA string `json:"sha"`
				}
				err := c.postJSON(ctx, fmt.Sprintf("repos/%s/git/blobs", c.Repo), body, &blob)
				select {
				case results <- result{rel: j.rel, sha: blob.SHA, err: err}:
				case <-ctx.Done():
					return
				}
			}
		}()
	}

	go func() {
		defer close(jobs)
		for rel, content := range files {
			select {
			case <-ctx.Done():
				return
			case jobs <- job{rel: rel, content: content}:
			}
		}
	}()

	go func() {
		wg.Wait()
		close(results)
	}()

	out := make(map[string]string, len(files))
	var counter int64 = int64(alreadyDone)
	for r := range results {
		if r.err != nil {
			cancel()
			// drain the rest
			for range results {
			}
			return nil, r.err
		}
		out[r.rel] = r.sha
		done := atomic.AddInt64(&counter, 1)
		if c.OnProgress != nil {
			c.OnProgress(int(done), total)
		}
	}
	return out, nil
}

// treeEntry is one row in a Git Data API tree payload. Hoisted to package
// scope so both publishOnce and pushTree share it.
type treeEntry struct {
	Path string  `json:"path"`
	Mode string  `json:"mode"`
	Type string  `json:"type"`
	SHA  *string `json:"sha"`
}

// GetJSON exposes the internal `gh api -X GET` helper so callers can hit
// arbitrary endpoints (e.g. `/user`) without duplicating the gh plumbing.
func (c *Client) GetJSON(ctx context.Context, endpoint string, out any) error {
	return c.getJSON(ctx, endpoint, out)
}

// ---------------------------------------------------------------- gh plumbing

type contentEntry struct {
	Name string `json:"name"`
	Type string `json:"type"` // "dir" or "file"
	SHA  string `json:"sha"`
}

type fileBlob struct {
	Encoding string `json:"encoding"`
	Content  string `json:"content"`
}

func (c *Client) contents(ctx context.Context, path string) ([]contentEntry, error) {
	endpoint := fmt.Sprintf("repos/%s/contents/%s", c.Repo, path)
	var entries []contentEntry
	if err := c.getJSON(ctx, endpoint, &entries); err != nil {
		if isStatus(err, 404) {
			return nil, nil
		}
		return nil, err
	}
	return entries, nil
}

func (c *Client) getJSON(ctx context.Context, endpoint string, out any) error {
	body, err := c.runGH(ctx, []string{"api", "-X", "GET", endpoint, "-H", "Accept: application/vnd.github+json"}, nil)
	if err != nil {
		return err
	}
	if out == nil || strings.TrimSpace(body) == "" {
		return nil
	}
	return json.Unmarshal([]byte(body), out)
}

func (c *Client) postJSON(ctx context.Context, endpoint string, payload []byte, out any) error {
	body, err := c.runGH(ctx, []string{"api", "-X", "POST", endpoint, "-H", "Accept: application/vnd.github+json", "--input", "-"}, payload)
	if err != nil {
		return err
	}
	if out == nil || strings.TrimSpace(body) == "" {
		return nil
	}
	return json.Unmarshal([]byte(body), out)
}

func (c *Client) patchJSON(ctx context.Context, endpoint string, payload []byte, out any) error {
	body, err := c.runGH(ctx, []string{"api", "-X", "PATCH", endpoint, "-H", "Accept: application/vnd.github+json", "--input", "-"}, payload)
	if err != nil {
		return err
	}
	if out == nil || strings.TrimSpace(body) == "" {
		return nil
	}
	return json.Unmarshal([]byte(body), out)
}

func (c *Client) putJSON(ctx context.Context, endpoint string, payload []byte, out any) error {
	body, err := c.runGH(ctx, []string{"api", "-X", "PUT", endpoint, "-H", "Accept: application/vnd.github+json", "--input", "-"}, payload)
	if err != nil {
		return err
	}
	if out == nil || strings.TrimSpace(body) == "" {
		return nil
	}
	return json.Unmarshal([]byte(body), out)
}

func (c *Client) runGH(ctx context.Context, args []string, stdin []byte) (string, error) {
	cmd := exec.CommandContext(ctx, c.GH, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = strings.TrimSpace(stdout.String())
		}
		return "", &apiError{endpoint: strings.Join(args, " "), status: parseStatus(msg), body: msg, raw: err}
	}
	return stdout.String(), nil
}

type apiError struct {
	endpoint string
	status   int
	body     string
	raw      error
}

func (e *apiError) Error() string {
	return fmt.Sprintf("gh %s failed (status %d): %s", e.endpoint, e.status, e.body)
}

func isStatus(err error, want int) bool {
	var apiErr *apiError
	if errors.As(err, &apiErr) {
		return apiErr.status == want
	}
	return false
}

var statusRe = regexp.MustCompile(`\b([1-5][0-9]{2})\b`)

func parseStatus(body string) int {
	m := statusRe.FindStringSubmatch(body)
	if m == nil {
		return 0
	}
	var n int
	fmt.Sscanf(m[1], "%d", &n)
	return n
}

// parseSummary extracts the skill display name + description from SKILL.md
// frontmatter. Falls back to the slug + first paragraph.
//
// Mirrors src/skills_mcp/frontmatter.py: handles flat “key: value“ lines
// AND YAML block scalars (“>“, “>-“, “|“, “|-“) — many SKILL.md files
// use the folded form for descriptions, and the previous version stored the
// indicator character ("> " / ">-") verbatim as the description.
func parseSummary(text, slug string) (string, string) {
	name := slug
	description := ""
	if strings.HasPrefix(text, "---") {
		lines := strings.Split(text, "\n")
		end := -1
		for i := 1; i < len(lines); i++ {
			if strings.TrimSpace(lines[i]) == "---" {
				end = i
				break
			}
		}
		if end > 0 {
			meta := parseFlatYAML(lines[1:end])
			if v := meta["name"]; v != "" {
				name = v
			}
			if v := meta["description"]; v != "" {
				description = v
			}
			if description == "" && end+1 < len(lines) {
				description = firstParagraph(strings.Join(lines[end+1:], "\n"))
			}
		}
	} else {
		description = firstParagraph(text)
	}
	description = strings.Join(strings.Fields(description), " ")
	if len(description) > 300 {
		description = description[:300]
	}
	if description == "" {
		description = "Skill: " + name
	}
	return name, description
}

// blockScalarMarkers are the YAML scalar indicators that introduce a
// multi-line value. We don't distinguish keep/strip/clip chomping because the
// caller folds whitespace later.
var blockScalarMarkers = map[string]bool{
	">": true, ">-": true, ">+": true,
	"|": true, "|-": true, "|+": true,
}

// parseFlatYAML reads a frontmatter line block and returns the top-level
// scalar values. Supports “key: value“ and YAML folded/literal block
// scalars introduced by “>“, “>-“, “|“, “|-“. Nested mappings and
// sequences are ignored.
func parseFlatYAML(body []string) map[string]string {
	out := map[string]string{}
	i := 0
	for i < len(body) {
		raw := body[i]
		stripped := strings.TrimSpace(raw)
		if stripped == "" || strings.HasPrefix(stripped, "#") || !strings.Contains(raw, ":") {
			i++
			continue
		}
		k, v, _ := strings.Cut(raw, ":")
		key := strings.TrimSpace(k)
		val := strings.TrimSpace(v)

		// YAML allows an inline comment after the block-scalar indicator
		// (e.g. "description: > # multi-line"). Compare against the first
		// whitespace-separated token so the comment doesn't make us miss it.
		head := val
		if fields := strings.Fields(val); len(fields) > 0 {
			head = fields[0]
		}
		if blockScalarMarkers[head] {
			folded := strings.HasPrefix(head, ">")
			block, nextI := collectBlockLines(body, i+1)
			i = nextI
			if folded {
				out[key] = foldBlockScalar(block)
			} else {
				out[key] = strings.TrimRight(strings.Join(block, "\n"), "\n")
			}
			continue
		}

		// Plain (implicit) scalar. YAML lets the value continue onto
		// subsequent indented lines, which fold into the value with
		// single-space separators. We only attempt the fold when the
		// key line itself carries a non-empty value — an empty value
		// ("metadata:") is the YAML signal for a nested mapping or
		// sequence, which this flat parser intentionally ignores.
		value := strings.Trim(val, "'\"")
		if value != "" {
			cont, nextI := collectPlainContinuationLines(body, i+1)
			if len(cont) > 0 {
				pieces := append([]string{value}, cont...)
				value = strings.Join(pieces, " ")
				i = nextI
			} else {
				i++
			}
		} else {
			i++
		}
		out[key] = value
	}
	return out
}

// collectPlainContinuationLines walks the lines after a plain-scalar key,
// returning the stripped continuation lines and the index of the first line
// that no longer belongs to the scalar. The scalar ends at a blank line, a
// non-indented line, an indented comment ("  # …"), or EOF. Indented
// comments are intentionally left to the outer loop's comment-skip so the
// "comments are ignored" contract still holds.
func collectPlainContinuationLines(body []string, start int) ([]string, int) {
	var cont []string
	i := start
	for i < len(body) {
		peek := body[i]
		stripped := strings.TrimSpace(peek)
		if stripped == "" || strings.HasPrefix(stripped, "#") {
			break
		}
		if !strings.HasPrefix(peek, " ") && !strings.HasPrefix(peek, "\t") {
			break
		}
		cont = append(cont, stripped)
		i++
	}
	return cont, i
}

// collectBlockLines gathers the indented continuation lines of a YAML block
// scalar starting at `start`. Returns the collected lines and the index of
// the first non-continuation line.
func collectBlockLines(body []string, start int) ([]string, int) {
	var block []string
	i := start
	for i < len(body) {
		peek := body[i]
		if strings.TrimSpace(peek) == "" {
			block = append(block, "")
			i++
			continue
		}
		if !strings.HasPrefix(peek, " ") && !strings.HasPrefix(peek, "\t") {
			break
		}
		block = append(block, strings.TrimSpace(peek))
		i++
	}
	return block, i
}

// foldBlockScalar joins block lines using YAML folded-scalar rules: blank
// lines separate paragraphs (joined with "\n\n"), consecutive non-blank lines
// are joined with " ".
func foldBlockScalar(block []string) string {
	var paragraphs [][]string
	var current []string
	for _, ln := range block {
		if ln == "" {
			if len(current) > 0 {
				paragraphs = append(paragraphs, current)
				current = nil
			}
			continue
		}
		current = append(current, ln)
	}
	if len(current) > 0 {
		paragraphs = append(paragraphs, current)
	}
	parts := make([]string, 0, len(paragraphs))
	for _, p := range paragraphs {
		parts = append(parts, strings.Join(p, " "))
	}
	return strings.Join(parts, "\n\n")
}

func firstParagraph(text string) string {
	for _, block := range strings.Split(text, "\n\n") {
		cleaned := strings.TrimSpace(block)
		if cleaned == "" || strings.HasPrefix(cleaned, "#") {
			continue
		}
		return cleaned
	}
	return strings.TrimSpace(text)
}
