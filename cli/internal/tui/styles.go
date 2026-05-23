// Package tui contains reusable Bubble Tea components for the skill-registry CLI.
package tui

import "github.com/charmbracelet/lipgloss"

// Shared lip-gloss styles. Defined once so the whole CLI feels coherent.
var (
	Primary = lipgloss.Color("#7D56F4")
	Accent  = lipgloss.Color("#43BF6D")
	Muted   = lipgloss.Color("241")
	Danger  = lipgloss.Color("196")

	TitleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(Primary)
	SubtitleStyle = lipgloss.NewStyle().
			Foreground(Muted).
			Italic(true)
	CursorStyle = lipgloss.NewStyle().
			Foreground(Primary).
			Bold(true)
	SelectedStyle = lipgloss.NewStyle().
			Foreground(Accent).
			Bold(true)
	HintStyle = lipgloss.NewStyle().
			Foreground(Muted)
	ErrorStyle = lipgloss.NewStyle().
			Foreground(Danger).
			Bold(true)
	OkStyle = lipgloss.NewStyle().
		Foreground(Accent).
		Bold(true)
)
