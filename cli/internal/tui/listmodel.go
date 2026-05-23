package tui

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ────────────────────────────────────────────────────────────────────────────
// Public types
// ────────────────────────────────────────────────────────────────────────────

// SkillRow is one skill in the registry list. Implements list.Item.
type SkillRow struct {
	Slug string
	Name string
	Desc string
}

// Title implements list.Item.
func (s SkillRow) Title() string {
	if s.Name != "" {
		return s.Name
	}
	return s.Slug
}

// Description implements list.Item.
func (s SkillRow) Description() string { return s.Desc }

// FilterValue implements list.Item — fuzzy search hits any of the fields.
func (s SkillRow) FilterValue() string {
	return s.Slug + " " + s.Name + " " + s.Desc
}

// RowLoader fetches the registry rows. Invoked once after the model starts so
// the spinner has time to mount; errors land in the error pane.
type RowLoader func() ([]SkillRow, error)

// Downloader pulls a skill into a local folder and returns the on-disk path.
// `reused` is non-empty when an existing folder was reused (e.g. a sibling
// directory whose name slugifies to the same canonical slug).
type Downloader func(ctx context.Context, slug string) (dest string, reused string, err error)

// RowStatus tracks the per-row download state in the list TUI.
type RowStatus int

const (
	StatusIdle RowStatus = iota
	StatusDownloading
	StatusDone
	StatusErr
)

// ────────────────────────────────────────────────────────────────────────────
// Internal messages
// ────────────────────────────────────────────────────────────────────────────

type rowsLoadedMsg struct{ rows []SkillRow }
type loadErrMsg struct{ err error }
type sparkleTickMsg struct{}
type revealTickMsg struct{}
type downloadDoneMsg struct {
	slug   string
	dest   string
	reused string
	err    error
}

func sparkleTick() tea.Cmd {
	return tea.Tick(180*time.Millisecond, func(time.Time) tea.Msg { return sparkleTickMsg{} })
}

func revealTick() tea.Cmd {
	return tea.Tick(28*time.Millisecond, func(time.Time) tea.Msg { return revealTickMsg{} })
}

// ────────────────────────────────────────────────────────────────────────────
// Model
// ────────────────────────────────────────────────────────────────────────────

type listState int

const (
	stateLoading listState = iota
	stateReady
	stateError
)

// ListModel is the main interactive list TUI for `skill-registry list`.
type ListModel struct {
	// configuration
	repo     string
	loader   RowLoader
	download Downloader
	ctx      context.Context

	// sub-components
	spinner spinner.Model
	list    list.Model
	preview viewport.Model

	// state
	state    listState
	err      error
	rows     []SkillRow
	width    int
	height   int
	showHelp bool

	// per-slug download state.
	rowState map[string]RowStatus
	rowDest  map[string]string
	rowErr   map[string]error
	inflight int

	// Last download status, shown above the footer until it's replaced by
	// the next downloadDoneMsg.
	toast   string
	toastOK bool

	// animation state
	sparkleIdx int
	revealCap  int // how many items are "revealed" — animated reveal
}

// NewList constructs a ready-to-run ListModel.
//
// `ctx` is the cobra command's context; it's threaded into each download so
// the underlying `gh` subprocess is cancelled when the host process receives
// a signal. Hitting `q` inside the TUI does *not* cancel ctx — bubbletea
// returns to cobra cleanly and downloads run to completion.
// `repo` is shown in the header chip (e.g. "owner/repo").
// `loader` is invoked once after the spinner mounts. Pre-filter inside
// the loader if you want a narrowed initial view.
// `downloader` is invoked when the user presses enter on a row; it runs in a
// goroutine so the TUI stays responsive.
func NewList(ctx context.Context, repo string, loader RowLoader, downloader Downloader) ListModel {
	sp := spinner.New()
	sp.Spinner = spinner.Points
	sp.Style = lipgloss.NewStyle().Foreground(ColPink).Bold(true)

	// Shared per-row download state. The delegate reads from the same map
	// the model mutates so status badges (⟳ / ✓ / ✗) stay in sync without
	// us re-syncing list items on every state change.
	rowState := map[string]RowStatus{}
	d := newSkillDelegate(func(slug string) RowStatus { return rowState[slug] })
	l := list.New([]list.Item{}, d, 0, 0)
	l.SetShowTitle(false)
	l.SetShowStatusBar(false)
	l.SetShowHelp(false)
	l.SetFilteringEnabled(true)
	l.SetShowPagination(true)
	l.DisableQuitKeybindings()
	l.FilterInput.Prompt = "filter · "
	l.FilterInput.PromptStyle = lipgloss.NewStyle().Foreground(ColPink).Bold(true)
	l.FilterInput.TextStyle = lipgloss.NewStyle().Foreground(ColAccent)
	l.FilterInput.Cursor.Style = lipgloss.NewStyle().Foreground(ColPrimary)
	l.Styles.FilterCursor = lipgloss.NewStyle().Foreground(ColPrimary)
	l.Styles.FilterPrompt = lipgloss.NewStyle().Foreground(ColPink).Bold(true)
	l.Styles.NoItems = lipgloss.NewStyle().Foreground(ColMuted).Italic(true).Padding(1, 2)
	l.Styles.StatusBar = lipgloss.NewStyle().Foreground(ColMuted).Padding(0, 1)
	l.Styles.PaginationStyle = lipgloss.NewStyle().Foreground(ColPrimary).PaddingLeft(2)
	l.Styles.ActivePaginationDot = lipgloss.NewStyle().Foreground(ColAccent).SetString("●")
	l.Styles.InactivePaginationDot = lipgloss.NewStyle().Foreground(ColFaint).SetString("○")

	vp := viewport.New(0, 0)

	return ListModel{
		repo:     repo,
		loader:   loader,
		download: downloader,
		ctx:      ctx,
		spinner:  sp,
		list:     l,
		preview:  vp,
		state:    stateLoading,
		rowState: rowState,
		rowDest:  map[string]string{},
		rowErr:   map[string]error{},
	}
}

// Init implements tea.Model.
func (m ListModel) Init() tea.Cmd {
	return tea.Batch(
		m.spinner.Tick,
		sparkleTick(),
		runLoader(m.loader),
	)
}

func runLoader(loader RowLoader) tea.Cmd {
	return func() tea.Msg {
		rows, err := loader()
		if err != nil {
			return loadErrMsg{err: err}
		}
		return rowsLoadedMsg{rows: rows}
	}
}

func startDownload(ctx context.Context, d Downloader, slug string) tea.Cmd {
	return func() tea.Msg {
		dest, reused, err := d(ctx, slug)
		return downloadDoneMsg{slug: slug, dest: dest, reused: reused, err: err}
	}
}

// findRow returns a pointer to the row with the given slug, or nil if absent.
func (m ListModel) findRow(slug string) *SkillRow {
	for i := range m.rows {
		if m.rows[i].Slug == slug {
			return &m.rows[i]
		}
	}
	return nil
}

// Update implements tea.Model.
func (m ListModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.resize()
		return m, nil

	case rowsLoadedMsg:
		m.rows = msg.rows
		// Begin with just the first item visible; subsequent revealTick
		// messages cascade the rest into the list one frame at a time.
		if len(m.rows) > 0 {
			m.list.SetItems([]list.Item{m.rows[0]})
			m.revealCap = 1
		} else {
			m.list.SetItems(nil)
			m.revealCap = 0
		}
		m.state = stateReady
		m.refreshPreview()
		return m, revealTick()

	case loadErrMsg:
		m.err = msg.err
		m.state = stateError
		return m, nil

	case spinner.TickMsg:
		if m.state == stateLoading || m.inflight > 0 {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}
		return m, nil

	case downloadDoneMsg:
		if m.inflight > 0 {
			m.inflight--
		}
		row := m.findRow(msg.slug)
		name := msg.slug
		if row != nil && row.Name != "" {
			name = row.Name
		}
		if msg.err != nil {
			m.rowState[msg.slug] = StatusErr
			m.rowErr[msg.slug] = msg.err
			// `gh` subprocess errors are routinely multi-line; flatten so
			// the toast stays a single row and doesn't push the footer
			// off-screen.
			errText := strings.ReplaceAll(msg.err.Error(), "\n", " · ")
			m.toast = fmt.Sprintf("✗ %s: %s", name, errText)
			m.toastOK = false
		} else {
			m.rowState[msg.slug] = StatusDone
			m.rowDest[msg.slug] = msg.dest
			if msg.reused != "" {
				m.toast = fmt.Sprintf("✓ %s → %s (reused)", name, msg.dest)
			} else {
				m.toast = fmt.Sprintf("✓ %s → %s", name, msg.dest)
			}
			m.toastOK = true
		}
		return m, nil

	case sparkleTickMsg:
		m.sparkleIdx++
		return m, sparkleTick()

	case revealTickMsg:
		if m.revealCap < len(m.rows) {
			m.revealCap++
			items := make([]list.Item, m.revealCap)
			for i := 0; i < m.revealCap; i++ {
				items[i] = m.rows[i]
			}
			m.list.SetItems(items)
			return m, revealTick()
		}
		return m, nil

	case tea.KeyMsg:
		// Help overlay swallows most keys.
		if m.showHelp {
			switch msg.String() {
			case "?", "esc", "q":
				m.showHelp = false
			}
			return m, nil
		}
		// While the list's own filter input is active, defer everything except
		// ctrl+c so users can type freely.
		if m.list.FilterState() == list.Filtering {
			if msg.String() == "ctrl+c" {
				return m, tea.Quit
			}
			var cmd tea.Cmd
			m.list, cmd = m.list.Update(msg)
			m.refreshPreview()
			return m, cmd
		}
		switch msg.String() {
		case "ctrl+c", "q":
			return m, tea.Quit
		case "esc":
			if m.list.FilterValue() != "" {
				m.list.ResetFilter()
				m.refreshPreview()
				return m, nil
			}
			return m, tea.Quit
		case "?":
			m.showHelp = true
			return m, nil
		case "enter":
			if it, ok := m.list.SelectedItem().(SkillRow); ok {
				if m.download == nil {
					return m, nil
				}
				// Any non-idle row is a no-op: already downloading (double-
				// press), already downloaded this session, or previously
				// failed (retry is out of scope for this feature).
				if m.rowState[it.Slug] != StatusIdle {
					return m, nil
				}
				m.rowState[it.Slug] = StatusDownloading
				m.inflight++
				return m, tea.Batch(
					startDownload(m.ctx, m.download, it.Slug),
					m.spinner.Tick,
				)
			}
		}
	}

	// Forward to the list and refresh the preview pane based on the new
	// selection.
	if m.state == stateReady {
		var cmd tea.Cmd
		m.list, cmd = m.list.Update(msg)
		m.refreshPreview()
		return m, cmd
	}
	return m, nil
}

// View implements tea.Model.
func (m ListModel) View() string {
	switch m.state {
	case stateError:
		return m.renderError()
	case stateLoading:
		return m.renderLoading()
	}
	base := m.renderMain()
	if m.showHelp {
		overlay := m.renderHelp()
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, overlay)
	}
	return base
}

// ────────────────────────────────────────────────────────────────────────────
// Layout & rendering
// ────────────────────────────────────────────────────────────────────────────

const (
	dualPaneMinWidth = 100
	previewMinWidth  = 36
	listMinWidth     = 44
)

func (m *ListModel) resize() {
	if m.width <= 0 || m.height <= 0 {
		return
	}
	const headerBlock = 2 // header line + blank
	const toastBlock = 2  // blank + toast row (always reserved so the layout doesn't jitter)
	const footerBlock = 2 // blank + footer line
	panelInner := m.height - headerBlock - toastBlock - footerBlock
	if panelInner < 8 {
		panelInner = 8
	}
	// Each panel: 2 rows borders + 1 row internal heading → subtract 3 for
	// the actual list/viewport height.
	innerHeight := panelInner - 3
	if innerHeight < 4 {
		innerHeight = 4
	}

	if m.width >= dualPaneMinWidth {
		listW := m.width * 6 / 10
		if listW < listMinWidth {
			listW = listMinWidth
		}
		previewW := m.width - listW - 2 // -2 for the gap between panels
		if previewW < previewMinWidth {
			previewW = previewMinWidth
			listW = m.width - previewW - 2
		}
		// -4 cols per panel: 2 for the rounded border, 2 for horizontal padding.
		m.list.SetSize(listW-4, innerHeight)
		m.preview.Width = previewW - 4
		m.preview.Height = innerHeight
		return
	}
	// Narrow terminal: single column, no preview pane.
	m.list.SetSize(m.width-4, innerHeight)
	m.preview.Width = 0
	m.preview.Height = 0
}

func (m ListModel) renderMain() string {
	header := m.renderHeader()
	footer := m.renderFooter()

	listPane := m.renderListPanel()
	body := listPane
	if m.preview.Width > 0 {
		previewPane := m.renderPreviewPanel()
		body = lipgloss.JoinHorizontal(lipgloss.Top, listPane, "  ", previewPane)
	}

	// The toast row is always emitted (empty when there's nothing to say)
	// so the body block keeps a constant height and the footer doesn't
	// jump when downloads start and finish.
	return lipgloss.JoinVertical(lipgloss.Left,
		header,
		"",
		body,
		"",
		m.renderToast(),
		footer,
	)
}

// renderToast formats the most recent download status string. The model also
// shows an animated spinner glyph while any downloads are in flight so the
// user sees ongoing activity.
func (m ListModel) renderToast() string {
	if m.toast == "" && m.inflight == 0 {
		return ""
	}
	var out string
	if m.inflight > 0 {
		out = m.spinner.View() + lipgloss.NewStyle().Foreground(ColInk).Render(
			fmt.Sprintf(" downloading %d skill(s) …", m.inflight))
	}
	if m.toast != "" {
		style := OkStyle
		if !m.toastOK {
			style = ErrorStyle
		}
		if out != "" {
			out += KeySepStyle.Render("  ·  ")
		}
		out += style.Render(m.toast)
	}
	return out
}

func (m ListModel) renderHeader() string {
	sparkleA, sparkleB := m.sparkleChars()
	hero := lipgloss.JoinHorizontal(lipgloss.Top,
		SparkleStyle.Render(string(sparkleA)),
		" ",
		HeroStyle.Render("Skills Registry"),
		" ",
		SparkleStyle.Render(string(sparkleB)),
	)

	visible := m.visibleCount()
	total := len(m.rows)
	countText := fmt.Sprintf("%d skills", total)
	if total == 1 {
		countText = "1 skill"
	}
	if visible != total {
		countText = fmt.Sprintf("%d / %d shown", visible, total)
	}

	sep := KeySepStyle.Render("  ·  ")
	right := lipgloss.JoinHorizontal(lipgloss.Top,
		lipgloss.NewStyle().Foreground(ColAccent).Bold(true).Render(countText),
		sep,
		lipgloss.NewStyle().Foreground(ColPrimary).Bold(true).Render(m.repo),
	)
	if fv := m.list.FilterValue(); fv != "" {
		right = lipgloss.JoinHorizontal(lipgloss.Top,
			lipgloss.NewStyle().Foreground(ColPeach).Italic(true).Render("filter: "+truncate(fv, 24)),
			sep,
			right,
		)
	}

	// Gap-fill so the right cluster sits flush against the edge.
	gap := m.width - lipgloss.Width(hero) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, hero, strings.Repeat(" ", gap), right)
}

func (m ListModel) renderListPanel() string {
	listView := m.list.View()
	style := PanelFocused
	title := lipgloss.NewStyle().
		Foreground(ColPrimary).
		Bold(true).
		Padding(0, 1).
		Render("◆ Browse")
	content := lipgloss.JoinVertical(lipgloss.Left, title, listView)
	return style.Render(content)
}

func (m ListModel) renderPreviewPanel() string {
	heading := lipgloss.NewStyle().
		Foreground(ColAccent).
		Bold(true).
		Padding(0, 1).
		Render("✧ Preview")

	row, ok := m.list.SelectedItem().(SkillRow)
	body := ""
	if !ok {
		body = EmptyHint.Render("No skill selected.\n\nUse ↑/↓ to move,\n/ to filter,\nenter to download a skill.")
	} else {
		title := PreviewTitle.Render(row.Title())
		slug := PreviewSlug.Render(row.Slug)
		desc := row.Desc
		if desc == "" {
			desc = lipgloss.NewStyle().Foreground(ColMuted).Italic(true).Render("(no description)")
		}
		descBlock := PreviewBody.Width(m.preview.Width - 2).Render(desc)

		gradient := miniGradientBar(m.preview.Width-2, m.sparkleIdx)
		dest := ".agents/skills/" + row.Slug + "/"
		var hint string
		switch m.rowState[row.Slug] {
		case StatusDownloading:
			hint = lipgloss.NewStyle().Foreground(ColYellow).Render("⟳ downloading → ") +
				lipgloss.NewStyle().Foreground(ColPeach).Italic(true).Render(dest)
		case StatusDone:
			saved := dest
			if path, ok := m.rowDest[row.Slug]; ok && path != "" {
				saved = path
			}
			hint = lipgloss.NewStyle().Foreground(ColAccent).Render("✓ saved to ") +
				lipgloss.NewStyle().Foreground(ColPeach).Italic(true).Render(saved)
		case StatusErr:
			hint = lipgloss.NewStyle().Foreground(ColDanger).Render("✗ failed: ") +
				lipgloss.NewStyle().Foreground(ColInk).Render(m.rowErr[row.Slug].Error())
		default:
			hint = lipgloss.NewStyle().Foreground(ColMuted).Render("press ") +
				KeyStyle.Render("enter") +
				lipgloss.NewStyle().Foreground(ColMuted).Render(" to download → ") +
				lipgloss.NewStyle().Foreground(ColPeach).Italic(true).Render(dest)
		}

		meta := PreviewMeta.Render("registry · " + m.repo)

		body = lipgloss.JoinVertical(lipgloss.Left,
			title,
			slug,
			"",
			descBlock,
			"",
			gradient,
			"",
			meta,
			"",
			hint,
		)
	}

	// Pin the preview to the panel height so the box doesn't shrink as items
	// are filtered out.
	body = lipgloss.NewStyle().Width(m.preview.Width).Height(m.preview.Height).Render(body)
	content := lipgloss.JoinVertical(lipgloss.Left, heading, body)
	return PanelStyle.Render(content)
}

func (m ListModel) renderFooter() string {
	keys := []struct{ k, d string }{
		{"↑/↓", "navigate"},
		{"/", "filter"},
		{"enter", "download"},
		{"?", "help"},
		{"q", "quit"},
	}
	parts := make([]string, 0, len(keys)*3)
	for i, kv := range keys {
		if i > 0 {
			parts = append(parts, KeySepStyle.Render(" · "))
		}
		parts = append(parts, KeyStyle.Render(kv.k), " ", KeyDescStyle.Render(kv.d))
	}
	left := strings.Join(parts, "")

	right := SubtitleStyle.Render(animationDots(m.sparkleIdx))
	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, left, strings.Repeat(" ", gap), right)
}

func (m ListModel) renderLoading() string {
	sparkleA, sparkleB := m.sparkleChars()
	hero := lipgloss.JoinHorizontal(lipgloss.Center,
		SparkleStyle.Render(string(sparkleA)),
		" ",
		HeroStyle.Render("Skills Registry"),
		" ",
		SparkleStyle.Render(string(sparkleB)),
	)

	line := lipgloss.JoinHorizontal(lipgloss.Center,
		m.spinner.View(), " ",
		lipgloss.NewStyle().Foreground(ColInk).Render("Connecting to "),
		lipgloss.NewStyle().Foreground(ColPrimary).Bold(true).Render(m.repo),
		lipgloss.NewStyle().Foreground(ColInk).Render(" …"),
	)
	tip := SubtitleStyle.Render("Press q to abort.")

	gradient := miniGradientBar(40, m.sparkleIdx)
	body := lipgloss.JoinVertical(lipgloss.Center, hero, "", gradient, "", line, "", tip)
	if m.width <= 0 || m.height <= 0 {
		return body
	}
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, body)
}

func (m ListModel) renderError() string {
	hero := ErrorStyle.Render("✗ Failed to load registry")
	msg := lipgloss.NewStyle().Foreground(ColInk).Render(m.err.Error())
	hint := SubtitleStyle.Render("Press q or esc to exit. Try `gh auth status`.")

	body := lipgloss.JoinVertical(lipgloss.Center, hero, "", msg, "", hint)
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColDanger).
		Padding(1, 3).
		Render(body)
	if m.width <= 0 || m.height <= 0 {
		return box
	}
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}

func (m ListModel) renderHelp() string {
	rows := []struct{ k, d string }{
		{"↑ / k", "move up"},
		{"↓ / j", "move down"},
		{"pgup / pgdn", "page up / down"},
		{"g / G", "jump to top / bottom"},
		{"/", "start filtering"},
		{"esc", "clear filter (or quit)"},
		{"enter", "download into ./.agents/skills/<slug>/"},
		{"?", "toggle this help"},
		{"q / ctrl+c", "quit"},
	}
	var lines []string
	for _, r := range rows {
		lines = append(lines,
			KeyStyle.Render(padRight(r.k, 14))+
				KeyDescStyle.Render(r.d),
		)
	}
	title := lipgloss.NewStyle().
		Foreground(ColPrimary).
		Bold(true).
		Render("✦ Keybindings")
	footer := SubtitleStyle.Render("Press ? or esc to close.")

	return HelpOverlay.Render(
		lipgloss.JoinVertical(lipgloss.Left,
			title,
			"",
			strings.Join(lines, "\n"),
			"",
			footer,
		),
	)
}

// ────────────────────────────────────────────────────────────────────────────
// Helpers
// ────────────────────────────────────────────────────────────────────────────

var sparkleCycle = []rune{'✦', '✧', '⋆', '✨', '⊹', '∗'}

func (m ListModel) sparkleChars() (rune, rune) {
	a := sparkleCycle[m.sparkleIdx%len(sparkleCycle)]
	b := sparkleCycle[(m.sparkleIdx+3)%len(sparkleCycle)]
	return a, b
}

func (m ListModel) visibleCount() int {
	visible := 0
	for _, it := range m.list.VisibleItems() {
		if _, ok := it.(SkillRow); ok {
			visible++
		}
	}
	return visible
}

func (m *ListModel) refreshPreview() {
	if m.preview.Width <= 0 {
		return
	}
	// The preview pane content is rebuilt every View(); just bump the
	// viewport offset so long descriptions scroll if we ever need to.
	m.preview.GotoTop()
}

// truncate cuts s at n runes and appends an ellipsis.
func truncate(s string, n int) string {
	if n <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n <= 1 {
		return string(r[:n])
	}
	return string(r[:n-1]) + "…"
}

func padRight(s string, n int) string {
	if lipgloss.Width(s) >= n {
		return s
	}
	return s + strings.Repeat(" ", n-lipgloss.Width(s))
}

// miniGradientBar renders a small animated gradient bar used as a divider in
// the preview pane.
func miniGradientBar(width, phase int) string {
	if width <= 0 {
		return ""
	}
	if width > 60 {
		width = 60
	}
	palette := []lipgloss.AdaptiveColor{ColPrimary, ColPink, ColPeach, ColAccent, ColCyan}
	var b strings.Builder
	for i := 0; i < width; i++ {
		c := palette[(i+phase)%len(palette)]
		b.WriteString(lipgloss.NewStyle().Foreground(c).Render("▁"))
	}
	return b.String()
}

// animationDots returns a slow throbbing dots indicator for the footer.
func animationDots(phase int) string {
	frames := []string{"·  ", "·· ", "···", " ··", "  ·", "   "}
	return frames[phase%len(frames)]
}

// ────────────────────────────────────────────────────────────────────────────
// Custom list delegate
// ────────────────────────────────────────────────────────────────────────────

type skillDelegate struct {
	selectedBullet lipgloss.Style
	normalBullet   lipgloss.Style
	selectedTitle  lipgloss.Style
	normalTitle    lipgloss.Style
	selectedDesc   lipgloss.Style
	normalDesc     lipgloss.Style
	slug           lipgloss.Style
	selectedSlug   lipgloss.Style
	cursorBar      lipgloss.Style
	statusBadges   map[RowStatus]string
	statusOf       func(slug string) RowStatus
}

func newSkillDelegate(statusOf func(string) RowStatus) skillDelegate {
	return skillDelegate{
		selectedBullet: lipgloss.NewStyle().Foreground(ColPink).Bold(true),
		normalBullet:   lipgloss.NewStyle().Foreground(ColFaint),
		selectedTitle: lipgloss.NewStyle().
			Foreground(ColPrimary).
			Bold(true),
		normalTitle: lipgloss.NewStyle().
			Foreground(ColInk),
		selectedDesc: lipgloss.NewStyle().
			Foreground(ColAccent),
		normalDesc: lipgloss.NewStyle().
			Foreground(ColMuted),
		slug: lipgloss.NewStyle().
			Foreground(ColPeach).
			Italic(true),
		selectedSlug: lipgloss.NewStyle().
			Foreground(ColPink).
			Italic(true).
			Bold(true),
		cursorBar: lipgloss.NewStyle().
			Foreground(ColPrimary).
			Bold(true),
		statusBadges: map[RowStatus]string{
			StatusDownloading: lipgloss.NewStyle().Foreground(ColYellow).Bold(true).Render(" ⟳"),
			StatusDone:        lipgloss.NewStyle().Foreground(ColAccent).Bold(true).Render(" ✓"),
			StatusErr:         lipgloss.NewStyle().Foreground(ColDanger).Bold(true).Render(" ✗"),
		},
		statusOf: statusOf,
	}
}

func (d skillDelegate) Height() int                             { return 2 }
func (d skillDelegate) Spacing() int                            { return 1 }
func (d skillDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd { return nil }

func (d skillDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	row, ok := item.(SkillRow)
	if !ok {
		return
	}

	selected := index == m.Index()
	width := m.Width()
	if width <= 0 {
		width = 40
	}

	bullet := d.normalBullet.Render(" ✧")
	title := d.normalTitle
	desc := d.normalDesc
	slug := d.slug
	bar := "  "
	if selected {
		bullet = d.selectedBullet.Render("▸ ✦")
		title = d.selectedTitle
		desc = d.selectedDesc
		slug = d.selectedSlug
		bar = d.cursorBar.Render("│ ")
	}

	// Per-row download status badge, if any. The map's zero value ("") covers
	// StatusIdle as well as a nil statusOf.
	badge := ""
	if d.statusOf != nil {
		badge = d.statusBadges[d.statusOf(row.Slug)]
	}

	titleText := truncate(row.Title(), width-14)
	slugText := truncate(row.Slug, max(16, width/3))
	descText := truncate(strings.ReplaceAll(row.Desc, "\n", " "), width-6)

	// Line 1: bullet + title (left), faint slug (right) — gap-filled so the
	// slug right-aligns when there's room. The badge sits between the title
	// and the slug.
	leftLine := bullet + " " + title.Render(titleText) + badge
	rightLine := slug.Render(slugText)
	gap := width - lipgloss.Width(leftLine) - lipgloss.Width(rightLine)
	if gap < 1 {
		gap = 1
	}
	line1 := leftLine + strings.Repeat(" ", gap) + rightLine

	// Line 2: cursor bar + description.
	line2 := bar + desc.Render(descText)

	fmt.Fprint(w, line1)
	fmt.Fprintln(w)
	fmt.Fprint(w, line2)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
