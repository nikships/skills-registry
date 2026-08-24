package tui

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/nikships/skills-registry/cli/internal/scan"
)

// untrustedGate is the view the cmd side hands over for a blocked third-party
// import.
func untrustedGate() ImportGate {
	return ImportGate{
		Untrusted:    true,
		Reason:       "a public GitHub repository owned by openclaw",
		ScoreLines:   []string{"safety:        Poor", "completeness:  unscored", "executability: unscored"},
		Findings:     []string{"hostile: prompt injection · line 4 · hide-from-user · \"do not tell the user\""},
		BlockSummary: "hostile: the public skill index graded this skill's safety Poor",
		Disclaimer:   "The local scan is a regex heuristic, not a guarantee.",
	}
}

// addFlowAt drives the flow to the select step with the supplied gate, so the
// post-select routing can be exercised without a resolver or a registry.
func addFlowAt(t *testing.T, gate ImportGate, deps AddFlowDeps) AddFlowModel {
	t.Helper()
	m := NewAddFlow(context.Background(), "owner/repo", deps)
	next, _ := m.Update(addLoadedMsg{
		skills:  []scan.Skill{{Slug: "hostile", Name: "hostile"}},
		gate:    gate,
		cleanup: func() {},
	})
	mm, ok := next.(AddFlowModel)
	if !ok {
		t.Fatalf("model = %T, want AddFlowModel", next)
	}
	if mm.state != addStateSelect {
		t.Fatalf("state = %v, want addStateSelect", mm.state)
	}
	return mm
}

// selectAllAndContinue toggles the first row and presses enter, which is how a
// user leaves the multi-select.
func selectAllAndContinue(t *testing.T, m AddFlowModel) AddFlowModel {
	t.Helper()
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
	next, _ = next.(AddFlowModel).Update(tea.KeyMsg{Type: tea.KeyEnter})
	return next.(AddFlowModel)
}

// TestAddFlowUntrustedBlockedShowsWarningStep proves a blocked untrusted
// import cannot reach the publish confirmation without passing the warning
// step first.
func TestAddFlowUntrustedBlockedShowsWarningStep(t *testing.T) {
	m := selectAllAndContinue(t, addFlowAt(t, untrustedGate(), AddFlowDeps{}))
	if m.state != addStateGate {
		t.Fatalf("state = %v, want addStateGate", m.state)
	}
	view := m.View()
	for _, want := range []string{"Untrusted source", "safety:", "unscored", "prompt injection", "heuristic"} {
		if !strings.Contains(view, want) {
			t.Errorf("gate view missing %q:\n%s", want, view)
		}
	}
}

// TestAddFlowUntrustedWarningCancelDefault proves the warning step's default
// answer cancels: pressing enter on a warning must not import.
func TestAddFlowUntrustedWarningCancelDefault(t *testing.T) {
	m := selectAllAndContinue(t, addFlowAt(t, untrustedGate(), AddFlowDeps{}))
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter on the warning step returned no cmd")
	}
	msg, ok := cmd().(flowExitMsg)
	if !ok {
		t.Fatalf("cmd returned %T, want flowExitMsg", cmd())
	}
	if !strings.Contains(msg.toast, "cancelled") {
		t.Fatalf("toast = %q, want a cancellation", msg.toast)
	}
	if mm := next.(AddFlowModel); mm.state != addStateGate {
		t.Errorf("state = %v, want the flow to stay on the gate before exiting", mm.state)
	}
}

// TestAddFlowUntrustedAcceptedGoesToInstallOptIn proves that agreeing to the
// warning leads to the install opt-in, not straight to the agent picker.
func TestAddFlowUntrustedAcceptedGoesToInstallOptIn(t *testing.T) {
	deps := AddFlowDeps{
		InstallTargets: func() []InstallTarget {
			return []InstallTarget{{Display: "Agents", Hint: ".agents/skills", Locked: true, Value: "agents"}}
		},
		Install: func(context.Context, string, []any) ([]string, error) { return nil, nil },
	}
	m := selectAllAndContinue(t, addFlowAt(t, untrustedGate(), deps))
	// Move off the cancelling default onto "yes", then confirm.
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	next, _ = next.(AddFlowModel).Update(tea.KeyMsg{Type: tea.KeyEnter})
	mm := next.(AddFlowModel)
	if mm.state != addStateInstallOptIn {
		t.Fatalf("state = %v, want addStateInstallOptIn", mm.state)
	}
	if !strings.Contains(mm.View(), "untrusted skill") {
		t.Errorf("opt-in view should name the untrusted import:\n%s", mm.View())
	}
}

// TestAddFlowUntrustedInstallOptOutSkipsPicker is the flow's version of the
// registry-only default: declining the opt-in confirms the publish with no
// install targets and never opens the agent picker.
func TestAddFlowUntrustedInstallOptOutSkipsPicker(t *testing.T) {
	deps := AddFlowDeps{
		InstallTargets: func() []InstallTarget {
			return []InstallTarget{{Display: "Agents", Hint: ".agents/skills", Locked: true, Value: "agents"}}
		},
		Install: func(context.Context, string, []any) ([]string, error) {
			t.Error("install ran even though the user declined the opt-in")
			return nil, nil
		},
	}
	clean := untrustedGate()
	clean.BlockSummary = "" // nothing blocked, so the opt-in is the next step
	m := selectAllAndContinue(t, addFlowAt(t, clean, deps))
	if m.state != addStateInstallOptIn {
		t.Fatalf("state = %v, want addStateInstallOptIn for an unblocked untrusted import", m.state)
	}
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // default is "no"
	mm := next.(AddFlowModel)
	if mm.state != addStateConfirm {
		t.Fatalf("state = %v, want addStateConfirm", mm.state)
	}
	if len(mm.targets) != 0 {
		t.Errorf("targets = %v, want none after declining the install", mm.targets)
	}
	// The panel hard-wraps, so match on a fragment that survives wrapping.
	if !strings.Contains(mm.View(), "No local") {
		t.Errorf("confirm view should state that nothing is installed:\n%s", mm.View())
	}
	if !strings.Contains(mm.View(), "scripts/") {
		t.Errorf("confirm view for an untrusted import should state scripts are never run:\n%s", mm.View())
	}
}

// TestAddFlowTrustedSourceKeepsOldRouting pins the unchanged path: a trusted
// source goes select → agent picker, with no gate steps and no banner.
func TestAddFlowTrustedSourceKeepsOldRouting(t *testing.T) {
	deps := AddFlowDeps{
		InstallTargets: func() []InstallTarget {
			return []InstallTarget{{Display: "Agents", Hint: ".agents/skills", Locked: true, Value: "agents"}}
		},
		Install: func(context.Context, string, []any) ([]string, error) { return nil, nil },
	}
	m := addFlowAt(t, ImportGate{}, deps)
	if strings.Contains(m.View(), "Untrusted") {
		t.Errorf("trusted source rendered an untrusted banner:\n%s", m.View())
	}
	mm := selectAllAndContinue(t, m)
	if mm.state != addStateInstall {
		t.Fatalf("state = %v, want addStateInstall for a trusted source", mm.state)
	}
}

// TestAddFlowGateHookFailureAborts proves a gate error stops the flow rather
// than importing with an unknown verdict.
func TestAddFlowGateHookFailureAborts(t *testing.T) {
	cleaned := false
	deps := AddFlowDeps{
		Resolve: func(context.Context, string) (string, func(), error) {
			return t.TempDir(), func() { cleaned = true }, nil
		},
		Discover: func(string, string) ([]scan.Skill, error) {
			return []scan.Skill{{Slug: "x", Name: "x"}}, nil
		},
		Slugs: func(context.Context) (map[string]struct{}, error) { return map[string]struct{}{}, nil },
		Gate: func(context.Context, string, string, []scan.Skill) (ImportGate, error) {
			return ImportGate{}, context.DeadlineExceeded
		},
	}
	msg := runAddLoad(context.Background(), deps, "https://github.com/o/r/tree/main/skills/x")
	if msg.err == nil {
		t.Fatal("a gate failure must abort the load")
	}
	if !cleaned {
		t.Error("the fetched temp dir was not cleaned up after a gate failure")
	}
}

// TestAddFlowNoGateHookTreatsSourceAsTrusted keeps unit tests that omit the
// hook working, and documents the fallback explicitly.
func TestAddFlowNoGateHookTreatsSourceAsTrusted(t *testing.T) {
	deps := AddFlowDeps{
		Resolve:  func(context.Context, string) (string, func(), error) { return t.TempDir(), func() {}, nil },
		Discover: func(string, string) ([]scan.Skill, error) { return []scan.Skill{{Slug: "x", Name: "x"}}, nil },
		Slugs:    func(context.Context) (map[string]struct{}, error) { return map[string]struct{}{}, nil },
	}
	msg := runAddLoad(context.Background(), deps, "https://github.com/o/r/tree/main/skills/x")
	if msg.err != nil {
		t.Fatalf("runAddLoad: %v", msg.err)
	}
	if msg.gate.Untrusted {
		t.Error("gate defaulted to untrusted with no hook configured")
	}
}

func TestImportGateBlocked(t *testing.T) {
	if (ImportGate{}).Blocked() {
		t.Error("an empty gate must not be blocked")
	}
	if !(ImportGate{BlockSummary: "x"}).Blocked() {
		t.Error("a gate with a block summary must be blocked")
	}
}

func TestRenderGateIsEmptyForTrustedFlow(t *testing.T) {
	m := NewAddFlow(context.Background(), "owner/repo", AddFlowDeps{})
	if got := m.renderGate(); got != "" {
		t.Errorf("renderGate = %q, want empty for a trusted source", got)
	}
}
