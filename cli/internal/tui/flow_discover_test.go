package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/nikships/skills-registry/cli/internal/scan"
)

// discoverFixtureRows are two rows of the public index: one fully graded, and
// one the index never graded, which is the case every score-rendering
// assertion below turns on.
func discoverFixtureRows() []DiscoverRow {
	return []DiscoverRow{
		{
			Name:          "summarize",
			Desc:          "Summarize URLs, videos, articles, and PDFs.",
			Author:        "openclaw",
			Category:      "AIGC",
			SkillURL:      "https://github.com/openclaw/openclaw/blob/1300b22/skills/summarize",
			Safety:        "Good",
			Completeness:  "Good",
			Executability: "Average",
		},
		{
			Name:     "nano-pdf",
			Desc:     "Edit PDFs with natural-language instructions.",
			Author:   "clawdbot",
			Category: "Productivity",
			SkillURL: "https://github.com/clawdbot/clawdbot/blob/02aeff8/skills/nano-pdf",
		},
	}
}

// failIfAddRuns returns add deps whose every entry point fails the test. Any
// cancelling path must leave all of them untouched.
func failIfAddRuns(t *testing.T) AddFlowDeps {
	t.Helper()
	return AddFlowDeps{
		Resolve: func(context.Context, string) (string, func(), error) {
			t.Error("the import resolved a source even though the user cancelled")
			return "", func() {}, nil
		},
		Discover: func(string, string) ([]scan.Skill, error) {
			t.Error("the import scanned for skills even though the user cancelled")
			return nil, nil
		},
		Slugs: func(context.Context) (map[string]struct{}, error) {
			t.Error("the import read the registry even though the user cancelled")
			return nil, nil
		},
		Files: func(scan.Skill) (map[string][]byte, error) {
			t.Error("the import read skill files even though the user cancelled")
			return nil, nil
		},
		Publish: func(context.Context, string, map[string][]byte, string) (string, error) {
			t.Error("the import published even though the user cancelled")
			return "", nil
		},
		Gate: func(context.Context, string, string, []scan.Skill) (ImportGate, error) {
			t.Error("the import built a gate even though the user cancelled")
			return ImportGate{}, nil
		},
	}
}

// discoverFlowWith drives the flow to the loaded list state with the supplied
// rows, sized wide enough that the preview pane renders.
func discoverFlowWith(t *testing.T, rows []DiscoverRow, deps AddFlowDeps) DiscoverFlowModel {
	t.Helper()
	m := NewDiscoverFlow(context.Background(), DiscoverFlowDeps{
		Query:  "pdf",
		Repo:   "owner/registry",
		Search: func(context.Context) ([]DiscoverRow, error) { return rows, nil },
		Add:    deps,
	})
	next, _ := m.Update(tea.WindowSizeMsg{Width: 140, Height: 32})
	next, _ = next.(DiscoverFlowModel).Update(discoverLoadedMsg{rows: rows})
	mm, ok := next.(DiscoverFlowModel)
	if !ok {
		t.Fatalf("model = %T, want DiscoverFlowModel", next)
	}
	if mm.state != discoverStateList {
		t.Fatalf("state = %v, want discoverStateList", mm.state)
	}
	return mm
}

// TestDiscoverFlowRendersFixtureRows is the construction test: the fixture's
// title, category, description, author, grades, and URL all reach the frame.
func TestDiscoverFlowRendersFixtureRows(t *testing.T) {
	m := discoverFlowWith(t, discoverFixtureRows(), AddFlowDeps{})
	view := m.View()
	for _, want := range []string{
		"summarize",              // title line
		"AIGC",                   // category, shown beside the title
		"Summarize URLs",         // index description
		"author · openclaw",      // meta pane
		"safety:        Good",    // grade, rendered via importgate
		"executability: Average", // grade, rendered via importgate
		"skills/summarize",       // the skill_url the import would fetch
		"Public skill index",     // panel heading
		"2 results",              // header count
	} {
		if !strings.Contains(view, want) {
			t.Errorf("view missing %q:\n%s", want, view)
		}
	}
	// The second fixture row must be listed too, so a user can reach it.
	if !strings.Contains(view, "nano-pdf") {
		t.Errorf("view missing the second fixture row:\n%s", view)
	}
}

// TestDiscoverFlowUngradedRowRendersUnscored is the correctness requirement
// behind the meta pane: a grade the index never assigned must read as
// "unscored", never as a blank cell that looks like a pass.
func TestDiscoverFlowUngradedRowRendersUnscored(t *testing.T) {
	// The ungraded fixture row is second, so move the cursor onto it.
	m := discoverFlowWith(t, discoverFixtureRows(), AddFlowDeps{})
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	mm := next.(DiscoverFlowModel)
	view := mm.View()
	for _, want := range []string{
		"safety:        unscored",
		"completeness:  unscored",
		"executability: unscored",
	} {
		if !strings.Contains(view, want) {
			t.Errorf("ungraded row missing %q:\n%s", want, view)
		}
	}
	// The confirm screen shows the same grades before anything is fetched.
	next, _ = mm.Update(tea.KeyMsg{Type: tea.KeyEnter})
	confirm := next.(DiscoverFlowModel)
	if confirm.state != discoverStateConfirm {
		t.Fatalf("state = %v, want discoverStateConfirm", confirm.state)
	}
	if !strings.Contains(confirm.View(), "unscored") {
		t.Errorf("confirm screen must name the absent grade:\n%s", confirm.View())
	}
}

// TestDiscoverFlowSearchFailureShowsErrorState proves the fail-closed
// behavior: an unreachable index renders an error, not an empty list that
// reads as "no results".
func TestDiscoverFlowSearchFailureShowsErrorState(t *testing.T) {
	boom := errors.New("reach the skill index at http://example.invalid: no such host")
	m := NewDiscoverFlow(context.Background(), DiscoverFlowDeps{
		Query:  "pdf",
		Search: func(context.Context) ([]DiscoverRow, error) { return nil, boom },
		Add:    failIfAddRuns(t),
	})
	next, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	next, _ = next.(DiscoverFlowModel).Update(m.startSearch()())
	mm := next.(DiscoverFlowModel)
	if mm.state != discoverStateError {
		t.Fatalf("state = %v, want discoverStateError", mm.state)
	}
	if !errors.Is(mm.Err(), boom) {
		t.Errorf("Err() = %v, want the search failure", mm.Err())
	}
	view := mm.View()
	if !strings.Contains(view, "could not be searched") {
		t.Errorf("error view must say the search failed:\n%s", view)
	}
	if !strings.Contains(view, "no such host") {
		t.Errorf("error view must carry the underlying cause:\n%s", view)
	}
	// The list chrome must not appear at all: an empty panel next to an error
	// is exactly the ambiguity this state exists to remove.
	if strings.Contains(view, "Public skill index") {
		t.Errorf("error state rendered the result list:\n%s", view)
	}
}

// TestDiscoverFlowSearchFailureIsNotAnEmptyResultSet pins the distinction
// between the two states from the other side: no results is a list, a failure
// is not.
func TestDiscoverFlowSearchFailureIsNotAnEmptyResultSet(t *testing.T) {
	m := discoverFlowWith(t, nil, AddFlowDeps{})
	if m.state != discoverStateList {
		t.Fatalf("state = %v, want discoverStateList for a successful empty search", m.state)
	}
	if m.Err() != nil {
		t.Errorf("Err() = %v, want nil for a successful empty search", m.Err())
	}
	if !strings.Contains(m.View(), "Nothing in the index matched") {
		t.Errorf("an empty result set should say so:\n%s", m.View())
	}
}

// TestDiscoverFlowEscExitsWithoutImporting is the cancellation contract: esc
// closes the picker, nothing is imported, and no add dependency is touched.
func TestDiscoverFlowEscExitsWithoutImporting(t *testing.T) {
	m := discoverFlowWith(t, discoverFixtureRows(), failIfAddRuns(t))
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("esc returned no cmd")
	}
	mm := next.(DiscoverFlowModel)
	if mm.Imported() != 0 {
		t.Errorf("Imported() = %d, want 0 after esc", mm.Imported())
	}
	if mm.Err() != nil {
		t.Errorf("Err() = %v, want nil: a cancelled pick is not a failure", mm.Err())
	}
}

// TestDiscoverFlowQuitExitsWithoutImporting covers the other exit key.
func TestDiscoverFlowQuitExitsWithoutImporting(t *testing.T) {
	m := discoverFlowWith(t, discoverFixtureRows(), failIfAddRuns(t))
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if cmd == nil {
		t.Fatal("q returned no cmd")
	}
	if mm := next.(DiscoverFlowModel); mm.Imported() != 0 {
		t.Errorf("Imported() = %d, want 0 after q", mm.Imported())
	}
}

// TestDiscoverFlowEmptySelectionExitsWithoutImporting proves the empty case:
// enter with no row under the cursor writes nothing rather than importing
// whatever happened to be first.
func TestDiscoverFlowEmptySelectionExitsWithoutImporting(t *testing.T) {
	m := discoverFlowWith(t, nil, failIfAddRuns(t))
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter on an empty list returned no cmd")
	}
	mm := next.(DiscoverFlowModel)
	if mm.state == discoverStateConfirm || mm.state == discoverStateImport {
		t.Fatalf("state = %v, want the flow to exit rather than confirm", mm.state)
	}
	if mm.Imported() != 0 {
		t.Errorf("Imported() = %d, want 0", mm.Imported())
	}
	if toast, ok := mm.Toast(); !ok || !strings.Contains(toast, "nothing selected") {
		t.Errorf("toast = %q (ok=%v), want a neutral 'nothing selected'", toast, ok)
	}
}

// TestDiscoverFlowConfirmDefaultCancelsImport proves the confirm's default
// answer cancels: pressing enter twice from the list must not import.
func TestDiscoverFlowConfirmDefaultCancelsImport(t *testing.T) {
	m := discoverFlowWith(t, discoverFixtureRows(), failIfAddRuns(t))
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	confirm := next.(DiscoverFlowModel)
	if confirm.state != discoverStateConfirm {
		t.Fatalf("state = %v, want discoverStateConfirm", confirm.state)
	}
	// newFlowConfirm puts "yes" first, so cancel explicitly with esc — the
	// path a user takes when they change their mind at the confirm.
	next, _ = confirm.Update(tea.KeyMsg{Type: tea.KeyEsc})
	mm := next.(DiscoverFlowModel)
	if mm.state != discoverStateList {
		t.Fatalf("state = %v, want a return to the list", mm.state)
	}
	if mm.Imported() != 0 {
		t.Errorf("Imported() = %d, want 0 after cancelling the confirm", mm.Imported())
	}
	if toast, ok := mm.Toast(); !ok || !strings.Contains(toast, "nothing was written") {
		t.Errorf("toast = %q (ok=%v), want a 'nothing was written' cancellation", toast, ok)
	}
}

// TestDiscoverFlowConfirmDeclineCancelsImport covers declining at the choice
// rather than escaping out of it.
func TestDiscoverFlowConfirmDeclineCancelsImport(t *testing.T) {
	m := discoverFlowWith(t, discoverFixtureRows(), failIfAddRuns(t))
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	// Move onto "Cancel", then confirm that.
	next, _ = next.(DiscoverFlowModel).Update(tea.KeyMsg{Type: tea.KeyDown})
	next, _ = next.(DiscoverFlowModel).Update(tea.KeyMsg{Type: tea.KeyEnter})
	mm := next.(DiscoverFlowModel)
	if mm.state != discoverStateList {
		t.Fatalf("state = %v, want a return to the list", mm.state)
	}
	if mm.Imported() != 0 {
		t.Errorf("Imported() = %d, want 0 after declining", mm.Imported())
	}
}

// TestDiscoverFlowPoorSafetyConfirmDefaultsToCancel proves pressing enter on a
// row the index graded Poor for safety does not import it. The gate's own
// consent step still follows; this is the earlier of the two guards.
func TestDiscoverFlowPoorSafetyConfirmDefaultsToCancel(t *testing.T) {
	rows := []DiscoverRow{{
		Name:     "pdf-sign",
		Desc:     "Apply a detached signature to a PDF.",
		Author:   "anon-forks",
		Category: "Security",
		SkillURL: "https://github.com/anon-forks/misc/tree/main/pdf-sign",
		Safety:   "Poor",
	}}
	m := discoverFlowWith(t, rows, failIfAddRuns(t))
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	confirm := next.(DiscoverFlowModel)
	if confirm.state != discoverStateConfirm {
		t.Fatalf("state = %v, want discoverStateConfirm", confirm.state)
	}
	if !strings.Contains(confirm.View(), "safety:        Poor") {
		t.Errorf("confirm must show the Poor grade:\n%s", confirm.View())
	}
	// Enter accepts whatever is highlighted, which for a Poor grade must be
	// the cancelling answer.
	next, _ = confirm.Update(tea.KeyMsg{Type: tea.KeyEnter})
	mm := next.(DiscoverFlowModel)
	if mm.state != discoverStateList {
		t.Fatalf("state = %v, want the default answer to cancel back to the list", mm.state)
	}
	if mm.Imported() != 0 {
		t.Errorf("Imported() = %d, want 0", mm.Imported())
	}
}

// TestDiscoverConfirmDefaultAnswers pins the choice ordering both ways, since
// the safe default is the whole point of the branch.
func TestDiscoverConfirmDefaultAnswers(t *testing.T) {
	good := discoverConfirm(discoverFixtureRows()[0], "owner/registry")
	if got := good.Choices[0].Value; got != "yes" {
		t.Errorf("graded-Good default = %v, want yes", got)
	}
	poor := discoverConfirm(DiscoverRow{Name: "x", SkillURL: "u", Safety: "Poor"}, "owner/registry")
	if got := poor.Choices[0].Value; got != "no" {
		t.Errorf("Poor-safety default = %v, want no", got)
	}
}

// TestDiscoverFlowConfirmShowsGradesAndUntrustedDefault proves the confirm
// screen states what is about to happen before anything is fetched.
func TestDiscoverFlowConfirmShowsGradesAndUntrustedDefault(t *testing.T) {
	m := discoverFlowWith(t, discoverFixtureRows(), AddFlowDeps{})
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	view := next.(DiscoverFlowModel).View()
	for _, want := range []string{
		"summarize",
		"safety:",
		"untrusted source",
		"owner/registry",
		"scripts/",
	} {
		if !strings.Contains(view, want) {
			t.Errorf("confirm view missing %q:\n%s", want, view)
		}
	}
}

// TestDiscoverFlowAcceptedConfirmStartsImport proves the accepted path reaches
// the add flow with the row's URL, which is what routes the pick through the
// existing untrusted gate.
func TestDiscoverFlowAcceptedConfirmStartsImport(t *testing.T) {
	var resolved string
	deps := AddFlowDeps{
		Resolve: func(_ context.Context, source string) (string, func(), error) {
			resolved = source
			return t.TempDir(), func() {}, nil
		},
		Discover: func(string, string) ([]scan.Skill, error) {
			return []scan.Skill{{Slug: "summarize", Name: "summarize"}}, nil
		},
		Slugs: func(context.Context) (map[string]struct{}, error) { return map[string]struct{}{}, nil },
		Gate: func(context.Context, string, string, []scan.Skill) (ImportGate, error) {
			return ImportGate{Untrusted: true, Reason: "a public GitHub repository owned by openclaw"}, nil
		},
	}
	m := discoverFlowWith(t, discoverFixtureRows(), deps)
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	next, _ = next.(DiscoverFlowModel).Update(tea.KeyMsg{Type: tea.KeyEnter}) // "yes" is first
	mm := next.(DiscoverFlowModel)
	if mm.state != discoverStateImport {
		t.Fatalf("state = %v, want discoverStateImport", mm.state)
	}
	if mm.addFlow == nil {
		t.Fatal("no add flow was embedded")
	}
	// Run the load the embedded flow kicked off and check the source it used.
	inner, ok := mm.addFlow.(AddFlowModel)
	if !ok {
		t.Fatalf("embedded flow = %T, want AddFlowModel", mm.addFlow)
	}
	inner.startLoad(inner.presetSource)()
	want := "https://github.com/openclaw/openclaw/blob/1300b22/skills/summarize"
	if resolved != want {
		t.Errorf("resolved source = %q, want the row's skill_url %q", resolved, want)
	}
}

// TestDiscoverFlowImportDoneReturnsToList proves a finished import reports its
// count and hands control back to the picker rather than closing it.
func TestDiscoverFlowImportDoneReturnsToList(t *testing.T) {
	m := discoverFlowWith(t, discoverFixtureRows(), AddFlowDeps{})
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	next, _ = next.(DiscoverFlowModel).Update(tea.KeyMsg{Type: tea.KeyEnter})
	next, _ = next.(DiscoverFlowModel).Update(discoverImportDoneMsg{
		exit: addFlowExit{toast: "✓ added 1 skill(s)", ok: true, published: 1},
	})
	mm := next.(DiscoverFlowModel)
	if mm.state != discoverStateList {
		t.Fatalf("state = %v, want a return to the list", mm.state)
	}
	if mm.Imported() != 1 {
		t.Errorf("Imported() = %d, want 1", mm.Imported())
	}
	if toast, ok := mm.Toast(); !ok || !strings.Contains(toast, "added 1") {
		t.Errorf("toast = %q (ok=%v), want the import's own outcome", toast, ok)
	}
}

// TestDiscoverFlowCancelledImportReportsZero proves a cancelled add reports no
// import even though the flow ran.
func TestDiscoverFlowCancelledImportReportsZero(t *testing.T) {
	m := discoverFlowWith(t, discoverFixtureRows(), AddFlowDeps{})
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	next, _ = next.(DiscoverFlowModel).Update(tea.KeyMsg{Type: tea.KeyEnter})
	next, _ = next.(DiscoverFlowModel).Update(discoverImportDoneMsg{
		exit: addFlowExit{toast: "add · cancelled", ok: true},
	})
	if mm := next.(DiscoverFlowModel); mm.Imported() != 0 {
		t.Errorf("Imported() = %d, want 0 for a cancelled import", mm.Imported())
	}
}

// TestDiscoverFlowRowWithNoURLIsNotImportable proves a malformed index row
// cannot start an import: there is nothing to fetch.
func TestDiscoverFlowRowWithNoURLIsNotImportable(t *testing.T) {
	rows := []DiscoverRow{{Name: "broken", Desc: "no url", Category: "Misc"}}
	m := discoverFlowWith(t, rows, failIfAddRuns(t))
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	mm := next.(DiscoverFlowModel)
	if mm.state != discoverStateList {
		t.Fatalf("state = %v, want to stay on the list", mm.state)
	}
	if toast, ok := mm.Toast(); ok || !strings.Contains(toast, "no URL") {
		t.Errorf("toast = %q (ok=%v), want an error naming the missing URL", toast, ok)
	}
}

// TestDiscoverFlowHostedExitUsesOnExit proves the flow is embeddable: with
// OnExit wired, closing it yields the host's message instead of quitting the
// program, which is how the hub card will consume it.
func TestDiscoverFlowHostedExitUsesOnExit(t *testing.T) {
	m := discoverFlowWith(t, discoverFixtureRows(), AddFlowDeps{}).
		WithOnExit(func(final DiscoverFlowModel) tea.Msg {
			toast, ok := final.Toast()
			return flowExitMsg{toast: toast, ok: ok}
		})
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("esc returned no cmd")
	}
	msg, ok := cmd().(flowExitMsg)
	if !ok {
		t.Fatalf("cmd returned %T, want flowExitMsg", cmd())
	}
	if !msg.ok || !strings.Contains(msg.toast, "closed") {
		t.Errorf("flowExitMsg = %+v, want a neutral close", msg)
	}
}

// TestDiscoverFlowStandaloneExitQuits pins the other half of the same
// contract: with no OnExit the flow ends the program, which is what the cobra
// command needs.
func TestDiscoverFlowStandaloneExitQuits(t *testing.T) {
	m := discoverFlowWith(t, discoverFixtureRows(), AddFlowDeps{})
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("esc returned no cmd")
	}
	if _, isQuit := cmd().(tea.QuitMsg); !isQuit {
		t.Fatalf("standalone esc returned %T, want tea.QuitMsg", cmd())
	}
}

// TestDiscoverFlowDoesNotSortByPopularity pins the index's own ranking. The
// row type carries no star count at all, so the only way the picker could
// re-rank is by re-sorting, which it must not do.
func TestDiscoverFlowDoesNotSortByPopularity(t *testing.T) {
	rows := discoverFixtureRows()
	m := discoverFlowWith(t, rows, AddFlowDeps{})
	for i, item := range m.list.Items() {
		got, ok := item.(DiscoverRow)
		if !ok {
			t.Fatalf("item %d = %T, want DiscoverRow", i, item)
		}
		if got.Name != rows[i].Name {
			t.Errorf("row %d = %q, want %q: the index's ranking must be preserved",
				i, got.Name, rows[i].Name)
		}
	}
}

// TestDiscoverFlowFilterSwallowsQuit proves "q" types into the filter instead
// of closing the picker while the filter input is active.
func TestDiscoverFlowFilterSwallowsQuit(t *testing.T) {
	m := discoverFlowWith(t, discoverFixtureRows(), failIfAddRuns(t))
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	next, _ = next.(DiscoverFlowModel).Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	mm := next.(DiscoverFlowModel)
	if mm.list.FilterValue() != "q" {
		t.Errorf("filter = %q, want the typed %q", mm.list.FilterValue(), "q")
	}
}

// TestDiscoverFlowNarrowTerminalDropsPreview proves the layout degrades the
// same way the registry list's does rather than overflowing.
func TestDiscoverFlowNarrowTerminalDropsPreview(t *testing.T) {
	m := NewDiscoverFlow(context.Background(), DiscoverFlowDeps{
		Query:  "pdf",
		Search: func(context.Context) ([]DiscoverRow, error) { return discoverFixtureRows(), nil },
	})
	next, _ := m.Update(tea.WindowSizeMsg{Width: 70, Height: 24})
	next, _ = next.(DiscoverFlowModel).Update(discoverLoadedMsg{rows: discoverFixtureRows()})
	mm := next.(DiscoverFlowModel)
	if mm.previewW != 0 {
		t.Errorf("previewW = %d, want 0 on a narrow terminal", mm.previewW)
	}
	if strings.Contains(mm.View(), "✧ Preview") {
		t.Errorf("narrow terminal still rendered the preview pane:\n%s", mm.View())
	}
}

// TestDiscoverFlowUnconfiguredSearchIsAnError proves a missing dependency
// surfaces as the error state rather than as an empty list.
func TestDiscoverFlowUnconfiguredSearchIsAnError(t *testing.T) {
	m := NewDiscoverFlow(context.Background(), DiscoverFlowDeps{Query: "pdf"})
	next, _ := m.Update(m.startSearch()())
	mm := next.(DiscoverFlowModel)
	if mm.state != discoverStateError {
		t.Fatalf("state = %v, want discoverStateError", mm.state)
	}
	if mm.Err() == nil {
		t.Error("Err() = nil, want the configuration failure")
	}
}

// TestDiscoverRowListRowProjection pins how a row maps onto the shared list
// delegate: the name is the title, the category is the right-hand column, and
// the index description is the body.
func TestDiscoverRowListRowProjection(t *testing.T) {
	row := discoverFixtureRows()[0]
	got := row.listRow()
	if got.Name != "summarize" || got.Slug != "AIGC" || got.Desc != row.Desc {
		t.Errorf("listRow() = %+v, want name/category/description from the row", got)
	}
	// A row the index published without a name falls back to its URL so it is
	// still reachable rather than rendering as a blank line.
	unnamed := DiscoverRow{SkillURL: "https://github.com/o/r/tree/main/skills/x"}
	if unnamed.Title() != unnamed.SkillURL {
		t.Errorf("Title() = %q, want the URL fallback", unnamed.Title())
	}
}

// TestDiscoverRowFilterValueCoversMeta proves filtering reaches the author and
// category, not just the name.
func TestDiscoverRowFilterValueCoversMeta(t *testing.T) {
	fv := discoverFixtureRows()[0].FilterValue()
	for _, want := range []string{"summarize", "openclaw", "AIGC", "PDFs"} {
		if !strings.Contains(fv, want) {
			t.Errorf("FilterValue() = %q, missing %q", fv, want)
		}
	}
}
