package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/anand-92/skills-registry/cli/internal/config"
	"github.com/anand-92/skills-registry/cli/internal/registry"
	"github.com/anand-92/skills-registry/cli/internal/scan"
	"github.com/anand-92/skills-registry/cli/internal/tui"
)

func newGetCmd() *cobra.Command {
	var destFlag string
	cmd := &cobra.Command{
		Use:   "get <slug>",
		Short: "Download a registry skill into a local folder",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runGet(cmd.Context(), args[0], destFlag)
		},
	}
	cmd.Flags().StringVar(&destFlag, "dest", "", "Where to write the skill (default ./.agents/skills/<slug>).")
	return cmd
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
	cwd, _ := os.Getwd()
	finalDest, reused, err := DownloadSkill(ctx, client, slug, dest, cwd)
	if err != nil {
		return err
	}
	if reused != "" {
		fmt.Println(tui.HintStyle.Render("!"), "reusing existing folder", reused)
	}
	fmt.Println(tui.OkStyle.Render("✓"), "wrote skill to", finalDest)
	return nil
}

// DownloadSkill resolves the destination, downloads the skill, and returns
// the final on-disk path plus any sibling folder that was reused. Shared by
// the `get` command and the inline-download path in the `list` TUI.
func DownloadSkill(ctx context.Context, client *registry.Client, slug, destFlag, cwd string) (finalDest, reused string, err error) {
	finalDest, reused = resolveDest(slug, destFlag, cwd)
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
//  1. Empty destFlag → "<cwd>/.agents/skills/<canonSlug>".
//  2. destFlag with a basename that slugifies to canonSlug → use as-is.
//  3. Otherwise destFlag is treated as a parent directory and canonSlug is appended.
//
// After resolving, the parent directory is scanned for an existing sibling
// folder whose Slugify matches canonSlug. If one is found at a different path,
// that path is returned instead (the second return value is the path that's
// being reused, for user-facing logging). This prevents the
// "agp-9-upgrade vs agp_9_upgrade" duplicate-folder bug.
func resolveDest(slug, destFlag, cwd string) (finalDest, reused string) {
	canonSlug := scan.Slugify(slug)
	switch {
	case destFlag == "":
		finalDest = filepath.Join(cwd, ".agents", "skills", canonSlug)
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
