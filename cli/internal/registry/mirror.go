// Local git-backed mirror used by Client.List / Client.Slugs / Client.Get
// to avoid the 1 + N sequential `gh api` round-trips the original
// `repos/{repo}/contents/...` walk required.
//
// On the read path the Go CLI shallow-clones the registry repo once into
// ~/.cache/skills-mcp/mirror/<owner>/<repo>/ and subsequently fast-forwards
// it with a single `git fetch --depth=1` + `git reset --hard FETCH_HEAD`.
// Every per-skill SKILL.md read becomes a local file read instead of a
// `gh api repos/.../contents/<slug>/SKILL.md` subprocess.
//
// The Python MCP server keeps using `gh api` exclusively — it has to run
// in stripped GUI subprocess environments where `git` may be unavailable.
// This file is CLI-only and never imported from Python via FastMCP.
//
// Opt out with the SKILLS_MIRROR_DISABLE env var; the wrappers in
// registry.go fall through to the original gh-api impls in that case.

package registry

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// mirrorDisableEnv forces the read path back to `gh api` when set to a
// non-empty value. Useful for debugging a mis-synced mirror without
// having to delete the cache dir manually.
const mirrorDisableEnv = "SKILLS_MIRROR_DISABLE"

// mirrorEnabled reports whether List / Slugs / Get should consult the
// local git mirror before falling back to gh api. Disabled when the
// SKILLS_MIRROR_DISABLE env var is set or when no usable `git` binary
// can be located.
func (c *Client) mirrorEnabled() bool {
	if os.Getenv(mirrorDisableEnv) != "" {
		return false
	}
	if c.GitBin != "" {
		return true
	}
	_, err := exec.LookPath("git")
	return err == nil
}

// mirrorDir returns the absolute path to the per-repo mirror dir. The
// Client.MirrorRoot override (only set by tests) bypasses the cache
// resolution entirely.
func (c *Client) mirrorDir() string {
	if c.MirrorRoot != "" {
		return c.MirrorRoot
	}
	return filepath.Join(defaultMirrorRoot(), c.Repo)
}

// defaultMirrorRoot mirrors cache.CacheRoot's resolution rules but
// points at a sibling `mirror/` dir so the existing per-slug skill
// cache (`skills/`) used by the MCP server stays untouched.
func defaultMirrorRoot() string {
	if base := os.Getenv("XDG_CACHE_HOME"); base != "" {
		return filepath.Join(base, "skills-mcp", "mirror")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".cache", "skills-mcp", "mirror")
}

// ensureMirror brings the local mirror up to date with the remote
// branch and returns the working tree directory. On a brand-new
// registry with no commits the mirror dir is created empty and a nil
// error is returned — walkMirrorSummaries will then yield zero rows,
// matching the "no skills found" gh-api behavior.
func (c *Client) ensureMirror(ctx context.Context) (string, error) {
	gitBin, err := c.resolveGitBin()
	if err != nil {
		return "", err
	}
	dir := c.mirrorDir()
	gitDir := filepath.Join(dir, ".git")
	info, err := os.Stat(gitDir)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return dir, c.cloneMirror(ctx, gitBin, dir)
	case err != nil:
		return "", err
	case !info.IsDir():
		// `.git` exists but isn't a directory (file? broken state).
		// Wipe and re-clone.
		if rmErr := os.RemoveAll(dir); rmErr != nil {
			return "", rmErr
		}
		return dir, c.cloneMirror(ctx, gitBin, dir)
	}
	currentBranch, branchErr := mirrorBranch(ctx, gitBin, dir)
	if branchErr != nil || currentBranch != c.DefaultBranch {
		if rmErr := os.RemoveAll(dir); rmErr != nil {
			return "", rmErr
		}
		return dir, c.cloneMirror(ctx, gitBin, dir)
	}
	return dir, c.fetchMirror(ctx, gitBin, dir)
}

// cloneMirror initializes a fresh shallow clone at `dir`. Returns nil
// (with `dir` created but empty) when the remote branch doesn't exist
// yet — `gh repo create` produces an empty repo and the wizard hasn't
// pushed the bootstrap commit yet.
func (c *Client) cloneMirror(ctx context.Context, gitBin, dir string) error {
	remoteURL := c.remoteURL()
	exists, err := remoteBranchExists(ctx, gitBin, remoteURL, c.DefaultBranch)
	if err != nil {
		return err
	}
	if !exists {
		return os.MkdirAll(dir, 0o755)
	}
	if err := c.maybeSetupGitAuth(ctx, remoteURL); err != nil {
		return fmt.Errorf("configure git credentials via gh: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return err
	}
	// `git clone` insists the target be empty; wipe any stale partial
	// state from a previous failed run.
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, gitBin,
		"clone", "--depth", "1", "--branch", c.DefaultBranch, remoteURL, dir)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	cmd.Stdout = io.Discard
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git clone %s: %s", c.Repo, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// fetchMirror fast-forwards an existing mirror clone to the current
// remote HEAD. `reset --hard FETCH_HEAD` handles force-pushes correctly.
func (c *Client) fetchMirror(ctx context.Context, gitBin, dir string) error {
	remoteURL := c.remoteURL()
	if err := c.maybeSetupGitAuth(ctx, remoteURL); err != nil {
		return fmt.Errorf("configure git credentials via gh: %w", err)
	}
	if err := runGitIn(ctx, gitBin, dir, "fetch", "--depth", "1", "origin", c.DefaultBranch); err != nil {
		return err
	}
	return runGitIn(ctx, gitBin, dir, "reset", "--hard", "FETCH_HEAD")
}

// maybeSetupGitAuth runs `gh auth setup-git` only when the remote URL
// is a real github.com HTTPS URL. Local file:// or absolute-path
// remotes (only used by tests) don't need credential helpers and
// skipping the subprocess keeps the test path purely git-driven.
func (c *Client) maybeSetupGitAuth(ctx context.Context, remoteURL string) error {
	if !strings.HasPrefix(remoteURL, "https://github.com/") {
		return nil
	}
	return c.setupGitAuth(ctx)
}

// mirrorBranch reports the currently-checked-out branch in the mirror
// working tree. Used to detect when the user reconfigured DefaultBranch
// to a different branch since the last fetch.
func mirrorBranch(ctx context.Context, gitBin, dir string) (string, error) {
	cmd := exec.CommandContext(ctx, gitBin, "symbolic-ref", "--short", "HEAD")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// remoteBranchExists reports whether `<branch>` exists on the remote
// without cloning. Returns false (no error) for repos with no commits.
func remoteBranchExists(ctx context.Context, gitBin, remoteURL, branch string) (bool, error) {
	cmd := exec.CommandContext(ctx, gitBin,
		"ls-remote", "--heads", remoteURL, branch)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return false, fmt.Errorf("git ls-remote %s: %s", remoteURL, strings.TrimSpace(stderr.String()))
	}
	return len(strings.TrimSpace(string(out))) > 0, nil
}

// listViaMirror walks the mirror and returns one Summary per top-level
// folder that ships a SKILL.md. Sorted by slug. Skips dotfolders to
// match the gh-api impl's `strings.HasPrefix(name, ".")` filter.
func (c *Client) listViaMirror(ctx context.Context) ([]Summary, error) {
	dir, err := c.ensureMirror(ctx)
	if err != nil {
		return nil, err
	}
	return walkMirrorSummaries(dir)
}

// walkMirrorSummaries is the in-process counterpart to the gh-api
// `summarize` loop. Reads each `<slug>/SKILL.md` from disk and reuses
// the existing parseSummary helper so frontmatter handling stays
// identical between the API and mirror paths.
func walkMirrorSummaries(dir string) ([]Summary, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var out []Summary
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		skillMD := filepath.Join(dir, e.Name(), "SKILL.md")
		body, err := os.ReadFile(skillMD)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, err
		}
		name, desc := parseSummary(string(body), e.Name())
		out = append(out, Summary{
			Slug:        e.Name(),
			Name:        name,
			Description: desc,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Slug < out[j].Slug })
	return out, nil
}

// slugsViaMirror walks the mirror and returns a set of every top-level
// slug. Matches the gh-api Slugs filter (dirs only, no leading dot).
func (c *Client) slugsViaMirror(ctx context.Context) (map[string]struct{}, error) {
	dir, err := c.ensureMirror(ctx)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	out := map[string]struct{}{}
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		out[e.Name()] = struct{}{}
	}
	return out, nil
}

// getViaMirror copies `<mirror>/<slug>/` recursively into `dest`. A
// missing slug folder is treated as a no-op (matching the gh-api
// `Get`'s silent-on-404 behavior — `downloadRecursive` falls through
// when `contents` returns nil, nil for a 404).
func (c *Client) getViaMirror(ctx context.Context, slug, dest string) error {
	dir, err := c.ensureMirror(ctx)
	if err != nil {
		return err
	}
	src := filepath.Join(dir, slug)
	info, err := os.Stat(src)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return os.MkdirAll(dest, 0o755)
		}
		return err
	}
	if !info.IsDir() {
		return os.MkdirAll(dest, 0o755)
	}
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return err
	}
	return copyTree(src, dest)
}

// copyTree recursively materializes src → dest, skipping dotfiles
// (matches the publish-side `Client.Publish` hardening: dotfiles like
// `.git`, `.DS_Store`, and `__pycache__` never round-trip through the
// registry). Non-regular files (symlinks, devices) are skipped — the
// registry data model is plain text files.
func copyTree(src, dest string) error {
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") {
			continue
		}
		srcPath := filepath.Join(src, e.Name())
		destPath := filepath.Join(dest, e.Name())
		if e.IsDir() {
			if err := os.MkdirAll(destPath, 0o755); err != nil {
				return err
			}
			if err := copyTree(srcPath, destPath); err != nil {
				return err
			}
			continue
		}
		info, err := e.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			continue
		}
		data, err := os.ReadFile(srcPath)
		if err != nil {
			return err
		}
		if err := os.WriteFile(destPath, data, 0o644); err != nil {
			return err
		}
	}
	return nil
}
