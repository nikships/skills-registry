package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// failIfIndexSearched returns Discover deps whose search fails the test. Every
// assertion about "the index is not contacted" turns on it.
func failIfIndexSearched(t *testing.T) DiscoverHubDeps {
	t.Helper()
	return DiscoverHubDeps{
		Search: func(context.Context, string) ([]DiscoverRow, error) {
			t.Error("the public index was searched without the user asking for it")
			return nil, nil
		},
		Add: failIfAddRuns(t),
	}
}

// hubWithDiscover builds a hub program wired to the supplied Discover deps and
// sized so every frame renders the wide layout.
func hubWithDiscover(t *testing.T, deps DiscoverHubDeps) HubProgram {
	t.Helper()
	p := NewHubProgram(context.Background(), HubDeps{Repo: "owner/registry", Discover: deps})
	next, _ := p.Update(tea.WindowSizeMsg{Width: 162, Height: 40})
	return next.(HubProgram)
}

// launchDiscover drives the hub through the card launch and returns the
// embedded flow at its query prompt.
func launchDiscover(t *testing.T, p HubProgram) (HubProgram, discoverHubFlow) {
	t.Helper()
	next, _ := p.Update(hubLaunchMsg{action: HubActionDiscover})
	hp := next.(HubProgram)
	flow, ok := hp.flow.(discoverHubFlow)
	if !ok {
		t.Fatalf("flow = %T, want discoverHubFlow", hp.flow)
	}
	return hp, flow
}

// TestHubDiscoverCardIsPresent is the card contract: the tile exists, carries
// copy, and its ID is the action the launcher switches on.
func TestHubDiscoverCardIsPresent(t *testing.T) {
	var card HubCard
	for _, c := range DefaultHubCards() {
		if c.ID == HubActionDiscover {
			card = c
		}
	}
	if card.ID == "" {
		t.Fatal("no Discover card in DefaultHubCards()")
	}
	if card.Title != "Discover" {
		t.Errorf("title = %q, want Discover", card.Title)
	}
	if !strings.Contains(card.Description, "public skill index") {
		t.Errorf("description = %q, want it to name the public skill index", card.Description)
	}
}

// TestHubDiscoverCardLaunchesFlow proves enter on the Discover tile hands
// control to the Discover flow rather than to some other card's.
func TestHubDiscoverCardLaunchesFlow(t *testing.T) {
	hub := freshHub()
	for i, c := range hub.grid.Cards {
		if c.ID == HubActionDiscover {
			hub.grid.Focused = i
		}
	}
	next, cmd := hub.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter on the Discover card returned no cmd")
	}
	msg, ok := cmd().(hubLaunchMsg)
	if !ok {
		t.Fatalf("cmd returned %T, want hubLaunchMsg", cmd())
	}
	if msg.action != HubActionDiscover {
		t.Fatalf("launch action = %q, want %q", msg.action, HubActionDiscover)
	}
	if sel := next.(HubModel).Selection(); sel != "" {
		t.Errorf("Selection() = %q, want empty for an embedded launch", sel)
	}

	_, flow := launchDiscover(t, hubWithDiscover(t, failIfIndexSearched(t)))
	if flow.repo != "owner/registry" {
		t.Errorf("flow repo = %q, want the hub's registry", flow.repo)
	}
}

// TestHubIdleNeverSearchesTheIndex is the network contract: rendering the
// dashboard, animating it, and walking the cards must not contact the public
// index. Only a submitted query may.
func TestHubIdleNeverSearchesTheIndex(t *testing.T) {
	p := hubWithDiscover(t, failIfIndexSearched(t))
	// Init returns the hub's own commands; running them must not search.
	if cmd := p.Init(); cmd != nil {
		cmd()
	}
	var next tea.Model = p
	for _, msg := range []tea.Msg{
		sparkleTickMsg{},
		tea.KeyMsg{Type: tea.KeyRight},
		tea.KeyMsg{Type: tea.KeyDown},
		tea.WindowSizeMsg{Width: 120, Height: 30},
	} {
		var cmd tea.Cmd
		next, cmd = next.Update(msg)
		if cmd != nil {
			cmd()
		}
	}
	_ = next.(HubProgram).View()

	// Launching the card opens the query prompt; the index is still untouched
	// until a query is actually submitted.
	hp, flow := launchDiscover(t, next.(HubProgram))
	if cmd := flow.Init(); cmd != nil {
		cmd()
	}
	_ = hp.View()
	typed, cmd := flow.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("pdf")})
	if cmd != nil {
		cmd()
	}
	if typed.(discoverHubFlow).picker != nil {
		t.Error("typing a query opened the picker before the query was submitted")
	}
}

// TestHubDiscoverSubmittedQueryOpensPicker proves the submitted query reaches
// the index client and the picker it drives.
func TestHubDiscoverSubmittedQueryOpensPicker(t *testing.T) {
	var searched string
	p := hubWithDiscover(t, DiscoverHubDeps{
		Search: func(_ context.Context, query string) ([]DiscoverRow, error) {
			searched = query
			return discoverFixtureRows(), nil
		},
	})
	_, flow := launchDiscover(t, p)
	next, _ := flow.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("pdf")})
	next, cmd := next.(discoverHubFlow).Update(tea.KeyMsg{Type: tea.KeyEnter})
	mm := next.(discoverHubFlow)
	picker, ok := mm.picker.(DiscoverFlowModel)
	if !ok {
		t.Fatalf("picker = %T, want DiscoverFlowModel", mm.picker)
	}
	if picker.deps.Query != "pdf" {
		t.Errorf("picker query = %q, want pdf", picker.deps.Query)
	}
	if picker.deps.Repo != "owner/registry" {
		t.Errorf("picker repo = %q, want owner/registry", picker.deps.Repo)
	}
	if cmd == nil {
		t.Fatal("submitting a query returned no cmd")
	}
	// The search only runs now, from the picker's own start command.
	if msg, ok := picker.startSearch()().(discoverLoadedMsg); !ok || msg.err != nil {
		t.Fatalf("search msg = %+v, want loaded rows", msg)
	}
	if searched != "pdf" {
		t.Errorf("index searched for %q, want pdf", searched)
	}
}

// TestHubDiscoverEmptyQueryIsRejected proves the flow refuses to spend a
// request on a query the index would reject anyway.
func TestHubDiscoverEmptyQueryIsRejected(t *testing.T) {
	_, flow := launchDiscover(t, hubWithDiscover(t, failIfIndexSearched(t)))
	next, _ := flow.Update(tea.KeyMsg{Type: tea.KeyEnter})
	mm := next.(discoverHubFlow)
	if mm.picker != nil {
		t.Fatal("an empty query opened the picker")
	}
	if mm.query.err == nil {
		t.Error("an empty query produced no error caption")
	}
	if !strings.Contains(mm.View(), "required") {
		t.Errorf("view must surface the empty-query error:\n%s", mm.View())
	}
}

// TestHubDiscoverCancelReturnsToHubWithoutWriting is the cancellation
// contract: esc at the query prompt returns to the dashboard, and no add
// dependency — the only path that writes to the registry — is touched.
func TestHubDiscoverCancelReturnsToHubWithoutWriting(t *testing.T) {
	p, flow := launchDiscover(t, hubWithDiscover(t, failIfIndexSearched(t)))
	_, cmd := flow.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("esc returned no cmd")
	}
	exit, ok := cmd().(flowExitMsg)
	if !ok {
		t.Fatalf("cmd returned %T, want flowExitMsg", cmd())
	}
	if !exit.ok || !strings.Contains(exit.toast, "cancelled") {
		t.Errorf("exit = %+v, want a neutral cancellation", exit)
	}
	next, _ := p.Update(exit)
	hp := next.(HubProgram)
	if hp.flow != nil {
		t.Fatalf("flow still active after cancelling: %T", hp.flow)
	}
	if !strings.Contains(hp.hub.View(), "Discover") {
		t.Errorf("the dashboard did not come back:\n%s", hp.hub.View())
	}
}

// TestHubDiscoverPickerCancelReturnsToHub covers the same cancellation one
// step further in: closing the picker itself lands back on the dashboard.
func TestHubDiscoverPickerCancelReturnsToHub(t *testing.T) {
	p := hubWithDiscover(t, DiscoverHubDeps{
		Search: func(context.Context, string) ([]DiscoverRow, error) {
			return discoverFixtureRows(), nil
		},
		Add: failIfAddRuns(t),
	})
	_, flow := launchDiscover(t, p)
	next, _ := flow.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("pdf")})
	next, _ = next.(discoverHubFlow).Update(tea.KeyMsg{Type: tea.KeyEnter})
	next, _ = next.(discoverHubFlow).Update(discoverLoadedMsg{rows: discoverFixtureRows()})
	next, cmd := next.(discoverHubFlow).Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("esc in the picker returned no cmd")
	}
	if _, isQuit := cmd().(tea.QuitMsg); isQuit {
		t.Fatal("the hosted picker quit the program instead of returning to the hub")
	}
	exit, ok := cmd().(flowExitMsg)
	if !ok {
		t.Fatalf("cmd returned %T, want flowExitMsg", cmd())
	}
	if !exit.ok {
		t.Errorf("exit = %+v, want a neutral close", exit)
	}
	hp, _ := p.Update(exit)
	if hp.(HubProgram).flow != nil {
		t.Error("the hub kept the flow after the picker closed")
	}
	_ = next
}

// TestDiscoverFlowExitToasts pins the toast wording for each ending: a
// successful import names the skill, a failed search names the failure, and a
// plain close stays neutral.
func TestDiscoverFlowExitToasts(t *testing.T) {
	imported := discoverFlowWith(t, discoverFixtureRows(), AddFlowDeps{})
	imported.picked = discoverFixtureRows()[0]
	imported.imported = 1
	msg, ok := discoverFlowExit(imported).(flowExitMsg)
	if !ok {
		t.Fatalf("exit = %T, want flowExitMsg", discoverFlowExit(imported))
	}
	if !msg.ok || !strings.Contains(msg.toast, "summarize") {
		t.Errorf("success toast = %+v, want the imported skill named", msg)
	}

	imported.imported = 3
	multi := discoverFlowExit(imported).(flowExitMsg)
	if !multi.ok || !strings.Contains(multi.toast, "3 skills") {
		t.Errorf("multi-import toast = %+v, want the count", multi)
	}

	failed := DiscoverFlowModel{err: errors.New("reach the skill index: no such host")}
	fail := discoverFlowExit(failed).(flowExitMsg)
	if fail.ok {
		t.Error("an unreachable index must toast as a failure")
	}
	if !strings.Contains(fail.toast, "no such host") {
		t.Errorf("failure toast = %q, want the underlying cause", fail.toast)
	}

	closed := discoverFlowExit(DiscoverFlowModel{}).(flowExitMsg)
	if !closed.ok || !strings.Contains(closed.toast, "closed") {
		t.Errorf("neutral close toast = %+v, want \"discover · closed\"", closed)
	}
}

// TestHubRendersDiscoverToasts proves both outcomes reach the dashboard frame
// the user sees after the flow closes.
func TestHubRendersDiscoverToasts(t *testing.T) {
	cases := []struct {
		name string
		exit flowExitMsg
		want string
	}{
		{"success", flowExitMsg{toast: "✓ discover · imported summarize", ok: true}, "imported summarize"},
		{"failure", flowExitMsg{toast: "✗ discover: reach the skill index: no such host", ok: false}, "no such host"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, _ := launchDiscover(t, hubWithDiscover(t, failIfIndexSearched(t)))
			next, _ := p.Update(tc.exit)
			hp := next.(HubProgram)
			if hp.hub.toastOK != tc.exit.ok {
				t.Errorf("toastOK = %v, want %v", hp.hub.toastOK, tc.exit.ok)
			}
			if !strings.Contains(hp.View(), tc.want) {
				t.Errorf("hub view missing %q:\n%s", tc.want, hp.View())
			}
		})
	}
}

// TestHubDiscoverImportRefreshesSkillCount is the acceptance criterion shared
// with the Add card: a successful import re-runs the skill-count loader so the
// header stops showing the pre-import number.
func TestHubDiscoverImportRefreshesSkillCount(t *testing.T) {
	// The registry gains a skill between the two loads, which is exactly what
	// the post-import refresh has to pick up.
	var calls int
	p := NewHubProgram(context.Background(), HubDeps{
		Repo:     "owner/registry",
		Discover: failIfIndexSearched(t),
		Count: func(context.Context) (int, error) {
			calls++
			return calls, nil
		},
	})
	next, _ := p.Update(tea.WindowSizeMsg{Width: 162, Height: 40})
	for _, m := range drainBatch(next.(HubProgram).Init()) {
		if count, ok := m.(hubCountMsg); ok {
			next, _ = next.(HubProgram).Update(count)
		}
	}
	if !strings.Contains(next.(HubProgram).View(), "1 skill") {
		t.Fatalf("setup: the pre-import count did not land:\n%s", next.(HubProgram).View())
	}
	hp, _ := launchDiscover(t, next.(HubProgram))

	next, cmd := hp.Update(flowExitMsg{toast: "✓ discover · imported summarize", ok: true})
	if cmd == nil {
		t.Fatal("a discover import did not refresh the skill count")
	}
	reloaded := next.(HubProgram)
	if reloaded.hub.countLoaded {
		t.Error("the count chip should return to its loading state while refreshing")
	}
	// Draining the batch runs the reload; the header then shows the new count.
	for _, m := range drainBatch(cmd) {
		if count, ok := m.(hubCountMsg); ok {
			next, _ = reloaded.Update(count)
		}
	}
	if !strings.Contains(next.(HubProgram).View(), "2 skills") {
		t.Errorf("hub view did not pick up the post-import count:\n%s", next.(HubProgram).View())
	}
}

// drainBatch runs a tea.Cmd and flattens whatever it produced into the
// messages a running program would have delivered.
func drainBatch(cmd tea.Cmd) []tea.Msg {
	msg := cmd()
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		return []tea.Msg{msg}
	}
	out := make([]tea.Msg, 0, len(batch))
	for _, c := range batch {
		if c == nil {
			continue
		}
		out = append(out, c())
	}
	return out
}
