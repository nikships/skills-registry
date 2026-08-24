// Package main — the import gate for `add`.
//
// `add` accepts two very different kinds of source. A local folder or a
// repository under the user's own registry owner is something they already
// have; publishing it and installing it are both routine. A public GitHub
// folder, a third-party `owner/repo`, or a row out of the public skill index
// is a stranger's SKILL.md, and installing that into agent dot-folders means
// every agent loads a stranger's instructions on every session with no further
// prompt.
//
// So an untrusted import is gated: it publishes into the user's own registry
// and stops there unless the user opts into the durable install, the public
// index's grades are shown with an absent grade named as `unscored`, and a
// Poor safety grade or a hit from the local injection scan needs explicit
// consent. Nothing fetched is ever executed: `add` copies files and never runs
// anything under `scripts/`.
package main

import (
	"context"
	"fmt"
	"io"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/nikships/skills-registry/cli/internal/config"
	"github.com/nikships/skills-registry/cli/internal/discover"
	"github.com/nikships/skills-registry/cli/internal/importgate"
	"github.com/nikships/skills-registry/cli/internal/scan"
	"github.com/nikships/skills-registry/cli/internal/skillscan"
	"github.com/nikships/skills-registry/cli/internal/trust"
	"github.com/nikships/skills-registry/cli/internal/tui"
)

// allowUnsafeFlag is the escape hatch's flag name, quoted in every message
// that tells the user how to proceed anyway.
const allowUnsafeFlag = "--allow-unsafe"

// installFlag opts an untrusted import into the durable agent-folder install.
const installFlag = "--install"

// gate is the verdict for one `add` invocation: where the source came from
// and, for an untrusted source, the per-skill review.
type gate struct {
	assessment trust.Assessment
	// scores are the public index's grades for the source folder. They stay
	// zero when the index has no row for it, which renders as unscored.
	scores importgate.Scores
	// indexed reports whether the index actually had a row, so the renderer
	// can say "not in the index" rather than printing three unscored lines
	// with no explanation.
	indexed bool
	// category is the index's category for the source folder, empty when the
	// index has no row for it or graded it without one. It is stamped onto an
	// untrusted import's SKILL.md copy; an absent category is left absent
	// rather than guessed.
	category string
	// reviews is one entry per skill under review, in the order they were
	// passed. Empty for a trusted source: the gate does not second-guess a
	// folder the user already owns.
	reviews []importgate.Review
}

// untrusted reports whether the gate applies to this source.
func (g gate) untrusted() bool { return g.assessment.Untrusted }

// blocked returns the reviews needing explicit consent.
func (g gate) blocked() []importgate.Review { return importgate.Blocked(g.reviews) }

// review returns the review for one slug.
func (g gate) review(slug string) (importgate.Review, bool) {
	for _, r := range g.reviews {
		if r.Slug == slug {
			return r, true
		}
	}
	return importgate.Review{}, false
}

// assessSource classifies an add source. The registry's own owner is the one
// login treated as the user: a folder inside their registry's account is not a
// third-party import. Any other owner is untrusted even if the user happens to
// own it too, which errs toward showing the gate rather than skipping it.
func assessSource(source string, cfg config.Config, fromDiscover bool) trust.Assessment {
	return trust.Assess(source, trust.Options{
		Owners:       []string{cfg.Owner()},
		FromDiscover: fromDiscover,
	})
}

// indexRow is what the gate reads out of the public index for one source
// folder: the grades it shows before the import, and the category it stamps
// onto the imported copy.
type indexRow struct {
	scores   importgate.Scores
	category string
}

// lookupIndexRow resolves the public index's row for a source URL.
// Swapped in tests so no suite reaches the network.
var lookupIndexRow = func(ctx context.Context, source string) (indexRow, bool, error) {
	res, ok, err := discover.New().Lookup(ctx, source)
	if err != nil || !ok {
		return indexRow{}, false, err
	}
	return indexRow{
		scores: importgate.Scores{
			Safety:        res.Safety,
			Completeness:  res.Completeness,
			Executability: res.Executability,
		},
		category: res.Category,
	}, true, nil
}

// buildGate assesses the source and, when it is untrusted, reviews every
// candidate skill: the index's grades for the source folder plus a local
// heuristic scan of each SKILL.md.
//
// An index lookup failure is not fatal. The index is a convenience, and a
// skill with no grades is already handled — it renders as unscored and still
// needs the user's confirmation — so an unreachable index degrades to exactly
// that rather than blocking the import.
func buildGate(ctx context.Context, source string, cfg config.Config, skills []scan.Skill, fromDiscover bool) (gate, error) {
	g := gate{assessment: assessSource(source, cfg, fromDiscover)}
	if !g.untrusted() {
		return g, nil
	}
	if row, ok, err := lookupIndexRow(ctx, source); err == nil && ok {
		g.scores, g.category, g.indexed = row.scores, row.category, true
	}
	for _, sk := range skills {
		findings, err := skillscan.ScanSkill(sk.Folder, scan.MainFileName)
		if err != nil {
			return gate{}, fmt.Errorf("scan %s: %w", sk.Slug, err)
		}
		// The grades belong to the folder the URL named, so they apply to
		// every skill discovered inside it. For a folder URL that is one
		// skill; for a folder of skills, a Poor grade on the parent holding
		// them back is the safe direction.
		g.reviews = append(g.reviews, importgate.Evaluate(sk.Slug, g.scores, findings))
	}
	return g, nil
}

// renderGate prints the confirmation screen for an untrusted import: the
// origin, the index's grades with an absent grade named, and every scan
// finding. It writes nothing for a trusted source.
func renderGate(w io.Writer, g gate) {
	if !g.untrusted() {
		return
	}
	fmt.Fprintln(w, tui.HintStyle.Render("!  Untrusted source — "+g.assessment.Reason))
	fmt.Fprintln(w, "   Public skill index grades:")
	for _, line := range g.scores.Lines() {
		fmt.Fprintln(w, "     "+line)
	}
	if !g.indexed {
		fmt.Fprintln(w, tui.HintStyle.Render(
			"     (the index has no row for this folder; unscored means unvetted, not safe)"))
	}
	fmt.Fprintln(w, "   Default: publish to your registry only. No agent dot-folder is written")
	fmt.Fprintln(w, "   unless you opt in, and nothing under scripts/ is ever run.")
	renderGateFindings(w, g)
}

// renderGateFindings prints the local scan's hits, or a line stating that it
// matched nothing and what that is worth.
func renderGateFindings(w io.Writer, g gate) {
	total := 0
	for _, r := range g.reviews {
		total += len(r.Findings)
	}
	if total == 0 {
		fmt.Fprintln(w, tui.HintStyle.Render("   Local scan: no suspicious patterns. "+importgate.ScanDisclaimer))
		return
	}
	fmt.Fprintln(w, tui.WarnStyle.Render(fmt.Sprintf("   Local scan: %d suspicious line(s) in %s:",
		total, scan.MainFileName)))
	for _, r := range g.reviews {
		for _, f := range r.Findings {
			fmt.Fprintf(w, "     · %s: %s\n", tui.PreviewSlug.Render(r.Slug), f)
		}
	}
	fmt.Fprintln(w, tui.HintStyle.Render("   "+importgate.ScanDisclaimer))
}

// confirmUntrusted runs the extra confirmation an untrusted import needs.
// A source with no blocker is confirmed by the ordinary publish prompt, so
// this only prompts when something is blocked.
//
// `--allow-unsafe` is the one way past a blocker without a prompt; `--yes`
// deliberately is not, because a user asking to skip prompts has not agreed to
// import a skill graded Poor for safety.
func confirmUntrusted(g gate, opts addOptions) (bool, error) {
	blocked := g.blocked()
	if len(blocked) == 0 || opts.allowUnsafe {
		return true, nil
	}
	if opts.yes {
		return false, blockedError(blocked)
	}
	return confirmChoice(
		fmt.Sprintf("%s. Import anyway?", strings.ToUpper(blocked[0].Summary()[:1])+blocked[0].Summary()[1:]),
		"Nothing is installed into agent folders by this answer; it only allows the registry write.",
		"No, cancel the import",
		"Yes, import despite the warning",
	)
}

// blockedError explains a refusal and names the escape hatch. Used on every
// non-interactive path so the message is identical in JSON and in a scripted
// `--yes` run.
func blockedError(blocked []importgate.Review) error {
	parts := make([]string, 0, len(blocked))
	for _, r := range blocked {
		parts = append(parts, r.Slug+": "+r.Summary())
	}
	return fmt.Errorf("refused %d skill(s) — %s; pass %s to import anyway (%s)",
		len(blocked), strings.Join(parts, " | "), allowUnsafeFlag, importgate.ScanDisclaimer)
}

// confirmChoice runs a two-option prompt whose default (the highlighted first
// row) is the cancelling answer, so pressing enter on a warning is safe.
func confirmChoice(title, prompt, noLabel, yesLabel string) (bool, error) {
	model := tui.NewChoice(title, prompt, []tui.Choice{
		{Value: "no", Label: noLabel, Hint: "Make no changes"},
		{Value: "yes", Label: yesLabel, Hint: "Proceed with the registry write"},
	})
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

// allowedSkills partitions candidates into the ones that may be published and
// the reviews that were refused. A trusted source, and an untrusted one run
// with `--allow-unsafe`, refuse nothing.
func allowedSkills(g gate, skills []scan.Skill, allowUnsafe bool) ([]scan.Skill, []importgate.Review) {
	if !g.untrusted() || allowUnsafe {
		return skills, nil
	}
	allowed := make([]scan.Skill, 0, len(skills))
	var refused []importgate.Review
	for _, sk := range skills {
		r, ok := g.review(sk.Slug)
		if ok && r.Blocked() {
			refused = append(refused, r)
			continue
		}
		allowed = append(allowed, sk)
	}
	return allowed, refused
}

// scanFindingsFor exposes the scan results for one slug, so a caller that only
// wants the findings does not have to reach into the review.
func scanFindingsFor(g gate, slug string) []skillscan.Finding {
	if r, ok := g.review(slug); ok {
		return r.Findings
	}
	return nil
}
