package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/anand-92/skills-registry/cli/internal/config"
	"github.com/anand-92/skills-registry/cli/internal/jsonout"
	"github.com/anand-92/skills-registry/cli/internal/registry"
	"github.com/anand-92/skills-registry/cli/internal/tui"
)

// listJSONRow is the per-skill payload emitted by `list --json`. Field
// order matches the JSON-001 contract (slug, name, description) so
// consumers reading `jq '.[].slug'` see a stable shape across releases.
type listJSONRow struct {
	Slug        string `json:"slug"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

func newListCmd() *cobra.Command {
	var (
		queryFlag string
		plain     bool
	)
	cmd := &cobra.Command{
		Use:   "list",
		Short: "Browse your registry as an interactive list",
		RunE: func(cmd *cobra.Command, args []string) error {
			if jsonout.Enabled() {
				return runListJSON(cmd.Context(), queryFlag)
			}
			return runList(cmd.Context(), queryFlag, plain)
		},
	}
	cmd.Flags().StringVarP(&queryFlag, "query", "q", "", "Initial filter substring.")
	cmd.Flags().BoolVar(&plain, "plain", false, "Print a plain table instead of opening the TUI.")
	return cmd
}

// runListJSON is the --json code path: never enters a TUI, prints a
// single JSON array (one row per registry skill) to stdout, and exits
// with a non-zero code via os.Exit when an error occurs. The empty
// registry is rendered as `[]` so consumers can `jq 'length'` without
// special-casing a missing payload.
func runListJSON(ctx context.Context, query string) error {
	cfg, err := config.Load()
	if err != nil {
		jsonout.PrintError(err)
		os.Exit(1)
	}
	client, err := registry.New(cfg.Repo, cfg.DefaultBranch)
	if err != nil {
		jsonout.PrintError(err)
		os.Exit(1)
	}
	summaries, err := client.List(ctx)
	if err != nil {
		jsonout.PrintError(err)
		os.Exit(1)
	}
	rows := make([]listJSONRow, 0, len(summaries))
	needle := strings.ToLower(query)
	for _, s := range summaries {
		if needle != "" {
			hay := strings.ToLower(s.Slug + " " + s.Name + " " + s.Description)
			if !strings.Contains(hay, needle) {
				continue
			}
		}
		rows = append(rows, listJSONRow{
			Slug:        s.Slug,
			Name:        s.Name,
			Description: s.Description,
		})
	}
	return jsonout.Print(rows)
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

	downloader := func(downloadCtx context.Context, slug string) (string, string, error) {
		return DownloadSkill(downloadCtx, client, slug, "")
	}
	deleter := func(deleteCtx context.Context, slug string) (string, error) {
		report, err := runRemove(deleteCtx, slug, true, true)
		if err != nil {
			return "", err
		}
		if report == nil {
			return "", fmt.Errorf("remove %s cancelled", slug)
		}
		return report.CommitSHA, nil
	}

	model := tui.NewList(ctx, cfg.Repo, loader, downloader).WithDeleter(deleter)
	if _, err := tea.NewProgram(
		model,
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	).Run(); err != nil {
		return err
	}
	return nil
}

// printPlainList renders the registry as a fixed-width table. The plain
// path is used when stdout is piped (so a downstream `grep` / `awk` has
// stable columns), so the description column is truncated to 80 chars to
// keep one row per line.
func printPlainList(repo string, summaries []registry.Summary) {
	fmt.Printf("Registry: %s  (%d skill", repo, len(summaries))
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
		// Plain output is meant for piping; clip long descriptions so a
		// `grep` consumer sees one entry per line without unexpected wraps.
		// Slice on runes — not bytes — so a multi-byte UTF-8 char doesn't get
		// cut in half and emit an invalid sequence to stdout.
		if r := []rune(desc); len(r) > 80 {
			desc = string(r[:79]) + "…"
		}
		fmt.Printf("  %s  %s\n", pad(s.Slug), desc)
	}
}

// isTerminal reports whether os.Stdout is attached to a character
// device (i.e. an interactive terminal). The check tolerates a failed
// Stat — that path only fires in pathological environments (closed
// stdout on Windows, broken FDs), and treating it as non-interactive
// is the right default for both the routing in main.go and the plain
// fallback below.
func isTerminal() bool {
	fi, err := os.Stdout.Stat()
	if err != nil || fi == nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}

// isStdinTerminal reports whether os.Stdin is attached to a character
// device. Used together with jsonout.Enabled() to decide whether to
// auto-promote --yes on commands that support it: agents piping
// commands into the CLI (`echo ... | skills-registry sync --json`) need
// the destructive-action confirmation to skip itself silently rather
// than hang on a Bubble Tea prompt that can't render.
//
// Implemented as a package-level variable rather than a free function
// so unit tests can swap in a deterministic stub — `go test`'s harness
// may or may not attach a TTY stdin depending on the runner, which
// would otherwise make `shouldAutoYes` tests environment-dependent.
var isStdinTerminal = func() bool {
	fi, err := os.Stdin.Stat()
	if err != nil || fi == nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}

// shouldAutoYes reports whether destructive-action confirmations
// should be skipped automatically. Triggers when --json is set AND
// stdin is not a TTY — the combination an agent driving the CLI with
// piped stdin uses. Callers OR this into their `yes` flag so explicit
// `--yes` users keep their existing behavior unchanged.
func shouldAutoYes() bool {
	return jsonout.Enabled() && !isStdinTerminal()
}
