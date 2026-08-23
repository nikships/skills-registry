package tui

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

var testAgents = []WizardAgent{
	{Display: "Universal", Hint: ".agents/skills", Locked: true, Value: "u"},
	{Display: "Claude Code", Hint: ".claude/skills", Default: true, Value: "c"},
	{Display: "Cursor", Hint: ".cursor/skills", Value: "x"},
	{Display: "Factory", Hint: ".factory/skills", Default: true, Value: "f"},
}

func agentDepsFixture(t *testing.T) (WizardDeps, *int32, *[]any) {
	t.Helper()
	var calls int32
	var lastPicked []any
	deps := WizardDeps{
		AgentChoices: func() []WizardAgent { return testAgents },
		InstallAgents: func(_ context.Context, _ string, picked []any) ([]string, error) {
			atomic.AddInt32(&calls, 1)
			lastPicked = picked
			return []string{"/tmp/.claude/skills/skills-registry/SKILL.md"}, nil
		},
	}
	return deps, &calls, &lastPicked
}

func TestWizardAgentLoadOnEntry(t *testing.T) {
	deps, _, _ := agentDepsFixture(t)
	m := atStep(WizardStepPush).WithDeps(deps)
	m.pushDone = true
	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	nm, _ = nm.(WizardModel).Update(wizardTransitionMsg{to: WizardStepAgentSelect})
	wiz := nm.(WizardModel)
	if !wiz.agentLoaded || len(wiz.agentItems) != len(testAgents) {
		t.Fatalf("agent choices not loaded: loaded=%v count=%d", wiz.agentLoaded, len(wiz.agentItems))
	}
	if len(wiz.agentSelected) < 2 {
		t.Errorf("agentSelected = %d defaults checked, want >=2", len(wiz.agentSelected))
	}
	if !wiz.agentItems[0].Locked {
		t.Errorf("locked agent was not sorted first: %+v", wiz.agentItems[0])
	}
}

func TestWizardAgentSpaceTogglesSelection(t *testing.T) {
	deps, _, _ := agentDepsFixture(t)
	m := atStep(WizardStepAgentSelect).WithDeps(deps)
	m.loadAgentChoices()
	m.agentCursor = 0
	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(" ")})
	wiz := nm.(WizardModel)
	before := len(wiz.agentSelected)
	nm, _ = wiz.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(" ")})
	after := len(nm.(WizardModel).agentSelected)
	if before == after {
		t.Errorf("space did not toggle: before=%d after=%d", before, after)
	}
}

func TestWizardAgentFilterNarrowsRows(t *testing.T) {
	deps, _, _ := agentDepsFixture(t)
	m := atStep(WizardStepAgentSelect).WithDeps(deps)
	m.loadAgentChoices()
	full := len(m.agentFilteredIndices())
	nm := tea.Model(m)
	for _, ch := range []rune{'c', 'u', 'r'} {
		nm, _ = nm.(WizardModel).Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{ch}})
	}
	if narrow := len(nm.(WizardModel).agentFilteredIndices()); narrow >= full {
		t.Errorf("filter did not narrow rows: full=%d narrow=%d", full, narrow)
	}
	for range []rune{'c', 'u', 'r'} {
		nm, _ = nm.(WizardModel).Update(tea.KeyMsg{Type: tea.KeyBackspace})
	}
	if len(nm.(WizardModel).agentFilteredIndices()) != full {
		t.Error("backspace did not restore filter")
	}
}

func TestWizardAgentTabSelectsAllVisible(t *testing.T) {
	deps, _, _ := agentDepsFixture(t)
	m := atStep(WizardStepAgentSelect).WithDeps(deps)
	m.loadAgentChoices()
	m.agentSelected = map[int]struct{}{}
	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	wiz := nm.(WizardModel)
	if len(wiz.agentSelected) != len(wiz.agentFilteredIndices()) {
		t.Errorf("tab selected %d, want %d", len(wiz.agentSelected), len(wiz.agentFilteredIndices()))
	}
}

func TestWizardAgentSelectionAndInstall(t *testing.T) {
	deps, calls, lastPicked := agentDepsFixture(t)
	m := atStep(WizardStepAgentSelect).WithDeps(deps)
	m.loadAgentChoices()
	nm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	wiz := nm.(WizardModel)
	if !wiz.agentInstalling || cmd == nil {
		t.Fatal("enter did not start agent install")
	}
	msgs := collectMsgs(cmd)
	if atomic.LoadInt32(calls) != 1 || !containsMsgKind(msgs, wizardAgentInstallDoneMsg{}) {
		t.Fatalf("install calls=%d messages=%+v", *calls, msgs)
	}
	for _, picked := range *lastPicked {
		if picked == "u" {
			return
		}
	}
	t.Error("locked Universal target was not installed")
}

func TestWizardAgentInstallDoneAdvances(t *testing.T) {
	m := atStep(WizardStepAgentSelect)
	m.agentInstallDone = true
	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	wiz := nm.(WizardModel)
	if !wiz.transitioning || wiz.transitionTarget != WizardStepCleanup {
		t.Fatalf("target=%v transitioning=%v, want Cleanup", wiz.transitionTarget, wiz.transitioning)
	}
}

func TestWizardAgentViewSurfacesRows(t *testing.T) {
	deps, _, _ := agentDepsFixture(t)
	m := atStep(WizardStepAgentSelect).WithDeps(deps)
	m.loadAgentChoices()
	m.width, m.height = 120, 40
	v := m.View()
	for _, want := range []string{"Install into agents", "Always included", "Universal", "Filter", "Claude Code"} {
		if !strings.Contains(v, want) {
			t.Errorf("AgentSelect view missing %q:\n%s", want, v)
		}
	}
}

func TestWizardAgentViewSurfacesInstallSummary(t *testing.T) {
	m := atStep(WizardStepAgentSelect)
	m.agentInstallDone = true
	m.agentPaths = []string{
		"/tmp/.claude/skills/skills-registry/SKILL.md",
		"/tmp/.factory/skills/skills-registry/SKILL.md",
	}
	m.width, m.height = 120, 40
	v := m.View()
	if !strings.Contains(v, "installed into 2 folder") {
		t.Errorf("install summary missing count:\n%s", v)
	}
	if !strings.Contains(v, ".claude") {
		t.Errorf("install summary missing path preview:\n%s", v)
	}
}

func TestWizardCleanupLoadedWithEntriesShowsPrompt(t *testing.T) {
	m := atStep(WizardStepCleanup)
	entries := []WizardCleanupEntry{
		{Path: "/tmp/.claude/skills/foo", Source: "~/.claude/skills"},
		{Path: "/tmp/.cursor/skills/foo", Source: "~/.cursor/skills"},
	}
	nm, _ := m.Update(wizardCleanupLoadedMsg{entries: entries})
	wiz := nm.(WizardModel)
	if !wiz.cleanupLoaded {
		t.Fatal("cleanupLoaded = false after wizardCleanupLoadedMsg")
	}
	if wiz.cleanupChosen {
		t.Error("cleanupChosen = true before user input")
	}
	wiz.width, wiz.height = 120, 30
	v := wiz.View()
	for _, want := range []string{"Tidy local copies", "Yes, delete", "No, keep them"} {
		if !strings.Contains(v, want) {
			t.Errorf("cleanup prompt missing %q:\n%s", want, v)
		}
	}
}

func TestWizardCleanupLoadedEmptyAutoCompletes(t *testing.T) {
	m := atStep(WizardStepCleanup)
	nm, _ := m.Update(wizardCleanupLoadedMsg{})
	wiz := nm.(WizardModel)
	if !wiz.cleanupChosen || !wiz.cleanupDone {
		t.Fatalf("empty cleanup did not complete: chosen=%v done=%v", wiz.cleanupChosen, wiz.cleanupDone)
	}
}

func TestWizardCleanupYesRunsDelete(t *testing.T) {
	var called int32
	deps := WizardDeps{
		DeleteCleanup: func(entries []WizardCleanupEntry) (int, int) {
			atomic.AddInt32(&called, 1)
			return len(entries), 0
		},
	}
	m := atStep(WizardStepCleanup).WithDeps(deps)
	m.cleanupLoaded = true
	m.cleanupEntries = []WizardCleanupEntry{{Path: "/tmp/a"}, {Path: "/tmp/b"}}
	nm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	wiz := nm.(WizardModel)
	if !wiz.cleanupChosen || !wiz.cleanupYes || !wiz.cleanupRunning || cmd == nil {
		t.Fatal("yes did not start cleanup")
	}
	msgs := collectMsgs(cmd)
	if atomic.LoadInt32(&called) != 1 || !containsMsgKind(msgs, wizardCleanupDoneMsg{}) {
		t.Fatalf("delete calls=%d messages=%+v", called, msgs)
	}
}

func TestWizardCleanupNoKeeps(t *testing.T) {
	var called int32
	deps := WizardDeps{
		DeleteCleanup: func(_ []WizardCleanupEntry) (int, int) {
			atomic.AddInt32(&called, 1)
			return 0, 0
		},
	}
	m := atStep(WizardStepCleanup).WithDeps(deps)
	m.cleanupLoaded = true
	m.cleanupEntries = []WizardCleanupEntry{{Path: "/tmp/a"}}
	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	wiz := nm.(WizardModel)
	if !wiz.cleanupChosen || wiz.cleanupYes {
		t.Errorf("n did not keep entries: chosen=%v yes=%v", wiz.cleanupChosen, wiz.cleanupYes)
	}
	if !wiz.cleanupDone {
		t.Error("cleanupDone false after No choice")
	}
	if atomic.LoadInt32(&called) != 0 {
		t.Errorf("DeleteCleanup unexpectedly called after No: %d", called)
	}
}

func TestWizardCleanupDoneAdvancesDirectlyToDone(t *testing.T) {
	m := atStep(WizardStepCleanup)
	m.cleanupLoaded = true
	m.cleanupChosen = true
	m.cleanupDone = true
	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	wiz := nm.(WizardModel)
	if !wiz.transitioning || wiz.transitionTarget != WizardStepDone {
		t.Fatalf("target=%v transitioning=%v, want Done", wiz.transitionTarget, wiz.transitioning)
	}
}

func TestWizardCleanupViewSurfacesCounts(t *testing.T) {
	m := atStep(WizardStepCleanup)
	m.cleanupLoaded = true
	m.cleanupEntries = []WizardCleanupEntry{
		{Path: "/a", Source: "~/.claude/skills"},
		{Path: "/b", Source: "~/.claude/skills"},
		{Path: "/c", Source: "~/.cursor/skills"},
	}
	m.width, m.height = 120, 40
	v := m.View()
	for _, want := range []string{".claude/skills", ".cursor/skills", "3 local"} {
		if !strings.Contains(v, want) {
			t.Errorf("cleanup view missing %q:\n%s", want, v)
		}
	}
}

func TestWizardLoadAgentChoicesIsIdempotent(t *testing.T) {
	deps := WizardDeps{
		AgentChoices: func() []WizardAgent { return testAgents },
	}
	m := atStep(WizardStepAgentSelect).WithDeps(deps)
	m.loadAgentChoices()
	m.agentFilter = "claude"
	m.agentSelected = map[int]struct{}{2: {}}
	m.loadAgentChoices()
	if m.agentFilter != "claude" {
		t.Errorf("filter clobbered: %q", m.agentFilter)
	}
	if _, ok := m.agentSelected[2]; !ok {
		t.Error("selection clobbered by repeat loadAgentChoices")
	}
}

func TestWizardCancelOverlayDuringAgentInstall(t *testing.T) {
	m := atStep(WizardStepAgentSelect)
	m.agentInstalling = true
	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if !nm.(WizardModel).cancelOverlay {
		t.Fatal("esc during agent install did not open cancel overlay")
	}
}

func TestWizardAgentPlainEnterCompletesWithoutDep(t *testing.T) {
	m := atStep(WizardStepAgentSelect)
	m.loadAgentChoices()
	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	wiz := nm.(WizardModel)
	if !wiz.agentInstalling || !wiz.agentInstallDone {
		t.Errorf("enter without dep should mark install done; got installing=%v done=%v",
			wiz.agentInstalling, wiz.agentInstallDone)
	}
}

func TestWizardAgentInstalledAccessor(t *testing.T) {
	m := atStep(WizardStepAgentSelect)
	if m.AgentsInstalled() != 0 {
		t.Errorf("AgentsInstalled() = %d before install, want 0", m.AgentsInstalled())
	}
	m.agentPaths = []string{"/a", "/b"}
	if m.AgentsInstalled() != 2 {
		t.Errorf("AgentsInstalled() = %d, want 2", m.AgentsInstalled())
	}
}

func TestWizardDoneViewAndEnter(t *testing.T) {
	m := atStep(WizardStepDone)
	m.pushRepo = "owner/registry"
	m.pushed = 12
	m.agentPaths = []string{"/tmp/a", "/tmp/b", "/tmp/c"}
	m.width, m.height = 120, 30
	v := m.View()
	for _, want := range []string{"owner/registry", "12", "3", "continue to the hub"} {
		if !strings.Contains(v, want) {
			t.Errorf("Done view missing %q:\n%s", want, v)
		}
	}
	nm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !nm.(WizardModel).Completed() || cmd == nil {
		t.Fatal("enter on Done did not complete and quit")
	}
}
