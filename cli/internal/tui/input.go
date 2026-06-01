package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// InputModel wraps bubbles/textinput with a title and validation hook.
type InputModel struct {
	Title     string
	Prompt    string
	Help      string
	Input     textinput.Model
	Validate  func(string) error
	err       error
	cancelled bool
}

// NewInput builds an input model with sensible defaults.
func NewInput(title, prompt, placeholder, initial string) InputModel {
	ti := textinput.New()
	ti.Placeholder = placeholder
	ti.SetValue(initial)
	ti.Focus()
	ti.Prompt = "> "
	ti.PromptStyle = CursorStyle
	return InputModel{
		Title:  title,
		Prompt: prompt,
		Input:  ti,
	}
}

// Init implements tea.Model.
func (m InputModel) Init() tea.Cmd { return textinput.Blink }

// Update implements tea.Model.
func (m InputModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "ctrl+c", "esc":
			m.cancelled = true
			return m, tea.Quit
		case "enter":
			val := strings.TrimSpace(m.Input.Value())
			if m.Validate != nil {
				if err := m.Validate(val); err != nil {
					m.err = err
					return m, nil
				}
			}
			return m, tea.Quit
		}
	}
	var cmd tea.Cmd
	m.Input, cmd = m.Input.Update(msg)
	m.err = nil
	return m, cmd
}

// View implements tea.Model.
func (m InputModel) View() string {
	var b strings.Builder
	b.WriteString(TitleStyle.Render(m.Title))
	b.WriteString("\n")
	if m.Prompt != "" {
		b.WriteString(m.Prompt)
		b.WriteString("\n")
	}
	b.WriteString(m.Input.View())
	b.WriteString("\n")
	if m.err != nil {
		b.WriteString(ErrorStyle.Render(m.err.Error()))
		b.WriteString("\n")
	}
	if m.Help != "" {
		b.WriteString(HintStyle.Render(m.Help))
		b.WriteString("\n")
	}
	return b.String()
}

// Value returns the trimmed input value.
func (m InputModel) Value() string { return strings.TrimSpace(m.Input.Value()) }

// Cancelled reports whether the user pressed esc.
func (m InputModel) Cancelled() bool { return m.cancelled }
