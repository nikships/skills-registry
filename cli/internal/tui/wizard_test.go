package tui

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/nikships/skills-registry/cli/internal/scan"
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
// With no Scan dep wired the model treats the scan as instantly done,
// so enter at Scan advances to RepoName.
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
func TestWizardViewWithoutWindowSize(t *testing.T) {
	m := NewWizard(context.Background())
	if v := m.View(); v == "" {
		t.Fatal("View() returned empty before WindowSizeMsg")
	}
}

// ────────────────────────────────────────────────────────────────────────────
// F2.2 — Steps 1–4
// ────────────────────────────────────────────────────────────────────────────

// testSkills is a small fixture used by the scan/push tests; the slugs and
// names are deliberately different so the preview rendering's "skip when
// slug matches name" branch isn't always exercised.
var testSkills = []scan.Skill{
	{Slug: "alpha_skill", Name: "Alpha Skill", Description: "first"},
	{Slug: "beta_skill", Name: "Beta Skill", Description: "second"},
	{Slug: "gamma_skill", Name: "Gamma Skill", Description: "third"},
}

// TestWizardInitWithoutScanDepEmitsScanDone confirms that the Init Cmd
// pipeline auto-resolves the scan step in test mode so the rest of the
// state machine stays reachable without standing up real I/O.
func TestWizardInitWithoutScanDepEmitsScanDone(t *testing.T) {
	m := NewWizard(context.Background())
	cmd := m.Init()
	if cmd == nil {
		t.Fatal("Init returned nil Cmd")
	}
	// Drain the batch. Each Cmd returns either a tea.Msg or nil; we want
	// to see a wizardScanDoneMsg fall out.
	msgs := collectMsgs(cmd)
	if !containsMsgKind(msgs, wizardScanDoneMsg{}) {
		t.Fatalf("Init batch did not emit wizardScanDoneMsg; got %+v", msgs)
	}
}

// TestWizardInitWithScanDepRunsCallback verifies the scan dep is invoked
// once and the result lands as a wizardScanDoneMsg.
func TestWizardInitWithScanDepRunsCallback(t *testing.T) {
	var calls int32
	deps := WizardDeps{
		Scan: func(_ context.Context) ([]scan.Skill, error) {
			calls++
			return testSkills, nil
		},
	}
	m := NewWizard(context.Background()).WithDeps(deps)
	cmd := m.Init()
	msgs := collectMsgs(cmd)
	if calls != 1 {
		t.Fatalf("Scan called %d times, want 1", calls)
	}
	found := false
	for _, msg := range msgs {
		if done, ok := msg.(wizardScanDoneMsg); ok {
			found = true
			if len(done.skills) != len(testSkills) {
				t.Errorf("emitted skills = %d, want %d", len(done.skills), len(testSkills))
			}
		}
	}
	if !found {
		t.Fatalf("Init batch did not emit wizardScanDoneMsg; got %+v", msgs)
	}
}

// TestWizardScanDoneStartsRevealAnimation pins down the WIZARD-002
// behavior: receiving the scan results kicks off the animated counter
// that climbs from 0 to len(skills).
func TestWizardScanDoneStartsRevealAnimation(t *testing.T) {
	m := NewWizard(context.Background())
	nm, cmd := m.Update(wizardScanDoneMsg{skills: testSkills})
	wiz := nm.(WizardModel)
	if !wiz.scanDone {
		t.Fatal("scanDone not set after wizardScanDoneMsg")
	}
	if len(wiz.Skills()) != len(testSkills) {
		t.Fatalf("skills count = %d, want %d", len(wiz.Skills()), len(testSkills))
	}
	if cmd == nil {
		t.Fatal("scan done did not arm the reveal tick")
	}
	if wiz.scanReveal != 0 {
		t.Errorf("scanReveal = %d before first tick, want 0", wiz.scanReveal)
	}
}

// TestWizardScanRevealCountsUp drives the animated counter until it
// catches up to len(skills) and verifies the tick stops re-arming once
// fully revealed.
func TestWizardScanRevealCountsUp(t *testing.T) {
	m := NewWizard(context.Background())
	nm, _ := m.Update(wizardScanDoneMsg{skills: testSkills})
	for i := 1; i <= len(testSkills); i++ {
		nm, _ = nm.(WizardModel).Update(wizardScanRevealMsg{})
		wiz := nm.(WizardModel)
		if wiz.scanReveal != i {
			t.Fatalf("after tick %d: scanReveal = %d, want %d", i, wiz.scanReveal, i)
		}
	}
	// Subsequent ticks should be a no-op (we've revealed everything).
	nm, cmd := nm.(WizardModel).Update(wizardScanRevealMsg{})
	wiz := nm.(WizardModel)
	if wiz.scanReveal != len(testSkills) {
		t.Fatalf("scanReveal kept climbing past len(skills): %d", wiz.scanReveal)
	}
	if cmd != nil {
		t.Errorf("reveal tick kept re-arming after full reveal; got cmd %v", cmd)
	}
}

// TestWizardScanEnterRequiresDepResolution makes sure a wired scan
// (deps.Scan != nil) blocks advance until scanDone fires. The user
// shouldn't be racing the spinner against a slow filesystem.
func TestWizardScanEnterRequiresDepResolution(t *testing.T) {
	deps := WizardDeps{
		Scan: func(_ context.Context) ([]scan.Skill, error) {
			return nil, nil
		},
	}
	m := NewWizard(context.Background()).WithDeps(deps)
	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	wiz := nm.(WizardModel)
	if wiz.transitioning {
		t.Fatal("enter advanced before scanDone landed")
	}
}

// TestWizardScanEnterAdvancesAfterDone confirms enter advances once the
// scan resolves, even when there are zero discovered skills.
func TestWizardScanEnterAdvancesAfterDone(t *testing.T) {
	deps := WizardDeps{
		Scan: func(_ context.Context) ([]scan.Skill, error) {
			return nil, nil
		},
	}
	m := NewWizard(context.Background()).WithDeps(deps)
	nm, _ := m.Update(wizardScanDoneMsg{})
	nm, _ = nm.(WizardModel).Update(tea.KeyMsg{Type: tea.KeyEnter})
	wiz := nm.(WizardModel)
	if !wiz.transitioning {
		t.Fatal("enter did not start transition after scanDone")
	}
	if wiz.transitionTarget != WizardStepRepoName {
		t.Errorf("transitionTarget = %v, want WizardStepRepoName", wiz.transitionTarget)
	}
}

// TestWizardScanViewSurfacesCount checks that the rendered scan panel
// shows the animated count of discovered skills (WIZARD-002).
func TestWizardScanViewSurfacesCount(t *testing.T) {
	m := NewWizard(context.Background())
	m.width, m.height = 120, 40
	nm, _ := m.Update(wizardScanDoneMsg{skills: testSkills})
	// Reveal everything so the headline is stable.
	for range testSkills {
		nm, _ = nm.(WizardModel).Update(wizardScanRevealMsg{})
	}
	v := nm.(WizardModel).View()
	if !strings.Contains(v, "3") || !strings.Contains(v, "discovered locally") {
		t.Errorf("scan panel did not surface skill count headline:\n%s", v)
	}
	if !strings.Contains(v, "Alpha Skill") {
		t.Errorf("scan panel preview missing skill name:\n%s", v)
	}
}

// TestWizardScanViewSpinnerWhilePending is the "still working" check for
// WIZARD-002 — before scanDone, the panel shows the spinner glyph and the
// "Looking for SKILL.md files…" copy.
func TestWizardScanViewSpinnerWhilePending(t *testing.T) {
	deps := WizardDeps{
		Scan: func(_ context.Context) ([]scan.Skill, error) {
			return nil, nil
		},
	}
	m := NewWizard(context.Background()).WithDeps(deps)
	m.width, m.height = 120, 40
	v := m.View()
	if !strings.Contains(v, "Looking for SKILL.md files") {
		t.Errorf("scan panel did not surface pending copy:\n%s", v)
	}
}

// TestWizardRepoNameInputAcceptsTyping forwards a key into the embedded
// textinput and verifies the running value is captured.
func TestWizardRepoNameInputAcceptsTyping(t *testing.T) {
	m := atStep(WizardStepRepoName)
	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}})
	nm, _ = nm.(WizardModel).Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	wiz := nm.(WizardModel)
	if got := wiz.repoInput.Value(); got != "hi" {
		t.Errorf("textinput value = %q, want %q", got, "hi")
	}
}

// TestWizardRepoNameEnterRejectsEmpty is the WIZARD-003 validation check.
// Submitting an empty value must surface an inline error and refuse to
// advance.
func TestWizardRepoNameEnterRejectsEmpty(t *testing.T) {
	m := atStep(WizardStepRepoName)
	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	wiz := nm.(WizardModel)
	if wiz.transitioning {
		t.Fatal("enter advanced with empty repo name")
	}
	if wiz.repoErr == "" {
		t.Error("repoErr not surfaced after empty submission")
	}
	wiz.width, wiz.height = 120, 30
	v := wiz.View()
	if !strings.Contains(v, "can't be empty") {
		t.Errorf("View() does not surface validation error:\n%s", v)
	}
}

// TestWizardRepoNameEnterAdvancesWithValue verifies the happy path —
// non-empty input transitions to the Visibility step.
func TestWizardRepoNameEnterAdvancesWithValue(t *testing.T) {
	m := atStep(WizardStepRepoName)
	m.repoInput.SetValue("my-registry")
	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	wiz := nm.(WizardModel)
	if !wiz.transitioning {
		t.Fatal("valid repo name did not start transition")
	}
	if wiz.transitionTarget != WizardStepVisibility {
		t.Errorf("transitionTarget = %v, want WizardStepVisibility", wiz.transitionTarget)
	}
}

// TestWizardRepoNameViewSurfacesInput confirms the textinput renders
// inside the focused panel (WIZARD-003 visual contract). The bubbles
// textinput dims placeholder text via per-rune ANSI styling so we don't
// assert the placeholder text directly; the prompt glyph + step title
// being present is enough proof the panel wraps the input.
func TestWizardRepoNameViewSurfacesInput(t *testing.T) {
	m := atStep(WizardStepRepoName)
	m.width, m.height = 120, 30
	v := m.View()
	wants := []string{
		"Name your registry", // step title in panel + indicator
		"›",                  // textinput prompt
		"continue",           // CTA copy
	}
	for _, w := range wants {
		if !strings.Contains(v, w) {
			t.Errorf("RepoName view missing %q:\n%s", w, v)
		}
	}
}

// TestWizardVisibilityArrowsMoveCursor checks WIZARD-004 navigation:
// left/right (and h/l) shuttle between the two cards.
func TestWizardVisibilityArrowsMoveCursor(t *testing.T) {
	m := atStep(WizardStepVisibility)
	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyRight})
	wiz := nm.(WizardModel)
	if wiz.visCursor != 1 {
		t.Errorf("right arrow visCursor = %d, want 1", wiz.visCursor)
	}
	nm, _ = wiz.Update(tea.KeyMsg{Type: tea.KeyLeft})
	if nm.(WizardModel).visCursor != 0 {
		t.Errorf("left arrow visCursor = %d, want 0", nm.(WizardModel).visCursor)
	}
	// h/l aliases
	nm, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
	if nm.(WizardModel).visCursor != 1 {
		t.Errorf(`"l" alias visCursor = %d, want 1`, nm.(WizardModel).visCursor)
	}
}

// TestWizardVisibilityEnterLocksChoice exercises the WIZARD-004 commit
// path. The locked-in visibility must match the focused card and the
// step must transition to Push.
func TestWizardVisibilityEnterLocksChoice(t *testing.T) {
	for _, tc := range []struct {
		cursor int
		want   string
	}{
		{0, "private"},
		{1, "public"},
	} {
		m := atStep(WizardStepVisibility)
		m.visCursor = tc.cursor
		nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
		wiz := nm.(WizardModel)
		if wiz.visibility != tc.want {
			t.Errorf("cursor %d: visibility = %q, want %q", tc.cursor, wiz.visibility, tc.want)
		}
		if !wiz.transitioning {
			t.Errorf("cursor %d: enter did not advance", tc.cursor)
		}
		if wiz.transitionTarget != WizardStepPush {
			t.Errorf("cursor %d: target = %v, want WizardStepPush", tc.cursor, wiz.transitionTarget)
		}
	}
}

// TestWizardVisibilityViewSurfacesCards confirms WIZARD-004 surfaces both
// card options (Private and Public) inside the wizard frame.
func TestWizardVisibilityViewSurfacesCards(t *testing.T) {
	m := atStep(WizardStepVisibility)
	m.width, m.height = 120, 30
	v := m.View()
	wants := []string{"Private", "Public", "selected"}
	for _, w := range wants {
		if !strings.Contains(v, w) {
			t.Errorf("visibility view missing %q:\n%s", w, v)
		}
	}
}

// TestWizardPushOnEnterAutoStarts is the WIZARD-005 auto-start: landing
// on the push step kicks off the push goroutine without a key press.
func TestWizardPushOnEnterAutoStarts(t *testing.T) {
	var (
		called int32
		mu     sync.Mutex
	)
	deps := WizardDeps{
		Push: func(_ context.Context, _ string, _ []scan.Skill,
			_ func(int, int), _ func(string)) (int, error) {
			mu.Lock()
			called++
			mu.Unlock()
			return 0, nil
		},
	}
	m := NewWizard(context.Background()).WithDeps(deps)
	m.step = WizardStepVisibility
	m.visCursor = 0
	m.repoInput.SetValue("test-registry")
	m.skills = testSkills
	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	nm, _ = nm.(WizardModel).Update(wizardTransitionMsg{to: WizardStepPush})
	wiz := nm.(WizardModel)
	if !wiz.pushStarted {
		t.Fatal("pushStarted = false after transitioning to Push step")
	}
	if wiz.pushCh == nil {
		t.Fatal("pushCh = nil after transitioning to Push step")
	}
	// Drain the channel so the goroutine can exit and we observe the
	// terminal wizardPushDoneMsg.
	deadline := time.After(time.Second)
	for {
		select {
		case _, ok := <-wiz.pushCh:
			if !ok {
				goto done
			}
		case <-deadline:
			t.Fatal("push goroutine did not finish within 1s")
		}
	}
done:
	mu.Lock()
	defer mu.Unlock()
	if called != 1 {
		t.Errorf("Push dep called %d times, want 1", called)
	}
}

// TestWizardPushProgressUpdatesState verifies the wire-up between
// wizardPushProgressMsg and the rendered state.
func TestWizardPushProgressUpdatesState(t *testing.T) {
	m := atStep(WizardStepPush)
	m.pushCh = make(chan tea.Msg, 4)
	nm, _ := m.Update(wizardPushProgressMsg{done: 3, total: 10, status: "uploading…"})
	wiz := nm.(WizardModel)
	if wiz.pushDoneFiles != 3 || wiz.pushTotalFiles != 10 {
		t.Errorf("progress = %d/%d, want 3/10", wiz.pushDoneFiles, wiz.pushTotalFiles)
	}
	if wiz.pushStatus != "uploading…" {
		t.Errorf("status = %q, want %q", wiz.pushStatus, "uploading…")
	}
}

// TestWizardPushDoneCompletesStep verifies that a terminal push message
// flips pushDone, records the repo + pushed count, and stops the spinner.
func TestWizardPushDoneCompletesStep(t *testing.T) {
	m := atStep(WizardStepPush)
	nm, _ := m.Update(wizardPushDoneMsg{repo: "owner/repo", pushed: 7})
	wiz := nm.(WizardModel)
	if !wiz.pushDone {
		t.Fatal("pushDone = false after wizardPushDoneMsg")
	}
	if wiz.Repo() != "owner/repo" {
		t.Errorf("Repo() = %q, want %q", wiz.Repo(), "owner/repo")
	}
	if wiz.Pushed() != 7 {
		t.Errorf("Pushed() = %d, want 7", wiz.Pushed())
	}
}

// TestWizardPushEnterAfterDoneAdvances confirms enter advances to
// AgentSelect once the push has completed successfully.
func TestWizardPushEnterAfterDoneAdvances(t *testing.T) {
	m := atStep(WizardStepPush)
	m.pushDone = true
	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	wiz := nm.(WizardModel)
	if !wiz.transitioning {
		t.Fatal("enter after pushDone did not advance")
	}
	if wiz.transitionTarget != WizardStepAgentSelect {
		t.Errorf("target = %v, want WizardStepAgentSelect", wiz.transitionTarget)
	}
}

// TestWizardPushEnterBeforeDoneIsNoop confirms enter is swallowed while
// the push is still in flight — the user can't skip past a half-pushed
// registry.
func TestWizardPushEnterBeforeDoneIsNoop(t *testing.T) {
	m := atStep(WizardStepPush)
	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	wiz := nm.(WizardModel)
	if wiz.transitioning {
		t.Fatal("enter advanced before pushDone")
	}
}

// TestWizardPushEnterAfterErrorCancels — when push errors, enter cleanly
// exits the wizard instead of pretending we have a working registry.
func TestWizardPushEnterAfterErrorCancels(t *testing.T) {
	m := atStep(WizardStepPush)
	m.pushDone = true
	m.pushErr = errors.New("boom")
	nm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	wiz := nm.(WizardModel)
	if !wiz.Cancelled() {
		t.Fatal("enter after pushErr did not set Cancelled()")
	}
	if cmd == nil {
		t.Fatal("enter after pushErr did not return a Quit Cmd")
	}
}

// TestWizardPushViewSurfacesProgressBar checks WIZARD-005: the push panel
// renders the animated progress copy ("uploaded N/M files") once we're
// receiving updates.
func TestWizardPushViewSurfacesProgressBar(t *testing.T) {
	m := atStep(WizardStepPush)
	m.width, m.height = 120, 30
	m.repoInput.SetValue("my-registry")
	m.visibility = "private"
	m.pushDoneFiles = 5
	m.pushTotalFiles = 20
	m.pushStatus = "uploading skills…"
	v := m.View()
	if !strings.Contains(v, "uploaded 5 / 20 files") {
		t.Errorf("push view missing progress caption:\n%s", v)
	}
	if !strings.Contains(v, "uploading skills") {
		t.Errorf("push view missing status line:\n%s", v)
	}
}

// TestWizardPushViewSuccessCTA checks the WIZARD-005 happy-path closing
// state: the bar reads "pushed N skills" and the CTA hints at advancing
// to agent install.
func TestWizardPushViewSuccessCTA(t *testing.T) {
	m := atStep(WizardStepPush)
	m.width, m.height = 120, 30
	m.repoInput.SetValue("my-registry")
	m.visibility = "public"
	m.skills = testSkills
	m.pushDone = true
	m.pushed = 3
	m.pushTotalFiles = 12
	m.pushDoneFiles = 12
	v := m.View()
	if !strings.Contains(v, "pushed 3 skill") {
		t.Errorf("push panel did not surface success caption:\n%s", v)
	}
	if !strings.Contains(v, "agent install") {
		t.Errorf("push panel did not surface success CTA:\n%s", v)
	}
}

// TestRunPushJobInvokesDepsInOrder validates the WIZARD-010 contract by
// asserting CreateRepo → SaveConfig → Push fire in the right order with
// the right repo slug.
func TestRunPushJobInvokesDepsInOrder(t *testing.T) {
	var (
		order  []string
		repoIn string
		mu     sync.Mutex
	)
	deps := WizardDeps{
		CreateRepo: func(_ context.Context, name, _ string) (string, error) {
			mu.Lock()
			order = append(order, "create")
			mu.Unlock()
			return "owner/" + name, nil
		},
		SaveConfig: func(repo string) error {
			mu.Lock()
			order = append(order, "save")
			repoIn = repo
			mu.Unlock()
			return nil
		},
		Push: func(_ context.Context, repo string, _ []scan.Skill,
			progress func(int, int), status func(string)) (int, error) {
			mu.Lock()
			order = append(order, "push:"+repo)
			mu.Unlock()
			progress(2, 4)
			status("almost done")
			return 2, nil
		},
	}
	ch := make(chan tea.Msg, 16)
	runPushJob(context.Background(), ch, deps, "registry-name", "private", testSkills)
	got := drainMsgs(ch)
	mu.Lock()
	defer mu.Unlock()
	if len(order) != 3 {
		t.Fatalf("call order = %v, want 3 entries", order)
	}
	if order[0] != "create" || order[1] != "save" || order[2] != "push:owner/registry-name" {
		t.Errorf("unexpected order: %v", order)
	}
	if repoIn != "owner/registry-name" {
		t.Errorf("SaveConfig got repo %q, want %q", repoIn, "owner/registry-name")
	}
	// A wizardPushDoneMsg should always be the last message emitted.
	if len(got) == 0 {
		t.Fatal("runPushJob emitted zero messages")
	}
	if _, ok := got[len(got)-1].(wizardPushDoneMsg); !ok {
		t.Errorf("last message = %T, want wizardPushDoneMsg", got[len(got)-1])
	}
}

// TestRunPushJobSurfacesCreateError verifies that a CreateRepo failure
// terminates the push pipeline with the error captured in the done msg.
func TestRunPushJobSurfacesCreateError(t *testing.T) {
	deps := WizardDeps{
		CreateRepo: func(_ context.Context, _, _ string) (string, error) {
			return "", errors.New("repo create exploded")
		},
	}
	ch := make(chan tea.Msg, 8)
	runPushJob(context.Background(), ch, deps, "name", "private", testSkills)
	msgs := drainMsgs(ch)
	var done wizardPushDoneMsg
	for _, m := range msgs {
		if d, ok := m.(wizardPushDoneMsg); ok {
			done = d
		}
	}
	if done.err == nil {
		t.Fatal("done.err = nil, want create error")
	}
	if !strings.Contains(done.err.Error(), "repo create exploded") {
		t.Errorf("done.err = %v, want create exploded", done.err)
	}
}

// ────────────────────────────────────────────────────────────────────────────
// Helpers
// ────────────────────────────────────────────────────────────────────────────

// atStep builds a wizard parked on the given step with sensible defaults
// for the per-step tests. Tests can override individual fields after.
func atStep(s WizardStep) WizardModel {
	m := NewWizard(context.Background())
	m.step = s
	if s >= WizardStepRepoName {
		m.scanDone = true
		m.skills = testSkills
	}
	return m
}

// collectMsgs walks a tea.Cmd (possibly a batch) and returns every
// concrete tea.Msg it produces. tea.Tick-style Cmds (which sleep) are
// best-effort: we wait up to a few ms and bail out.
func collectMsgs(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := safeRun(cmd)
	if batch, ok := msg.(tea.BatchMsg); ok {
		var out []tea.Msg
		for _, sub := range batch {
			out = append(out, collectMsgs(sub)...)
		}
		return out
	}
	if msg == nil {
		return nil
	}
	return []tea.Msg{msg}
}

// safeRun executes the cmd in a goroutine so a long-running Tick can't
// hang the test. Returns nil if the goroutine doesn't finish promptly.
func safeRun(cmd tea.Cmd) tea.Msg {
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	select {
	case m := <-done:
		return m
	case <-time.After(200 * time.Millisecond):
		return nil
	}
}

// containsMsgKind reports whether the slice contains a message of the
// same concrete type as want.
func containsMsgKind(msgs []tea.Msg, want tea.Msg) bool {
	switch want.(type) {
	case wizardScanDoneMsg:
		for _, m := range msgs {
			if _, ok := m.(wizardScanDoneMsg); ok {
				return true
			}
		}
	case wizardScanRevealMsg:
		for _, m := range msgs {
			if _, ok := m.(wizardScanRevealMsg); ok {
				return true
			}
		}
	case wizardPushDoneMsg:
		for _, m := range msgs {
			if _, ok := m.(wizardPushDoneMsg); ok {
				return true
			}
		}
	case wizardPushProgressMsg:
		for _, m := range msgs {
			if _, ok := m.(wizardPushProgressMsg); ok {
				return true
			}
		}
	case wizardAgentInstallDoneMsg:
		for _, m := range msgs {
			if _, ok := m.(wizardAgentInstallDoneMsg); ok {
				return true
			}
		}
	case wizardCleanupLoadedMsg:
		for _, m := range msgs {
			if _, ok := m.(wizardCleanupLoadedMsg); ok {
				return true
			}
		}
	case wizardCleanupDoneMsg:
		for _, m := range msgs {
			if _, ok := m.(wizardCleanupDoneMsg); ok {
				return true
			}
		}
	case wizardMCPDoneMsg:
		for _, m := range msgs {
			if _, ok := m.(wizardMCPDoneMsg); ok {
				return true
			}
		}
	}
	return false
}

// drainMsgs reads every message currently buffered in ch. Used by the
// push-job tests where the goroutine has already finished.
func drainMsgs(ch chan tea.Msg) []tea.Msg {
	var out []tea.Msg
	for {
		select {
		case m, ok := <-ch:
			if !ok {
				return out
			}
			out = append(out, m)
		default:
			return out
		}
	}
}
