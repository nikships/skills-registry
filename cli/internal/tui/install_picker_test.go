package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func runeKey(r rune) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}
}

// freshPicker builds a picker with one locked target plus two
// togglable rows. The Default flag on "claude" pre-checks the row so
// constructor behavior is observable without the test driving any
// keys.
func freshPicker() InstallPickerModel {
	return NewInstallPicker("Install into which agents?", "demo_skill", []InstallTarget{
		{Display: "Project (.agents)", Hint: ".agents/skills", Locked: true, Value: "universal"},
		{Display: "Claude Code", Hint: ".claude/skills", Default: true, Value: "claude"},
		{Display: "Cursor", Hint: ".cursor/skills", Value: "cursor"},
	})
}

// TestNewInstallPickerSeedsLockedAndDefault pins the constructor
// contract: locked targets are always present in SelectedValues and
// Default=true rows are pre-checked.
func TestNewInstallPickerSeedsLockedAndDefault(t *testing.T) {
	p := freshPicker()
	values := p.SelectedValues()
	if len(values) != 2 || values[0] != "universal" || values[1] != "claude" {
		t.Fatalf("initial selection = %v, want [universal claude]", values)
	}
	if p.Done() || p.Cancelled() {
		t.Fatal("fresh picker reports done/cancelled")
	}
}

// TestInstallPickerEnterMarksDone covers the happy path: pressing
// Enter flips Done() without emitting tea.Quit (embedded usage —
// parent state machine drives advancement).
func TestInstallPickerEnterMarksDone(t *testing.T) {
	p := freshPicker()
	next, cmd := p.Update(tea.KeyMsg{Type: tea.KeyEnter})
	pm := next.(InstallPickerModel)
	if !pm.Done() {
		t.Fatal("enter did not flip Done()")
	}
	if pm.Cancelled() {
		t.Fatal("enter incorrectly flipped Cancelled()")
	}
	if cmd != nil {
		t.Fatalf("embedded enter should not emit a cmd, got %T", cmd)
	}
}

// TestInstallPickerSpaceToggle covers the per-row toggle: pressing
// space on a togglable row flips its selection state.
func TestInstallPickerSpaceToggle(t *testing.T) {
	p := freshPicker()
	// Cursor starts at 0 = "Claude Code" (already default-selected).
	// Space removes it.
	next, _ := p.Update(runeKey(' '))
	pm := next.(InstallPickerModel)
	values := pm.SelectedValues()
	// Only the locked "universal" should remain.
	if len(values) != 1 || values[0] != "universal" {
		t.Fatalf("after toggle off, selection = %v, want [universal]", values)
	}
	// Space again — toggles back on.
	next2, _ := pm.Update(runeKey(' '))
	values = next2.(InstallPickerModel).SelectedValues()
	if len(values) != 2 || values[1] != "claude" {
		t.Fatalf("after toggle back on, selection = %v, want [universal claude]", values)
	}
}

// TestInstallPickerTabSelectsAllFiltered exercises the tab shortcut:
// every togglable row currently visible (post-filter) becomes
// selected.
func TestInstallPickerTabSelectsAllFiltered(t *testing.T) {
	p := freshPicker()
	next, _ := p.Update(tea.KeyMsg{Type: tea.KeyTab})
	values := next.(InstallPickerModel).SelectedValues()
	if len(values) != 3 {
		t.Fatalf("after tab, selection len = %d, want 3 (locked+claude+cursor)", len(values))
	}
}

// TestInstallPickerEscCancels pins the cancellation contract: esc
// flips Cancelled() and never flips Done().
func TestInstallPickerEscCancels(t *testing.T) {
	p := freshPicker()
	next, _ := p.Update(tea.KeyMsg{Type: tea.KeyEsc})
	pm := next.(InstallPickerModel)
	if !pm.Cancelled() {
		t.Fatal("esc did not flip Cancelled()")
	}
	if pm.Done() {
		t.Fatal("esc incorrectly flipped Done()")
	}
}

// TestInstallPickerFilterNarrowsRows covers the rune-driven fuzzy
// filter. Typing "cur" narrows the visible togglable rows to just
// "Cursor", and the cursor jumps back to zero so the visible row is
// the active one. The "Selected: …" summary line still reflects every
// pre-existing selection (filtered or not) so the user can see what
// will be installed; we only assert the togglable section between
// Search and Selected has shrunk.
func TestInstallPickerFilterNarrowsRows(t *testing.T) {
	p := freshPicker()
	next, _ := p.Update(runeKey('c'))
	next, _ = next.(InstallPickerModel).Update(runeKey('u'))
	next, _ = next.(InstallPickerModel).Update(runeKey('r'))
	pm := next.(InstallPickerModel)
	view := pm.View()
	body := view[strings.Index(view, "Search:"):]
	body = body[:strings.Index(body, "Selected:")]
	if !strings.Contains(body, "Cursor") {
		t.Errorf("filter body missing Cursor:\n%s", body)
	}
	if strings.Contains(body, "Claude Code") {
		t.Errorf("filter body still includes filtered-out Claude Code:\n%s", body)
	}
}

// TestInstallPickerStandaloneEnterQuits covers the AsStandalone()
// adapter path: hosting the picker via tea.NewProgram needs the
// Update to return tea.Quit once the user is done.
func TestInstallPickerStandaloneEnterQuits(t *testing.T) {
	p := freshPicker().AsStandalone()
	next, cmd := p.Update(tea.KeyMsg{Type: tea.KeyEnter})
	pm := next.(InstallPickerModel)
	if !pm.Done() {
		t.Fatal("standalone enter did not flip Done()")
	}
	if cmd == nil {
		t.Fatal("standalone enter did not emit tea.Quit")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatalf("standalone enter emitted %T, want tea.QuitMsg", cmd())
	}
}

// TestInstallPickerStandaloneEscQuits ensures the standalone exit on
// cancellation matches the enter path.
func TestInstallPickerStandaloneEscQuits(t *testing.T) {
	p := freshPicker().AsStandalone()
	next, cmd := p.Update(tea.KeyMsg{Type: tea.KeyEsc})
	pm := next.(InstallPickerModel)
	if !pm.Cancelled() {
		t.Fatal("standalone esc did not flip Cancelled()")
	}
	if cmd == nil {
		t.Fatal("standalone esc did not emit tea.Quit")
	}
}

// TestInstallPickerLockedTargetAlwaysIncluded asserts that the
// universal-locked target appears in SelectedValues even after every
// togglable row has been turned off — the "always-on" guarantee from
// the durable-install spec.
func TestInstallPickerLockedTargetAlwaysIncluded(t *testing.T) {
	p := NewInstallPicker("title", "subtitle", []InstallTarget{
		{Display: "Universal", Locked: true, Value: "u"},
	})
	values := p.SelectedValues()
	if len(values) != 1 || values[0] != "u" {
		t.Fatalf("locked-only picker selection = %v, want [u]", values)
	}
	// Try to "toggle" — should be a no-op since there's nothing
	// filterable.
	next, _ := p.Update(runeKey(' '))
	values = next.(InstallPickerModel).SelectedValues()
	if len(values) != 1 || values[0] != "u" {
		t.Fatalf("locked-only picker post-space selection = %v, want [u]", values)
	}
}

// TestInstallPickerNonKeyMessageIsNoOp covers the standalone path's
// passthrough behavior for non-key messages (window size, mouse, …).
// They should leave the model untouched.
func TestInstallPickerNonKeyMessageIsNoOp(t *testing.T) {
	p := freshPicker()
	next, cmd := p.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	pm := next.(InstallPickerModel)
	if pm.Done() || pm.Cancelled() {
		t.Fatal("non-key msg flipped done/cancelled")
	}
	if cmd != nil {
		t.Fatalf("non-key msg emitted cmd %T", cmd)
	}
}
