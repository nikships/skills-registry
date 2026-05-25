// Package tui contains reusable Bubble Tea components for the skills-registry CLI.
package tui

import "github.com/charmbracelet/lipgloss"

// Legacy plain colors — kept for backwards compatibility with the other
// TUI prompts (confirm/input/multiselect) that already reference them.
var (
	Primary = lipgloss.Color("#7D56F4")
	Accent  = lipgloss.Color("#43BF6D")
	Muted   = lipgloss.Color("241")
	Danger  = lipgloss.Color("196")
)

// Adaptive palette. Each color renders well on both light and dark
// terminals — Charm's AdaptiveColor picks the right side at render time.
var (
	ColPrimary  = lipgloss.AdaptiveColor{Light: "#5A2EE5", Dark: "#B8A6FF"}
	ColAccent   = lipgloss.AdaptiveColor{Light: "#0F8F5A", Dark: "#7CFFB0"}
	ColPink     = lipgloss.AdaptiveColor{Light: "#D6336C", Dark: "#FF6FB5"}
	ColPeach    = lipgloss.AdaptiveColor{Light: "#C44A1A", Dark: "#FFB57A"}
	ColCyan     = lipgloss.AdaptiveColor{Light: "#0E7C9E", Dark: "#7FE7FF"}
	ColYellow   = lipgloss.AdaptiveColor{Light: "#B58300", Dark: "#FFE066"}
	ColMuted    = lipgloss.AdaptiveColor{Light: "240", Dark: "245"}
	ColFaint    = lipgloss.AdaptiveColor{Light: "245", Dark: "240"}
	ColBorder   = lipgloss.AdaptiveColor{Light: "#C6B8FF", Dark: "#5C3FAA"}
	ColBorderHi = lipgloss.AdaptiveColor{Light: "#7D56F4", Dark: "#B8A6FF"}
	ColDanger   = lipgloss.AdaptiveColor{Light: "#C92A2A", Dark: "#FF6B6B"}
	ColInk      = lipgloss.AdaptiveColor{Light: "#1E1B30", Dark: "#F5F1FF"}
)

// Shared lip-gloss styles. Defined once so the whole CLI feels coherent.
var (
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

// Rich styles used by the list view.
var (
	HeroStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(ColPrimary).
			Padding(0, 1)

	SparkleStyle = lipgloss.NewStyle().
			Foreground(ColPink).
			Bold(true)

	ChipPrimary = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FFFDF5")).
			Background(ColPrimary).
			Padding(0, 1)

	ChipAccent = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#0A1F12")).
			Background(ColAccent).
			Padding(0, 1)

	ChipPeach = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#1E120A")).
			Background(ColPeach).
			Padding(0, 1)

	ChipMuted = lipgloss.NewStyle().
			Foreground(ColInk).
			Background(ColFaint).
			Padding(0, 1)

	PanelStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(ColBorder).
			Padding(0, 1)

	PanelFocused = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(ColBorderHi).
			Padding(0, 1)

	PreviewTitle = lipgloss.NewStyle().
			Bold(true).
			Foreground(ColAccent)

	PreviewSlug = lipgloss.NewStyle().
			Foreground(ColPeach).
			Italic(true)

	PreviewBody = lipgloss.NewStyle().
			Foreground(ColInk)

	PreviewMeta = lipgloss.NewStyle().
			Foreground(ColMuted)

	KeyStyle = lipgloss.NewStyle().
			Foreground(ColAccent).
			Bold(true)

	KeyDescStyle = lipgloss.NewStyle().
			Foreground(ColMuted)

	KeySepStyle = lipgloss.NewStyle().
			Foreground(ColFaint)

	HelpOverlay = lipgloss.NewStyle().
			Border(lipgloss.DoubleBorder()).
			BorderForeground(ColPrimary).
			Padding(1, 3).
			Foreground(ColInk).
			Bold(false)

	StatusLine = lipgloss.NewStyle().
			Padding(0, 1)

	EmptyHint = lipgloss.NewStyle().
			Foreground(ColMuted).
			Italic(true).
			Align(lipgloss.Center).
			Padding(2, 4)

	// Chip used for the "press enter to download" CTA in the preview pane.
	// Sits on a muted background so the keycap pops on both light and dark
	// terminals without screaming for attention.
	DownloadChip = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FFFDF5")).
			Background(ColPrimary).
			Padding(0, 1)
)
