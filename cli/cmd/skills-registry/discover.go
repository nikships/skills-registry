package main

import (
	"context"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/nikships/skills-registry/cli/internal/config"
	"github.com/nikships/skills-registry/cli/internal/discover"
	"github.com/nikships/skills-registry/cli/internal/jsonout"
	"github.com/nikships/skills-registry/cli/internal/tui"
)

// addFallbackHint is appended to every discover failure. The index is a
// convenience, not a dependency: a user who already has a GitHub URL can
// import it without the index being up.
const addFallbackHint = "the skill index is optional — you can still import any skill directly with `skills-registry add <github-url>`"

// Column caps for the human table. The URL column is deliberately never
// truncated: it is the one field the user copies straight into `add`.
const (
	discoverNameWidth     = 28
	discoverCategoryWidth = 14
	discoverScoreWidth    = 8
	discoverAuthorWidth   = 18

	// unscoredLabel renders a score the index did not assign. It must never
	// read like a pass: an unscored skill has not been vetted.
	unscoredLabel = "unscored"
)

func newDiscoverCmd() *cobra.Command {
	var (
		mode     string
		category string
		limit    int
		plain    bool
	)
	cmd := &cobra.Command{
		Use:   "discover QUERY",
		Short: "Search the public skill index for third-party skills to import",
		Long: fmt.Sprintf(`Searches the public SkillNet index of third-party skills and lists the top
matches with their safety scores and importable GitHub URLs.

On a terminal this opens an interactive picker: browse the hits, read each
row's grades and source URL, and press enter to import one straight into your
registry. Piped output, --plain, and --json print the result table instead and
download nothing — pass a result's URL to "skills-registry add" to import it.

This is the counterpart to "search": "search" fuzzy-ranks the skills already in
your own registry, while "discover" queries a public index of tens of thousands
of published skills.

An import from here is an untrusted import and travels the same gate as
"skills-registry add <url>": it publishes to your registry only, no agent
dot-folder is written unless you opt in, and a Poor safety grade or a local
scan hit needs an extra confirmation.

Modes:
  --mode keyword   literal term matching (default)
  --mode vector    embedding similarity, for describing what you want

Results carry the index's own safety, completeness, and executability grades
(Good / Average / Poor). A skill the index has not graded shows as %q — that
means unvetted, not safe. Review any skill's source before importing it. GitHub
star counts are not shown: they belong to the host repository, not the
individual skill, so they say nothing about its quality.

Transport: the index endpoint is plain HTTP, because the host serves a
certificate that does not match it and HTTPS cannot be verified. The request
therefore carries no credentials of any kind — no GitHub token, no gh auth
header, no registry contents — so only your search terms leave the machine.
Point %s at a mirror to use a different endpoint.

Examples:
  skills-registry discover pdf
  skills-registry discover "summarize a youtube video" --mode vector
  skills-registry discover pdf --category Productivity --limit 25
  skills-registry discover pdf --json`, unscoredLabel, discover.BaseURLEnv),
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// A search failure is a remote or input problem, not a misuse of
			// the command, so neither the usage block nor cobra's own error
			// line belongs in the output. main prints the error once;
			// argument-count validation still shows usage because it runs
			// before RunE.
			cmd.SilenceUsage = true
			cmd.SilenceErrors = true
			q := discover.Query{
				Text:     args[0],
				Mode:     discover.Mode(mode),
				Category: category,
				Limit:    limit,
			}
			if jsonout.Enabled() {
				return runDiscoverJSON(cmd.Context(), q)
			}
			if plain || !isTerminal() {
				return runDiscover(cmd.Context(), cmd.OutOrStdout(), q)
			}
			return runDiscoverTUI(cmd.Context(), q)
		},
	}
	cmd.Flags().StringVar(&mode, "mode", string(discover.ModeKeyword),
		"Ranking mode: keyword (literal terms) or vector (embedding similarity).")
	cmd.Flags().StringVar(&category, "category", "",
		"Restrict results to one index category (for example Productivity).")
	cmd.Flags().IntVar(&limit, "limit", discover.DefaultLimit,
		fmt.Sprintf("Maximum results to return (capped at %d).", discover.MaxLimit))
	cmd.Flags().BoolVar(&plain, "plain", false,
		"Print the result table instead of opening the interactive picker.")
	return cmd
}

// runDiscoverJSON emits the published payload on success and
// {"error": "..."} with a non-zero exit on any failure, so a consumer can
// branch on `jq '.error // empty'` without ever seeing a partial result set.
func runDiscoverJSON(ctx context.Context, q discover.Query) error {
	resp, err := discover.New().Search(ctx, q)
	if err != nil {
		jsonout.PrintError(err)
		return err
	}
	return jsonout.Print(resp)
}

// discoverSearch runs one index query, swapped in tests so no suite reaches
// the network.
var discoverSearch = func(ctx context.Context, q discover.Query) (discover.Response, error) {
	return discover.New().Search(ctx, q)
}

// runDiscoverTUI opens the interactive picker. A row picked there is imported
// through the same untrusted gate `add --from-discover` uses: the flow is
// handed the add dependency set, so this function contains no trust, grade, or
// install logic of its own.
//
// Exit codes follow the flow's own report: a search failure is an error, while
// esc, q, a declined confirmation, or an empty selection all exit 0 having
// written nothing.
func runDiscoverTUI(ctx context.Context, q discover.Query) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	flow := tui.NewDiscoverFlow(ctx, discoverFlowDeps(cfg, q))
	out, err := tea.NewProgram(flow, tea.WithAltScreen(), tea.WithMouseCellMotion()).Run()
	if err != nil {
		return err
	}
	final, ok := out.(tui.DiscoverFlowModel)
	if !ok {
		return nil
	}
	if searchErr := final.Err(); searchErr != nil {
		return fmt.Errorf("%w\n%s", searchErr, addFallbackHint)
	}
	if toast, toastOK := final.Toast(); toast != "" {
		if toastOK {
			fmt.Println(tui.OkStyle.Render(toast))
		} else {
			fmt.Println(tui.ErrorStyle.Render(toast))
		}
	}
	return nil
}

// discoverFlowDeps wires the picker to the index and to `add`'s own import
// path. The add deps carry the gate hook, so a picked row is assessed exactly
// as a folder URL passed on the command line would be.
func discoverFlowDeps(cfg config.Config, q discover.Query) tui.DiscoverFlowDeps {
	return tui.DiscoverFlowDeps{
		Query: q.Text,
		Repo:  cfg.Repo,
		Search: func(searchCtx context.Context) ([]tui.DiscoverRow, error) {
			resp, err := discoverSearch(searchCtx, q)
			if err != nil {
				return nil, err
			}
			return discoverRows(resp), nil
		},
		Add: buildDiscoverAddDeps(cfg),
	}
}

// buildDiscoverAddDeps is the hub's add dependency set with the gate forced
// into public-index mode: a row out of the index is untrusted whatever shape
// its URL has, which is the same rule `add --from-discover` applies.
func buildDiscoverAddDeps(cfg config.Config) tui.AddFlowDeps {
	deps := buildAddFlowDeps(cfg)
	deps.Gate = importGateHook(cfg, true)
	return deps
}

// discoverRows maps the index payload onto the picker's row type. Order is
// preserved: the index's own ranking stands, and the picker never re-sorts by
// repository popularity.
func discoverRows(resp discover.Response) []tui.DiscoverRow {
	rows := make([]tui.DiscoverRow, 0, len(resp.Results))
	for _, r := range resp.Results {
		rows = append(rows, tui.DiscoverRow{
			Name:          r.Name,
			Desc:          r.Description,
			Author:        r.Author,
			Category:      r.Category,
			SkillURL:      r.SkillURL,
			Safety:        r.Safety,
			Completeness:  r.Completeness,
			Executability: r.Executability,
		})
	}
	return rows
}

func runDiscover(ctx context.Context, w io.Writer, q discover.Query) error {
	resp, err := discover.New().Search(ctx, q)
	if err != nil {
		return fmt.Errorf("%w\n%s", err, addFallbackHint)
	}
	renderDiscover(w, resp)
	return nil
}

// renderDiscover prints the fixed-width result table. Columns are name,
// category, safety, author, and URL; the URL is last and unclipped so it can
// be copied straight into `add`.
func renderDiscover(w io.Writer, resp discover.Response) {
	fmt.Fprintf(w, "Skill index (%s): %d result", resp.Source, len(resp.Results))
	if len(resp.Results) != 1 {
		fmt.Fprint(w, "s")
	}
	fmt.Fprintf(w, " for %q (%s mode)\n\n", resp.Query, resp.Mode)
	if len(resp.Results) == 0 {
		fmt.Fprintf(w, "  Nothing in the index matched %q.\n", resp.Query)
		if resp.Mode != string(discover.ModeVector) {
			fmt.Fprintln(w, "  Try --mode vector to search by meaning instead of literal terms.")
		}
		return
	}
	name := columnWidth("NAME", discoverNameWidth, resp.Results, func(r discover.Result) string { return r.Name })
	category := columnWidth("CATEGORY", discoverCategoryWidth, resp.Results, func(r discover.Result) string { return r.Category })
	safety := columnWidth("SAFETY", discoverScoreWidth, resp.Results, func(r discover.Result) string { return scoreLabel(r.Safety) })
	author := columnWidth("AUTHOR", discoverAuthorWidth, resp.Results, func(r discover.Result) string { return r.Author })

	fmt.Fprintf(w, "  %s  %s  %s  %s  %s\n",
		pad("NAME", name), pad("CATEGORY", category), pad("SAFETY", safety), pad("AUTHOR", author), "URL")
	fmt.Fprintf(w, "  %s  %s  %s  %s  %s\n",
		rule(name), rule(category), rule(safety), rule(author), rule(3))
	for _, r := range resp.Results {
		fmt.Fprintf(w, "  %s  %s  %s  %s  %s\n",
			pad(clip(r.Name, name), name),
			pad(clip(r.Category, category), category),
			pad(clip(scoreLabel(r.Safety), safety), safety),
			pad(clip(r.Author, author), author),
			r.SkillURL)
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "  Import one with: skills-registry add <URL>")
	fmt.Fprintf(w, "  Grades are the index's own (%s = not graded, not vetted). Review the source before importing.\n", unscoredLabel)
}

// scoreLabel renders one grade, naming an absent grade explicitly rather than
// leaving a blank cell that reads as "fine".
func scoreLabel(level string) string {
	if strings.TrimSpace(level) == "" {
		return unscoredLabel
	}
	return level
}

// columnWidth sizes a column to its widest value, bounded by max so one long
// value cannot push the URL column off screen.
func columnWidth(header string, max int, results []discover.Result, field func(discover.Result) string) int {
	width := utf8.RuneCountInString(header)
	for _, r := range results {
		if n := utf8.RuneCountInString(field(r)); n > width {
			width = n
		}
	}
	if width > max {
		return max
	}
	return width
}

// pad right-pads to width, counting runes so a multi-byte value still aligns.
func pad(s string, width int) string {
	if n := utf8.RuneCountInString(s); n < width {
		return s + strings.Repeat(" ", width-n)
	}
	return s
}

// clip shortens a value to width, marking the cut with an ellipsis. It slices
// runes, never bytes, so a multi-byte character is never split.
func clip(s string, width int) string {
	r := []rune(s)
	if len(r) <= width {
		return s
	}
	if width <= 1 {
		return string(r[:width])
	}
	return string(r[:width-1]) + "…"
}

func rule(width int) string { return strings.Repeat("─", width) }
