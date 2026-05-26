// Package tui contains reusable Bubble Tea components for the skills-registry CLI.
package tui

import "github.com/charmbracelet/lipgloss"

// Legacy plain colors — kept for backwards compatibility with the other
// TUI prompts (confirm/input/multiselect) that already reference them.
var (
	Primary = lipgloss.Color("#E10600")
	Accent  = lipgloss.Color("#B00020")
	Muted   = lipgloss.Color("241")
	Danger  = lipgloss.Color("196")
)

// Adaptive red/black palette. Each color renders well on both light and dark
// terminals — Charm's AdaptiveColor picks the right side at render time.
var (
	ColPrimary  = lipgloss.AdaptiveColor{Light: "#C1121F", Dark: "#FF4D4D"}
	ColAccent   = lipgloss.AdaptiveColor{Light: "#780000", Dark: "#FF1E1E"}
	ColPink     = lipgloss.AdaptiveColor{Light: "#A4161A", Dark: "#FF6B6B"}
	ColPeach    = lipgloss.AdaptiveColor{Light: "#660708", Dark: "#D90429"}
	ColCyan     = lipgloss.AdaptiveColor{Light: "#1A1A1A", Dark: "#8D0801"}
	ColYellow   = lipgloss.AdaptiveColor{Light: "#2B2B2B", Dark: "#FF8A80"}
	ColMuted    = lipgloss.AdaptiveColor{Light: "240", Dark: "245"}
	ColFaint    = lipgloss.AdaptiveColor{Light: "245", Dark: "240"}
	ColBorder   = lipgloss.AdaptiveColor{Light: "#161A1D", Dark: "#660708"}
	ColBorderHi = lipgloss.AdaptiveColor{Light: "#E10600", Dark: "#FF4D4D"}
	ColDanger   = lipgloss.AdaptiveColor{Light: "#BA181B", Dark: "#FF6B6B"}
	ColInk      = lipgloss.AdaptiveColor{Light: "#0B090A", Dark: "#F5F3F4"}
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
			Foreground(lipgloss.Color("#0B090A")).
			Background(ColPrimary).
			Padding(0, 1)

	ChipAccent = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#0B090A")).
			Background(ColAccent).
			Padding(0, 1)

	ChipPeach = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#0B090A")).
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
			Foreground(lipgloss.Color("#0B090A")).
			Background(ColPrimary).
			Padding(0, 1)
)
