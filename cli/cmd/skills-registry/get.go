package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/anand-92/skills-registry/cli/internal/cache"
	"github.com/anand-92/skills-registry/cli/internal/config"
	"github.com/anand-92/skills-registry/cli/internal/jsonout"
	"github.com/anand-92/skills-registry/cli/internal/registry"
	"github.com/anand-92/skills-registry/cli/internal/scan"
	"github.com/anand-92/skills-registry/cli/internal/tui"
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
		Short: "Download a registry skill into a local folder",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if jsonout.Enabled() {
				cmd.SilenceErrors = true
				return runGetJSON(cmd.Context(), args[0], destFlag)
			}
			return runGet(cmd.Context(), args[0], destFlag)
		},
	}
	cmd.Flags().StringVar(&destFlag, "dest", "", "Where to write the skill (default ~/.cache/skills-mcp/skills/<slug>).")
	return cmd
}

// runGetJSON is the --json code path: downloads the skill and emits
// {slug, path} to stdout. Failures land as {"error": "..."} with a
// non-zero exit, so a `jq '.error // empty'` consumer can branch on success.
func runGetJSON(ctx context.Context, slug, dest string) error {
	cfg, err := config.Load()
	if err != nil {
		jsonout.PrintError(err)
		return err
	}
	client, err := registry.New(cfg.Repo, cfg.DefaultBranch)
	if err != nil {
		jsonout.PrintError(err)
		return err
	}
	finalDest, _, err := DownloadSkill(ctx, client, slug, dest)
	if err != nil {
		jsonout.PrintError(err)
		return err
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
func DownloadSkill(ctx context.Context, client *registry.Client, slug, destFlag string) (finalDest, reused string, err error) {
	defaultParent := cache.CacheRoot()
	if defaultParent == "" || !filepath.IsAbs(defaultParent) {
		return "", "", fmt.Errorf("resolve cache root (set HOME or XDG_CACHE_HOME, or pass --dest)")
	}
	finalDest, reused = resolveDest(slug, destFlag, defaultParent)
	if err := os.MkdirAll(finalDest, 0o755); err != nil {
		return "", "", err
	}
	if err := client.Get(ctx, scan.Slugify(slug), finalDest); err != nil {
		return "", "", err
	}
	return finalDest, reused, nil
}

// resolveDest decides where to write a fetched skill so that the on-disk folder
// name stays in lockstep with the registry's canonical slug.
//
// Rules:
//  1. Empty destFlag → "<defaultParent>/<canonSlug>". Production callers
//     pass cache.CacheRoot() so downloads land in the global cache, not
//     a stray ./.agents/ tree under cwd (issue #29).
//  2. destFlag with a basename that slugifies to canonSlug → use as-is.
//  3. Otherwise destFlag is treated as a parent directory and canonSlug is appended.
//
// After resolving, the parent directory is scanned for an existing sibling
// folder whose Slugify matches canonSlug. If one is found at a different path,
// that path is returned instead (the second return value is the path that's
// being reused, for user-facing logging). This prevents the
// "agp-9-upgrade vs agp_9_upgrade" duplicate-folder bug.
func resolveDest(slug, destFlag, defaultParent string) (finalDest, reused string) {
	canonSlug := scan.Slugify(slug)
	switch {
	case destFlag == "":
		finalDest = filepath.Join(defaultParent, canonSlug)
	case scan.Slugify(filepath.Base(destFlag)) == canonSlug:
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
// name slugifies to canonSlug, if one exists.
func findSlugSibling(parent, canonSlug string) (string, bool) {
	entries, err := os.ReadDir(parent)
	if err != nil {
		return "", false
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if scan.Slugify(e.Name()) == canonSlug {
			return filepath.Join(parent, e.Name()), true
		}
	}
	return "", false
}
