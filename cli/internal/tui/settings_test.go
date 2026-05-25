package tui

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// stubSaver records the last (repo, branch) pair it was called with so
// tests can assert the model passes the trimmed values verbatim.
type stubSaver struct {
	calls []struct {
		repo, branch string
	}
	returnPath string
	returnErr  error
}

func (s *stubSaver) fn() SettingsSaver {
	return func(repo, branch string) (string, error) {
		s.calls = append(s.calls, struct{ repo, branch string }{repo, branch})
		return s.returnPath, s.returnErr
	}
}

// freshSettings returns a SettingsModel sized to a comfortable test
// terminal with a stub saver attached.
func freshSettings(saver SettingsSaver) SettingsModel {
	m := NewSettings("owner/repo", "main",
		"/home/u/.cache/skills-mcp/skills",
		"https://mcp.skills-registry.dev/mcp",
		saver,
	)
	m.width, m.height = 100, 24
	return m
}

// TestNewSettingsCapturesAllFields pins the constructor contract: every
// field the user can see is populated and the focused field starts at
// the top (Repository).
func TestNewSettingsCapturesAllFields(t *testing.T) {
	m := freshSettings(nil)
	if m.Repo() != "owner/repo" {
		t.Errorf("Repo() = %q, want owner/repo", m.Repo())
	}
	if m.Branch() != "main" {
		t.Errorf("Branch() = %q, want main", m.Branch())
	}
	if m.cacheRoot != "/home/u/.cache/skills-mcp/skills" {
		t.Errorf("cacheRoot = %q", m.cacheRoot)
	}
	if m.hostedMCP != "https://mcp.skills-registry.dev/mcp" {
		t.Errorf("hostedMCP = %q", m.hostedMCP)
	}
	if m.focused != settingsFieldRepo {
		t.Errorf("initial focus = %v, want repo", m.focused)
	}
}

// TestSettingsViewSurfacesAllFields is the smoke test: every label and
// value the spec calls out must appear somewhere in the rendered View.
func TestSettingsViewSurfacesAllFields(t *testing.T) {
	m := freshSettings(nil)
	v := m.View()
	wants := []string{
		"Settings",
		"Repository",
		"owner/repo",
		"Default branch",
		"main",
		"Cache location",
		"/home/u/.cache/skills-mcp/skills",
		"Hosted MCP URL",
		"https://mcp.skills-registry.dev/mcp",
	}
	for _, want := range wants {
		if !strings.Contains(v, want) {
			t.Errorf("View() missing %q:\n%s", want, v)
		}
	}
}

// TestSettingsNavigationTabCyclesFields walks the focus down through
// each field and confirms it wraps back to the top.
func TestSettingsNavigationTabCyclesFields(t *testing.T) {
	m := freshSettings(nil)
	if m.focused != settingsFieldRepo {
		t.Fatalf("initial focus = %v", m.focused)
	}
	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	if got := nm.(SettingsModel).focused; got != settingsFieldBranch {
		t.Errorf("after tab: focus = %v, want branch", got)
	}
	nm, _ = nm.(SettingsModel).Update(tea.KeyMsg{Type: tea.KeyTab})
	if got := nm.(SettingsModel).focused; got != settingsFieldRepo {
		t.Errorf("after second tab: focus wrap = %v, want repo", got)
	}
}

// TestSettingsArrowKeysNavigation verifies down/up walk the focus the
// same way as tab/shift-tab.
func TestSettingsArrowKeysNavigation(t *testing.T) {
	m := freshSettings(nil)
	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	if got := nm.(SettingsModel).focused; got != settingsFieldBranch {
		t.Errorf("down: focus = %v, want branch", got)
	}
	nm, _ = nm.(SettingsModel).Update(tea.KeyMsg{Type: tea.KeyUp})
	if got := nm.(SettingsModel).focused; got != settingsFieldRepo {
		t.Errorf("up: focus = %v, want repo", got)
	}
}

// TestSettingsEnterStartsEditingFocusedField checks the e/enter
// affordance: it should focus the textinput tied to the current row.
func TestSettingsEnterStartsEditingFocusedField(t *testing.T) {
	m := freshSettings(nil)
	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	wiz := nm.(SettingsModel)
	if !wiz.editing {
		t.Fatal("enter did not flip editing=true")
	}
	if !wiz.repoInput.Focused() {
		t.Error("enter did not focus the repoInput")
	}
}

// TestSettingsEditingForwardsKeysToInput verifies the typing path:
// runes typed while editing reach the focused textinput and update the
// value the model returns via Repo().
func TestSettingsEditingForwardsKeysToInput(t *testing.T) {
	m := freshSettings(nil)
	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	// Move cursor to end and append "-renamed".
	nm, _ = nm.(SettingsModel).Update(tea.KeyMsg{Type: tea.KeyEnd})
	for _, r := range "-renamed" {
		nm, _ = nm.(SettingsModel).Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	wiz := nm.(SettingsModel)
	if got := wiz.Repo(); got != "owner/repo-renamed" {
		t.Errorf("Repo() after typing = %q, want owner/repo-renamed", got)
	}
}

// TestSettingsEscCancelsEditAndRestoresValue exercises the esc-in-edit
// branch: the typed-but-uncommitted edit reverts and the model returns
// to nav mode.
func TestSettingsEscCancelsEditAndRestoresValue(t *testing.T) {
	m := freshSettings(nil)
	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	// Replace the repo with garbage, then esc.
	nm, _ = nm.(SettingsModel).Update(tea.KeyMsg{Type: tea.KeyEnd})
	for _, r := range "-typo" {
		nm, _ = nm.(SettingsModel).Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	nm, _ = nm.(SettingsModel).Update(tea.KeyMsg{Type: tea.KeyEsc})
	wiz := nm.(SettingsModel)
	if wiz.editing {
		t.Error("esc did not exit edit mode")
	}
	if wiz.Repo() != "owner/repo" {
		t.Errorf("esc did not restore original repo: %q", wiz.Repo())
	}
}

// TestSettingsEnterCommitsEditWithoutSave pins the commit-but-don't-save
// behaviour: pressing enter inside an edit returns to nav mode keeping
// the new value, but the value is not persisted until the user presses
// `s`.
func TestSettingsEnterCommitsEditWithoutSave(t *testing.T) {
	stub := &stubSaver{returnPath: "/tmp/registry.toml"}
	m := freshSettings(stub.fn())
	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	nm, _ = nm.(SettingsModel).Update(tea.KeyMsg{Type: tea.KeyEnd})
	for _, r := range "-new" {
		nm, _ = nm.(SettingsModel).Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	// Enter while editing commits the field.
	nm, _ = nm.(SettingsModel).Update(tea.KeyMsg{Type: tea.KeyEnter})
	wiz := nm.(SettingsModel)
	if wiz.editing {
		t.Error("enter did not exit editing")
	}
	if wiz.Repo() != "owner/repo-new" {
		t.Errorf("commit did not retain typed value: %q", wiz.Repo())
	}
	if len(stub.calls) != 0 {
		t.Errorf("commit unexpectedly invoked saver: %d call(s)", len(stub.calls))
	}
}

// TestSettingsSaveInvokesSaverWithTrimmedValues pins the save-keystroke
// contract: pressing `s` calls the SettingsSaver with the trimmed repo
// and branch values; the model records the returned path so the next
// render surfaces a green ✓ caption.
func TestSettingsSaveInvokesSaverWithTrimmedValues(t *testing.T) {
	stub := &stubSaver{returnPath: "/tmp/registry.toml"}
	m := freshSettings(stub.fn())
	m.repoInput.SetValue("  new-owner/new-repo  ")
	m.branchInput.SetValue("  develop  ")

	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("s")})
	wiz := nm.(SettingsModel)
	if len(stub.calls) != 1 {
		t.Fatalf("saver calls = %d, want 1", len(stub.calls))
	}
	c := stub.calls[0]
	if c.repo != "new-owner/new-repo" {
		t.Errorf("saver repo = %q, want new-owner/new-repo", c.repo)
	}
	if c.branch != "develop" {
		t.Errorf("saver branch = %q, want develop", c.branch)
	}
	if wiz.SavedPath() != "/tmp/registry.toml" {
		t.Errorf("SavedPath() = %q, want /tmp/registry.toml", wiz.SavedPath())
	}
	if !strings.Contains(wiz.View(), "/tmp/registry.toml") {
		t.Errorf("View() does not surface saved-at path:\n%s", wiz.View())
	}
}

// TestSettingsSaveDefaultsBranchToMain verifies the empty-branch
// safety net: a blank branch is auto-replaced with "main" before
// hitting the saver so a config save never blanks out the branch field.
func TestSettingsSaveDefaultsBranchToMain(t *testing.T) {
	stub := &stubSaver{returnPath: "/tmp/registry.toml"}
	m := freshSettings(stub.fn())
	m.branchInput.SetValue("")
	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("s")})
	_ = nm
	if len(stub.calls) != 1 || stub.calls[0].branch != "main" {
		t.Errorf("saver branch = %v, want main", stub.calls)
	}
}

// TestSettingsSaveSurfacesError verifies the failure path: a saver
// returning an error sets SaveError() and renders a ✗ caption in the
// View, but doesn't quit the program so the user can retry.
func TestSettingsSaveSurfacesError(t *testing.T) {
	stub := &stubSaver{returnErr: errors.New("disk full")}
	m := freshSettings(stub.fn())
	nm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("s")})
	wiz := nm.(SettingsModel)
	if wiz.SaveError() == nil {
		t.Fatal("SaveError() = nil after failure")
	}
	if cmd != nil {
		t.Errorf("save error returned tea.Cmd %T, want nil", cmd)
	}
	if !strings.Contains(wiz.View(), "disk full") {
		t.Errorf("View() does not surface error:\n%s", wiz.View())
	}
}

// TestSettingsSaveNoSaverIsReadOnly verifies that a model built without
// a SettingsSaver surfaces a friendly "read-only" message when the user
// hits `s` rather than panicking or silently no-op-ing.
func TestSettingsSaveNoSaverIsReadOnly(t *testing.T) {
	m := freshSettings(nil)
	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("s")})
	wiz := nm.(SettingsModel)
	if wiz.SaveError() == nil {
		t.Fatal("SaveError() = nil without saver")
	}
	if !strings.Contains(strings.ToLower(wiz.SaveError().Error()), "read-only") {
		t.Errorf("SaveError() = %q, want it to mention read-only", wiz.SaveError())
	}
}

// TestSettingsQuitKeysSetQuitFlag covers the three exit keys: each
// must flip Quit()=true and emit tea.Quit so the launcher returns.
func TestSettingsQuitKeysSetQuitFlag(t *testing.T) {
	keys := []tea.KeyMsg{
		{Type: tea.KeyRunes, Runes: []rune("q")},
		{Type: tea.KeyEsc},
		{Type: tea.KeyCtrlC},
	}
	for _, k := range keys {
		m := freshSettings(nil)
		nm, cmd := m.Update(k)
		wiz := nm.(SettingsModel)
		if !wiz.Quit() {
			t.Errorf("key %q did not set Quit()", k.String())
		}
		if cmd == nil {
			t.Errorf("key %q did not emit tea.Quit", k.String())
		}
	}
}

func TestSettingsOnExitSwapsQuitMessage(t *testing.T) {
	m := freshSettings(nil).WithOnExit(func(SettingsModel) tea.Msg {
		return flowExitMsg{toast: "settings · closed", ok: true}
	})
	nm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	if !nm.(SettingsModel).Quit() {
		t.Fatal("q did not set Quit()")
	}
	if cmd == nil {
		t.Fatal("q with OnExit returned nil cmd")
	}
	msg, ok := cmd().(flowExitMsg)
	if !ok {
		t.Fatalf("exit cmd returned %T, want flowExitMsg", cmd())
	}
	if msg.toast != "settings · closed" || !msg.ok {
		t.Fatalf("flowExitMsg = %+v", msg)
	}
}

// TestSettingsCtrlCWhileEditingStillQuits verifies the
// editing-mode-doesn't-trap-ctrl+c contract — matches the wizard / hub
// behaviour so users always have a working escape hatch.
func TestSettingsCtrlCWhileEditingStillQuits(t *testing.T) {
	m := freshSettings(nil)
	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	nm, _ = nm.(SettingsModel).Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if !nm.(SettingsModel).Quit() {
		t.Error("ctrl+c during edit did not set Quit()")
	}
}

// TestSettingsResizePropagates ensures a WindowSizeMsg lands in the
// model's width/height fields (used to budget the panel).
func TestSettingsResizePropagates(t *testing.T) {
	m := freshSettings(nil)
	nm, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	wiz := nm.(SettingsModel)
	if wiz.width != 120 || wiz.height != 30 {
		t.Errorf("width/height = %d/%d, want 120/30", wiz.width, wiz.height)
	}
}

// TestTruncateLongString pins the width-aware truncate contract: the
// rendered result must report a display width ≤ n via lipgloss.Width.
// Exercises the 200+ char path called out by the F3.3 spec.
func TestTruncateLongString(t *testing.T) {
	long := strings.Repeat("A skill description that goes on and on. ", 6) // ~250 chars
	got := truncate(long, 60)
	if lipgloss.Width(got) > 60 {
		t.Errorf("truncate width = %d, want ≤ 60", lipgloss.Width(got))
	}
	if !strings.HasSuffix(got, "…") {
		t.Errorf("truncated string missing ellipsis: %q", got)
	}
}

// TestTruncateMultiByteUTF8 verifies multi-byte runes (e.g. emoji and
// CJK) don't crash the truncator and the ellipsis is appended after a
// clean rune boundary.
func TestTruncateMultiByteUTF8(t *testing.T) {
	// Mix of ASCII, emoji (2 cells), and CJK (2 cells).
	s := "Hello 🌈 世界 — a multi-byte test"
	got := truncate(s, 12)
	if lipgloss.Width(got) > 12 {
		t.Errorf("truncate width = %d, want ≤ 12 for %q", lipgloss.Width(got), got)
	}
	// Ellipsis terminus when truncation actually happened.
	if lipgloss.Width(s) > 12 && !strings.HasSuffix(got, "…") {
		t.Errorf("expected ellipsis suffix in %q", got)
	}
}

// TestTruncateExactlyAtBudget exercises the early-return path: when
// the source already fits, no ellipsis is appended.
func TestTruncateExactlyAtBudget(t *testing.T) {
	got := truncate("exactly-13-c.", 13)
	if got != "exactly-13-c." {
		t.Errorf("truncate(exact) = %q, want unchanged", got)
	}
}

// TestTruncateZeroWidth handles the n=0 edge case.
func TestTruncateZeroWidth(t *testing.T) {
	if got := truncate("anything", 0); got != "" {
		t.Errorf("truncate(_, 0) = %q, want empty", got)
	}
}

// TestSettingsViewHandlesLongPath confirms that a long cache path is
// width-clamped so the body row never wraps and pushes the footer
// off-screen. We rely on rune-aware truncate to handle a long path
// gracefully.
func TestSettingsViewHandlesLongPath(t *testing.T) {
	long := strings.Repeat("/very-long-path-segment", 20) // ~440 chars
	m := NewSettings("owner/repo", "main", long, long, nil)
	m.width, m.height = 80, 24
	v := m.View()
	// Each line of the rendered body should fit within the terminal width.
	for _, line := range strings.Split(v, "\n") {
		if lipgloss.Width(line) > 80 {
			t.Errorf("rendered line exceeds width 80: %d cells: %q",
				lipgloss.Width(line), line)
		}
	}
}
