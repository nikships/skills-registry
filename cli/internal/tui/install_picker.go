package tui

import (
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ────────────────────────────────────────────────────────────────────────────
// InstallPickerModel — the embedded agent multi-select used by the list /
// manage TUI and the add flow when picking which agent dot-folders should
// receive a durable install of a registry skill.
//
// Mirrors MultiSelectModel's locked-section + fuzzy-filter pattern but
// drives its lifecycle via Done()/Cancelled() flags instead of tea.Quit
// so the parent state machine (ListModel, AddFlowModel) can stay in the
// alt-screen program after the user confirms.
// ────────────────────────────────────────────────────────────────────────────

// InstallTarget is one row in the install agent picker. The shape
// matches WizardAgent so cmd-side callers can populate both pickers
// from the same agents.All() source without redefining the row.
type InstallTarget struct {
	Display string // user-facing label, e.g. "Claude Code"
	Hint    string // dim text after the label, e.g. ".claude/skills"
	Locked  bool   // always selected; can't be toggled off
	Default bool   // pre-checked when the picker opens
	Value   any    // opaque, returned via SelectedValues()
}

// InstallPickerModel runs a fuzzy-filterable multi-select picker as
// an embedded sub-state. Unlike MultiSelectModel it never returns
// tea.Quit; the parent reads Done()/Cancelled() to advance.
//
// When used as a standalone Bubble Tea program (`tea.NewProgram`),
// pass standalone=true so Init returns nil and Update returns tea.Quit
// once Done()/Cancelled() flips. The embedded path used by ListModel
// constructs the picker with NewInstallPicker (standalone=false).
type InstallPickerModel struct {
	title    string
	subtitle string
	targets  []InstallTarget
	selected map[int]struct{}
	cursor   int
	filter   string

	done       bool
	cancelled  bool
	standalone bool
}

// AsStandalone returns a copy of the picker that quits Bubble Tea after
// Enter / Esc. Use only when the caller hosts the picker inside its
// own tea.NewProgram — the embedded use site (ListModel) leaves
// standalone=false so the parent state machine stays in control.
func (m InstallPickerModel) AsStandalone() InstallPickerModel {
	m.standalone = true
	return m
}

// Init implements tea.Model. Returns nil; standalone callers expect
// the picker to render immediately without a kick-off command.
func (m InstallPickerModel) Init() tea.Cmd { return nil }

// NewInstallPicker builds a picker. `subtitle` is shown beneath the
// title (e.g. the skill name). Locked targets are always included;
// Default targets start pre-checked.
func NewInstallPicker(title, subtitle string, targets []InstallTarget) InstallPickerModel {
	selected := map[int]struct{}{}
	for i, t := range targets {
		if t.Locked {
			continue
		}
		if t.Default {
			selected[i] = struct{}{}
		}
	}
	return InstallPickerModel{
		title:    title,
		subtitle: subtitle,
		targets:  targets,
		selected: selected,
	}
}

// Reset clears the picker's transient state so a second invocation
// (e.g. after a previous install completed) starts fresh.
func (m InstallPickerModel) Reset() InstallPickerModel {
	fresh := NewInstallPicker(m.title, m.subtitle, m.targets)
	fresh.standalone = m.standalone
	return fresh
}

// Done reports whether the user pressed Enter to confirm a selection.
func (m InstallPickerModel) Done() bool { return m.done }

// Cancelled reports whether the user pressed Esc / Ctrl-C to abort.
func (m InstallPickerModel) Cancelled() bool { return m.cancelled }

// SelectedValues returns the opaque values of every locked + user-
// toggled target, in the original ordering.
func (m InstallPickerModel) SelectedValues() []any {
	out := make([]any, 0, len(m.targets))
	for i, t := range m.targets {
		if t.Locked {
			out = append(out, t.Value)
			continue
		}
		if _, ok := m.selected[i]; ok {
			out = append(out, t.Value)
		}
	}
	return out
}

// Update implements tea.Model. The embedded path (ListModel) routes a
// pre-typed tea.KeyMsg here and casts the returned tea.Model back to
// InstallPickerModel; the standalone path uses tea.NewProgram which
// dispatches every message (WindowSize, mouse, …) through this entry
// point. The picker only reacts to key presses — all other messages
// pass through untouched.
func (m InstallPickerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	next := m.handleKey(key)
	if next.standalone && (next.done || next.cancelled) {
		return next, tea.Quit
	}
	return next, nil
}

// handleKey runs the key-driven state machine. Returned by value so
// callers can chain (e.g. m = m.handleKey(k)) without worrying about
// pointer aliasing.
func (m InstallPickerModel) handleKey(msg tea.KeyMsg) InstallPickerModel {
	filtered := m.filteredIndices()
	switch msg.String() {
	case "ctrl+c", "esc":
		m.cancelled = true
		return m
	case "enter":
		// At least one locked target is always included, so the
		// "must pick something" guard from MultiSelectModel is
		// unnecessary here.
		m.done = true
		return m
	case "up":
		if m.cursor > 0 {
			m.cursor--
		}
		return m
	case "down":
		if m.cursor < len(filtered)-1 {
			m.cursor++
		}
		return m
	case " ":
		return m.toggleCursor(filtered)
	case "tab":
		for _, idx := range filtered {
			m.selected[idx] = struct{}{}
		}
		return m
	case "backspace":
		if len(m.filter) > 0 {
			runes := []rune(m.filter)
			m.filter = string(runes[:len(runes)-1])
			m.cursor = 0
		}
		return m
	}
	if msg.Type == tea.KeyRunes && len(msg.String()) == 1 {
		m.filter += msg.String()
		m.cursor = 0
	}
	return m
}

func (m InstallPickerModel) toggleCursor(filtered []int) InstallPickerModel {
	if len(filtered) == 0 || m.cursor >= len(filtered) {
		return m
	}
	idx := filtered[m.cursor]
	if _, ok := m.selected[idx]; ok {
		delete(m.selected, idx)
	} else {
		m.selected[idx] = struct{}{}
	}
	return m
}

func (m InstallPickerModel) filteredIndices() []int {
	out := make([]int, 0, len(m.targets))
	lower := strings.ToLower(m.filter)
	for i, t := range m.targets {
		if t.Locked {
			continue
		}
		if lower == "" || strings.Contains(strings.ToLower(t.Display+" "+t.Hint), lower) {
			out = append(out, i)
		}
	}
	return out
}

// View renders the picker as a self-contained block. The caller is
// responsible for wrapping it in an overlay frame.
func (m InstallPickerModel) View() string {
	var b strings.Builder
	b.WriteString(TitleStyle.Render(m.title))
	if m.subtitle != "" {
		b.WriteString("  ")
		b.WriteString(SubtitleStyle.Render(m.subtitle))
	}
	b.WriteString("\n")
	b.WriteString(HintStyle.Render(
		"type to filter · space toggles · tab selects all · enter installs · esc cancels"))
	b.WriteString("\n\n")

	m.renderLocked(&b)

	b.WriteString(lipgloss.NewStyle().Bold(true).Render("Search: "))
	if m.filter == "" {
		b.WriteString(HintStyle.Render("(type to filter)"))
	} else {
		b.WriteString(m.filter)
	}
	b.WriteString("\n\n")

	filtered := m.filteredIndices()
	const maxVisible = 8
	if len(filtered) == 0 {
		b.WriteString(HintStyle.Render("  (no matches)\n"))
	} else {
		start, end := windowAround(m.cursor, len(filtered), maxVisible)
		for i := start; i < end; i++ {
			idx := filtered[i]
			t := m.targets[idx]
			_, picked := m.selected[idx]
			indicator := "○"
			if picked {
				indicator = "●"
			}
			line := indicator + " " + t.Display
			if t.Hint != "" {
				line += "  " + HintStyle.Render(t.Hint)
			}
			if i == m.cursor {
				b.WriteString(CursorStyle.Render("❯ "))
				b.WriteString(SelectedStyle.Render(line))
			} else {
				b.WriteString("  ")
				if picked {
					b.WriteString(SelectedStyle.Render(line))
				} else {
					b.WriteString(line)
				}
			}
			b.WriteString("\n")
		}
		if start > 0 || end < len(filtered) {
			b.WriteString(HintStyle.Render("  …\n"))
		}
	}

	b.WriteString("\n")
	b.WriteString(SubtitleStyle.Render("Selected: "))
	selected := m.SelectedValues()
	if len(selected) == 0 {
		b.WriteString(HintStyle.Render("(none)"))
	} else {
		b.WriteString(OkStyle.Render(m.formatSelectedLabels(selected, 4)))
	}
	b.WriteString("\n")
	return b.String()
}

func (m InstallPickerModel) renderLocked(b *strings.Builder) {
	hasLocked := false
	for _, t := range m.targets {
		if !t.Locked {
			continue
		}
		if !hasLocked {
			b.WriteString(SubtitleStyle.Render("Always installed:"))
			b.WriteString("\n")
			hasLocked = true
		}
		b.WriteString(SelectedStyle.Render("  ✓ " + t.Display))
		if t.Hint != "" {
			b.WriteString(HintStyle.Render("  " + t.Hint))
		}
		b.WriteString("\n")
	}
	if hasLocked {
		b.WriteString("\n")
	}
}

func (m InstallPickerModel) formatSelectedLabels(values []any, maxShown int) string {
	labels := make([]string, 0, len(values))
	for _, v := range values {
		for _, t := range m.targets {
			if t.Value == v {
				labels = append(labels, t.Display)
				break
			}
		}
	}
	if len(labels) <= maxShown {
		return strings.Join(labels, ", ")
	}
	more := len(labels) - maxShown
	return strings.Join(labels[:maxShown], ", ") + " · +" + strconv.Itoa(more) + " more"
}
