package main

import (
	"context"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/anand-92/skills-registry/cli/internal/agents"
	"github.com/anand-92/skills-registry/cli/internal/config"
	"github.com/anand-92/skills-registry/cli/internal/registry"
	"github.com/anand-92/skills-registry/cli/internal/scan"
	"github.com/anand-92/skills-registry/cli/internal/tui"
)

func newSyncCmd() *cobra.Command {
	var (
		yes bool
		all bool
	)
	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Push local dot-folder skills missing from the registry",
		Long: `Scans every known AI tool dot-folder (e.g. ~/.claude/skills,
~/.factory/skills, .agents/skills) for skills whose slug isn't already in
your registry. Pick which to push with the interactive multi-select.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSync(cmd.Context(), yes, all)
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip the confirmation prompt.")
	cmd.Flags().BoolVar(&all, "all", false, "Select every missing skill without prompting.")
	return cmd
}

func runSync(ctx context.Context, yes, all bool) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	client, err := registry.New(cfg.Repo, cfg.DefaultBranch)
	if err != nil {
		return err
	}

	home, _ := os.UserHomeDir()
	cwd, _ := os.Getwd()
	dotDirs := make([]string, 0, len(agents.All()))
	for _, a := range agents.All() {
		dotDirs = append(dotDirs, a.DotDir)
	}
	sources := scan.DiscoverSources(home, cwd, nil, dotDirs)
	local, err := scan.Discover(sources)
	if err != nil {
		return err
	}
	remote, err := client.Slugs(ctx)
	if err != nil {
		return err
	}
	missing := scan.DedupeAgainst(local, remote)
	if len(missing) == 0 {
		fmt.Println("Registry is already in sync with your dot-folders.")
		return nil
	}

	var picked []scan.Skill
	if all {
		picked = missing
	} else {
		picked, err = promptSync(missing)
		if err != nil {
			return err
		}
	}
	if len(picked) == 0 {
		fmt.Println("Nothing to push.")
		return nil
	}

	if !yes && !all {
		fmt.Printf("\nAbout to push %d skill(s) to %s. Continue? [Y/n] ", len(picked), cfg.Repo)
		var resp string
		_, _ = fmt.Scanln(&resp)
		if resp != "" && resp != "y" && resp != "Y" {
			fmt.Println("Aborted.")
			return nil
		}
	}

	for _, sk := range picked {
		files := map[string][]byte{}
		if err := walkSkillIntoFiles(sk, files); err != nil {
			return err
		}
		// Re-key paths to be relative to the skill folder.
		bySlug := map[string][]byte{}
		for k, v := range files {
			// Drop the leading "<slug>/" we added in walkSkillIntoFiles.
			if len(k) > len(sk.Slug)+1 {
				bySlug[k[len(sk.Slug)+1:]] = v
			}
		}
		if _, err := client.Publish(ctx, sk.Slug, bySlug, fmt.Sprintf("sync: %s", sk.Slug)); err != nil {
			return fmt.Errorf("publish %s: %w", sk.Slug, err)
		}
		fmt.Println(tui.OkStyle.Render("✓"), sk.Slug)
	}
	return nil
}

func promptSync(missing []scan.Skill) ([]scan.Skill, error) {
	items := make([]tui.MultiSelectItem, 0, len(missing))
	for _, s := range missing {
		items = append(items, tui.MultiSelectItem{
			Value: s,
			Label: s.Name,
			Hint:  s.Slug + " · " + s.Source,
		})
	}
	model := tui.NewMultiSelect(
		fmt.Sprintf("Found %d local skill(s) missing from the registry — pick which to push", len(missing)),
		items, nil, true,
	)
	out, err := tea.NewProgram(model).Run()
	if err != nil {
		return nil, err
	}
	final := out.(tui.MultiSelectModel)
	if final.Cancelled() {
		return nil, fmt.Errorf("cancelled")
	}
	var picked []scan.Skill
	for _, v := range final.SelectedValues() {
		picked = append(picked, v.(scan.Skill))
	}
	return picked, nil
}
