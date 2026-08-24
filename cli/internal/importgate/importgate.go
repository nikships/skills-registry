// Package importgate holds the rules for reviewing one skill before it is
// imported from an untrusted source: how a grade from the public index is
// rendered, what counts as a blocker, and how a blocker is explained.
//
// The rules live here, apart from the cobra command, so the CLI, the Discover
// TUI, and any other surface reach the same verdict for the same input. Two
// of them are correctness requirements rather than presentation choices:
//
//   - A missing grade renders as "unscored". The public index leaves a grade
//     empty when it never evaluated the skill, and an empty cell reads as
//     "fine". Unscored means unvetted.
//   - Poor safety blocks. So does a hit from the local injection scan. A block
//     is not a refusal to ever import: it means the user has to say so
//     explicitly, with the finding in front of them.
package importgate

import (
	"fmt"
	"strings"

	"github.com/nikships/skills-registry/cli/internal/skillscan"
)

// UnscoredLabel is how an absent grade is rendered. It must never be
// substituted with a passing grade or an empty string.
const UnscoredLabel = "unscored"

// LevelPoor is the public index's failing grade.
const LevelPoor = "Poor"

// Scores are the public index's grades for one skill. Each is `Good`,
// `Average`, `Poor`, or empty for "the index never graded this".
type Scores struct {
	Safety        string `json:"safety"`
	Completeness  string `json:"completeness"`
	Executability string `json:"executability"`
}

// Label renders one grade, naming an absent grade explicitly.
func Label(level string) string {
	if strings.TrimSpace(level) == "" {
		return UnscoredLabel
	}
	return strings.TrimSpace(level)
}

// Any reports whether the index graded this skill at all.
func (s Scores) Any() bool {
	return Label(s.Safety) != UnscoredLabel ||
		Label(s.Completeness) != UnscoredLabel ||
		Label(s.Executability) != UnscoredLabel
}

// SafetyIsPoor reports whether the index graded safety as Poor. An unscored
// safety grade is not Poor, and is not a pass either: it is reported as
// unscored and the import still needs the user's confirmation.
func (s Scores) SafetyIsPoor() bool {
	return strings.EqualFold(strings.TrimSpace(s.Safety), LevelPoor)
}

// Lines renders the three grades for a confirmation screen, always all three
// and always naming an absent one.
func (s Scores) Lines() []string {
	return []string{
		"safety:        " + Label(s.Safety),
		"completeness:  " + Label(s.Completeness),
		"executability: " + Label(s.Executability),
	}
}

// BlockKind identifies why an import needs explicit consent.
type BlockKind string

const (
	// BlockPoorSafety is a Poor safety grade from the public index.
	BlockPoorSafety BlockKind = "poor_safety"

	// BlockInjectionScan is a hit from the local heuristic scan of SKILL.md.
	BlockInjectionScan BlockKind = "injection_scan"
)

// Block is one reason an import is held back.
type Block struct {
	Kind   BlockKind `json:"kind"`
	Reason string    `json:"reason"`
}

// Review is the verdict for one skill from an untrusted source.
type Review struct {
	Slug     string              `json:"slug"`
	Scores   Scores              `json:"scores"`
	Findings []skillscan.Finding `json:"scan_findings,omitempty"`
	Blocks   []Block             `json:"blocks,omitempty"`
}

// Blocked reports whether this skill needs explicit consent
// (`--allow-unsafe`, or an extra interactive confirmation).
func (r Review) Blocked() bool { return len(r.Blocks) > 0 }

// Reasons returns each block's prose, for a JSON payload or a warning line.
func (r Review) Reasons() []string {
	out := make([]string, 0, len(r.Blocks))
	for _, b := range r.Blocks {
		out = append(out, b.Reason)
	}
	return out
}

// Summary renders the blocks as one line.
func (r Review) Summary() string { return strings.Join(r.Reasons(), "; ") }

// Evaluate produces the verdict for one skill. Both inputs are optional: no
// grades means the index never saw the skill, and no findings means the
// heuristic scan matched nothing (which is not a guarantee of safety).
func Evaluate(slug string, scores Scores, findings []skillscan.Finding) Review {
	r := Review{Slug: slug, Scores: scores, Findings: findings}
	if scores.SafetyIsPoor() {
		r.Blocks = append(r.Blocks, Block{
			Kind:   BlockPoorSafety,
			Reason: "the public skill index graded this skill's safety " + LevelPoor,
		})
	}
	if len(findings) > 0 {
		r.Blocks = append(r.Blocks, Block{
			Kind: BlockInjectionScan,
			Reason: fmt.Sprintf("the local scan of %s matched %d suspicious line(s) (%s)",
				"SKILL.md", len(findings), skillscan.Summary(findings)),
		})
	}
	return r
}

// Blocked filters reviews down to the ones needing explicit consent.
func Blocked(reviews []Review) []Review {
	out := make([]Review, 0, len(reviews))
	for _, r := range reviews {
		if r.Blocked() {
			out = append(out, r)
		}
	}
	return out
}

// ScanDisclaimer is the one-line statement of what the local scan is worth. It
// is shown wherever findings are reported so nobody reads a clean scan as a
// clean bill of health.
const ScanDisclaimer = "The local scan is a regex heuristic, not a guarantee: " +
	"it catches obvious prompt injection, credential exfiltration, and pipe-to-shell lines, and nothing subtler. " +
	"Read the skill's source."
