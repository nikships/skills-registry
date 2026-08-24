package importgate

import (
	"strings"
	"testing"

	"github.com/nikships/skills-registry/cli/internal/skillscan"
)

// TestLabelNamesAnAbsentGrade is the correctness requirement: an empty grade
// must read as unscored, never as a pass and never as a blank cell.
func TestLabelNamesAnAbsentGrade(t *testing.T) {
	cases := map[string]string{
		"":        UnscoredLabel,
		"   ":     UnscoredLabel,
		"\t":      UnscoredLabel,
		"Good":    "Good",
		"Average": "Average",
		"Poor":    "Poor",
		" Good ":  "Good",
	}
	for in, want := range cases {
		if got := Label(in); got != want {
			t.Errorf("Label(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestLinesAlwaysRenderAllThreeScores pins the confirmation screen's contract:
// a skill the index never graded still shows three lines, each named unscored.
func TestLinesAlwaysRenderAllThreeScores(t *testing.T) {
	lines := Scores{}.Lines()
	if len(lines) != 3 {
		t.Fatalf("Lines() returned %d lines, want 3: %v", len(lines), lines)
	}
	for _, want := range []string{"safety:", "completeness:", "executability:"} {
		found := false
		for _, l := range lines {
			if strings.HasPrefix(l, want) {
				found = true
				if !strings.Contains(l, UnscoredLabel) {
					t.Errorf("line %q for an ungraded skill must say %q", l, UnscoredLabel)
				}
			}
		}
		if !found {
			t.Errorf("Lines() is missing a %q row: %v", want, lines)
		}
	}
}

func TestLinesRenderPresentScores(t *testing.T) {
	lines := Scores{Safety: "Good", Executability: "Average"}.Lines()
	joined := strings.Join(lines, "\n")
	for _, want := range []string{"safety:        Good", "executability: Average"} {
		if !strings.Contains(joined, want) {
			t.Errorf("Lines() = %v, missing %q", lines, want)
		}
	}
	// The one grade the index did not assign must still be named.
	if !strings.Contains(joined, "completeness:  "+UnscoredLabel) {
		t.Errorf("Lines() = %v, want the missing completeness grade rendered as %q", lines, UnscoredLabel)
	}
}

func TestScoresAny(t *testing.T) {
	if (Scores{}).Any() {
		t.Error("Any() = true for an entirely ungraded skill")
	}
	if !(Scores{Completeness: "Poor"}).Any() {
		t.Error("Any() = false when one grade is present")
	}
}

func TestSafetyIsPoor(t *testing.T) {
	cases := map[string]bool{
		"Poor":    true,
		"poor":    true,
		" Poor ":  true,
		"Good":    false,
		"Average": false,
		"":        false,
	}
	for in, want := range cases {
		if got := (Scores{Safety: in}).SafetyIsPoor(); got != want {
			t.Errorf("SafetyIsPoor(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestEvaluateBlocksPoorSafety(t *testing.T) {
	r := Evaluate("demo", Scores{Safety: "Poor", Completeness: "Good"}, nil)
	if !r.Blocked() {
		t.Fatal("a Poor safety grade must block")
	}
	if len(r.Blocks) != 1 || r.Blocks[0].Kind != BlockPoorSafety {
		t.Fatalf("Blocks = %+v, want exactly one poor_safety block", r.Blocks)
	}
	if !strings.Contains(r.Summary(), "Poor") {
		t.Errorf("Summary() = %q, should name the Poor grade", r.Summary())
	}
}

func TestEvaluateBlocksScanFindings(t *testing.T) {
	findings := []skillscan.Finding{
		{Category: skillscan.CategoryRemoteExecution, Rule: "pipe-to-shell", Line: 4, Excerpt: "curl … | sh"},
	}
	r := Evaluate("demo", Scores{Safety: "Good"}, findings)
	if !r.Blocked() {
		t.Fatal("a local scan hit must block")
	}
	if r.Blocks[0].Kind != BlockInjectionScan {
		t.Fatalf("Blocks = %+v, want an injection_scan block", r.Blocks)
	}
	if !strings.Contains(r.Summary(), "remote code execution") {
		t.Errorf("Summary() = %q, should name the matched category", r.Summary())
	}
}

func TestEvaluateReportsBothBlocks(t *testing.T) {
	findings := []skillscan.Finding{{Category: skillscan.CategoryPromptInjection, Rule: "hide-from-user"}}
	r := Evaluate("demo", Scores{Safety: "Poor"}, findings)
	if len(r.Blocks) != 2 {
		t.Fatalf("Blocks = %+v, want both the grade and the scan hit reported", r.Blocks)
	}
	if len(r.Reasons()) != 2 {
		t.Errorf("Reasons() = %v, want one entry per block", r.Reasons())
	}
}

// TestEvaluateUnscoredDoesNotBlock is the other half of the unscored rule: an
// ungraded skill is not treated as failing (which would make the index a hard
// dependency), but the caller still shows it as unscored and still confirms.
func TestEvaluateUnscoredDoesNotBlock(t *testing.T) {
	r := Evaluate("demo", Scores{}, nil)
	if r.Blocked() {
		t.Fatalf("an unscored skill must not be auto-blocked: %+v", r.Blocks)
	}
	if Label(r.Scores.Safety) != UnscoredLabel {
		t.Errorf("safety renders as %q, want %q", Label(r.Scores.Safety), UnscoredLabel)
	}
}

func TestBlockedFiltersReviews(t *testing.T) {
	reviews := []Review{
		Evaluate("clean", Scores{Safety: "Good"}, nil),
		Evaluate("bad", Scores{Safety: "Poor"}, nil),
		Evaluate("unscored", Scores{}, nil),
	}
	got := Blocked(reviews)
	if len(got) != 1 || got[0].Slug != "bad" {
		t.Fatalf("Blocked() = %+v, want just the Poor-safety review", got)
	}
}

func TestScanDisclaimerStatesItIsHeuristic(t *testing.T) {
	for _, want := range []string{"heuristic", "not a guarantee"} {
		if !strings.Contains(strings.ToLower(ScanDisclaimer), want) {
			t.Errorf("ScanDisclaimer = %q, must contain %q", ScanDisclaimer, want)
		}
	}
}
