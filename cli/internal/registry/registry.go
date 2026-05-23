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
	// PushTree or Publish operation. `done` is the cumulative count, `total`
	// is the operation's total file count. Useful for progress bars.
	OnProgress func(done, total int)
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

// RepoURL returns the canonical https URL for this repo (no trailing slash).
func (c *Client) RepoURL() string {
	return "https://github.com/" + c.Repo
}

// Summary is one row in the listing.
type Summary struct {
	Slug        string
	Name        string
	Description string
	TreeSHA     string
}

// Slugs returns a set of every top-level slug in the registry.
func (c *Client) Slugs(ctx context.Context) (map[string]struct{}, error) {
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

// List enumerates registry skills with their summaries.
func (c *Client) List(ctx context.Context) ([]Summary, error) {
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

// Get downloads the full <slug>/ folder into dest. Existing files are overwritten.
func (c *Client) Get(ctx context.Context, slug, dest string) error {
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

// PushTree commits a set of files to the repo using the same atomic Git Data
// API path Publish uses, but populating multiple top-level folders in one
// commit. Used by `bootstrap` for the initial import. Blob uploads run in
// parallel (see Client.Workers) and progress is reported via Client.OnProgress.
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
			for _, raw := range lines[1:end] {
				if !strings.Contains(raw, ":") || strings.HasPrefix(strings.TrimSpace(raw), "#") {
					continue
				}
				k, v, _ := strings.Cut(raw, ":")
				key := strings.TrimSpace(k)
				val := strings.Trim(strings.TrimSpace(v), "'\"")
				switch key {
				case "name":
					if val != "" {
						name = val
					}
				case "description":
					if val != "" {
						description = val
					}
				}
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
