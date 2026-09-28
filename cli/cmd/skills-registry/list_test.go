package main

import (
	"strings"
	"testing"

	"github.com/nikships/skills-registry/cli/internal/registry"
	"github.com/nikships/skills-registry/cli/internal/tui"
)

// listFilterFixtures mirrors the cli-tui-4/10 repro: "pdf" must select
// nano-pdf and github-image-upload only.
func listFilterFixtures() []registry.Summary {
	return []registry.Summary{
		{Slug: "nano_pdf", Name: "nano-pdf", Description: "Edit PDF text via CLI."},
		{Slug: "design_md", Name: "design-md", Description: "Author/validate/export DESIGN.md token spec files."},
		{Slug: "github_image_upload", Name: "github-image-upload", Description: "Upload images and files (PDF, zip) to GitHub."},
		{Slug: "spike", Name: "Spike", Description: "Throwaway experiments to validate an idea."},
	}
}

// TestSummaryMatches_SubstringOverSlugNameDescription pins the --query
// rule: case-insensitive substring over slug+name+description, with an
// empty needle matching everything.
func TestSummaryMatches_SubstringOverSlugNameDescription(t *testing.T) {
	rows := listFilterFixtures()
	cases := []struct {
		row    int
		needle string
		want   bool
	}{
		{0, "pdf", true},
		{2, "pdf", true},
		{1, "pdf", false},
		{3, "pdf", false},
		{1, "design", true},
		{3, "spike", true},
		{0, "edit pdf", true},
		{0, "", true},
		{3, "", true},
	}
	for _, tc := range cases {
		if got := summaryMatches(rows[tc.row], tc.needle); got != tc.want {
			t.Errorf("summaryMatches(row %d %q, %q) = %v, want %v",
				tc.row, rows[tc.row].Slug, tc.needle, got, tc.want)
		}
	}
}

// TestSummaryMatches_AgreesWithTUIFilter pins the shared-predicate
// contract: every --query verdict must equal the TUI `/` filter's
// verdict on the same row, so --plain, --json, and the TUI select
// identically.
func TestSummaryMatches_AgreesWithTUIFilter(t *testing.T) {
	needles := []string{"", "pdf", "design", "spike", "zzz-no-such", " ", "nano_pdf nano-pdf"}
	for _, s := range listFilterFixtures() {
		hay := tui.SkillRow{Slug: s.Slug, Name: s.Name, Desc: s.Description}.FilterValue()
		for _, n := range needles {
			if got, want := summaryMatches(s, n), tui.FilterMatches(hay, n); got != want {
				t.Errorf("summaryMatches(%q, %q) = %v, TUI FilterMatches = %v",
					s.Slug, n, got, want)
			}
		}
	}
}

// TestFilterSummaries_SelectsAndPreservesOrder pins the helper shared
// by the --json, --plain, and TUI-loader paths: substring hits only,
// in stable registry order.
func TestFilterSummaries_SelectsAndPreservesOrder(t *testing.T) {
	got := filterSummaries(listFilterFixtures(), "pdf")
	if len(got) != 2 || got[0].Slug != "nano_pdf" || got[1].Slug != "github_image_upload" {
		t.Fatalf("filterSummaries(pdf) slugs = %v, want [nano_pdf github_image_upload]", slugsOf(got))
	}
	if got := filterSummaries(listFilterFixtures(), ""); len(got) != 4 {
		t.Errorf("filterSummaries(\"\") matched %d/4 rows, want all", len(got))
	}
	if got := filterSummaries(listFilterFixtures(), "zzz-no-such"); len(got) != 0 {
		t.Errorf("filterSummaries(zzz-no-such) matched %d rows, want 0", len(got))
	}
}

// TestListQueryHelp_MentionsPlainAndJSON pins the cli-tui-4 doc fix:
// --query now filters every list surface, and the help must say so.
func TestListQueryHelp_MentionsPlainAndJSON(t *testing.T) {
	f := newListCmd().Flags().Lookup("query")
	if f == nil {
		t.Fatal("list has no --query flag")
	}
	for _, want := range []string{"--plain", "--json"} {
		if !strings.Contains(f.Usage, want) {
			t.Errorf("--query usage %q does not mention %s", f.Usage, want)
		}
	}
}
