package tui

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/nikships/skills-registry/cli/internal/importgate"
)

// DiscoverRow is one row of the public skill index, in the shape the picker
// renders. It mirrors the index client's published contract (name,
// description, author, category, skill_url, and the three grades) without
// importing it, so this package stays a presentation layer that a fixture can
// drive.
//
// There is deliberately no star count. The index reports stars for the
// repository hosting a skill, not for the skill, so ranking or labelling rows
// by them would advertise a number that says nothing about the row.
type DiscoverRow struct {
	Name          string
	Desc          string
	Author        string
	Category      string
	SkillURL      string
	Safety        string
	Completeness  string
	Executability string
}

// FilterValue implements list.Item. Filtering hits every field the row shows,
// so a user can narrow by author or category as well as by name.
func (r DiscoverRow) FilterValue() string {
	return strings.Join([]string{r.Name, r.Desc, r.Category, r.Author}, " ")
}

// Title is the row's headline, falling back to the URL for a row the index
// published without a name.
func (r DiscoverRow) Title() string {
	if r.Name != "" {
		return r.Name
	}
	return r.SkillURL
}

// scores renders the row's three grades through the shared import gate, which
// is what guarantees a grade the index never assigned reads as "unscored"
// rather than as a blank that looks like a pass.
func (r DiscoverRow) scores() importgate.Scores {
	return importgate.Scores{
		Safety:        r.Safety,
		Completeness:  r.Completeness,
		Executability: r.Executability,
	}
}

// listRow projects the row onto the registry list's item type so the picker
// can reuse that delegate verbatim: the name is the title, the index
// description is the two-line body, and the category takes the right-hand
// column the registry list gives a slug.
func (r DiscoverRow) listRow() SkillRow {
	return SkillRow{Slug: r.Category, Name: r.Title(), Desc: r.Desc}
}

// DiscoverFlowDeps wires the picker to its search and import primitives. Both
// live in cli/cmd/skills-registry/, so this package reaches neither the index
// nor the registry itself.
type DiscoverFlowDeps struct {
	// Query is the search text, shown in the header.
	Query string
	// Repo is the registry a picked row is published to, named in the confirm.
	Repo string
	// Search runs one index query. An error must surface as the error state:
	// an index that could not be reached and an index with no match for the
	// query are not the same answer, and rendering the first as an empty list
	// would report the wrong one.
	Search func(context.Context) ([]DiscoverRow, error)
	// Add is the import path a picked row travels. It is the same dependency
	// set the Add flow uses, so a pick is held to the existing untrusted
	// import gate rather than to a second copy of those rules.
	Add AddFlowDeps
}

type discoverFlowState int

const (
	discoverStateLoading discoverFlowState = iota
	discoverStateList
	discoverStateConfirm
	discoverStateImport
	discoverStateError
)

// DiscoverFlowModel is the interactive public-index picker: it searches the
// index, lists the hits with a preview pane, and hands a picked row to the
// Add flow for import.
//
// It is written as an embeddable flow rather than as logic inside the cobra
// command. Standalone (`skills-registry discover QUERY`) it ends with
// tea.Quit and the caller reads Toast/Imported/Err off the final model; hosted
// by the hub, WithOnExit turns the same ending into the hub's flow-exit
// message.
type DiscoverFlowModel struct {
	ctx  context.Context
	deps DiscoverFlowDeps

	// OnExit, when set, replaces the flow's tea.Quit ending with the message
	// it returns, which is how the hub keeps its program alive after the
	// picker closes.
	OnExit func(DiscoverFlowModel) tea.Msg

	state        discoverFlowState
	spinner      spinner.Model
	list         list.Model
	confirmModel ChoiceModel
	addFlow      tea.Model

	rows     []DiscoverRow
	picked   DiscoverRow
	err      error
	toast    string
	toastOK  bool
	imported int

	width, height        int
	previewW, previewH   int
	sparkleIdx           int
	sparkleOwnedByImport bool
}

type discoverLoadedMsg struct {
	rows []DiscoverRow
	err  error
}

// discoverImportDoneMsg reports the embedded Add flow's outcome back to the
// picker, so finishing (or cancelling) an import returns to the list instead
// of closing both flows.
type discoverImportDoneMsg struct {
	exit addFlowExit
}

// NewDiscoverFlow builds the picker. The search runs from Init, so the first
// frame is already the spinner rather than an empty list.
func NewDiscoverFlow(ctx context.Context, deps DiscoverFlowDeps) DiscoverFlowModel {
	sp := spinner.New()
	sp.Spinner = spinner.Points
	sp.Style = lipgloss.NewStyle().Foreground(ColPink).Bold(true)
	return DiscoverFlowModel{
		ctx:     ctx,
		deps:    deps,
		state:   discoverStateLoading,
		spinner: sp,
		list:    newDiscoverList(),
	}
}

// WithOnExit wires the hosted ending. Leave it unset for the standalone
// command.
func (m DiscoverFlowModel) WithOnExit(fn func(DiscoverFlowModel) tea.Msg) DiscoverFlowModel {
	m.OnExit = fn
	return m
}

// Toast returns the closing status line and whether it reports success, for a
// standalone caller to print once the alt screen is gone.
func (m DiscoverFlowModel) Toast() (string, bool) { return m.toast, m.toastOK }

// Imported reports how many skills this session published. Zero is the
// contract for every cancelling path: esc, q, an empty selection, or a
// declined confirmation all leave the registry untouched.
func (m DiscoverFlowModel) Imported() int { return m.imported }

// Err returns the search failure, if any. It is non-nil only when the index
// could not be searched, so a caller can exit non-zero on a failed search
// while a cancelled pick still exits 0.
func (m DiscoverFlowModel) Err() error { return m.err }

// newDiscoverList builds the bubbles list with the registry list's own
// delegate and chrome, so the picker does not introduce a second visual
// language for a list of skills.
func newDiscoverList() list.Model {
	l := list.New([]list.Item{}, discoverDelegate{skillDelegate: newSkillDelegate(idleRowStatus)}, 0, 0)
	l.SetShowTitle(false)
	l.SetShowStatusBar(false)
	l.SetShowHelp(false)
	l.SetFilteringEnabled(true)
	l.SetShowPagination(true)
	l.DisableQuitKeybindings()
	l.FilterInput.Prompt = "/"
	l.FilterInput.PromptStyle = lipgloss.NewStyle().Foreground(ColPink).Bold(true)
	l.FilterInput.TextStyle = lipgloss.NewStyle().Foreground(ColAccent)
	l.FilterInput.Cursor.Style = lipgloss.NewStyle().Foreground(ColPrimary)
	l.Styles.FilterCursor = lipgloss.NewStyle().Foreground(ColPrimary)
	l.Styles.FilterPrompt = lipgloss.NewStyle().Foreground(ColPink).Bold(true)
	l.Styles.NoItems = lipgloss.NewStyle().Foreground(ColMuted).Italic(true).Padding(1, 2)
	l.Styles.PaginationStyle = lipgloss.NewStyle().Foreground(ColPrimary).PaddingLeft(2)
	l.Styles.ActivePaginationDot = lipgloss.NewStyle().Foreground(ColAccent).SetString("●")
	l.Styles.InactivePaginationDot = lipgloss.NewStyle().Foreground(ColFaint).SetString("○")
	return l
}

func idleRowStatus(string) RowStatus { return StatusIdle }

// discoverDelegate renders a DiscoverRow through the registry list's delegate.
// The projection lives here rather than in the delegate so the shared row
// rendering has exactly one implementation.
type discoverDelegate struct{ skillDelegate }

func (d discoverDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	row, ok := item.(DiscoverRow)
	if !ok {
		return
	}
	d.skillDelegate.Render(w, m, index, row.listRow())
}

func (m DiscoverFlowModel) Init() tea.Cmd {
	return tea.Batch(sparkleTick(), m.spinner.Tick, m.startSearch())
}

func (m DiscoverFlowModel) startSearch() tea.Cmd {
	return func() tea.Msg {
		if m.deps.Search == nil {
			return discoverLoadedMsg{err: fmt.Errorf("discover flow is not configured")}
		}
		rows, err := m.deps.Search(m.ctx)
		return discoverLoadedMsg{rows: rows, err: err}
	}
}

func (m DiscoverFlowModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		return m.handleResize(msg)
	case discoverLoadedMsg:
		return m.handleLoaded(msg)
	case discoverImportDoneMsg:
		return m.handleImportDone(msg)
	case sparkleTickMsg:
		return m.handleSparkle(msg)
	case spinner.TickMsg:
		return m.handleSpinner(msg)
	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m.forward(msg)
}

func (m DiscoverFlowModel) handleResize(msg tea.WindowSizeMsg) (tea.Model, tea.Cmd) {
	m.width, m.height = msg.Width, msg.Height
	m.resize()
	if m.addFlow == nil {
		return m, nil
	}
	next, cmd := m.addFlow.Update(msg)
	m.addFlow = next
	return m, cmd
}

// resize splits the terminal the same way the registry list does, including
// dropping the preview pane on a narrow terminal.
func (m *DiscoverFlowModel) resize() {
	if m.width <= 0 || m.height <= 0 {
		return
	}
	const chrome = 6 // header + blank + blank + toast + footer + panel heading
	innerHeight := max(4, m.height-chrome-3)
	if m.width < dualPaneMinWidth {
		m.list.SetSize(m.width-4, innerHeight)
		m.previewW, m.previewH = 0, 0
		return
	}
	listW := max(listMinWidth, m.width*6/10)
	previewW := m.width - listW - 2
	if previewW < previewMinWidth {
		previewW = previewMinWidth
		listW = m.width - previewW - 2
	}
	m.list.SetSize(listW-4, innerHeight)
	m.previewW, m.previewH = previewW-4, innerHeight
}

func (m DiscoverFlowModel) handleLoaded(msg discoverLoadedMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		// Fail closed: the error state replaces the list entirely, so a
		// search that never reached the index can never be mistaken for a
		// search that found nothing.
		m.err = msg.err
		m.state = discoverStateError
		return m, nil
	}
	m.rows = msg.rows
	// The index's own ranking order is preserved: the picker does not re-sort
	// rows, least of all by repository popularity.
	items := make([]list.Item, len(msg.rows))
	for i, row := range msg.rows {
		items[i] = row
	}
	m.list.SetItems(items)
	m.state = discoverStateList
	return m, nil
}

func (m DiscoverFlowModel) handleSparkle(msg tea.Msg) (tea.Model, tea.Cmd) {
	// While the Add flow is embedded it owns the sparkle chain, so the tick
	// is forwarded rather than duplicated; doubling it would compound on
	// every frame.
	if m.state == discoverStateImport && m.addFlow != nil {
		next, cmd := m.addFlow.Update(msg)
		m.addFlow = next
		m.sparkleOwnedByImport = true
		return m, cmd
	}
	m.sparkleIdx++
	return m, sparkleTick()
}

func (m DiscoverFlowModel) handleSpinner(msg spinner.TickMsg) (tea.Model, tea.Cmd) {
	if m.state == discoverStateImport && m.addFlow != nil {
		next, cmd := m.addFlow.Update(msg)
		m.addFlow = next
		return m, cmd
	}
	if m.state != discoverStateLoading {
		return m, nil
	}
	var cmd tea.Cmd
	m.spinner, cmd = m.spinner.Update(msg)
	return m, cmd
}

func (m DiscoverFlowModel) forward(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m.state {
	case discoverStateImport:
		if m.addFlow == nil {
			return m, nil
		}
		next, cmd := m.addFlow.Update(msg)
		m.addFlow = next
		return m, cmd
	case discoverStateList:
		var cmd tea.Cmd
		m.list, cmd = m.list.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m DiscoverFlowModel) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.state {
	case discoverStateImport:
		return m.forward(msg)
	case discoverStateConfirm:
		return m.handleConfirmKey(msg)
	case discoverStateLoading:
		if msg.String() == "ctrl+c" {
			return m.exit("discover · cancelled", true)
		}
		return m, nil
	case discoverStateError:
		switch msg.String() {
		case "ctrl+c", "esc", "q", "enter":
			return m.exit("", false)
		}
		return m, nil
	}
	return m.handleListKey(msg)
}

func (m DiscoverFlowModel) handleListKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// While the list's own filter input is active every key belongs to it,
	// so "q" types a letter instead of quitting.
	if m.list.FilterState() == list.Filtering {
		if msg.String() == "ctrl+c" {
			return m.exit("discover · closed", true)
		}
		return m.forward(msg)
	}
	switch msg.String() {
	case "ctrl+c", "q":
		return m.exit("discover · closed", true)
	case "esc":
		if m.list.FilterValue() != "" {
			m.list.ResetFilter()
			return m, nil
		}
		return m.exit("discover · closed", true)
	case "enter":
		return m.openConfirm()
	}
	return m.forward(msg)
}

// openConfirm asks before anything is fetched. An empty selection — no
// results, or a filter that matched nothing — exits without writing.
func (m DiscoverFlowModel) openConfirm() (tea.Model, tea.Cmd) {
	row, ok := m.list.SelectedItem().(DiscoverRow)
	if !ok {
		return m.exit("discover · nothing selected", true)
	}
	if strings.TrimSpace(row.SkillURL) == "" {
		m.toast, m.toastOK = "✗ "+row.Title()+": the index published no URL for this row", false
		return m, nil
	}
	m.picked = row
	m.confirmModel = discoverConfirm(row, m.deps.Repo)
	m.state = discoverStateConfirm
	return m, nil
}

// discoverConfirm builds the pre-fetch confirmation. A row the index graded
// Poor for safety gets the cancelling answer as its default, so pressing enter
// on a warning is safe; the gate's own extra consent step still follows.
func discoverConfirm(row DiscoverRow, repo string) ChoiceModel {
	title := "Import " + row.Title() + " from the public index?"
	prompt := discoverConfirmPrompt(row, repo)
	yes := Choice{Value: "yes", Label: "Yes, review and import", Hint: "Fetches the folder and shows the import gate"}
	no := Choice{Value: "no", Label: "Cancel", Hint: "Make no changes"}
	if row.scores().SafetyIsPoor() {
		return NewChoice(title, prompt, []Choice{no, yes})
	}
	return NewChoice(title, prompt, []Choice{yes, no})
}

// discoverConfirmPrompt states what is about to be fetched, the index's own
// grades for it, and what the import does and does not touch.
func discoverConfirmPrompt(row DiscoverRow, repo string) string {
	lines := []string{row.SkillURL, ""}
	if row.Author != "" {
		lines = append(lines, "author: "+row.Author)
	}
	lines = append(lines, row.scores().Lines()...)
	lines = append(lines, "")
	target := "your registry"
	if repo != "" {
		target = repo
	}
	lines = append(lines,
		"A public-index row is an untrusted source: it publishes to "+target+" only,",
		"nothing is written to an agent folder unless you opt in, and nothing under",
		"scripts/ is ever run. "+importgate.UnscoredLabel+" means unvetted, not safe.")
	return strings.Join(lines, "\n")
}

func (m DiscoverFlowModel) handleConfirmKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "ctrl+c" || msg.String() == "esc" {
		return m.cancelConfirm()
	}
	next, cmd := m.confirmModel.Update(msg)
	m.confirmModel = next.(ChoiceModel)
	if msg.String() != "enter" {
		return m, cmd
	}
	if m.confirmModel.Value() != "yes" {
		return m.cancelConfirm()
	}
	return m.startImport()
}

// cancelConfirm returns to the list having written nothing.
func (m DiscoverFlowModel) cancelConfirm() (tea.Model, tea.Cmd) {
	m.state = discoverStateList
	m.toast, m.toastOK = "import cancelled · nothing was written", true
	return m, nil
}

// startImport hands the picked row to the Add flow, which owns the untrusted
// gate: the grades, the local scan, the registry-only default, and the extra
// consent a Poor grade or a scan hit needs.
func (m DiscoverFlowModel) startImport() (tea.Model, tea.Cmd) {
	flow := NewAddFlowFromSource(m.ctx, m.deps.Repo, m.deps.Add, m.picked.SkillURL).
		WithOnExit(func(exit addFlowExit) tea.Msg { return discoverImportDoneMsg{exit: exit} })
	m.addFlow = flow
	m.state = discoverStateImport
	m.toast, m.toastOK = "", false
	cmds := []tea.Cmd{flow.Init()}
	if m.width > 0 || m.height > 0 {
		next, cmd := m.addFlow.Update(tea.WindowSizeMsg{Width: m.width, Height: m.height})
		m.addFlow = next
		cmds = append(cmds, cmd)
	}
	return m, tea.Batch(cmds...)
}

func (m DiscoverFlowModel) handleImportDone(msg discoverImportDoneMsg) (tea.Model, tea.Cmd) {
	m.addFlow = nil
	m.state = discoverStateList
	m.imported += msg.exit.published
	m.toast, m.toastOK = msg.exit.toast, msg.exit.ok
	// The embedded flow owned the sparkle chain while it ran, so restart it
	// here rather than leaving the chrome frozen.
	if m.sparkleOwnedByImport {
		m.sparkleOwnedByImport = false
		return m, sparkleTick()
	}
	return m, nil
}

// exit ends the flow: tea.Quit standalone, or OnExit's message when hosted.
func (m DiscoverFlowModel) exit(toast string, ok bool) (tea.Model, tea.Cmd) {
	if toast != "" {
		m.toast, m.toastOK = toast, ok
	}
	if m.OnExit == nil {
		return m, tea.Quit
	}
	snapshot := m
	return m, func() tea.Msg { return snapshot.OnExit(snapshot) }
}

// ────────────────────────────────────────────────────────────────────────────
// Rendering
// ────────────────────────────────────────────────────────────────────────────

func (m DiscoverFlowModel) View() string {
	switch m.state {
	case discoverStateLoading:
		return m.renderLoading()
	case discoverStateError:
		return m.renderError()
	case discoverStateImport:
		if m.addFlow != nil {
			return m.addFlow.View()
		}
	}
	base := m.renderMain()
	if m.state != discoverStateConfirm {
		return base
	}
	overlay := HelpOverlay.Render(m.confirmModel.View())
	if m.width <= 0 || m.height <= 0 {
		return overlay
	}
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, overlay)
}

func (m DiscoverFlowModel) renderLoading() string {
	body := lipgloss.JoinVertical(lipgloss.Center,
		flowHero("Skills Registry · Discover"),
		"",
		miniGradientBar(40, m.sparkleIdx),
		"",
		lipgloss.JoinHorizontal(lipgloss.Center,
			m.spinner.View(), " ",
			lipgloss.NewStyle().Foreground(ColInk).Render("Searching the public skill index for "),
			lipgloss.NewStyle().Foreground(ColPrimary).Bold(true).Render(m.deps.Query),
			lipgloss.NewStyle().Foreground(ColInk).Render(" …"),
		),
		"",
		SubtitleStyle.Render("Only your search terms leave the machine."),
	)
	if m.width <= 0 || m.height <= 0 {
		return body
	}
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, body)
}

const (
	// errorBoxChrome is the cells the error box spends on its border, padding,
	// and the terminal margin, subtracted from the width available to the
	// message itself.
	errorBoxChrome = 12
	// errorDetailMinWidth keeps the message readable on a very narrow
	// terminal instead of collapsing it to a column of single words.
	errorDetailMinWidth = 40
	// errorDetailMaxLines bounds a long transport error so the box cannot
	// grow taller than the screen.
	errorDetailMaxLines = 6
)

// renderError states that the search failed. It never renders a list, empty
// or otherwise: "the index is unreachable" and "the index has no match" are
// different answers and must not look alike.
func (m DiscoverFlowModel) renderError() string {
	// A transport error carries the full endpoint URL twice, so it is wrapped
	// to the terminal rather than allowed to push the box off screen.
	detail := max(errorDetailMinWidth, m.width-errorBoxChrome)
	body := lipgloss.JoinVertical(lipgloss.Center,
		ErrorStyle.Render("✗ The public skill index could not be searched"),
		"",
		lipgloss.NewStyle().Foreground(ColInk).Width(detail).
			Render(wrapToLines(flattenErr(m.err), detail, errorDetailMaxLines)),
		"",
		SubtitleStyle.Render("No results were loaded. Press q or esc to exit."),
	)
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

func (m DiscoverFlowModel) renderMain() string {
	body := m.renderListPanel()
	if m.previewW > 0 {
		body = lipgloss.JoinHorizontal(lipgloss.Top, body, "  ", m.renderPreviewPanel())
	}
	return lipgloss.JoinVertical(lipgloss.Left,
		m.renderHeader(),
		"",
		body,
		"",
		m.renderToast(),
		m.renderFooter(),
	)
}

func (m DiscoverFlowModel) renderHeader() string {
	hero := flowHero("Skills Registry · Discover")
	count := fmt.Sprintf("%d results", len(m.rows))
	if len(m.rows) == 1 {
		count = "1 result"
	}
	sep := KeySepStyle.Render("  ·  ")
	right := lipgloss.JoinHorizontal(lipgloss.Top,
		lipgloss.NewStyle().Foreground(ColPeach).Italic(true).Render("public index"),
		sep,
		lipgloss.NewStyle().Foreground(ColAccent).Bold(true).Render(count),
	)
	if m.deps.Query != "" {
		right = lipgloss.JoinHorizontal(lipgloss.Top,
			right, sep,
			lipgloss.NewStyle().Foreground(ColPrimary).Bold(true).Render(truncate(m.deps.Query, 32)),
		)
	}
	gap := max(1, m.width-lipgloss.Width(hero)-lipgloss.Width(right))
	return lipgloss.JoinHorizontal(lipgloss.Top, hero, strings.Repeat(" ", gap), right)
}

func (m DiscoverFlowModel) renderListPanel() string {
	title := lipgloss.NewStyle().
		Foreground(ColPrimary).
		Bold(true).
		Padding(0, 1).
		Render("◆ Public skill index")
	body := m.list.View()
	if len(m.rows) == 0 {
		body = EmptyHint.Render("Nothing in the index matched\n" + m.deps.Query +
			".\n\nTry --mode vector to search\nby meaning instead of terms.")
	}
	return PanelFocused.Render(lipgloss.JoinVertical(lipgloss.Left, title, body))
}

// renderPreviewPanel is the meta pane: author, the index's three grades, and
// the skill URL the import would fetch.
func (m DiscoverFlowModel) renderPreviewPanel() string {
	heading := lipgloss.NewStyle().
		Foreground(ColAccent).
		Bold(true).
		Padding(0, 1).
		Render("✧ Preview")
	row, ok := m.list.SelectedItem().(DiscoverRow)
	body := EmptyHint.Render("No skill selected.\n\nUse ↑/↓ to move,\n/ to filter,\nenter to import a skill.")
	if ok {
		body = m.renderPreviewBody(row)
	}
	body = lipgloss.NewStyle().Width(m.previewW).Height(m.previewH).Render(body)
	return PanelStyle.Render(lipgloss.JoinVertical(lipgloss.Left, heading, body))
}

// discoverPreviewDescLines caps the description in the preview pane. The row
// delegate already shows two lines; the pane affords a few more without
// pushing the grades or the URL out of the panel.
const discoverPreviewDescLines = 5

func (m DiscoverFlowModel) renderPreviewBody(row DiscoverRow) string {
	inner := max(8, m.previewW-2)
	blocks := []string{PreviewTitle.Render(truncate(row.Title(), inner))}
	if row.Category != "" {
		blocks = append(blocks, PreviewSlug.Render(truncate("category · "+row.Category, inner)))
	}
	desc := row.Desc
	if strings.TrimSpace(desc) == "" {
		desc = "(no description in the index)"
	}
	blocks = append(blocks,
		"",
		PreviewBody.Width(inner).Render(wrapToLines(desc, inner, discoverPreviewDescLines)),
		"",
		miniGradientBar(m.previewW-2, m.sparkleIdx),
		"",
	)
	author := row.Author
	if author == "" {
		author = "(unknown)"
	}
	blocks = append(blocks, PreviewMeta.Render(truncate("author · "+author, inner)))
	for _, line := range row.scores().Lines() {
		blocks = append(blocks, PreviewMeta.Render(truncate(line, inner)))
	}
	blocks = append(blocks,
		"",
		PreviewSlug.Render(wrapToLines(row.SkillURL, inner, 2)),
		"",
		DownloadChip.Render("⏎ enter")+
			lipgloss.NewStyle().Foreground(ColMuted).Render("  import → ")+
			lipgloss.NewStyle().Foreground(ColPeach).Italic(true).Render("registry only by default"),
	)
	return lipgloss.JoinVertical(lipgloss.Left, blocks...)
}

func (m DiscoverFlowModel) renderToast() string {
	if m.toast == "" {
		return ""
	}
	style := OkStyle
	if !m.toastOK {
		style = ErrorStyle
	}
	return style.Render(m.toast)
}

func (m DiscoverFlowModel) renderFooter() string {
	return flowFooter(m.width, m.sparkleIdx, []flowKey{
		{"↑/↓", "navigate"},
		{"/", "filter"},
		{"enter", "import"},
		{"q", "back"},
	})
}
