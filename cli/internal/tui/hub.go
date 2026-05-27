package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ────────────────────────────────────────────────────────────────────────────
// HubModel — the F3.1 dashboard for returning users
//
// The hub is launched whenever `skills-registry` is invoked without a
// subcommand AND a registry is already configured. It's an alt-screen
// Bubble Tea model so it can render the same hero chrome as the list /
// wizard TUIs.
//
// F3.1 builds the frame: animated header, responsive card grid, footer.
// F3.2 adds the toast row used to surface the outcome of dispatched
// actions. The long-lived HubProgram catches hubLaunchMsg, runs the
// matching embedded flow, then re-renders the dashboard with WithToast.
// ────────────────────────────────────────────────────────────────────────────

// Hub action IDs. Each constant matches one tile in the default card grid
// and is the value HubModel.Selection() returns when the user picks that
// tile. Callers should compare against these constants rather than
// hard-coded strings.
const (
	HubActionManage   = "manage"
	HubActionSync     = "sync"
	HubActionAdd      = "add"
	HubActionPublish  = "publish"
	HubActionPurge    = "purge"
	HubActionSettings = "settings"

	// Deprecated action IDs kept so older tests / integrations that
	// reference the names still compile; the default grid no longer emits
	// them. HubActionManage absorbs both browse and remove.
	HubActionBrowse = "browse"
	HubActionRemove = "remove"
)

// DefaultHubCards returns the six tiles the dashboard ships with.
// Exposed so the launcher can render the same labels in non-TUI fallback
// paths and tests can reference the same data the production view does.
func DefaultHubCards() []HubCard {
	return []HubCard{
		{
			ID:          HubActionManage,
			Icon:        "📚",
			Title:       "Manage skills",
			Description: "Browse, download, and remove registry skills from one searchable list.",
		},
		{
			ID:          HubActionSync,
			Icon:        "🔄",
			Title:       "Sync",
			Description: "Push any new local skills up to the registry in one batch.",
		},
		{
			ID:          HubActionAdd,
			Icon:        "➕",
			Title:       "Add",
			Description: "Clone a remote source and multi-select which skills to publish.",
		},
		{
			ID:          HubActionPublish,
			Icon:        "📤",
			Title:       "Publish",
			Description: "Upload a single local skill folder to the registry.",
		},
		{
			ID:          HubActionPurge,
			Icon:        "🧹",
			Title:       "Purge local",
			Description: "Delete every local skill folder under your known dot-folders — registry untouched.",
		},
		{
			ID:          HubActionSettings,
			Icon:        "⚙",
			Title:       "Settings",
			Description: "Inspect or edit your repo, branch, cache, and MCP wiring.",
		},
	}
}

// HubCountLoader fetches the number of skills in the registry. Invoked
// once on Init(); the result arrives as a hubCountMsg.
type HubCountLoader func(ctx context.Context) (int, error)

// hubCountMsg is the terminal signal from the count loader goroutine.
type hubCountMsg struct {
	count int
	err   error
}

type hubLaunchMsg struct{ action string }

// HubModel is the alt-screen dashboard model.
type HubModel struct {
	ctx    context.Context
	repo   string
	loader HubCountLoader

	width, height int
	spinner       spinner.Model
	sparkleIdx    int

	grid CardGrid

	countLoaded bool
	count       int
	countErr    error

	// Toast captures a one-line status string surfaced just above the
	// footer. The launcher seeds it via WithToast after a dispatched
	// action returns so the user gets immediate success/failure
	// feedback without an out-of-band log line. Cleared automatically
	// by the next NewHub call.
	toast   string
	toastOK bool

	// Final state inspected by the launcher after tea.Quit returns.
	selection string
	quit      bool
}

// NewHub builds a HubModel with the given repo slug and async count loader.
// A nil loader is treated as "skill count unavailable" — the header just
// shows the repo and a muted hint, no spinner.
func NewHub(ctx context.Context, repo string, loader HubCountLoader) HubModel {
	sp := spinner.New()
	sp.Spinner = spinner.Points
	sp.Style = lipgloss.NewStyle().Foreground(ColPink).Bold(true)
	m := HubModel{
		ctx:     ctx,
		repo:    repo,
		loader:  loader,
		spinner: sp,
		grid:    NewCardGrid(DefaultHubCards()),
	}
	// Without a loader there's nothing to count — surface that on the
	// first frame so the header doesn't spin forever.
	if loader == nil {
		m.countLoaded = true
	}
	return m
}

// WithToast attaches a one-line status caption to the hub. The launcher
// calls this between hub iterations so the next frame surfaces the
// outcome of the previously-dispatched action (e.g. "✓ sync complete"
// or "✗ publish: missing SKILL.md"). Passing an empty string clears
// the toast.
func (m HubModel) WithToast(text string, ok bool) HubModel {
	m.toast = text
	m.toastOK = ok
	return m
}

func (m HubModel) WithSelectionReset() HubModel {
	m.selection = ""
	m.quit = false
	return m
}

// Selection returns the ID of the card the user chose, or "" if the user
// quit without selecting. The launcher dispatches off this value.
func (m HubModel) Selection() string { return m.selection }

// Quit reports whether the user explicitly chose to exit (q / esc /
// ctrl+c) without selecting a card.
func (m HubModel) Quit() bool { return m.quit }

// Init implements tea.Model. Kicks off the persistent sparkle / spinner
// animations and, when wired, fires the skill-count loader.
func (m HubModel) Init() tea.Cmd {
	cmds := []tea.Cmd{sparkleTick(), m.spinner.Tick}
	if m.loader != nil {
		cmds = append(cmds, runHubCountLoader(m.ctx, m.loader))
	}
	return tea.Batch(cmds...)
}

// runHubCountLoader fires the loader in a goroutine and posts the result
// back to the model. Errors land in countErr; the header surfaces a
// muted "unavailable" caption rather than failing the whole TUI.
func runHubCountLoader(ctx context.Context, loader HubCountLoader) tea.Cmd {
	return func() tea.Msg {
		n, err := loader(ctx)
		return hubCountMsg{count: n, err: err}
	}
}

// Update implements tea.Model.
func (m HubModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil
	case sparkleTickMsg:
		m.sparkleIdx++
		return m, sparkleTick()
	case spinner.TickMsg:
		return m.handleSpinnerTick(msg)
	case hubCountMsg:
		m.countLoaded = true
		m.count = msg.count
		m.countErr = msg.err
		return m, nil
	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

// handleSpinnerTick keeps the spinner glyph animating until the skill
// count arrives. Idle ticks would burn CPU for no visible gain.
func (m HubModel) handleSpinnerTick(msg spinner.TickMsg) (tea.Model, tea.Cmd) {
	if m.countLoaded {
		return m, nil
	}
	var cmd tea.Cmd
	m.spinner, cmd = m.spinner.Update(msg)
	return m, cmd
}

// handleKey routes navigation, launch, and quit keys. Arrow keys and the
// matching hjkl bindings walk the grid; enter emits hubLaunchMsg; q / esc /
// ctrl+c exits the long-lived hub program.
func (m HubModel) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	cols := m.grid.Cols(m.width)
	switch msg.String() {
	case "ctrl+c", "q", "esc":
		m.quit = true
		return m, tea.Quit
	case "left", "h":
		m.grid = m.grid.Move("left", cols)
	case "right", "l":
		m.grid = m.grid.Move("right", cols)
	case "up", "k":
		m.grid = m.grid.Move("up", cols)
	case "down", "j":
		m.grid = m.grid.Move("down", cols)
	case "enter":
		action := m.grid.Selected().ID
		m.selection = ""
		return m, func() tea.Msg { return hubLaunchMsg{action: action} }
	}
	return m, nil
}

// View implements tea.Model.
func (m HubModel) View() string {
	header := m.renderHeader()
	gradient := miniGradientBar(m.width-2, m.sparkleIdx)
	footer := m.renderFooter()
	body := m.grid.Render(m.bodyWidth())
	parts := []string{header, gradient, "", body}
	if toast := m.renderToast(); toast != "" {
		parts = append(parts, "", toast)
	}
	parts = append(parts, "", footer)
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

// renderToast formats the post-action status caption set via WithToast.
// Success toasts use OkStyle (green ✓ family); failures use ErrorStyle
// (red ✗) so the user spots the outcome at a glance. Mirrors the
// listmodel.go toast pattern so the visual language stays consistent
// across the hub and the browse list.
func (m HubModel) renderToast() string {
	if m.toast == "" {
		return ""
	}
	if m.toastOK {
		return OkStyle.Render(m.toast)
	}
	return ErrorStyle.Render(m.toast)
}

// bodyWidth returns the width available to the card grid. Reserves 2
// cols so the grid doesn't sit flush against the terminal edge.
func (m HubModel) bodyWidth() int {
	if m.width <= 4 {
		return 40
	}
	return m.width - 2
}

// renderHeader builds the top line: sparkle-bracketed hero on the left,
// repo chip + skill count on the right, gap-filled so the right cluster
// pins to the edge.
func (m HubModel) renderHeader() string {
	hero := flowHero("Skills Registry · Hub")
	right := m.renderHeaderRight()
	gap := m.width - lipgloss.Width(hero) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, hero, strings.Repeat(" ", gap), right)
}

// renderHeaderRight renders the skill-count + repo cluster shown on the
// right side of the header.
func (m HubModel) renderHeaderRight() string {
	repoChip := lipgloss.NewStyle().Foreground(ColPrimary).Bold(true).Render(m.repo)
	sep := KeySepStyle.Render("  ·  ")
	return lipgloss.JoinHorizontal(lipgloss.Top, m.renderCount(), sep, repoChip)
}

// renderCount returns the skill-count chip. While the loader is in
// flight we surface a spinner + "counting…" caption; on success we
// render "<N> skills"; on error we drop to a muted "unavailable" hint
// so a transient network failure doesn't dominate the header.
func (m HubModel) renderCount() string {
	if !m.countLoaded {
		return lipgloss.JoinHorizontal(lipgloss.Top,
			m.spinner.View(),
			lipgloss.NewStyle().Foreground(ColMuted).Render(" counting skills…"),
		)
	}
	if m.countErr != nil {
		return lipgloss.NewStyle().Foreground(ColPeach).Italic(true).
			Render("· skill count unavailable")
	}
	noun := "skills"
	if m.count == 1 {
		noun = "skill"
	}
	return lipgloss.NewStyle().Foreground(ColAccent).Bold(true).
		Render(fmt.Sprintf("%d %s", m.count, noun))
}

// renderFooter renders the keybinding hints + animated dots.
func (m HubModel) renderFooter() string {
	return flowFooter(m.width, m.sparkleIdx, []flowKey{
		{"←/→/↑/↓", "navigate"},
		{"enter", "select"},
		{"q", "quit"},
	})
}
