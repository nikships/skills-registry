package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/nikships/skills-registry/cli/internal/cache"
	"github.com/nikships/skills-registry/cli/internal/config"
	"github.com/nikships/skills-registry/cli/internal/jsonout"
	"github.com/nikships/skills-registry/cli/internal/registry"
	"github.com/nikships/skills-registry/cli/internal/scan"
	"github.com/nikships/skills-registry/cli/internal/tui"
)

// getJSONResult is the payload emitted by `get --json`. Field order
// matches the JSON-002 contract ({slug, path}) so a `jq '.path'`
// consumer always finds the on-disk destination it just downloaded to.
type getJSONResult struct {
	Slug string `json:"slug"`
	Path string `json:"path"`
}

func newGetCmd() *cobra.Command {
	var destFlag string
	cmd := &cobra.Command{
		Use:   "get <slug>",
		Short: "Temporarily fetch a registry skill into the global cache (use `list` to durably install)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// A runtime failure is not a misuse of the command, so neither
			// the usage block nor cobra's own error line belongs in the
			// output; main prints the error once. Argument-count validation
			// still shows usage because it runs before RunE.
			cmd.SilenceUsage = true
			cmd.SilenceErrors = true
			if jsonout.Enabled() {
				return runGetJSON(cmd.Context(), args[0], destFlag)
			}
			return runGet(cmd.Context(), args[0], destFlag)
		},
	}
	cmd.Flags().StringVar(&destFlag, "dest", "", "Where to write the skill (default ~/.cache/skills-registry/skills/<slug>).")
	return cmd
}

// runGetJSON is the --json code path: downloads the skill and emits
// {slug, path} to stdout. Failures land as {"error": "..."} with a
// non-zero exit, so a `jq '.error // empty'` consumer can branch on success.
func runGetJSON(ctx context.Context, slug, dest string) error {
	cfg, err := config.Load()
	if err != nil {
		return jsonout.PrintErrorHandled(err)
	}
	client, err := registry.New(cfg.Repo, cfg.DefaultBranch)
	if err != nil {
		return jsonout.PrintErrorHandled(err)
	}
	finalDest, _, err := DownloadSkill(ctx, client, slug, dest)
	if err != nil {
		return jsonout.PrintErrorHandled(err)
	}
	return jsonout.Print(getJSONResult{
		Slug: scan.Slugify(slug),
		Path: finalDest,
	})
}

func runGet(ctx context.Context, slug, dest string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	client, err := registry.New(cfg.Repo, cfg.DefaultBranch)
	if err != nil {
		return err
	}
	finalDest, reused, err := DownloadSkill(ctx, client, slug, dest)
	if err != nil {
		return err
	}
	if reused != "" {
		fmt.Println(tui.HintStyle.Render("!  reusing existing folder"), tui.PreviewSlug.Render(reused))
	}
	// Two-line output: a chip-style header and a faint path on the next line
	// so the destination stands on its own and can be copy-pasted cleanly.
	fmt.Println(tui.OkStyle.Render("✓  saved"), tui.HintStyle.Render("→"), tui.PreviewSlug.Render(finalDest))
	return nil
}

// DownloadSkill resolves the destination, downloads the skill, and returns
// the final on-disk path plus any sibling folder that was reused. Shared by
// the `get` command and the inline-download path in the `list` TUI.
//
// An unknown slug fails with ErrSlugNotFound (naming the slug and pointing
// at `search`/`list`) instead of printing a fake success: the error leaves
// no directory behind, and Client.Get itself returns the same sentinel on
// 404 / missing-mirror-dir so every Get caller benefits.
func DownloadSkill(ctx context.Context, client *registry.Client, slug, destFlag string) (finalDest, reused string, err error) {
	defaultParent := cache.CacheRoot()
	if defaultParent == "" || !filepath.IsAbs(defaultParent) {
		return "", "", fmt.Errorf("resolve cache root (set HOME or XDG_CACHE_HOME, or pass --dest)")
	}

	// Resolve the actual slug from the registry (handles separator/case drift).
	// An unresolved slug is a hard failure: never fabricate a success line
	// for a skill the registry doesn't have.
	canonSlug, found, err := client.Resolve(ctx, scan.Slugify(slug))
	if err != nil {
		return "", "", err
	}
	if !found {
		return "", "", slugNotFoundError(client.Repo, scan.Slugify(slug))
	}

	finalDest, reused = resolveDest(canonSlug, destFlag, defaultParent)
	// Remember whether the folder pre-existed so the not-found path below
	// only removes directories this call created — never a user's folder.
	_, statErr := os.Stat(finalDest)
	created := errors.Is(statErr, os.ErrNotExist)
	if err := os.MkdirAll(finalDest, 0o755); err != nil {
		return "", "", err
	}
	if err := client.Get(ctx, canonSlug, finalDest); err != nil {
		if !errors.Is(err, registry.ErrSlugNotFound) {
			return "", "", err
		}
		// The slug vanished (or the mirror lagged) between Resolve and Get:
		// remove the directory we just created so no empty folder lingers.
		if created {
			_ = os.Remove(finalDest)
		}
		return "", "", slugNotFoundError(client.Repo, canonSlug)
	}
	return finalDest, reused, nil
}

// slugNotFoundError reports an unknown slug with the repo it was looked up
// in and the commands that show what's actually there. Wraps
// registry.ErrSlugNotFound so errors.Is keeps working for callers that
// branch on the sentinel.
func slugNotFoundError(repo, slug string) error {
	return fmt.Errorf("%w: %q in %s (run `skills-registry search` or `list` to see available skills)", registry.ErrSlugNotFound, slug, repo)
}

// resolveDest decides where to write a fetched skill so that the on-disk folder
// name stays in lockstep with the registry's canonical slug.
//
// Rules:
//  1. Empty destFlag → "<defaultParent>/<canonSlug>". Production callers
//     pass cache.CacheRoot() so downloads land in the global cache, not
//     a stray ./.agents/ tree under cwd (issue #29).
//  2. destFlag whose basename normalizes (NormalizeForMatch) to canonSlug → use as-is.
//  3. Otherwise destFlag is treated as a parent directory and canonSlug is appended.
//
// After resolving, the parent directory is scanned for an existing sibling
// folder whose normalized name (NormalizeForMatch) matches canonSlug. If one
// is found at a different path, that path is returned instead (the second
// return value is the path that's being reused, for user-facing logging).
// This prevents the "agp-9-upgrade vs agp_9_upgrade" duplicate-folder bug.
func resolveDest(slug, destFlag, defaultParent string) (finalDest, reused string) {
	canonSlug := scan.Slugify(slug)
	switch {
	case destFlag == "":
		finalDest = filepath.Join(defaultParent, canonSlug)
	case scan.NormalizeForMatch(filepath.Base(destFlag)) == scan.NormalizeForMatch(canonSlug):
		finalDest = destFlag
	default:
		finalDest = filepath.Join(destFlag, canonSlug)
	}
	if sibling, ok := findSlugSibling(filepath.Dir(finalDest), canonSlug); ok && sibling != finalDest {
		return sibling, sibling
	}
	return finalDest, ""
}

// findSlugSibling returns the path of an existing directory under parent whose
// name normalizes (NormalizeForMatch) to the same key as canonSlug, if one
// exists.
func findSlugSibling(parent, canonSlug string) (string, bool) {
	entries, err := os.ReadDir(parent)
	if err != nil {
		return "", false
	}
	normalizedCanon := scan.NormalizeForMatch(canonSlug)
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if scan.NormalizeForMatch(e.Name()) == normalizedCanon {
			return filepath.Join(parent, e.Name()), true
		}
	}
	return "", false
}
