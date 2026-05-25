package tui

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// ────────────────────────────────────────────────────────────────────────────
// F2.3 — Step 5: Agent select
// ────────────────────────────────────────────────────────────────────────────

// testAgents is a small fixture covering the locked + filterable + default
// combinations that the agent multi-select renderer must distinguish.
var testAgents = []WizardAgent{
	{Display: "Universal", Hint: ".agents/skills", Locked: true, Value: "u"},
	{Display: "Claude Code", Hint: ".claude/skills", Default: true, Value: "c"},
	{Display: "Cursor", Hint: ".cursor/skills", Value: "x"},
	{Display: "Factory", Hint: ".factory/skills", Default: true, Value: "f"},
}

// agentDepsFixture wires AgentChoices + a recording InstallAgents stub.
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

// TestWizardAgentLoadOnEntry confirms that landing on the AgentSelect
// step populates agentItems from deps.AgentChoices.
func TestWizardAgentLoadOnEntry(t *testing.T) {
	deps, _, _ := agentDepsFixture(t)
	m := atStep(WizardStepPush).WithDeps(deps)
	m.pushDone = true
	// Press enter to fire the transition, then deliver the transition
	// message ourselves so onEnterStep runs.
	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	nm, _ = nm.(WizardModel).Update(wizardTransitionMsg{to: WizardStepAgentSelect})
	wiz := nm.(WizardModel)
	if !wiz.agentLoaded {
		t.Fatal("agentLoaded = false after entering AgentSelect")
	}
	if len(wiz.agentItems) != len(testAgents) {
		t.Errorf("agentItems = %d, want %d", len(wiz.agentItems), len(testAgents))
	}
	// Defaults should be checked.
	if len(wiz.agentSelected) < 2 {
		t.Errorf("agentSelected = %d defaults checked, want >=2", len(wiz.agentSelected))
	}
	// Locked entry should sort first.
	if !wiz.agentItems[0].Locked {
		t.Errorf("first item not locked: %+v", wiz.agentItems[0])
	}
}

// TestWizardAgentSpaceTogglesSelection covers the WIZARD-006 toggle
// keymap: space flips the selection state of the cursor row.
func TestWizardAgentSpaceTogglesSelection(t *testing.T) {
	deps, _, _ := agentDepsFixture(t)
	m := atStep(WizardStepAgentSelect).WithDeps(deps)
	m.loadAgentChoices()
	m.agentCursor = 0
	// First space picks the unselected row at the cursor.
	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(" ")})
	wiz := nm.(WizardModel)
	before := len(wiz.agentSelected)
	nm, _ = wiz.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(" ")})
	after := len(nm.(WizardModel).agentSelected)
	if before == after {
		t.Errorf("space did not toggle: before=%d after=%d", before, after)
	}
}

// TestWizardAgentFilterNarrowsRows verifies the filterable list narrows
// in response to typed characters and recovers on backspace. We type
// "cur" to single out Cursor — the other test agents (Claude Code,
// Factory) and their hints ".claude/skills", ".factory/skills" don't
// contain that substring.
func TestWizardAgentFilterNarrowsRows(t *testing.T) {
	deps, _, _ := agentDepsFixture(t)
	m := atStep(WizardStepAgentSelect).WithDeps(deps)
	m.loadAgentChoices()
	full := len(m.agentFilteredIndices())
	nm := tea.Model(m)
	for _, ch := range []rune{'c', 'u', 'r'} {
		nm, _ = nm.(WizardModel).Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{ch}})
	}
	narrow := len(nm.(WizardModel).agentFilteredIndices())
	if narrow >= full {
		t.Errorf("filter did not narrow rows: full=%d narrow=%d", full, narrow)
	}
	for range []rune{'c', 'u', 'r'} {
		nm, _ = nm.(WizardModel).Update(tea.KeyMsg{Type: tea.KeyBackspace})
	}
	if len(nm.(WizardModel).agentFilteredIndices()) != full {
		t.Errorf("backspace did not restore filter")
	}
}

// TestWizardAgentTabSelectsAllVisible exercises the bulk-select shortcut.
func TestWizardAgentTabSelectsAllVisible(t *testing.T) {
	deps, _, _ := agentDepsFixture(t)
	m := atStep(WizardStepAgentSelect).WithDeps(deps)
	m.loadAgentChoices()
	// Clear defaults so we can observe the tab effect cleanly.
	m.agentSelected = map[int]struct{}{}
	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	wiz := nm.(WizardModel)
	if len(wiz.agentSelected) != len(wiz.agentFilteredIndices()) {
		t.Errorf("tab selected %d, want %d", len(wiz.agentSelected), len(wiz.agentFilteredIndices()))
	}
}

// TestWizardAgentEnterStartsInstall confirms enter fires the install
// goroutine and the InstallAgents stub sees the locked + selected values.
func TestWizardAgentEnterStartsInstall(t *testing.T) {
	deps, calls, lastPicked := agentDepsFixture(t)
	m := atStep(WizardStepAgentSelect).WithDeps(deps)
	m.loadAgentChoices()
	nm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	wiz := nm.(WizardModel)
	if !wiz.agentInstalling {
		t.Fatal("enter did not flip agentInstalling")
	}
	if cmd == nil {
		t.Fatal("enter did not return a Cmd")
	}
	// Drain the batch to fire the install func.
	msgs := collectMsgs(cmd)
	if atomic.LoadInt32(calls) != 1 {
		t.Errorf("InstallAgents called %d times, want 1", *calls)
	}
	// Locked Universal entry must be in picked.
	found := false
	for _, v := range *lastPicked {
		if v == "u" {
			found = true
		}
	}
	if !found {
		t.Error("InstallAgents did not receive the locked Universal value")
	}
	if !containsMsgKind(msgs, wizardAgentInstallDoneMsg{}) {
		t.Error("install batch did not emit wizardAgentInstallDoneMsg")
	}
}

// TestWizardAgentInstallDoneAdvances confirms enter after install
// completion transitions to the cleanup step.
func TestWizardAgentInstallDoneAdvances(t *testing.T) {
	m := atStep(WizardStepAgentSelect)
	m.agentInstalling = false
	m.agentInstallDone = true
	m.agentPaths = []string{"/tmp/a"}
	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	wiz := nm.(WizardModel)
	if !wiz.transitioning {
		t.Fatal("enter after install did not advance")
	}
	if wiz.transitionTarget != WizardStepCleanup {
		t.Errorf("target = %v, want WizardStepCleanup", wiz.transitionTarget)
	}
}

// TestWizardAgentViewSurfacesRows checks the rendered panel includes the
// locked strip, filter line, and a visible agent row.
func TestWizardAgentViewSurfacesRows(t *testing.T) {
	deps, _, _ := agentDepsFixture(t)
	m := atStep(WizardStepAgentSelect).WithDeps(deps)
	m.loadAgentChoices()
	m.width, m.height = 120, 40
	v := m.View()
	wants := []string{"Install into agents", "Always included", "Universal", "Filter", "Claude Code"}
	for _, w := range wants {
		if !strings.Contains(v, w) {
			t.Errorf("AgentSelect view missing %q:\n%s", w, v)
		}
	}
}

// TestWizardAgentViewSurfacesInstallSummary confirms the post-install
// view shows the success count and a path preview.
func TestWizardAgentViewSurfacesInstallSummary(t *testing.T) {
	m := atStep(WizardStepAgentSelect)
	m.agentInstalling = false
	m.agentInstallDone = true
	m.agentPaths = []string{"/tmp/.claude/skills/skills-registry/SKILL.md",
		"/tmp/.factory/skills/skills-registry/SKILL.md"}
	m.width, m.height = 120, 40
	v := m.View()
	if !strings.Contains(v, "installed into 2 folder") {
		t.Errorf("install summary missing count:\n%s", v)
	}
	if !strings.Contains(v, ".claude") {
		t.Errorf("install summary missing path preview:\n%s", v)
	}
}

// ────────────────────────────────────────────────────────────────────────────
// F2.3 — Step 6: Cleanup
// ────────────────────────────────────────────────────────────────────────────

// TestWizardCleanupLoadedWithEntriesShowsPrompt confirms the prompt
// renders when LoadCleanup returned >0 entries.
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
	wants := []string{"Tidy local copies", "Yes, delete", "No, keep them"}
	for _, w := range wants {
		if !strings.Contains(v, w) {
			t.Errorf("cleanup prompt missing %q:\n%s", w, v)
		}
	}
}

// TestWizardCleanupLoadedEmptyAutoCompletes confirms zero-entry results
// short-circuit the prompt so the user isn't asked "delete 0 things".
func TestWizardCleanupLoadedEmptyAutoCompletes(t *testing.T) {
	m := atStep(WizardStepCleanup)
	nm, _ := m.Update(wizardCleanupLoadedMsg{})
	wiz := nm.(WizardModel)
	if !wiz.cleanupChosen || !wiz.cleanupDone {
		t.Fatalf("empty cleanup did not auto-complete: chosen=%v done=%v",
			wiz.cleanupChosen, wiz.cleanupDone)
	}
}

// TestWizardCleanupYesRunsDelete confirms "Yes" kicks off the delete
// goroutine and the resulting wizardCleanupDoneMsg sets the deleted
// count.
func TestWizardCleanupYesRunsDelete(t *testing.T) {
	var called int32
	deps := WizardDeps{
		DeleteCleanup: func(entries []WizardCleanupEntry) (int, int) {
			atomic.AddInt32(&called, 1)
			return len(entries), 0
		},
	}
	entries := []WizardCleanupEntry{{Path: "/tmp/a"}, {Path: "/tmp/b"}}
	m := atStep(WizardStepCleanup).WithDeps(deps)
	m.cleanupLoaded = true
	m.cleanupEntries = entries
	nm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	wiz := nm.(WizardModel)
	if !wiz.cleanupChosen || !wiz.cleanupYes {
		t.Fatal("y did not lock in the Yes choice")
	}
	if !wiz.cleanupRunning {
		t.Fatal("cleanupRunning not set when DeleteCleanup is wired")
	}
	if cmd == nil {
		t.Fatal("y did not return a Cmd")
	}
	msgs := collectMsgs(cmd)
	if atomic.LoadInt32(&called) != 1 {
		t.Errorf("DeleteCleanup called %d times, want 1", called)
	}
	if !containsMsgKind(msgs, wizardCleanupDoneMsg{}) {
		t.Errorf("delete batch did not emit wizardCleanupDoneMsg; got %+v", msgs)
	}
}

// TestWizardCleanupNoKeeps confirms "No" exits the step without firing
// DeleteCleanup, even if it's wired.
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
		t.Errorf("n did not lock in the No choice: chosen=%v yes=%v",
			wiz.cleanupChosen, wiz.cleanupYes)
	}
	if !wiz.cleanupDone {
		t.Error("cleanupDone false after No choice")
	}
	if atomic.LoadInt32(&called) != 0 {
		t.Errorf("DeleteCleanup unexpectedly called after No: %d", called)
	}
}

// TestWizardCleanupDoneAdvances confirms enter after the deletion
// finishes transitions to step 7.
func TestWizardCleanupDoneAdvances(t *testing.T) {
	m := atStep(WizardStepCleanup)
	m.cleanupLoaded = true
	m.cleanupChosen = true
	m.cleanupDone = true
	m.cleanupYes = true
	m.cleanupDeleted = 3
	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	wiz := nm.(WizardModel)
	if !wiz.transitioning {
		t.Fatal("enter did not advance after cleanup done")
	}
	if wiz.transitionTarget != WizardStepMCPConnect {
		t.Errorf("target = %v, want WizardStepMCPConnect", wiz.transitionTarget)
	}
}

// ────────────────────────────────────────────────────────────────────────────
// F2.3 — Step 7: Connect MCP client
//
// The CLI never installs or boots an MCP server — this step just
// captures the snippet from deps.MCPSnippet and renders it for the user
// to paste into Claude / Cursor / VS Code.
// ────────────────────────────────────────────────────────────────────────────

// TestWizardMCPConnectCapturesSnippet wires MCPSnippet on the deps and
// verifies that landing on the MCP step records the returned body.
func TestWizardMCPConnectCapturesSnippet(t *testing.T) {
	var calls int32
	snippet := `{"mcpServers":{"skills-registry":{"url":"https://mcp.skills-registry.dev/mcp"}}}`
	deps := WizardDeps{
		MCPSnippet: func() string {
			atomic.AddInt32(&calls, 1)
			return snippet
		},
	}
	m := atStep(WizardStepCleanup).WithDeps(deps)
	m.cleanupDone = true
	m.cleanupChosen = true
	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	nm, _ = nm.(WizardModel).Update(wizardTransitionMsg{to: WizardStepMCPConnect})
	wiz := nm.(WizardModel)
	if !wiz.mcpStarted {
		t.Fatal("mcpStarted = false after entering WizardStepMCPConnect")
	}
	if atomic.LoadInt32(&calls) != 1 {
		t.Errorf("MCPSnippet called %d times, want 1", calls)
	}
	// Deliver the wizardMCPDoneMsg the start command emitted so the
	// snippet lands on the model.
	nm2, _ := wiz.Update(wizardMCPDoneMsg{snippet: snippet})
	wiz = nm2.(WizardModel)
	if wiz.mcpSnippet != snippet {
		t.Errorf("mcpSnippet = %q, want %q", wiz.mcpSnippet, snippet)
	}
	if !wiz.mcpDone {
		t.Error("mcpDone = false after wizardMCPDoneMsg")
	}
}

// TestWizardMCPSnippetPanelRenders checks the body shows the hosted
// snippet and the Codex caveat.
func TestWizardMCPSnippetPanelRenders(t *testing.T) {
	m := atStep(WizardStepMCPConnect)
	m.width, m.height = 120, 40
	nm, _ := m.Update(wizardMCPDoneMsg{
		snippet: "{\n  \"mcpServers\": {\"skills-registry\": {\"url\": \"https://mcp.skills-registry.dev/mcp\"}}\n}",
	})
	wiz := nm.(WizardModel)
	if !wiz.mcpDone {
		t.Fatal("mcpDone = false after wizardMCPDoneMsg")
	}
	v := wiz.View()
	wants := []string{"Paste this", "mcpServers", "mcp.skills-registry.dev", "Codex"}
	for _, w := range wants {
		if !strings.Contains(v, w) {
			t.Errorf("MCP panel missing %q:\n%s", w, v)
		}
	}
}

// TestWizardMCPEnterAdvancesAfterDone confirms enter on a finished MCP
// step transitions to Done.
func TestWizardMCPEnterAdvancesAfterDone(t *testing.T) {
	m := atStep(WizardStepMCPConnect)
	m.mcpDone = true
	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	wiz := nm.(WizardModel)
	if !wiz.transitioning {
		t.Fatal("enter after mcpDone did not advance")
	}
	if wiz.transitionTarget != WizardStepDone {
		t.Errorf("target = %v, want WizardStepDone", wiz.transitionTarget)
	}
}

// ────────────────────────────────────────────────────────────────────────────
// F2.3 — Step 8: Done
// ────────────────────────────────────────────────────────────────────────────

// TestWizardDoneViewSurfacesSummary checks the Done body lists repo,
// skill count, agent count, and the launch-hub CTA.
func TestWizardDoneViewSurfacesSummary(t *testing.T) {
	m := atStep(WizardStepDone)
	m.pushRepo = "owner/registry"
	m.pushed = 12
	m.agentPaths = []string{"/tmp/a", "/tmp/b", "/tmp/c"}
	m.width, m.height = 120, 30
	v := m.View()
	wants := []string{"owner/registry", "12", "3", "continue to the hub"}
	for _, w := range wants {
		if !strings.Contains(v, w) {
			t.Errorf("Done view missing %q:\n%s", w, v)
		}
	}
}

// TestWizardDoneEnterCompletes verifies that enter on Done sets
// Completed()=true and emits a Quit Cmd (the launcher then interprets
// this as "launch hub").
func TestWizardDoneEnterCompletes(t *testing.T) {
	m := atStep(WizardStepDone)
	nm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	wiz := nm.(WizardModel)
	if !wiz.Completed() {
		t.Fatal("enter on Done did not set Completed()")
	}
	if cmd == nil {
		t.Fatal("enter on Done did not return a Quit Cmd")
	}
}

// TestWizardLoadAgentChoicesIsIdempotent guards against double-loading
// if the user revisits the AgentSelect step (e.g. via a future "back"
// button); the user's selection / filter must survive.
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

// TestWizardCancelOverlayDuringAgentInstall covers the always-on escape
// hatch: even after the install goroutine has fired, Esc must show the
// cancel overlay.
func TestWizardCancelOverlayDuringAgentInstall(t *testing.T) {
	m := atStep(WizardStepAgentSelect)
	m.agentInstalling = true
	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	wiz := nm.(WizardModel)
	if !wiz.cancelOverlay {
		t.Fatal("esc during agent install did not open cancel overlay")
	}
}

// TestWizardCleanupViewSurfacesCounts checks the cleanup prompt surfaces
// the per-source breakdown the user needs to make an informed decision.
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
	wants := []string{".claude/skills", ".cursor/skills", "3 local"}
	for _, w := range wants {
		if !strings.Contains(v, w) {
			t.Errorf("cleanup view missing %q:\n%s", w, v)
		}
	}
}

// TestMCPConnectHandlesNilDeps ensures startMCPConnect doesn't crash
// when MCPSnippet is unwired (test-mode invariant) and that the
// downstream wizardMCPDoneMsg still settles the step.
func TestMCPConnectHandlesNilDeps(t *testing.T) {
	m := atStep(WizardStepMCPConnect).WithDeps(WizardDeps{})
	cmd := m.startMCPConnect()
	if !m.mcpStarted {
		t.Error("mcpStarted = false after startMCPConnect")
	}
	if cmd == nil {
		t.Fatal("startMCPConnect returned nil cmd")
	}
	msg := safeRun(cmd)
	done, ok := msg.(wizardMCPDoneMsg)
	if !ok {
		t.Fatalf("cmd returned %T, want wizardMCPDoneMsg", msg)
	}
	if done.snippet != "" {
		t.Errorf("snippet = %q with nil deps; want empty", done.snippet)
	}
	nm, _ := m.Update(done)
	wiz := nm.(WizardModel)
	if !wiz.mcpDone {
		t.Error("mcpDone = false after empty wizardMCPDoneMsg")
	}
}

// TestWizardAgentPlainEnterIsNoopWithoutDep verifies the test-mode path:
// without an InstallAgents dep wired, enter still flips agentInstalling
// → agentInstallDone immediately so the state machine stays exercisable.
func TestWizardAgentPlainEnterIsNoopWithoutDep(t *testing.T) {
	m := atStep(WizardStepAgentSelect)
	m.loadAgentChoices() // no-op without dep
	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	wiz := nm.(WizardModel)
	if !wiz.agentInstalling || !wiz.agentInstallDone {
		t.Errorf("enter without dep should mark install done; got installing=%v done=%v",
			wiz.agentInstalling, wiz.agentInstallDone)
	}
}

// TestWizardAgentInstalledAccessor confirms AgentsInstalled() reflects
// the install path count.
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
