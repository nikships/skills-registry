package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/nikships/skills-registry/cli/internal/config"
	"github.com/nikships/skills-registry/cli/internal/jsonout"
	"github.com/nikships/skills-registry/cli/internal/registry"
	"github.com/nikships/skills-registry/cli/internal/tui"
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
		Short: "List registry skills (interactive mode also supports durable installation)",
		Long: `Browse every skill in your registry.

On a terminal this opens an interactive list with a live SKILL.md
preview: press "/" to filter as you type, enter on a row to durably
install it into agent dot-folders, "d" to remove a skill, and "?" for
the full key list.

--plain prints a fixed-width table instead of opening the TUI, and
--json emits the skill array for scripts. --query seeds the initial
filter substring (matched case-insensitively against slug, name, and
description).`,
		Example: `  skills-registry list
  skills-registry list --query pdf
  skills-registry list --plain
  skills-registry list --json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// A runtime failure is not a misuse of the command, so neither
			// the usage block nor cobra's own error line belongs in the
			// output; main prints the error once. Argument-count validation
			// still shows usage because it runs before RunE.
			cmd.SilenceUsage = true
			cmd.SilenceErrors = true
			if jsonout.Enabled() {
				return runListJSON(cmd.Context(), queryFlag)
			}
			return runList(cmd.Context(), queryFlag, plain)
		},
	}
	cmd.Flags().StringVarP(&queryFlag, "query", "q", "", "Filter by substring over slug, name, and description (TUI, --plain, --json). In the TUI it seeds the filter (shows a filter chip; esc clears).")
	cmd.Flags().BoolVar(&plain, "plain", false, "Print a plain table instead of opening the TUI.")
	return cmd
}

// runListJSON is the --json code path: never enters a TUI, prints a
// single JSON array (one row per registry skill) to stdout, and returns a
// marked error (via PrintErrorHandled, so main exits non-zero without
// re-printing the envelope) when an error occurs. The empty registry is
// rendered as `[]` so consumers can `jq 'length'` without special-casing a
// missing payload.
func runListJSON(ctx context.Context, query string) error {
	cfg, err := config.Load()
	if err != nil {
		return jsonout.PrintErrorHandled(err)
	}
	client, err := registry.New(cfg.Repo, cfg.DefaultBranch)
	if err != nil {
		return jsonout.PrintErrorHandled(err)
	}
	summaries, err := client.List(ctx)
	if err != nil {
		return jsonout.PrintErrorHandled(err)
	}
	rows := make([]listJSONRow, 0, len(summaries))
	for _, s := range filterSummaries(summaries, strings.ToLower(query)) {
		rows = append(rows, listJSONRow{
			Slug:        s.Slug,
			Name:        s.Name,
			Description: s.Description,
		})
	}
	return jsonout.Print(rows)
}

// summaryMatches reports whether s contains the already-lowercased
// needle across its slug/name/description. An empty needle matches
// everything. Thin delegate over tui.FilterMatches so the --json,
// --plain, and TUI-loader paths share the TUI `/` filter's predicate
// and all four select identically.
func summaryMatches(s registry.Summary, needle string) bool {
	return tui.FilterMatches(s.Slug+" "+s.Name+" "+s.Description, needle)
}

// filterSummaries returns the summaries matching the already-lowercased
// needle, preserving registry order. Shared by the --json, --plain, and
// TUI-loader paths so `list --query` selects identically everywhere.
func filterSummaries(summaries []registry.Summary, needle string) []registry.Summary {
	filtered := make([]registry.Summary, 0, len(summaries))
	for _, s := range summaries {
		if summaryMatches(s, needle) {
			filtered = append(filtered, s)
		}
	}
	return filtered
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
		filtered := filterSummaries(summaries, strings.ToLower(query))
		if len(filtered) == 0 {
			fmt.Printf("No skills matching %q in %s\n", query, cfg.Repo)
			return nil
		}
		printPlainList(cfg.Repo, filtered)
		return nil
	}

	loader := func() ([]tui.SkillRow, error) {
		summaries, err := client.List(ctx)
		if err != nil {
			return nil, err
		}
		// The loader returns the full row set; the initial query seeds
		// the bubbles filter (WithInitialFilter) instead of pre-dropping
		// rows, so the header shows a `filter: <q>` chip and esc clears
		// back to the full list rather than quitting.
		rows := make([]tui.SkillRow, 0, len(summaries))
		for _, s := range filterSummaries(summaries, strings.ToLower(query)) {
			rows = append(rows, tui.SkillRow{Slug: s.Slug, Name: s.Name, Desc: s.Description})
		}
		return rows, nil
	}

	installer := func(installCtx context.Context, slug string, values []any) ([]string, error) {
		targets, err := installAnyValuesToTargets(values)
		if err != nil {
			return nil, err
		}
		return installSkillIntoTargets(installCtx, client, slug, targets)
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

	model := tui.NewList(ctx, cfg.Repo, loader, installer).
		WithDeleter(deleter).
		WithInstallTargets(installPickerTargets).
		WithInitialFilter(query)
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
	printPlainSummaryTable("Registry", repo, summaries)
}

// printPlainSummaryTable is the shared fixed-width renderer used by
// both `list --plain` and `search`. `label` is the headline prefix
// (e.g. "Registry" or "Search Results (top 10)") and the rest of the layout
// matches across both commands so a piped consumer sees identical
// columns regardless of which command produced the output.
func printPlainSummaryTable(label, repo string, summaries []registry.Summary) {
	fmt.Printf("%s: %s  (%d skill", label, repo, len(summaries))
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
