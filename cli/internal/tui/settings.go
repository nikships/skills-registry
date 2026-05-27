package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ────────────────────────────────────────────────────────────────────────────
// SettingsModel — F3.3 hub "Settings" tile
//
// Surfaces the resolved registry config (repo, default branch, cache root,
// hosted MCP URL) and lets the user edit the two mutable fields inline.
// Repo and branch are the only writable values in registry.toml; cache
// root and hosted MCP URL are derived at runtime and shown for diagnostics.
// ────────────────────────────────────────────────────────────────────────────

// SettingsSaver persists the edited repo + branch and returns the path
// the config was written to. Returning an error short-circuits the save
// and the TUI surfaces the message inline so the user can retry.
type SettingsSaver func(repo, branch string) (path string, err error)

// settingsField enumerates the editable fields. The TUI's "focus"
// concept is just the index of one of these constants.
type settingsField int

const (
	settingsFieldRepo settingsField = iota
	settingsFieldBranch
	settingsFieldCount // sentinel — size of the focus ring
)

// SettingsModel is the alt-screen sub-TUI launched from the hub's
// Settings tile. It owns two textinput.Model fields (repo + branch) and
// surfaces the read-only cache + hosted-MCP rows for diagnostics.
type SettingsModel struct {
	cacheRoot string
	hostedMCP string

	// Original values captured at load time; updated on successful save.
	origRepo   string
	origBranch string

	// Pre-edit values captured when the user enters edit mode so esc
	// reverts to the state before this specific edit session.
	preEditValue string

	repoInput   textinput.Model
	branchInput textinput.Model

	focused    settingsField
	editing    bool
	saver      SettingsSaver
	OnExit     func(SettingsModel) tea.Msg
	saveErr    error
	savedPath  string
	statusNote string

	width, height int
	sparkleIdx    int

	quit bool
}

// NewSettings builds a settings model populated from the resolved
// config. A nil saver renders the view as read-only — useful for tests
// and for the (future) `--json` settings dump.
func NewSettings(repo, branch, cacheRoot, hostedMCP string, saver SettingsSaver) SettingsModel {
	repoInput := textinput.New()
	repoInput.Placeholder = "owner/repo"
	repoInput.SetValue(repo)
	repoInput.Prompt = "› "
	repoInput.PromptStyle = lipgloss.NewStyle().Foreground(ColPink).Bold(true)
	repoInput.TextStyle = lipgloss.NewStyle().Foreground(ColInk)
	repoInput.Cursor.Style = lipgloss.NewStyle().Foreground(ColPrimary)

	branchInput := textinput.New()
	branchInput.Placeholder = "main"
	branchInput.SetValue(branch)
	branchInput.Prompt = "› "
	branchInput.PromptStyle = lipgloss.NewStyle().Foreground(ColPink).Bold(true)
	branchInput.TextStyle = lipgloss.NewStyle().Foreground(ColInk)
	branchInput.Cursor.Style = lipgloss.NewStyle().Foreground(ColPrimary)

	return SettingsModel{
		cacheRoot:   cacheRoot,
		hostedMCP:   hostedMCP,
		origRepo:    repo,
		origBranch:  branch,
		repoInput:   repoInput,
		branchInput: branchInput,
		saver:       saver,
	}
}

// Quit reports whether the user explicitly chose to leave the settings
// screen (q / ctrl+c / esc-while-not-editing).
func (m SettingsModel) Quit() bool { return m.quit }

// Repo / Branch expose the current input values so the launcher (or
// tests) can read them after the program exits.
func (m SettingsModel) Repo() string {
	return strings.TrimSpace(m.repoInput.Value())
}
func (m SettingsModel) Branch() string {
	return strings.TrimSpace(m.branchInput.Value())
}

// SavedPath returns the path returned by the last successful save, or
// "" if the user quit without saving.
func (m SettingsModel) SavedPath() string { return m.savedPath }

// SaveError returns the error from the most recent save attempt, or
// nil. Cleared on the next save.
func (m SettingsModel) SaveError() error { return m.saveErr }

// Init implements tea.Model.
func (m SettingsModel) Init() tea.Cmd {
	return tea.Batch(sparkleTick(), textinput.Blink)
}

// Update implements tea.Model. Keys gate three modes:
//   - editing == true: forward keystrokes to the focused textinput;
//     enter commits, esc reverts.
//   - editing == false: navigate (tab/up/down), enter editing (e/enter),
//     save (s), or quit (q/esc/ctrl+c).
func (m SettingsModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil
	case sparkleTickMsg:
		m.sparkleIdx++
		return m, sparkleTick()
	case tea.KeyMsg:
		if m.editing {
			return m.handleEditingKey(msg)
		}
		return m.handleNavKey(msg)
	}
	return m, nil
}

// handleNavKey handles keystrokes while the user is browsing the
// fields (no input is focused). e / enter starts editing the focused
// field; s saves; q / esc / ctrl+c exit.
func (m SettingsModel) handleNavKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "q", "esc":
		m.quit = true
		return m, m.exitCmd()
	case "tab", "down", "j":
		m.focused = (m.focused + 1) % settingsFieldCount
		return m, nil
	case "shift+tab", "up", "k":
		m.focused = (m.focused - 1 + settingsFieldCount) % settingsFieldCount
		return m, nil
	case "e", "enter":
		return m.startEdit()
	case "s":
		return m.save()
	}
	return m, nil
}

// startEdit focuses the textinput tied to the current m.focused field
// and flips into editing mode. The launcher captures the value back via
// Repo()/Branch() on exit.
func (m SettingsModel) startEdit() (tea.Model, tea.Cmd) {
	m.editing = true
	m.saveErr = nil
	m.statusNote = ""
	if m.focused == settingsFieldRepo {
		m.preEditValue = m.repoInput.Value()
		m.branchInput.Blur()
		m.repoInput.Focus()
		return m, textinput.Blink
	}
	m.preEditValue = m.branchInput.Value()
	m.repoInput.Blur()
	m.branchInput.Focus()
	return m, textinput.Blink
}

// handleEditingKey routes keystrokes to the focused textinput. Enter
// commits (back to nav mode); esc reverts the field to its original
// value and exits editing.
func (m SettingsModel) handleEditingKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		// Ctrl+C inside edit mode quits the whole program — matches the
		// other TUIs (list, hub, wizard) so users have a consistent
		// escape hatch.
		m.quit = true
		return m, m.exitCmd()
	case "esc":
		return m.cancelEdit(), nil
	case "enter":
		return m.commitEdit(), nil
	}
	return m.forwardToInput(msg)
}

func (m SettingsModel) WithOnExit(onExit func(SettingsModel) tea.Msg) SettingsModel {
	m.OnExit = onExit
	return m
}

func (m SettingsModel) exitCmd() tea.Cmd {
	if m.OnExit == nil {
		return tea.Quit
	}
	snapshot := m
	return func() tea.Msg { return m.OnExit(snapshot) }
}

// cancelEdit restores the focused field to its pre-edit value and exits
// editing mode without saving.
func (m SettingsModel) cancelEdit() SettingsModel {
	if m.focused == settingsFieldRepo {
		m.repoInput.SetValue(m.preEditValue)
		m.repoInput.Blur()
	} else {
		m.branchInput.SetValue(m.preEditValue)
		m.branchInput.Blur()
	}
	m.editing = false
	return m
}

// commitEdit exits editing mode but keeps the new value. The change is
// not persisted until the user presses `s` from nav mode — this mirrors
// the wizard's "press enter to confirm" beat and lets the user back out
// of a typo with esc before committing.
func (m SettingsModel) commitEdit() SettingsModel {
	if m.focused == settingsFieldRepo {
		m.repoInput.Blur()
	} else {
		m.branchInput.Blur()
	}
	m.editing = false
	return m
}

// forwardToInput passes the key to whichever textinput is currently
// focused so typing / cursor movement / backspace all just work.
func (m SettingsModel) forwardToInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	if m.focused == settingsFieldRepo {
		m.repoInput, cmd = m.repoInput.Update(msg)
	} else {
		m.branchInput, cmd = m.branchInput.Update(msg)
	}
	return m, cmd
}

// save invokes the SettingsSaver with the current trimmed values and
// records the outcome for the next render. Saves with no actual changes
// (and no saver wired) still surface a friendly note so the user knows
// the keystroke registered.
func (m SettingsModel) save() (tea.Model, tea.Cmd) {
	if m.saver == nil {
		m.saveErr = fmt.Errorf("settings are read-only in this context")
		m.statusNote = ""
		return m, nil
	}
	repo := strings.TrimSpace(m.repoInput.Value())
	branch := strings.TrimSpace(m.branchInput.Value())
	if branch == "" {
		branch = "main"
	}
	path, err := m.saver(repo, branch)
	if err != nil {
		m.saveErr = err
		m.statusNote = ""
		return m, nil
	}
	m.saveErr = nil
	m.savedPath = path
	m.origRepo = repo
	m.origBranch = branch
	m.statusNote = fmt.Sprintf("saved → %s", path)
	return m, nil
}

// View implements tea.Model.
func (m SettingsModel) View() string {
	header := m.renderHeader()
	body := m.renderBody()
	footer := m.renderFooter()
	parts := []string{header, "", body}
	if status := m.renderStatus(); status != "" {
		parts = append(parts, "", status)
	}
	parts = append(parts, "", footer)
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

// renderHeader builds the sparkle-bracketed title.
func (m SettingsModel) renderHeader() string {
	hero := flowHero("Skills Registry · Settings")
	if m.width <= lipgloss.Width(hero) {
		return hero
	}
	right := SubtitleStyle.Render(animationDots(m.sparkleIdx))
	gap := m.width - lipgloss.Width(hero) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, hero, strings.Repeat(" ", gap), right)
}

// renderBody renders the focused settings panel with all four fields.
// The two editable fields render the live textinput when editing; the
// other two render their resolved values verbatim in a muted style.
func (m SettingsModel) renderBody() string {
	width := m.bodyWidth()
	rows := []string{
		m.renderField("Repository", m.repoFieldValue(), settingsFieldRepo, width),
		m.renderField("Default branch", m.branchFieldValue(), settingsFieldBranch, width),
		m.renderReadOnly("Cache location", m.cacheRoot, width),
		m.renderReadOnly("Hosted MCP URL", m.hostedMCP, width),
	}
	content := lipgloss.JoinVertical(lipgloss.Left, rows...)
	return PanelFocused.Width(width).Render(content)
}

// bodyWidth returns the panel's outer width, leaving a small margin so
// the rounded border doesn't sit flush against the terminal edge.
func (m SettingsModel) bodyWidth() int {
	const minWidth = 50
	if m.width <= 4 {
		return minWidth
	}
	w := m.width - 2
	if w < minWidth {
		w = minWidth
	}
	return w
}

// renderField formats one editable row. The chosen field gets a
// highlighted bullet + bracketed value or live textinput. Long values
// are width-clamped so the row never wraps and pushes the footer
// off-screen.
func (m SettingsModel) renderField(label, value string, field settingsField, width int) string {
	focused := m.focused == field
	bullet := normalBullet()
	labelStyle := lipgloss.NewStyle().Foreground(ColMuted)
	valueStyle := lipgloss.NewStyle().Foreground(ColInk)
	if focused {
		bullet = focusedBullet()
		labelStyle = lipgloss.NewStyle().Foreground(ColPrimary).Bold(true)
		valueStyle = lipgloss.NewStyle().Foreground(ColAccent).Bold(true)
	}

	labelText := labelStyle.Render(label)
	valueWidth := fieldValueWidth(width)
	var rendered string
	if focused && m.editing {
		rendered = m.activeInputView()
	} else {
		rendered = valueStyle.Render(truncate(value, valueWidth))
	}
	return lipgloss.JoinHorizontal(lipgloss.Top,
		bullet, " ",
		lipgloss.NewStyle().Width(labelWidth()).Render(labelText),
		"  ",
		rendered,
	)
}

// renderReadOnly formats a diagnostic row (cache / MCP path). These
// fields never enter edit mode so the bullet stays dim and the value is
// rendered with a muted, italic style.
func (m SettingsModel) renderReadOnly(label, value string, width int) string {
	bullet := normalBullet()
	labelStyle := lipgloss.NewStyle().Foreground(ColMuted)
	valueStyle := lipgloss.NewStyle().Foreground(ColFaint).Italic(true)
	if value == "" {
		value = "(not set)"
	}
	valueWidth := fieldValueWidth(width)
	return lipgloss.JoinHorizontal(lipgloss.Top,
		bullet, " ",
		lipgloss.NewStyle().Width(labelWidth()).Render(labelStyle.Render(label)),
		"  ",
		valueStyle.Render(truncate(value, valueWidth)),
	)
}

// activeInputView returns the currently focused textinput's rendered
// view. Used when the field is in edit mode.
func (m SettingsModel) activeInputView() string {
	if m.focused == settingsFieldRepo {
		return m.repoInput.View()
	}
	return m.branchInput.View()
}

// repoFieldValue returns the current repo value (live, since edits
// commit on enter but stay in the textinput until saved).
func (m SettingsModel) repoFieldValue() string {
	v := strings.TrimSpace(m.repoInput.Value())
	if v == "" {
		return "(not set)"
	}
	return v
}

// branchFieldValue returns the current branch value, defaulting to
// "main" when blank so the displayed value matches what `save` will
// actually persist.
func (m SettingsModel) branchFieldValue() string {
	v := strings.TrimSpace(m.branchInput.Value())
	if v == "" {
		return "main"
	}
	return v
}

// renderStatus surfaces the most recent save outcome — either a green
// ✓ caption with the saved-at path, or a red ✗ with the error message.
// An empty return suppresses the row entirely so the layout stays tight
// before the first save.
func (m SettingsModel) renderStatus() string {
	if m.saveErr != nil {
		flat := strings.ReplaceAll(m.saveErr.Error(), "\n", " · ")
		return ErrorStyle.Render("✗ " + flat)
	}
	if m.statusNote != "" {
		return OkStyle.Render("✓ " + m.statusNote)
	}
	return ""
}

// renderFooter prints the keybinding hints. The keys vary by mode so
// the user always sees the relevant escape hatch.
func (m SettingsModel) renderFooter() string {
	keys := []flowKey{
		{"↑/↓", "field"},
		{"e", "edit"},
		{"s", "save"},
		{"q", "back"},
	}
	if m.editing {
		keys = []flowKey{
			{"enter", "commit"},
			{"esc", "cancel edit"},
		}
	}
	return flowFooter(m.width, m.sparkleIdx, keys)
}

// labelWidth pins a column for the label so the values line up. Wide
// enough for the longest of our four labels ("Hosted MCP URL") plus a
// small breathing margin.
func labelWidth() int { return 18 }

// fieldValueWidth returns how many display cells a field value may
// consume on a single row given the panel width and the fixed label
// column. The math accounts for the rounded panel's border + padding
// (4 cells per side) and the inter-column gaps (bullet + space ≈ 4
// cells, double-space between label and value = 2 cells).
func fieldValueWidth(panelWidth int) int {
	const panelChrome = 6   // border + horizontal padding
	const rowChrome = 2 + 2 // bullet + space, label/value gap
	w := panelWidth - panelChrome - rowChrome - labelWidth()
	if w < 10 {
		return 10
	}
	return w
}

// normalBullet returns the dim bullet used for unfocused rows.
func normalBullet() string {
	return lipgloss.NewStyle().Foreground(ColFaint).Render("·")
}

// focusedBullet returns the highlighted bullet used for the focused row.
func focusedBullet() string {
	return lipgloss.NewStyle().Foreground(ColPink).Bold(true).Render("▸")
}
