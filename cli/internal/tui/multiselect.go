package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// MultiSelectItem is one row in the multi-select.
type MultiSelectItem struct {
	Value  any    // opaque value the caller cares about
	Label  string // display label
	Hint   string // dim text after the label
	Group  string // optional group label; entries with the same group cluster
	Locked bool   // locked items are always selected; can't toggle off
}

// MultiSelectModel renders a fuzzy-filterable multi-select prompt.
//
// Borrows the locked-section pattern from
// ~/dsg-skills/packages/cli/src/prompts/search-multiselect.ts: items with
// Locked=true are shown at the top, always selected, and excluded from the
// filterable list below.
type MultiSelectModel struct {
	Title      string
	Items      []MultiSelectItem
	selected   map[int]struct{} // keys are indices into filtered() of non-locked items
	cursor     int
	filter     string
	width      int
	height     int
	cancelled  bool
	required   bool
	maxVisible int
}

// NewMultiSelect builds the model. `defaultSelected` is a set of values that
// start checked (locked items are always checked regardless).
func NewMultiSelect(title string, items []MultiSelectItem, defaultSelected []any, required bool) MultiSelectModel {
	selected := map[int]struct{}{}
	defaults := map[any]struct{}{}
	for _, v := range defaultSelected {
		defaults[v] = struct{}{}
	}
	for i, it := range items {
		if it.Locked {
			continue
		}
		if _, ok := defaults[it.Value]; ok {
			selected[i] = struct{}{}
		}
	}
	return MultiSelectModel{
		Title:      title,
		Items:      items,
		selected:   selected,
		required:   required,
		maxVisible: 8,
	}
}

// Init implements tea.Model.
func (m MultiSelectModel) Init() tea.Cmd { return nil }

// SelectedValues returns the values picked by the user. Locked items are
// always included.
func (m MultiSelectModel) SelectedValues() []any {
	var out []any
	for i, it := range m.Items {
		if it.Locked {
			out = append(out, it.Value)
			continue
		}
		if _, ok := m.selected[i]; ok {
			out = append(out, it.Value)
		}
	}
	return out
}

// Cancelled reports whether the user pressed Esc / Ctrl-C.
func (m MultiSelectModel) Cancelled() bool { return m.cancelled }

// Update implements tea.Model.
func (m MultiSelectModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil
	case tea.KeyMsg:
		filtered := m.filteredIndices()
		switch msg.String() {
		case "ctrl+c", "esc":
			m.cancelled = true
			return m, tea.Quit
		case "enter":
			if m.required && len(m.SelectedValues()) == 0 {
				return m, nil
			}
			return m, tea.Quit
		case "up":
			if m.cursor > 0 {
				m.cursor--
			}
			return m, nil
		case "down":
			if m.cursor < len(filtered)-1 {
				m.cursor++
			}
			return m, nil
		case " ":
			if len(filtered) == 0 {
				return m, nil
			}
			idx := filtered[m.cursor]
			if _, ok := m.selected[idx]; ok {
				delete(m.selected, idx)
			} else {
				m.selected[idx] = struct{}{}
			}
			return m, nil
		case "backspace":
			if len(m.filter) > 0 {
				m.filter = m.filter[:len(m.filter)-1]
				m.cursor = 0
			}
			return m, nil
		case "tab":
			// Select all filtered.
			for _, idx := range filtered {
				m.selected[idx] = struct{}{}
			}
			return m, nil
		}
		if len(msg.String()) == 1 && msg.Type == tea.KeyRunes {
			m.filter += msg.String()
			m.cursor = 0
		}
	}
	return m, nil
}

func (m MultiSelectModel) filteredIndices() []int {
	var out []int
	lower := strings.ToLower(m.filter)
	for i, it := range m.Items {
		if it.Locked {
			continue
		}
		if lower == "" || strings.Contains(strings.ToLower(it.Label+" "+it.Hint), lower) {
			out = append(out, i)
		}
	}
	return out
}

// View implements tea.Model.
func (m MultiSelectModel) View() string {
	var b strings.Builder
	b.WriteString(TitleStyle.Render(m.Title))
	b.WriteString("\n")
	b.WriteString(HintStyle.Render("type to filter · space toggles · tab selects all · enter confirms · esc cancels"))
	b.WriteString("\n\n")

	// Locked section
	hasLocked := false
	for _, it := range m.Items {
		if it.Locked {
			if !hasLocked {
				b.WriteString(SubtitleStyle.Render("Always included:"))
				b.WriteString("\n")
				hasLocked = true
			}
			b.WriteString(SelectedStyle.Render("  ✓ " + it.Label))
			if it.Hint != "" {
				b.WriteString(HintStyle.Render("  " + it.Hint))
			}
			b.WriteString("\n")
		}
	}
	if hasLocked {
		b.WriteString("\n")
	}

	b.WriteString(lipgloss.NewStyle().Bold(true).Render("Search: "))
	if m.filter == "" {
		b.WriteString(HintStyle.Render("(type to filter)"))
	} else {
		b.WriteString(m.filter)
	}
	b.WriteString("\n\n")

	filtered := m.filteredIndices()
	if len(filtered) == 0 {
		b.WriteString(HintStyle.Render("  (no matches)\n"))
	} else {
		start, end := windowAround(m.cursor, len(filtered), m.maxVisible)
		for i := start; i < end; i++ {
			idx := filtered[i]
			it := m.Items[idx]
			_, picked := m.selected[idx]
			indicator := "○"
			if picked {
				indicator = "●"
			}
			line := indicator + " " + it.Label
			if it.Hint != "" {
				line += "  " + HintStyle.Render(it.Hint)
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

	selected := m.SelectedValues()
	b.WriteString("\n")
	b.WriteString(SubtitleStyle.Render("Selected: "))
	if len(selected) == 0 {
		b.WriteString(HintStyle.Render("(none)"))
	} else {
		b.WriteString(OkStyle.Render(joinLabels(m.Items, selected, 4)))
	}
	b.WriteString("\n")
	return b.String()
}

func windowAround(cursor, total, max int) (int, int) {
	if total <= max {
		return 0, total
	}
	half := max / 2
	start := cursor - half
	if start < 0 {
		start = 0
	}
	end := start + max
	if end > total {
		end = total
		start = end - max
	}
	return start, end
}

func joinLabels(items []MultiSelectItem, values []any, maxShown int) string {
	labels := make([]string, 0, len(values))
	for _, v := range values {
		for _, it := range items {
			if it.Value == v {
				labels = append(labels, it.Label)
				break
			}
		}
	}
	if len(labels) <= maxShown {
		return strings.Join(labels, ", ")
	}
	more := len(labels) - maxShown
	return strings.Join(labels[:maxShown], ", ") + " +" + itoa(more) + " more"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	if neg {
		return "-" + string(digits)
	}
	return string(digits)
}
