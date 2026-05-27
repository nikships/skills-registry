package main

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"unicode"

	"github.com/spf13/cobra"

	"github.com/anand-92/skills-registry/cli/internal/config"
	"github.com/anand-92/skills-registry/cli/internal/jsonout"
	"github.com/anand-92/skills-registry/cli/internal/registry"
)

// Scoring constants for the fzf V1-style fuzzy matcher. These mirror
// the values in “infa-not-for-users/skills_mcp/github_api.py“ —
// bumping one without the other breaks the contract that the MCP and
// CLI return the same ranking for the same registry.
const (
	fuzzyBaseMatchScore   = 16
	fuzzyBoundaryBonus    = 8
	fuzzyCamelBonus       = 7
	fuzzyConsecutiveBonus = 5
	fuzzyCaseBonus        = 1
	fuzzyGapPenalty       = 2

	// Cap on the number of results ``search`` surfaces. Matches the MCP
	// tool's ``_SEARCH_TOP_N``.
	searchTopN = 10
)

// Field weights for “scoreSkill“. Aligned with “_FIELD_WEIGHTS“ on
// the Python side: name is the most semantically precise label, slug
// and description are tiebreakers.
var fieldWeights = []struct {
	name   string
	weight int
}{
	{"name", 2},
	{"slug", 1},
	{"description", 1},
}

// isWordBoundaryChar reports whether “r“ is one of the delimiters
// that earns the matched-next char a boundary bonus. Mirrors
// “_WORD_BOUNDARY_CHARS“ on the Python side.
func isWordBoundaryChar(r rune) bool {
	switch r {
	case ' ', '\t', '\n', '_', '-', '/', '.', '\\', ':':
		return true
	}
	return false
}

type searchJSONRow struct {
	Slug        string `json:"slug"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

func newSearchCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "search QUERY",
		Short: "Fuzzy-search your registry (top 10 matches). Use `list` to enumerate every skill.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			query := args[0]
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
		fmt.Printf("No skills matching %q in %s\n", query, cfg.Repo)
		return nil
	}

	printPlainSearch(cfg.Repo, results)
	return nil
}

// findAlignment locates the tightest right-anchored alignment of
// qLower in tLower. Returns nil if any query rune doesn't appear in
// order. Mirrors “_find_alignment“ on the Python side.
func findAlignment(qLower, tLower []rune) []int {
	// Forward pass — find any valid alignment; record end index.
	qi := 0
	endIdx := -1
	for ti := 0; ti < len(tLower); ti++ {
		if tLower[ti] == qLower[qi] {
			qi++
			if qi == len(qLower) {
				endIdx = ti
				break
			}
		}
	}
	if endIdx < 0 {
		return nil
	}
	// Backward tighten.
	matches := make([]int, len(qLower))
	qi = len(qLower) - 1
	for ti := endIdx; ti >= 0 && qi >= 0; ti-- {
		if tLower[ti] == qLower[qi] {
			matches[qi] = ti
			qi--
		}
	}
	if qi >= 0 {
		// Defensive — the forward pass already proved a match exists.
		return nil
	}
	return matches
}

// scoreCharMatch scores one matched rune in the alignment returned by
// findAlignment. Mirrors “_char_score“ on the Python side.
func scoreCharMatch(
	qPos, tPos int,
	matches []int,
	queryRunes, textRunes, tLower []rune,
) int {
	score := fuzzyBaseMatchScore
	if tPos == 0 || isWordBoundaryChar(tLower[tPos-1]) {
		score += fuzzyBoundaryBonus
	} else if tPos > 0 &&
		unicode.IsUpper(textRunes[tPos]) &&
		unicode.IsLower(textRunes[tPos-1]) {
		score += fuzzyCamelBonus
	}
	if qPos > 0 && matches[qPos] == matches[qPos-1]+1 {
		score += fuzzyConsecutiveBonus
	}
	if textRunes[tPos] == queryRunes[qPos] {
		score += fuzzyCaseBonus
	}
	return score
}

// fuzzyScore returns an fzf V1-style match score for query against
// text (0 if any query rune doesn't appear in order).
//
// See “findAlignment“ for the alignment logic and “scoreCharMatch“
// for the per-rune weighting. MUST stay in lockstep with
// “_fuzzy_score“ in
// “infa-not-for-users/skills_mcp/github_api.py“ — both surfaces
// (CLI + MCP) depend on identical ranking.
func fuzzyScore(query, text string) int {
	if query == "" || text == "" {
		return 0
	}
	queryRunes := []rune(query)
	textRunes := []rune(text)
	if len(queryRunes) > len(textRunes) {
		return 0
	}
	qLower := []rune(strings.ToLower(query))
	tLower := []rune(strings.ToLower(text))
	matches := findAlignment(qLower, tLower)
	if matches == nil {
		return 0
	}
	score := 0
	for qPos, tPos := range matches {
		score += scoreCharMatch(qPos, tPos, matches, queryRunes, textRunes, tLower)
	}
	span := matches[len(matches)-1] - matches[0] + 1
	score -= (span - len(qLower)) * fuzzyGapPenalty
	if score < 0 {
		return 0
	}
	return score
}

// scoreSkill scores a summary by summing per-field fuzzy scores under
// the canonical field weights. Mirrors “_score_skill“ on the Python
// side.
func scoreSkill(query string, s registry.Summary) int {
	q := strings.TrimSpace(query)
	if q == "" {
		return 0
	}
	score := 0
	for _, fw := range fieldWeights {
		var field string
		switch fw.name {
		case "slug":
			field = s.Slug
		case "name":
			field = s.Name
		case "description":
			field = s.Description
		}
		score += fuzzyScore(q, field) * fw.weight
	}
	return score
}

// scoreAndSort returns the top-N summaries ranked by “scoreSkill“.
// An empty / whitespace-only query returns an empty slice — “search“
// requires a query. Callers wanting the full registry should use
// “list“.
func scoreAndSort(summaries []registry.Summary, query string) []registry.Summary {
	q := strings.TrimSpace(query)
	if q == "" {
		return nil
	}

	type scored struct {
		score   int
		summary registry.Summary
	}
	scoredList := make([]scored, 0, len(summaries))
	for _, s := range summaries {
		if score := scoreSkill(q, s); score > 0 {
			scoredList = append(scoredList, scored{score: score, summary: s})
		}
	}

	sort.Slice(scoredList, func(i, j int) bool {
		if scoredList[i].score != scoredList[j].score {
			return scoredList[i].score > scoredList[j].score
		}
		return scoredList[i].summary.Slug < scoredList[j].summary.Slug
	})

	limit := searchTopN
	if len(scoredList) < limit {
		limit = len(scoredList)
	}
	results := make([]registry.Summary, 0, limit)
	for i := 0; i < limit; i++ {
		results = append(results, scoredList[i].summary)
	}
	return results
}

func printPlainSearch(repo string, summaries []registry.Summary) {
	printPlainSummaryTable("Search Results", repo, summaries)
}
