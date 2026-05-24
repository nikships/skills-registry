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
	"github.com/mattn/go-runewidth"
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

// sparkleTick paces the header / footer animations. 600ms is slow enough that
// the chrome reads as "alive" without strobing while a user is reading rows.
func sparkleTick() tea.Cmd {
	return tea.Tick(600*time.Millisecond, func(time.Time) tea.Msg { return sparkleTickMsg{} })
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
	// The header indicator already surfaces the active filter ("filter: foo ·
	// 23 / 91 shown"); the bubbles built-in filter row inside the panel just
	// duplicates that. Keep only the typing affordance (cursor + text) so
	// users still see what they're typing while filtering.
	l.FilterInput.Prompt = "/"
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
		return m.handleRowsLoaded(msg)
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
		return m.handleDownloadDone(msg)
	case sparkleTickMsg:
		m.sparkleIdx++
		return m, sparkleTick()
	case revealTickMsg:
		return m.handleRevealTick()
	case tea.KeyMsg:
		return m.handleKeyMsg(msg)
	}
	return m.forwardToList(msg)
}

func (m ListModel) handleRowsLoaded(msg rowsLoadedMsg) (tea.Model, tea.Cmd) {
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
}

func (m ListModel) handleDownloadDone(msg downloadDoneMsg) (tea.Model, tea.Cmd) {
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
}

func (m ListModel) handleRevealTick() (tea.Model, tea.Cmd) {
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
}

func (m ListModel) handleKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
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
	// Forward unmatched keys to the list so navigation (j/k, pgup/pgdn,
	// etc.) still works.
	return m.forwardToList(msg)
}

// forwardToList delegates a message to the inner bubbles list and refreshes
// the preview pane based on the new selection.
func (m ListModel) forwardToList(msg tea.Msg) (tea.Model, tea.Cmd) {
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
		// Inner width available for any single-line element in the
		// preview pane. The pane itself already accounts for the
		// rounded border + padding; we reserve two extra cells so a
		// title that fits exactly doesn't sit flush against the
		// right edge.
		innerWidth := m.preview.Width - 2
		if innerWidth < 8 {
			innerWidth = 8
		}
		// Multi-byte / very long names get cell-aware truncation so
		// the title row never overflows the panel.
		title := PreviewTitle.Render(truncate(row.Title(), innerWidth))
		// Slug line only adds info when the slug isn't just a trivial
		// normalization of the name. Suppress on either exact match or when
		// the only difference is hyphens→underscores+lowercase (the normal
		// Slugify output). The download path under the CTA already shows the
		// slug, so this line would otherwise read like a stutter under the
		// title.
		slugLine := ""
		if row.Slug != "" && !slugMatchesName(row.Slug, row.Name) {
			slugLine = PreviewSlug.Render(truncate("slug · "+row.Slug, innerWidth))
		}
		desc := row.Desc
		if desc == "" {
			desc = lipgloss.NewStyle().Foreground(ColMuted).Italic(true).Render("(no description)")
		}
		// PreviewBody.Width(...) soft-wraps long descriptions to fit
		// the panel; combined with the explicit innerWidth budget
		// above, the whole preview stays inside the rounded border
		// regardless of the source string's length.
		descBlock := PreviewBody.Width(innerWidth).Render(desc)

		gradient := miniGradientBar(m.preview.Width-2, m.sparkleIdx)
		dest := ".agents/skills/" + row.Slug + "/"
		var hint string
		switch m.rowState[row.Slug] {
		case StatusDownloading:
			hint = lipgloss.NewStyle().Foreground(ColYellow).Bold(true).Render("⟳ downloading") +
				lipgloss.NewStyle().Foreground(ColMuted).Render(" → ") +
				lipgloss.NewStyle().Foreground(ColPeach).Italic(true).Render(dest)
		case StatusDone:
			saved := dest
			if path, ok := m.rowDest[row.Slug]; ok && path != "" {
				saved = path
			}
			hint = lipgloss.NewStyle().Foreground(ColAccent).Bold(true).Render("✓ saved") +
				lipgloss.NewStyle().Foreground(ColMuted).Render(" → ") +
				lipgloss.NewStyle().Foreground(ColPeach).Italic(true).Render(saved)
		case StatusErr:
			hint = lipgloss.NewStyle().Foreground(ColDanger).Bold(true).Render("✗ failed") +
				lipgloss.NewStyle().Foreground(ColMuted).Render(" — ") +
				lipgloss.NewStyle().Foreground(ColInk).Render(m.rowErr[row.Slug].Error())
		default:
			// CTA: an actual keycap chip + arrow + target path. Reads as a
			// button rather than a sentence.
			hint = DownloadChip.Render("⏎ enter") +
				lipgloss.NewStyle().Foreground(ColMuted).Render("  download → ") +
				lipgloss.NewStyle().Foreground(ColPeach).Italic(true).Render(dest)
		}

		meta := PreviewMeta.Render("registry · " + m.repo)

		blocks := []string{title}
		if slugLine != "" {
			blocks = append(blocks, slugLine)
		}
		blocks = append(blocks,
			"",
			descBlock,
			"",
			gradient,
			"",
			meta,
			"",
			hint,
		)
		body = lipgloss.JoinVertical(lipgloss.Left, blocks...)
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
		lipgloss.NewStyle().Foreground(ColInk).Render("Fetching skills from "),
		lipgloss.NewStyle().Foreground(ColPrimary).Bold(true).Render(m.repo),
		lipgloss.NewStyle().Foreground(ColInk).Render(" …"),
	)
	url := lipgloss.NewStyle().Foreground(ColMuted).Italic(true).
		Render("github.com/" + m.repo)
	tip := SubtitleStyle.Render("Press q to abort.")

	gradient := miniGradientBar(40, m.sparkleIdx)
	body := lipgloss.JoinVertical(lipgloss.Center, hero, "", gradient, "", line, url, "", tip)
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

// slugMatchesName returns true when slug is the canonical Slugify(name)
// — i.e. the slug carries no information the title doesn't. The slugifier
// lower-cases, collapses runs of non-alnum chars to a single "_", and trims
// stray underscores; replicate that locally so the TUI package doesn't have
// to import internal/scan.
func slugMatchesName(slug, name string) bool {
	if slug == "" || name == "" {
		return slug == name
	}
	var b strings.Builder
	lastUnder := false
	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastUnder = false
		} else if !lastUnder {
			b.WriteByte('_')
			lastUnder = true
		}
	}
	canon := strings.Trim(b.String(), "_")
	return canon == slug
}

// Stable sparkle pair. The original version cycled through six glyphs every
// ~180ms, which read as flickering chrome while a user was reading rows. The
// gradient bar in the preview pane plus the footer dots already convey "this
// thing is alive"; the title sparkles can be still.
func (m ListModel) sparkleChars() (rune, rune) {
	return '✦', '✧'
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

// truncate clamps s to a maximum of n display cells, appending an
// ellipsis when truncation is necessary. The function is rune-aware
// (slices the input as []rune so multi-byte UTF-8 sequences are never
// cut mid-codepoint) AND display-width aware (uses lipgloss.Width so a
// CJK glyph or emoji that occupies two terminal cells counts as two
// toward the budget). Returns "" when n ≤ 0 and "…" when n == 1 and
// the source would overflow.
func truncate(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= n {
		return s
	}
	if n == 1 {
		return "…"
	}
	runes := []rune(s)
	w := 0
	cut := 0
	for i, r := range runes {
		rw := runewidth.RuneWidth(r)
		// Reserve one cell for the trailing ellipsis.
		if w+rw+1 > n {
			cut = i
			break
		}
		w += rw
		cut = i + 1
	}
	return string(runes[:cut]) + "…"
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

	// Bullet markers are the same display width (3 cells: glyph + glyph +
	// trailing space) on selected and unselected rows so titles don't
	// jitter horizontally as the cursor moves.
	bullet := d.normalBullet.Render("· ✧")
	title := d.normalTitle
	desc := d.normalDesc
	slug := d.slug
	bar := d.normalBullet.Render(" · ")
	if selected {
		bullet = d.selectedBullet.Render("▸ ✦")
		title = d.selectedTitle
		desc = d.selectedDesc
		slug = d.selectedSlug
		bar = d.cursorBar.Render(" │ ")
	}

	// Per-row download status badge, if any. The map's zero value ("") covers
	// StatusIdle as well as a nil statusOf.
	badge := ""
	if d.statusOf != nil {
		badge = d.statusBadges[d.statusOf(row.Slug)]
	}

	// Only render the right-side slug column when it adds information — i.e.
	// when the slug isn't just the canonical Slugify(name). When they
	// effectively match, the column is pure noise (the title already says it).
	//
	// Budget math has to be done with the slug in mind: leftLine is
	//   bullet(3) + " "(1) + title(titleBudget) + badge(0 or 2)
	// rightLine is
	//   slug(slugBudget)
	// plus at least one space of gap between them. Reserving 7 cells
	// (4 for bullet+space, 2 for the badge slot, 1 for the gap) keeps the
	// row inside the list width regardless of badge state, so a row doesn't
	// reflow when a download badge appears.
	showSlug := row.Slug != "" && row.Name != "" && !slugMatchesName(row.Slug, row.Name)
	slugBudget := 0
	titleBudget := width - 6
	if showSlug {
		slugBudget = max(16, width/3)
		if slugBudget > width-7 {
			slugBudget = max(0, width-7)
		}
		titleBudget = width - slugBudget - 7
		if titleBudget < 1 {
			titleBudget = 1
		}
	}
	titleText := truncate(row.Title(), titleBudget)
	descText := truncate(strings.ReplaceAll(row.Desc, "\n", " "), width-6)

	// Line 1: bullet + title (left), faint slug (right) — gap-filled so the
	// slug right-aligns when there's room. The badge sits between the title
	// and the slug.
	leftLine := bullet + " " + title.Render(titleText) + badge
	line1 := leftLine
	if showSlug && slugBudget > 0 {
		slugText := truncate(row.Slug, slugBudget)
		rightLine := slug.Render(slugText)
		gap := width - lipgloss.Width(leftLine) - lipgloss.Width(rightLine)
		if gap < 1 {
			gap = 1
		}
		line1 = leftLine + strings.Repeat(" ", gap) + rightLine
	}

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
