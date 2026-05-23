package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// SkillRow is a list.Item suitable for skill enumeration.
type SkillRow struct {
	Slug     string
	Name     string
	Desc     string
}

// Title implements list.Item.
func (s SkillRow) Title() string { return s.Name }

// Description implements list.Item.
func (s SkillRow) Description() string {
	return s.Desc + "  " + HintStyle.Render("("+s.Slug+")")
}

// FilterValue implements list.Item.
func (s SkillRow) FilterValue() string {
	return s.Name + " " + s.Desc + " " + s.Slug
}

// ListModel wraps bubbles/list with our color palette.
type ListModel struct {
	List   list.Model
	Picked *SkillRow
}

// NewList constructs a list of SkillRow items.
func NewList(title string, rows []SkillRow) ListModel {
	items := make([]list.Item, len(rows))
	for i, r := range rows {
		items[i] = r
	}
	delegate := list.NewDefaultDelegate()
	delegate.Styles.SelectedTitle = delegate.Styles.SelectedTitle.
		Foreground(Primary).
		BorderLeftForeground(Primary)
	delegate.Styles.SelectedDesc = delegate.Styles.SelectedDesc.
		Foreground(Accent).
		BorderLeftForeground(Primary)

	l := list.New(items, delegate, 0, 0)
	l.Title = title
	l.Styles.Title = lipgloss.NewStyle().
		Foreground(Primary).
		Bold(true)
	l.SetShowStatusBar(true)
	l.SetFilteringEnabled(true)
	return ListModel{List: l}
}

// Init implements tea.Model.
func (m ListModel) Init() tea.Cmd { return nil }

// Update implements tea.Model.
func (m ListModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		// Leave room for the status bar and instructions.
		m.List.SetSize(msg.Width-2, msg.Height-4)
		return m, nil
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "esc":
			return m, tea.Quit
		case "enter":
			if it, ok := m.List.SelectedItem().(SkillRow); ok {
				cp := it
				m.Picked = &cp
				return m, tea.Quit
			}
		}
	}
	var cmd tea.Cmd
	m.List, cmd = m.List.Update(msg)
	return m, cmd
}

// View implements tea.Model.
func (m ListModel) View() string {
	var b strings.Builder
	b.WriteString(m.List.View())
	b.WriteString("\n")
	b.WriteString(HintStyle.Render("enter to view · esc to quit · / to filter"))
	return b.String()
}
