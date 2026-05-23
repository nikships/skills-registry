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
	cmd.Flags().StringVar(&destFlag, "dest", "", "Where to write the skill (default ./skill-registry/<slug>).")
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
	if dest == "" {
		cwd, _ := os.Getwd()
		dest = filepath.Join(cwd, "skill-registry", scan.Slugify(slug))
	}
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return err
	}
	if err := client.Get(ctx, scan.Slugify(slug), dest); err != nil {
		return err
	}
	fmt.Println(tui.OkStyle.Render("✓"), "wrote skill to", dest)
	return nil
}
