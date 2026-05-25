package main

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/sahilm/fuzzy"
	"github.com/spf13/cobra"

	"github.com/anand-92/skills-registry/cli/internal/config"
	"github.com/anand-92/skills-registry/cli/internal/jsonout"
	"github.com/anand-92/skills-registry/cli/internal/registry"
)

type searchJSONRow struct {
	Slug        string `json:"slug"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

type fuzzySource struct {
	summaries []registry.Summary
}

func (f *fuzzySource) String(i int) string {
	s := f.summaries[i]
	return s.Slug + " " + s.Name + " " + s.Description
}

func (f *fuzzySource) Len() int {
	return len(f.summaries)
}

func newSearchCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "search [QUERY]",
		Short: "Fuzzy-search your registry and return the top 10 matches",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			query := ""
			if len(args) > 0 {
				query = args[0]
			}
			if jsonout.Enabled() {
				cmd.SilenceErrors = true
				return runSearchJSON(cmd.Context(), query)
			}
			return runSearch(cmd.Context(), query)
		},
	}
	return cmd
}

func runSearchJSON(ctx context.Context, query string) error {
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
	summaries, err := client.List(ctx)
	if err != nil {
		jsonout.PrintError(err)
		return err
	}

	results := scoreAndSort(summaries, query)
	rows := make([]searchJSONRow, 0, len(results))
	for _, s := range results {
		rows = append(rows, searchJSONRow{
			Slug:        s.Slug,
			Name:        s.Name,
			Description: s.Description,
		})
	}
	return jsonout.Print(rows)
}

func runSearch(ctx context.Context, query string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	client, err := registry.New(cfg.Repo, cfg.DefaultBranch)
	if err != nil {
		return err
	}
	summaries, err := client.List(ctx)
	if err != nil {
		return err
	}

	results := scoreAndSort(summaries, query)
	if len(results) == 0 {
		if query != "" {
			fmt.Printf("No skills matching %q in %s\n", query, cfg.Repo)
		} else {
			fmt.Println("No skills in", cfg.Repo)
		}
		return nil
	}

	printPlainSearch(cfg.Repo, results)
	return nil
}

func scoreAndSort(summaries []registry.Summary, query string) []registry.Summary {
	q := strings.TrimSpace(query)
	if q == "" {
		return summaries
	}

	source := &fuzzySource{summaries: summaries}
	matches := fuzzy.FindFrom(q, source)

	// Sort matches by best score, with alphabetical slug fallback
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].Score != matches[j].Score {
			return matches[i].Score > matches[j].Score
		}
		return summaries[matches[i].Index].Slug < summaries[matches[j].Index].Slug
	})

	limit := 10
	if len(matches) < limit {
		limit = len(matches)
	}

	results := make([]registry.Summary, 0, limit)
	for i := 0; i < limit; i++ {
		results = append(results, summaries[matches[i].Index])
	}
	return results
}

func printPlainSearch(repo string, summaries []registry.Summary) {
	fmt.Printf("Search Results: %s  (%d skill", repo, len(summaries))
	if len(summaries) != 1 {
		fmt.Print("s")
	}
	fmt.Println(")")
	fmt.Println()
	width := len("SLUG")
	for _, s := range summaries {
		if len(s.Slug) > width {
			width = len(s.Slug)
		}
	}
	pad := func(s string) string {
		if len(s) >= width {
			return s
		}
		return s + strings.Repeat(" ", width-len(s))
	}
	fmt.Printf("  %s  %s\n", pad("SLUG"), "DESCRIPTION")
	fmt.Printf("  %s  %s\n", strings.Repeat("─", width), strings.Repeat("─", 11))
	for _, s := range summaries {
		desc := s.Description
		if r := []rune(desc); len(r) > 80 {
			desc = string(r[:79]) + "…"
		}
		fmt.Printf("  %s  %s\n", pad(s.Slug), desc)
	}
}
