package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// ChoiceModel renders a single-select prompt (e.g. private/public, yes/no).
type ChoiceModel struct {
	Title     string
	Prompt    string
	Choices   []Choice
	cursor    int
	submitted bool
	cancelled bool
}

// Choice is one option in a ChoiceModel.
type Choice struct {
	Value any
	Label string
	Hint  string
}

// NewChoice builds the model with the first choice highlighted.
func NewChoice(title, prompt string, choices []Choice) ChoiceModel {
	return ChoiceModel{Title: title, Prompt: prompt, Choices: choices}
}

// Init implements tea.Model.
func (m ChoiceModel) Init() tea.Cmd { return nil }

// Update implements tea.Model.
func (m ChoiceModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "esc":
			m.cancelled = true
			return m, tea.Quit
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.Choices)-1 {
				m.cursor++
			}
		case "enter":
			m.submitted = true
			return m, tea.Quit
		}
	}
	return m, nil
}

// View implements tea.Model.
func (m ChoiceModel) View() string {
	var b strings.Builder
	b.WriteString(TitleStyle.Render(m.Title))
	b.WriteString("\n")
	if m.Prompt != "" {
		b.WriteString(m.Prompt)
		b.WriteString("\n\n")
	}
	for i, ch := range m.Choices {
		marker := "  "
		label := ch.Label
		if i == m.cursor {
			marker = CursorStyle.Render("❯ ")
			label = SelectedStyle.Render(label)
		}
		b.WriteString(marker + label)
		if ch.Hint != "" {
			b.WriteString("  " + HintStyle.Render(ch.Hint))
		}
		b.WriteString("\n")
	}
	b.WriteString("\n")
	b.WriteString(HintStyle.Render("↑/↓ move · enter confirm · esc cancel"))
	return b.String()
}

// Value returns the chosen value, or nil if cancelled.
func (m ChoiceModel) Value() any {
	if !m.submitted || m.cursor < 0 || m.cursor >= len(m.Choices) {
		return nil
	}
	return m.Choices[m.cursor].Value
}

// Cancelled reports whether the user pressed esc.
func (m ChoiceModel) Cancelled() bool { return m.cancelled }
