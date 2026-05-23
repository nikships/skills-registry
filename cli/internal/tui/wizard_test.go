package tui

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// TestNewWizardStartsAtScan pins down the first-step contract. Subsequent
// features (F2.2 / F2.3) drive their step transitions off this baseline,
// so a regression here would propagate everywhere.
func TestNewWizardStartsAtScan(t *testing.T) {
	m := NewWizard(context.Background())
	if m.Step() != WizardStepScan {
		t.Fatalf("step = %v, want WizardStepScan", m.Step())
	}
	if m.Cancelled() {
		t.Error("fresh wizard reports Cancelled()=true")
	}
	if m.Completed() {
		t.Error("fresh wizard reports Completed()=true")
	}
}

// TestWizardStepTitlesAreNonEmpty guards against an enum value being added
// without a matching label in WizardStep.Title — a forgotten Title would
// render the progress indicator as "Step 5 / 8 · Unknown".
func TestWizardStepTitlesAreNonEmpty(t *testing.T) {
	for s := WizardStepScan; s <= WizardStepDone; s++ {
		if title := s.Title(); title == "" || title == "Unknown" {
			t.Errorf("step %d has placeholder title %q", s, title)
		}
	}
}

// TestWizardEscOpensCancelOverlay covers the WIZARD-013 trigger: a bare Esc
// must show a confirmation, not abort immediately.
func TestWizardEscOpensCancelOverlay(t *testing.T) {
	m := NewWizard(context.Background())
	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	wiz, ok := nm.(WizardModel)
	if !ok {
		t.Fatalf("Update returned %T, want WizardModel", nm)
	}
	if !wiz.cancelOverlay {
		t.Fatal("Esc did not open the cancel overlay")
	}
	if wiz.Cancelled() {
		t.Fatal("Esc set Cancelled()=true before the user confirmed")
	}
	if !strings.Contains(wiz.View(), "Cancel onboarding?") {
		t.Errorf("View() does not surface cancel overlay copy:\n%s", wiz.View())
	}
}

// TestWizardCancelOverlayYesConfirms exercises the WIZARD-013 confirm path:
// "y" (or enter while the destructive button is focused) must set Cancelled
// and emit tea.Quit so the launcher returns without falling through to
// bootstrap.
func TestWizardCancelOverlayYesConfirms(t *testing.T) {
	m := NewWizard(context.Background())
	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	nm, cmd := nm.(WizardModel).Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	wiz := nm.(WizardModel)
	if !wiz.Cancelled() {
		t.Fatal(`Cancelled() = false after typing "y" in confirmation`)
	}
	if cmd == nil {
		t.Fatal("Confirming cancel did not return tea.Quit Cmd")
	}
}

// TestWizardCancelOverlayDismissed verifies the safe-default path: "n" or
// Esc dismisses the overlay and keeps the wizard running.
func TestWizardCancelOverlayDismissed(t *testing.T) {
	for _, key := range []tea.KeyMsg{
		{Type: tea.KeyRunes, Runes: []rune("n")},
		{Type: tea.KeyEsc},
	} {
		m := NewWizard(context.Background())
		nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
		nm, _ = nm.(WizardModel).Update(key)
		wiz := nm.(WizardModel)
		if wiz.cancelOverlay {
			t.Errorf("key %q did not dismiss cancel overlay", key)
		}
		if wiz.Cancelled() {
			t.Errorf("key %q set Cancelled() while dismissing", key)
		}
	}
}

// TestWizardCtrlCAlwaysQuits is the unconditional escape hatch — Ctrl+C
// must abort even when the cancel overlay is open or a transition is
// running. Terminal users expect this regardless of the model state.
func TestWizardCtrlCAlwaysQuits(t *testing.T) {
	m := NewWizard(context.Background())
	nm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	wiz := nm.(WizardModel)
	if !wiz.Cancelled() {
		t.Fatal("Ctrl+C did not set Cancelled()")
	}
	if cmd == nil {
		t.Fatal("Ctrl+C did not return a Quit Cmd")
	}
}

// TestWizardEnterStartsTransition exercises the WIZARD-012 trigger path:
// the indicator advances on transition (visible target step) and the
// transition flag is set so the panel renders the spinner placeholder.
func TestWizardEnterStartsTransition(t *testing.T) {
	m := NewWizard(context.Background())
	nm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	wiz := nm.(WizardModel)
	if !wiz.transitioning {
		t.Fatal("Enter did not start a transition")
	}
	if wiz.transitionTarget != WizardStepRepoName {
		t.Errorf("transitionTarget = %v, want WizardStepRepoName", wiz.transitionTarget)
	}
	if cmd == nil {
		t.Fatal("Enter returned nil Cmd; expected wizardTransition tick")
	}
}

// TestWizardTransitionMessageAdvancesStep mirrors the actual delivery path:
// the wizardTransitionMsg fired by wizardTransition() arrives and the step
// swaps + the transitioning flag clears.
func TestWizardTransitionMessageAdvancesStep(t *testing.T) {
	m := NewWizard(context.Background())
	m.transitioning = true
	m.transitionTarget = WizardStepRepoName
	nm, _ := m.Update(wizardTransitionMsg{to: WizardStepRepoName})
	wiz := nm.(WizardModel)
	if wiz.Step() != WizardStepRepoName {
		t.Fatalf("step = %v, want WizardStepRepoName", wiz.Step())
	}
	if wiz.transitioning {
		t.Fatal("transitioning flag still set after wizardTransitionMsg")
	}
}

// TestWizardEnterOnDoneCompletes verifies the terminal-step semantics:
// pressing Enter on WizardStepDone hands control back to the launcher
// (Completed()=true + tea.Quit), no further transitions.
func TestWizardEnterOnDoneCompletes(t *testing.T) {
	m := NewWizard(context.Background())
	m.step = WizardStepDone
	nm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	wiz := nm.(WizardModel)
	if !wiz.Completed() {
		t.Fatal("Enter on Done did not set Completed()")
	}
	if wiz.Cancelled() {
		t.Fatal("Enter on Done flipped Cancelled() = true")
	}
	if cmd == nil {
		t.Fatal("Enter on Done did not return a Quit Cmd")
	}
}

// TestWizardEnterDuringTransitionIsNoop guards against a user mashing
// enter mid-animation and stacking transitions. The frame stays where
// it was until the in-flight wizardTransitionMsg resolves.
func TestWizardEnterDuringTransitionIsNoop(t *testing.T) {
	m := NewWizard(context.Background())
	m.transitioning = true
	m.transitionTarget = WizardStepRepoName
	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	wiz := nm.(WizardModel)
	if wiz.transitionTarget != WizardStepRepoName {
		t.Errorf("enter-during-transition shifted target to %v", wiz.transitionTarget)
	}
}

// TestWizardViewSurfacesChrome is the WIZARD-014 / -012 smoke test. The
// rendered View() must include the hero copy, the step indicator caption
// and the footer keybinding hints — these are the three visual contracts
// tuistory will assert on.
func TestWizardViewSurfacesChrome(t *testing.T) {
	m := NewWizard(context.Background())
	m.width, m.height = 100, 30
	v := m.View()
	wants := []string{
		"Skills Registry",   // hero
		"Step 1 / 8",        // progress caption
		"Scan local skills", // current step title
		"enter",             // footer
		"esc",               // footer
	}
	for _, want := range wants {
		if !strings.Contains(v, want) {
			t.Errorf("View() missing %q:\n%s", want, v)
		}
	}
}

// TestWizardViewIndicatorAdvancesWithTransition is the direct check for
// WIZARD-012: the step indicator caption must include the upcoming step
// number while the transition animation is in flight, not the old one.
func TestWizardViewIndicatorAdvancesWithTransition(t *testing.T) {
	m := NewWizard(context.Background())
	m.width, m.height = 100, 30
	m.transitioning = true
	m.transitionTarget = WizardStepVisibility
	v := m.View()
	if !strings.Contains(v, "Step 3 / 8") {
		t.Errorf("View() did not surface upcoming step during transition:\n%s", v)
	}
}

// TestWizardViewRendersAtMultipleSizes catches reflow regressions: the
// wizard should render without panicking at 40x12 (narrow), 80x24 (the
// pre-flight minimum), and 200x60 (wide).
func TestWizardViewRendersAtMultipleSizes(t *testing.T) {
	for _, dim := range []struct{ w, h int }{
		{40, 12},
		{80, 24},
		{200, 60},
	} {
		m := NewWizard(context.Background())
		m.width, m.height = dim.w, dim.h
		if v := m.View(); v == "" {
			t.Errorf("View() returned empty at %dx%d", dim.w, dim.h)
		}
	}
}

// TestWizardViewWithoutWindowSize ensures the model degrades gracefully
// when no tea.WindowSizeMsg has arrived yet (m.width / m.height == 0).
// Bubble Tea fires the first WindowSizeMsg shortly after Init, so the
// first render can land before it. lipgloss.Place would panic on a
// zero-sized canvas, so renderFrame must skip the centering call.
func TestWizardViewWithoutWindowSize(t *testing.T) {
	m := NewWizard(context.Background())
	if v := m.View(); v == "" {
		t.Fatal("View() returned empty before WindowSizeMsg")
	}
}
