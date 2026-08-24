package tui

import (
	"context"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
)

// ────────────────────────────────────────────────────────────────────────────
// discoverHubFlow — the hub's Discover card
//
// The `discover` subcommand takes its query as an argument; the hub has no
// argument to take, so this flow asks for one and then hands it to the very
// same picker. Nothing about the picker changes here: it owns the search, the
// grades, the confirm, and the untrusted import gate, and this file only
// supplies the query and translates the picker's ending into the hub's
// flow-exit message.
//
// The index is contacted from the picker's Init, which runs only once a query
// has been submitted — a hub sitting on the dashboard, or on this flow's
// query prompt, makes no network call at all.
// ────────────────────────────────────────────────────────────────────────────

// DiscoverHubDeps wires the Discover card to the index and to the import path.
// Search takes the query because the hub collects it interactively rather than
// from the command line; Add is the same dependency set the Add card uses,
// with the public-index gate applied, so a pick is held to the existing
// untrusted-import rules.
type DiscoverHubDeps struct {
	Search func(ctx context.Context, query string) ([]DiscoverRow, error)
	Add    AddFlowDeps
}

type discoverHubFlow struct {
	ctx  context.Context
	deps DiscoverHubDeps
	repo string

	query InputModel
	// picker is nil until a query is submitted, which is what keeps the
	// index untouched while the flow shows its prompt.
	picker tea.Model

	width, height int
	sparkleIdx    int
}

func newDiscoverHubFlow(ctx context.Context, repo string, deps DiscoverHubDeps) discoverHubFlow {
	input := NewInput(
		"Discover public skills",
		SubtitleStyle.Render("Searches the public skill index. Only your search terms leave the machine."),
		"pdf, summarize a youtube video, …",
		"",
	)
	input.Help = "enter to search · esc to cancel"
	return discoverHubFlow{ctx: ctx, deps: deps, repo: repo, query: input}
}

func (m discoverHubFlow) Init() tea.Cmd {
	return tea.Batch(sparkleTick(), m.query.Init())
}

func (m discoverHubFlow) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		return m.handleResize(msg)
	case sparkleTickMsg:
		if m.picker == nil {
			m.sparkleIdx++
			return m, sparkleTick()
		}
	case tea.KeyMsg:
		if m.picker == nil {
			return m.handleQueryKey(msg)
		}
	}
	return m.forward(msg)
}

func (m discoverHubFlow) handleResize(msg tea.WindowSizeMsg) (tea.Model, tea.Cmd) {
	m.width, m.height = msg.Width, msg.Height
	return m.forward(msg)
}

func (m discoverHubFlow) forward(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.picker != nil {
		next, cmd := m.picker.Update(msg)
		m.picker = next
		return m, cmd
	}
	next, cmd := m.query.Update(msg)
	m.query = next.(InputModel)
	return m, cmd
}

func (m discoverHubFlow) handleQueryKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "esc":
		return m, flowExitCmd("discover · cancelled", true)
	case "enter":
		// The index rejects an empty query, so catch it here rather than
		// spending a request to be told so.
		query := m.query.Value()
		if query == "" {
			m.query.err = fmt.Errorf("a search query is required")
			return m, nil
		}
		return m.startPicker(query)
	}
	return m.forward(msg)
}

// startPicker hands the typed query to the standalone picker, wired to end
// with the hub's flow-exit message instead of quitting the program.
func (m discoverHubFlow) startPicker(query string) (tea.Model, tea.Cmd) {
	flow := NewDiscoverFlow(m.ctx, DiscoverFlowDeps{
		Query:  query,
		Repo:   m.repo,
		Search: m.searchFn(query),
		Add:    m.deps.Add,
	}).WithOnExit(discoverFlowExit)
	m.picker = flow
	cmds := []tea.Cmd{flow.Init()}
	if m.width > 0 || m.height > 0 {
		next, cmd := m.picker.Update(tea.WindowSizeMsg{Width: m.width, Height: m.height})
		m.picker = next
		cmds = append(cmds, cmd)
	}
	return m, tea.Batch(cmds...)
}

func (m discoverHubFlow) searchFn(query string) func(context.Context) ([]DiscoverRow, error) {
	search := m.deps.Search
	return func(ctx context.Context) ([]DiscoverRow, error) {
		if search == nil {
			return nil, fmt.Errorf("the discover card is not configured")
		}
		return search(ctx, query)
	}
}

func (m discoverHubFlow) View() string {
	if m.picker != nil {
		return m.picker.View()
	}
	return flowFrame("Skills Registry · Discover", m.width, m.sparkleIdx, m.query.View(), m.renderFooter())
}

func (m discoverHubFlow) renderFooter() string {
	return flowFooter(m.width, m.sparkleIdx, []flowKey{
		{"type", "query"},
		{"enter", "search"},
		{"esc", "cancel"},
	})
}

// discoverFlowExit turns the picker's final model into the hub's toast.
//
// The count is authoritative rather than the picker's own caption: closing the
// picker overwrites that caption with a neutral "closed", so a session that
// did import something would otherwise report nothing.
func discoverFlowExit(m DiscoverFlowModel) tea.Msg {
	if err := m.Err(); err != nil {
		return flowExitMsg{toast: "✗ discover: " + flattenErr(err), ok: false}
	}
	switch n := m.Imported(); {
	case n == 1:
		return flowExitMsg{toast: "✓ discover · imported " + m.picked.Title(), ok: true}
	case n > 1:
		return flowExitMsg{toast: fmt.Sprintf("✓ discover · imported %d skills", n), ok: true}
	}
	if toast, ok := m.Toast(); toast != "" {
		return flowExitMsg{toast: toast, ok: ok}
	}
	return flowExitMsg{toast: "discover · closed", ok: true}
}
