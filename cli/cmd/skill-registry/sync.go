package main

import (
	"context"
	"fmt"
	"os"
	"strings"

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

	picked, err := selectSkillsForSync(missing, yes, all, cfg.Repo)
	if err != nil {
		return err
	}
	if picked == nil {
		return nil
	}
	if len(picked) == 0 {
		fmt.Println("Nothing to push.")
		return nil
	}

	return publishSkills(ctx, client, picked, func(slug string) string {
		return fmt.Sprintf("sync: %s", slug)
	})
}

// selectSkillsForSync handles the interactive multi-select and confirmation
// for sync. Returns nil with no error when the user cancels or selects nothing.
func selectSkillsForSync(missing []scan.Skill, yes, all bool, repo string) ([]scan.Skill, error) {
	if all {
		return missing, nil
	}
	picked, err := promptSync(missing)
	if err != nil {
		if strings.Contains(err.Error(), "cancelled") {
			return nil, nil
		}
		return nil, err
	}
	if len(picked) == 0 {
		return []scan.Skill{}, nil
	}
	if !yes {
		ok, err := confirmPush(fmt.Sprintf(
			"Push %d skill(s) to %s?", len(picked), repo))
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, nil
		}
	}
	return picked, nil
}

// publishSkills walks and publishes each skill, printing a checkmark per slug.
// commitMsg is called once per skill to build the commit message.
func publishSkills(ctx context.Context, client *registry.Client, picked []scan.Skill, commitMsg func(string) string) error {
	for _, sk := range picked {
		files := map[string][]byte{}
		if err := walkSkillIntoFiles(sk, files); err != nil {
			return err
		}
		bySlug := rekeyBySlug(sk.Slug, files)
		if _, err := client.Publish(ctx, sk.Slug, bySlug, commitMsg(sk.Slug)); err != nil {
			return fmt.Errorf("publish %s: %w", sk.Slug, err)
		}
		fmt.Println(tui.OkStyle.Render("✓"), sk.Slug)
	}
	return nil
}

// rekeyBySlug strips the "<slug>/" prefix that walkSkillIntoFiles adds,
// returning paths relative to the skill folder.
func rekeyBySlug(slug string, files map[string][]byte) map[string][]byte {
	bySlug := map[string][]byte{}
	prefix := slug + "/"
	for k, v := range files {
		if strings.HasPrefix(k, prefix) {
			bySlug[k[len(prefix):]] = v
		}
	}
	return bySlug
}

// confirmPush is the shared yes/no confirmation prompt used by `sync` and
// `add` before any registry write. Returns true when the user picks "yes"
// (or hits enter on the default), false on explicit "no" or esc. Replaces
// the older `fmt.Scanln`-based prompt so cancellation/SIGINT behaves like
// every other prompt in the CLI.
func confirmPush(title string) (bool, error) {
	choices := []tui.Choice{
		{Value: "yes", Label: "Yes, push", Hint: "Continue with the registry write"},
		{Value: "no", Label: "Cancel", Hint: "Make no changes"},
	}
	model := tui.NewChoice(title, "Nothing local is touched — only the registry repo is updated.", choices)
	out, err := tea.NewProgram(model).Run()
	if err != nil {
		return false, err
	}
	final := out.(tui.ChoiceModel)
	if final.Cancelled() || final.Value() == nil {
		return false, nil
	}
	return final.Value().(string) == "yes", nil
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
