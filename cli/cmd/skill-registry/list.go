package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/anand-92/skills-registry/cli/internal/config"
	"github.com/anand-92/skills-registry/cli/internal/registry"
	"github.com/anand-92/skills-registry/cli/internal/tui"
)

func newListCmd() *cobra.Command {
	var (
		queryFlag string
		plain     bool
	)
	cmd := &cobra.Command{
		Use:   "list",
		Short: "Browse your registry as an interactive list",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runList(cmd.Context(), queryFlag, plain)
		},
	}
	cmd.Flags().StringVarP(&queryFlag, "query", "q", "", "Initial filter substring.")
	cmd.Flags().BoolVar(&plain, "plain", false, "Print a plain table instead of opening the TUI.")
	return cmd
}

func runList(ctx context.Context, query string, plain bool) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	client, err := registry.New(cfg.Repo, cfg.DefaultBranch)
	if err != nil {
		return err
	}

	if plain || !isTerminal() {
		summaries, err := client.List(ctx)
		if err != nil {
			return err
		}
		if len(summaries) == 0 {
			fmt.Println("No skills in", cfg.Repo)
			return nil
		}
		printPlainList(cfg.Repo, summaries)
		return nil
	}

	loader := func() ([]tui.SkillRow, error) {
		summaries, err := client.List(ctx)
		if err != nil {
			return nil, err
		}
		rows := make([]tui.SkillRow, 0, len(summaries))
		needle := strings.ToLower(query)
		for _, s := range summaries {
			if needle != "" {
				hay := strings.ToLower(s.Slug + " " + s.Name + " " + s.Description)
				if !strings.Contains(hay, needle) {
					continue
				}
			}
			rows = append(rows, tui.SkillRow{Slug: s.Slug, Name: s.Name, Desc: s.Description})
		}
		return rows, nil
	}

	cwd, _ := os.Getwd()
	downloader := func(downloadCtx context.Context, slug string) (string, string, error) {
		return DownloadSkill(downloadCtx, client, slug, "", cwd)
	}

	model := tui.NewList(ctx, cfg.Repo, loader, downloader)
	if _, err := tea.NewProgram(
		model,
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	).Run(); err != nil {
		return err
	}
	return nil
}

func printPlainList(repo string, summaries []registry.Summary) {
	fmt.Printf("Registry: %s (%d skills)\n\n", repo, len(summaries))
	width := 0
	for _, s := range summaries {
		if len(s.Slug) > width {
			width = len(s.Slug)
		}
	}
	for _, s := range summaries {
		pad := strings.Repeat(" ", width-len(s.Slug))
		fmt.Printf("  %s%s  %s\n", s.Slug, pad, s.Description)
	}
}

func isTerminal() bool {
	fi, _ := os.Stdout.Stat()
	return (fi.Mode() & os.ModeCharDevice) != 0
}
