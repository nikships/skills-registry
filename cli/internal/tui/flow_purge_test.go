package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/nikships/skills-registry/cli/internal/scan"
)

// TestPurgeFlowLoadedEmptyExitsCleanly verifies that when Discover
// returns no skills, the flow short-circuits to a neutral "nothing to
// delete" toast rather than prompting the user to confirm zero deletions.
func TestPurgeFlowLoadedEmptyExitsCleanly(t *testing.T) {
	m := NewPurgeFlow(context.Background(), PurgeFlowDeps{})
	_, cmd := m.Update(purgeLoadedMsg{})
	if cmd == nil {
		t.Fatal("empty purge load returned nil cmd")
	}
	msg, ok := cmd().(flowExitMsg)
	if !ok {
		t.Fatalf("cmd returned %T, want flowExitMsg", cmd())
	}
	if !msg.ok || !strings.Contains(msg.toast, "nothing to delete") {
		t.Fatalf("flowExitMsg = %+v, want ok with 'nothing to delete'", msg)
	}
}

// TestPurgeFlowLoadedErrorExitsWithErrorToast pins the discovery-error
// branch: a failure to enumerate local skills lands as a red toast
// rather than dropping the user back to the hub with no feedback.
func TestPurgeFlowLoadedErrorExitsWithErrorToast(t *testing.T) {
	m := NewPurgeFlow(context.Background(), PurgeFlowDeps{})
	_, cmd := m.Update(purgeLoadedMsg{err: errors.New("scan failed")})
	if cmd == nil {
		t.Fatal("purge load error returned nil cmd")
	}
	msg := cmd().(flowExitMsg)
	if msg.ok || !strings.Contains(msg.toast, "scan failed") {
		t.Fatalf("flowExitMsg = %+v, want error with 'scan failed'", msg)
	}
}

// TestPurgeFlowLoadedSkillsEntersConfirmState verifies that a non-empty
// scan moves the model into the confirmation phase with the candidate
// list captured. We don't run the program — we just exercise the state
// machine update directly.
func TestPurgeFlowLoadedSkillsEntersConfirmState(t *testing.T) {
	skills := []scan.Skill{
		{Slug: "foo", Folder: "/tmp/.claude/skills/foo", Source: "~/.claude/skills"},
		{Slug: "bar", Folder: "/tmp/.cursor/skills/bar", Source: "~/.cursor/skills"},
	}
	m := NewPurgeFlow(context.Background(), PurgeFlowDeps{})
	got, cmd := m.Update(purgeLoadedMsg{skills: skills})
	if cmd != nil {
		t.Fatalf("entering confirm should not return cmd, got %T", cmd)
	}
	mm := got.(PurgeFlowModel)
	if mm.state != purgeStateConfirm {
		t.Fatalf("state = %v, want purgeStateConfirm", mm.state)
	}
	if len(mm.skills) != 2 {
		t.Fatalf("captured %d skills, want 2", len(mm.skills))
	}
	mm.width, mm.height = 120, 40
	v := mm.View()
	wants := []string{
		"Delete 2 local skill",
		"~/.claude/skills",
		"~/.cursor/skills",
		"· foo",
		"· bar",
		"Delete the local folders shown above",
	}
	for _, w := range wants {
		if !strings.Contains(v, w) {
			t.Errorf("confirm View() missing %q:\n%s", w, v)
		}
	}
	if strings.Contains(v, "Continue with the registry write") {
		t.Errorf("confirm View() leaks registry-write copy:\n%s", v)
	}
}

// TestPurgeFlowConfirmYesKicksOffDelete verifies that pressing Enter on
// the Yes branch transitions the state machine to deleting and returns
// a command that, when run, invokes the Delete dep.
func TestPurgeFlowConfirmYesKicksOffDelete(t *testing.T) {
	called := false
	deps := PurgeFlowDeps{
		Delete: func(_ context.Context, skills []scan.Skill) (int, int, error) {
			called = true
			return len(skills), 0, nil
		},
	}
	m := NewPurgeFlow(context.Background(), deps)
	skills := []scan.Skill{{Slug: "foo", Folder: "/tmp/foo"}}
	got, _ := m.Update(purgeLoadedMsg{skills: skills})
	mm := got.(PurgeFlowModel)
	// Choice already on yes (cursor=0).
	got2, cmd := mm.Update(tea.KeyMsg{Type: tea.KeyEnter})
	mm2 := got2.(PurgeFlowModel)
	if mm2.state != purgeStateDeleting {
		t.Fatalf("state after Yes = %v, want purgeStateDeleting", mm2.state)
	}
	if cmd == nil {
		t.Fatal("Yes did not return a Cmd")
	}
	// Drain the batch — the delete goroutine should run.
	for _, msg := range collectMsgs(cmd) {
		if done, ok := msg.(purgeDoneMsg); ok && done.deleted != 1 {
			t.Errorf("purgeDoneMsg.deleted = %d, want 1", done.deleted)
		}
	}
	if !called {
		t.Error("Delete dep was not invoked")
	}
}

// TestPurgeFlowConfirmEscCancels confirms Esc during the prompt exits
// with a neutral "cancelled" toast and never calls Delete.
func TestPurgeFlowConfirmEscCancels(t *testing.T) {
	called := false
	deps := PurgeFlowDeps{
		Delete: func(context.Context, []scan.Skill) (int, int, error) {
			called = true
			return 0, 0, nil
		},
	}
	m := NewPurgeFlow(context.Background(), deps)
	got, _ := m.Update(purgeLoadedMsg{skills: []scan.Skill{{Slug: "foo"}}})
	mm := got.(PurgeFlowModel)
	_, cmd := mm.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("esc returned nil cmd")
	}
	msg := cmd().(flowExitMsg)
	if !msg.ok || !strings.Contains(msg.toast, "cancelled") {
		t.Fatalf("flowExitMsg = %+v, want neutral 'cancelled'", msg)
	}
	if called {
		t.Error("Delete dep should not be invoked on esc")
	}
}

// TestPurgeFlowDoneSuccessToast confirms a clean delete lands a green
// success toast carrying the deleted count.
func TestPurgeFlowDoneSuccessToast(t *testing.T) {
	m := NewPurgeFlow(context.Background(), PurgeFlowDeps{})
	_, cmd := m.Update(purgeDoneMsg{deleted: 3})
	if cmd == nil {
		t.Fatal("done returned nil cmd")
	}
	msg := cmd().(flowExitMsg)
	if !msg.ok {
		t.Errorf("success toast should be ok=true; got %+v", msg)
	}
	if !strings.Contains(msg.toast, "purged 3") {
		t.Errorf("success toast missing count: %q", msg.toast)
	}
}

// TestPurgeFlowDonePartialFailureToast verifies the toast surfaces a
// partial failure with both counts and the red ✗ glyph.
func TestPurgeFlowDonePartialFailureToast(t *testing.T) {
	m := NewPurgeFlow(context.Background(), PurgeFlowDeps{})
	_, cmd := m.Update(purgeDoneMsg{deleted: 2, failed: 1})
	if cmd == nil {
		t.Fatal("partial failure returned nil cmd")
	}
	msg := cmd().(flowExitMsg)
	if msg.ok {
		t.Errorf("partial-failure toast should be ok=false; got %+v", msg)
	}
	if !strings.Contains(msg.toast, "removed 2") || !strings.Contains(msg.toast, "1 failed") {
		t.Errorf("partial failure toast missing counts: %q", msg.toast)
	}
}

// TestPurgeFlowDoneErrorToast confirms a hard error from Delete lands
// as a flat error toast.
func TestPurgeFlowDoneErrorToast(t *testing.T) {
	m := NewPurgeFlow(context.Background(), PurgeFlowDeps{})
	_, cmd := m.Update(purgeDoneMsg{err: errors.New("permission denied")})
	if cmd == nil {
		t.Fatal("error done returned nil cmd")
	}
	msg := cmd().(flowExitMsg)
	if msg.ok {
		t.Errorf("error toast should be ok=false; got %+v", msg)
	}
	if !strings.Contains(msg.toast, "permission denied") {
		t.Errorf("error toast missing reason: %q", msg.toast)
	}
}

// TestPurgeFlowMissingDiscoverDepErrors guarantees the flow surfaces a
// configuration error rather than panicking when its deps are not wired.
func TestPurgeFlowMissingDiscoverDepErrors(t *testing.T) {
	m := NewPurgeFlow(context.Background(), PurgeFlowDeps{})
	cmd := m.startLoad()
	msg := cmd().(purgeLoadedMsg)
	if msg.err == nil {
		t.Fatal("missing Discover dep did not surface configuration error")
	}
	if !strings.Contains(msg.err.Error(), "not configured") {
		t.Errorf("error = %v, want 'not configured'", msg.err)
	}
}

// TestPurgeFlowConfirmPromptListsSkillsBySource pins the body shape:
// each source appears once, its slugs sorted underneath, and no leftover
// folder-count copy from the old aggregate breakdown.
func TestPurgeFlowConfirmPromptListsSkillsBySource(t *testing.T) {
	got := purgeConfirmPrompt([]scan.Skill{
		{Slug: "beta", Source: "~/.claude/skills"},
		{Slug: "alpha", Source: "~/.claude/skills"},
		{Slug: "gamma", Source: "~/.cursor/skills"},
	})
	wants := []string{
		"Removes these local SKILL.md folders",
		"The registry repo is not touched.",
		"~/.claude/skills",
		"  · alpha",
		"  · beta",
		"~/.cursor/skills",
		"  · gamma",
	}
	for _, w := range wants {
		if !strings.Contains(got, w) {
			t.Errorf("prompt missing %q:\n%s", w, got)
		}
	}
	for _, banned := range []string{"Breakdown:", "folder(s))"} {
		if strings.Contains(got, banned) {
			t.Errorf("prompt should not contain %q:\n%s", banned, got)
		}
	}
	// Sources rendered alphabetically: ~/.claude/skills < ~/.cursor/skills.
	if idxClaude, idxCursor := strings.Index(got, "~/.claude/skills"), strings.Index(got, "~/.cursor/skills"); idxClaude < 0 || idxCursor < 0 || idxClaude > idxCursor {
		t.Errorf("sources out of order:\n%s", got)
	}
	// Each slug rendered once.
	for _, slug := range []string{"alpha", "beta", "gamma"} {
		if strings.Count(got, "· "+slug) != 1 {
			t.Errorf("slug %q rendered %d times:\n%s", slug, strings.Count(got, "· "+slug), got)
		}
	}
}

// TestPurgeFlowConfirmPromptTruncatesLongLists pins the cap behavior so
// a 200-skill registry doesn't drown the alt-screen panel.
func TestPurgeFlowConfirmPromptTruncatesLongLists(t *testing.T) {
	skills := make([]scan.Skill, 0, purgeConfirmMaxListed+5)
	for i := 0; i < purgeConfirmMaxListed+5; i++ {
		skills = append(skills, scan.Skill{
			Slug:   fmt.Sprintf("skill-%03d", i),
			Source: "~/.claude/skills",
		})
	}
	got := purgeConfirmPrompt(skills)
	if !strings.Contains(got, "…and 5 more") {
		t.Errorf("truncation footer missing:\n%s", got)
	}
	// The very last slug should NOT be in the body — it lives behind the cap.
	if strings.Contains(got, fmt.Sprintf("skill-%03d", purgeConfirmMaxListed+4)) {
		t.Errorf("expected to truncate skill past cap, got:\n%s", got)
	}
}

// TestPurgeFlowConfirmPromptUnknownSource keeps the fallback when a
// scan.Skill arrives without a Source label.
func TestPurgeFlowConfirmPromptUnknownSource(t *testing.T) {
	got := purgeConfirmPrompt([]scan.Skill{{Slug: "orphan"}})
	if !strings.Contains(got, "(unknown)") || !strings.Contains(got, "· orphan") {
		t.Errorf("missing unknown-source fallback:\n%s", got)
	}
}

// TestPurgeFlowConfirmPromptCapDoesNotEmitOrphanHeaders ensures the cap
// short-circuits at the source level too — if every slug under a source
// would land past the cap, the source header is suppressed entirely so
// the user doesn't see bare labels with no bullets underneath.
func TestPurgeFlowConfirmPromptCapDoesNotEmitOrphanHeaders(t *testing.T) {
	var skills []scan.Skill
	for i := 0; i < purgeConfirmMaxListed; i++ {
		skills = append(skills, scan.Skill{
			Slug:   fmt.Sprintf("a-skill-%02d", i),
			Source: "~/.alpha/skills",
		})
	}
	for i := 0; i < 3; i++ {
		skills = append(skills, scan.Skill{
			Slug:   fmt.Sprintf("b-skill-%02d", i),
			Source: "~/.beta/skills",
		})
	}
	skills = append(skills, scan.Skill{Slug: "c-skill", Source: "~/.gamma/skills"})

	got := purgeConfirmPrompt(skills)
	for _, banned := range []string{"~/.beta/skills", "~/.gamma/skills"} {
		if strings.Contains(got, banned) {
			t.Errorf("orphan source header %q rendered past cap:\n%s", banned, got)
		}
	}
	if !strings.Contains(got, "…and 4 more") {
		t.Errorf("expected '…and 4 more' footer, got:\n%s", got)
	}
}

// TestHubProgramLaunchesPurgeFlow verifies the HubProgram dispatch wires
// the Purge action to a PurgeFlowModel — the regression test for the
// hub_program.go switch statement.
func TestHubProgramLaunchesPurgeFlow(t *testing.T) {
	p := NewHubProgram(context.Background(), HubDeps{
		Repo: "owner/repo",
		Purge: PurgeFlowDeps{
			Discover: func(context.Context) ([]scan.Skill, error) { return nil, nil },
		},
	})
	nm, cmd := p.Update(hubLaunchMsg{action: HubActionPurge})
	hp := nm.(HubProgram)
	if _, ok := hp.flow.(PurgeFlowModel); !ok {
		t.Fatalf("flow = %T, want PurgeFlowModel", hp.flow)
	}
	if cmd == nil {
		t.Fatal("launching purge should return init command")
	}
}
