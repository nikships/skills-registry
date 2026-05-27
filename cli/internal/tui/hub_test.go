package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// freshHub returns a HubModel sized to a comfortable test terminal so
// View() exercises the wide-column layout. Loader stays nil so the
// count chip immediately renders the muted "unavailable" caption — keeps
// the spinner-tick branch out of the test.
func freshHub() HubModel {
	m := NewHub(context.Background(), "owner/repo", nil)
	m.width, m.height = 120, 30
	return m
}

// TestNewHubInitialState pins down the constructor contract: focus
// starts at the first card, the user hasn't quit, and the grid carries
// the five default tiles.
func TestNewHubInitialState(t *testing.T) {
	m := freshHub()
	if got := m.Selection(); got != "" {
		t.Errorf("fresh hub Selection() = %q, want \"\"", got)
	}
	if m.Quit() {
		t.Error("fresh hub Quit() = true")
	}
	if len(m.grid.Cards) != 6 {
		t.Fatalf("default hub has %d cards, want 6", len(m.grid.Cards))
	}
	if m.grid.Focused != 0 {
		t.Errorf("fresh hub Focused = %d, want 0", m.grid.Focused)
	}
}

// TestDefaultHubCardsCoverAllActions enumerates the six action constants
// and asserts each appears exactly once in the default card list. A
// regression here would mean the launcher's switch statement loses a
// branch.
func TestDefaultHubCardsCoverAllActions(t *testing.T) {
	cards := DefaultHubCards()
	want := map[string]bool{
		HubActionManage:   false,
		HubActionSync:     false,
		HubActionAdd:      false,
		HubActionPublish:  false,
		HubActionPurge:    false,
		HubActionSettings: false,
	}
	for _, c := range cards {
		seen, ok := want[c.ID]
		if !ok {
			t.Errorf("unexpected card ID %q in default cards", c.ID)
			continue
		}
		if seen {
			t.Errorf("card ID %q appears more than once", c.ID)
		}
		want[c.ID] = true
		if c.Title == "" || c.Description == "" {
			t.Errorf("card %q has empty title/description", c.ID)
		}
	}
	for id, seen := range want {
		if !seen {
			t.Errorf("missing default card for %q", id)
		}
	}
}

// TestHubArrowNavigation drives the focus across the grid and confirms
// the model tracks the move. We rely on the responsive Cols() picking 3
// at width=120 (set by freshHub).
func TestHubArrowNavigation(t *testing.T) {
	m := freshHub()
	for _, key := range []string{"right", "down"} {
		nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
		// Bubble Tea doesn't map "right" / "down" runes to KeyRight /
		// KeyDown — we emit the appropriate KeyType instead.
		_ = nm
	}
	// Direct KeyType deliveries.
	m2 := freshHub()
	nm, _ := m2.Update(tea.KeyMsg{Type: tea.KeyRight})
	nm, _ = nm.(HubModel).Update(tea.KeyMsg{Type: tea.KeyDown})
	wiz := nm.(HubModel)
	if wiz.grid.Focused != 4 {
		t.Errorf("after right,down at cols=3: Focused = %d, want 4", wiz.grid.Focused)
	}
}

// TestHubHJKLNavigation verifies the vim-style bindings walk the grid
// the same way the arrow keys do.
func TestHubHJKLNavigation(t *testing.T) {
	m := freshHub()
	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("l")})
	nm, _ = nm.(HubModel).Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	wiz := nm.(HubModel)
	if wiz.grid.Focused != 4 {
		t.Errorf("after l, j at cols=3: Focused = %d, want 4", wiz.grid.Focused)
	}
}

// TestHubEnterLaunchesFlow pins down the long-lived hand-off contract:
// pressing enter emits a hubLaunchMsg instead of quitting the program.
func TestHubEnterRecordsSelection(t *testing.T) {
	m := freshHub()
	m.grid.Focused = 2 // "Add"
	nm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	wiz := nm.(HubModel)
	if wiz.Selection() != "" {
		t.Errorf("Selection() = %q, want empty after embedded launch", wiz.Selection())
	}
	if wiz.Quit() {
		t.Error("Quit() = true after selecting; selection should not flag Quit")
	}
	if cmd == nil {
		t.Fatal("Enter did not return a launch Cmd")
	}
	msg, ok := cmd().(hubLaunchMsg)
	if !ok {
		t.Fatalf("cmd returned %T, want hubLaunchMsg", cmd())
	}
	if msg.action != HubActionAdd {
		t.Errorf("launch action = %q, want %q", msg.action, HubActionAdd)
	}
}

// TestHubQuitKeysSetQuitFlag covers the three exit keys. Each must set
// Quit()=true and emit tea.Quit without recording a selection.
func TestHubQuitKeysSetQuitFlag(t *testing.T) {
	keys := []tea.KeyMsg{
		{Type: tea.KeyRunes, Runes: []rune("q")},
		{Type: tea.KeyEsc},
		{Type: tea.KeyCtrlC},
	}
	for _, k := range keys {
		m := freshHub()
		nm, cmd := m.Update(k)
		wiz := nm.(HubModel)
		if !wiz.Quit() {
			t.Errorf("key %q did not set Quit()", k.String())
		}
		if wiz.Selection() != "" {
			t.Errorf("key %q recorded selection %q", k.String(), wiz.Selection())
		}
		if cmd == nil {
			t.Errorf("key %q did not emit tea.Quit", k.String())
		}
	}
}

// TestHubViewSurfacesChrome is the smoke test for the rendered frame.
// Every visual contract called out in the spec must appear somewhere
// in the View() output.
func TestHubViewSurfacesChrome(t *testing.T) {
	m := freshHub()
	v := m.View()
	wants := []string{
		"Skills Registry",
		"Hub",        // hero suffix
		"owner/repo", // repo chip
		"Manage",     // first card
		"Settings",   // last card
		"navigate",   // footer
		"select",     // footer
		"quit",       // footer
	}
	for _, want := range wants {
		if !strings.Contains(v, want) {
			t.Errorf("View() missing %q:\n%s", want, v)
		}
	}
}

// TestHubCountLoaderSuccess wires a loader that returns a positive count
// and verifies the chip lands as "N skills" after the message arrives.
func TestHubCountLoaderSuccess(t *testing.T) {
	loader := func(_ context.Context) (int, error) { return 42, nil }
	m := NewHub(context.Background(), "owner/repo", loader)
	m.width, m.height = 120, 30
	nm, _ := m.Update(hubCountMsg{count: 42})
	wiz := nm.(HubModel)
	if !wiz.countLoaded {
		t.Fatal("hubCountMsg did not flip countLoaded")
	}
	if wiz.count != 42 {
		t.Errorf("count = %d, want 42", wiz.count)
	}
	if !strings.Contains(wiz.View(), "42 skills") {
		t.Errorf("View() does not surface \"42 skills\":\n%s", wiz.View())
	}
}

// TestHubCountLoaderError demotes a loader failure to a muted caption
// rather than a fatal error — the user can still navigate the cards.
func TestHubCountLoaderError(t *testing.T) {
	m := freshHub()
	nm, _ := m.Update(hubCountMsg{err: errors.New("network down")})
	wiz := nm.(HubModel)
	if !wiz.countLoaded {
		t.Fatal("hubCountMsg did not flip countLoaded on error")
	}
	if !strings.Contains(wiz.View(), "unavailable") {
		t.Errorf("View() missing fallback hint:\n%s", wiz.View())
	}
}

// TestHubCountSingularNoun verifies the "1 skill" vs "N skills" branch
// so the header reads naturally on tiny registries.
func TestHubCountSingularNoun(t *testing.T) {
	m := freshHub()
	nm, _ := m.Update(hubCountMsg{count: 1})
	v := nm.(HubModel).View()
	if !strings.Contains(v, "1 skill") {
		t.Errorf("View() missing \"1 skill\":\n%s", v)
	}
	if strings.Contains(v, "1 skills") {
		t.Errorf("View() rendered grammatically wrong \"1 skills\":\n%s", v)
	}
}

// TestHubResizePropagates ensures a WindowSizeMsg flows into the model's
// width/height (used by the renderer to budget the card grid).
func TestHubResizePropagates(t *testing.T) {
	m := NewHub(context.Background(), "owner/repo", nil)
	nm, _ := m.Update(tea.WindowSizeMsg{Width: 90, Height: 28})
	wiz := nm.(HubModel)
	if wiz.width != 90 || wiz.height != 28 {
		t.Errorf("width/height = %d/%d, want 90/28", wiz.width, wiz.height)
	}
	if got := wiz.grid.Cols(wiz.width); got != 2 {
		t.Errorf("Cols at width=90 = %d, want 2", got)
	}
}

// TestHubNilLoaderSkipsSpinner verifies the constructor short-circuits
// to "count loaded" when no loader is wired so the header doesn't spin
// indefinitely.
func TestHubNilLoaderSkipsSpinner(t *testing.T) {
	m := NewHub(context.Background(), "owner/repo", nil)
	if !m.countLoaded {
		t.Error("NewHub with nil loader did not flip countLoaded=true")
	}
}

// TestHubWithToastSurfacesText pins the F3.2 contract that the toast
// text seeded by WithToast reaches View(), where the launcher's
// dispatched-action feedback lands.
func TestHubWithToastSurfacesText(t *testing.T) {
	m := freshHub().WithToast("✓ published demo-skill", true)
	if got := m.toast; got != "✓ published demo-skill" {
		t.Errorf("toast field = %q, want \"✓ published demo-skill\"", got)
	}
	if !m.toastOK {
		t.Error("toastOK should be true after success WithToast")
	}
	if !strings.Contains(m.View(), "✓ published demo-skill") {
		t.Errorf("View() missing toast text:\n%s", m.View())
	}
}

// TestHubWithToastErrorVariant exercises the failure styling branch —
// the rendered toast must still surface the text even though it'll be
// painted in ErrorStyle red rather than OkStyle green.
func TestHubWithToastErrorVariant(t *testing.T) {
	m := freshHub().WithToast("✗ sync failed: gh auth", false)
	if m.toastOK {
		t.Error("toastOK should be false on error toast")
	}
	if !strings.Contains(m.View(), "✗ sync failed: gh auth") {
		t.Errorf("View() missing error toast:\n%s", m.View())
	}
}

// TestHubWithoutToastSkipsRow guarantees the toast row stays out of the
// rendered View when no toast is set, so a fresh hub frame keeps its
// compact layout.
func TestHubWithoutToastSkipsRow(t *testing.T) {
	m := freshHub()
	if got := m.renderToast(); got != "" {
		t.Errorf("renderToast on fresh hub = %q, want empty", got)
	}
}

// TestHubToastClearsAfterReconstruction verifies the launcher's "next
// hub launch starts toast-free" assumption: NewHub does not carry over
// the previous iteration's toast.
func TestHubToastClearsAfterReconstruction(t *testing.T) {
	prev := freshHub().WithToast("✓ stale", true)
	if prev.renderToast() == "" {
		t.Fatal("setup: prev should have a toast")
	}
	// Simulate the launcher building the next iteration without a toast.
	next := NewHub(context.Background(), "owner/repo", nil)
	if got := next.renderToast(); got != "" {
		t.Errorf("NewHub carried over toast: %q", got)
	}
}

func TestHubProgramFlowExitReturnsToDashboardWithToast(t *testing.T) {
	p := NewHubProgram(context.Background(), HubDeps{Repo: "owner/repo"})
	p.flow = NewPublishFlow(context.Background(), PublishFlowDeps{})
	nm, cmd := p.Update(flowExitMsg{toast: "✓ done", ok: true})
	if cmd != nil {
		t.Fatalf("flowExitMsg returned cmd %T, want nil", cmd)
	}
	hp := nm.(HubProgram)
	if hp.flow != nil {
		t.Fatalf("flow still active after flowExitMsg: %T", hp.flow)
	}
	if hp.hub.toast != "✓ done" || !hp.hub.toastOK {
		t.Fatalf("hub toast = %q ok=%v", hp.hub.toast, hp.hub.toastOK)
	}
}

func TestHubProgramLaunchesManageFlow(t *testing.T) {
	p := NewHubProgram(context.Background(), HubDeps{
		Repo: "owner/repo",
		Manage: ManageFlowDeps{
			Rows: func() ([]SkillRow, error) {
				return []SkillRow{{Slug: "demo", Name: "Demo"}}, nil
			},
			Install: func(context.Context, string, []any) ([]string, error) {
				return nil, nil
			},
			InstallTargets: func() []InstallTarget {
				return []InstallTarget{
					{Display: "Universal", Locked: true, Value: "u"},
				}
			},
		},
	})
	nm, cmd := p.Update(hubLaunchMsg{action: HubActionManage})
	hp := nm.(HubProgram)
	list, ok := hp.flow.(ListModel)
	if !ok {
		t.Fatalf("flow = %T, want ListModel", hp.flow)
	}
	if list.loadTargets == nil {
		t.Error("manage flow did not propagate InstallTargets loader")
	}
	if list.install == nil {
		t.Error("manage flow did not propagate Installer")
	}
	if cmd == nil {
		t.Fatal("launching manage should return init command")
	}
}

func TestHubProgramUnknownActionToastsError(t *testing.T) {
	p := NewHubProgram(context.Background(), HubDeps{Repo: "owner/repo"})
	nm, cmd := p.Update(hubLaunchMsg{action: "bogus"})
	if cmd != nil {
		t.Fatalf("unknown launch returned cmd %T, want nil", cmd)
	}
	hp := nm.(HubProgram)
	if hp.flow != nil {
		t.Fatalf("unknown action should not set flow, got %T", hp.flow)
	}
	if hp.hub.toastOK || !strings.Contains(hp.hub.toast, "bogus") {
		t.Fatalf("hub toast = %q ok=%v, want error mentioning bogus", hp.hub.toast, hp.hub.toastOK)
	}
}
