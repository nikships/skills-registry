package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// HubCard is one tile in the dashboard's card grid.
//
// ID is an opaque identifier consumed by the launcher (HubModel.Selection
// returns the ID of whichever card the user chose); Icon, Title, and
// Description are user-visible.
type HubCard struct {
	ID          string
	Icon        string
	Title       string
	Description string
}

// CardGrid is a responsive card layout. Column count adapts to the outer
// width: 3 at ≥120, 2 at ≥80, 1 below. Focus walks with the arrow / hjkl
// keys and wraps at the edges so a held key cycles cleanly.
//
// The grid is pure data — it owns no goroutines or tea.Cmds. HubModel
// folds keystrokes into Move() and renders the result via Render().
type CardGrid struct {
	Cards   []HubCard
	Focused int
}

// NewCardGrid builds a grid with focus pinned at index 0.
func NewCardGrid(cards []HubCard) CardGrid {
	return CardGrid{Cards: cards}
}

// Cols picks the column count for the given outer width. The thresholds
// match the spec: 3 columns at ≥120, 2 at ≥80, 1 otherwise.
func (g CardGrid) Cols(width int) int {
	switch {
	case width >= 120:
		return 3
	case width >= 80:
		return 2
	default:
		return 1
	}
}

// Move walks the focus in the given direction at the supplied column count.
// Vertical moves wrap top↔bottom; horizontal moves wrap left↔right.
// Returns a value-receiver copy so callers stay aligned with Bubble Tea's
// value-semantics convention.
func (g CardGrid) Move(dir string, cols int) CardGrid {
	if len(g.Cards) == 0 || cols <= 0 {
		return g
	}
	rows := (len(g.Cards) + cols - 1) / cols
	row := g.Focused / cols
	col := g.Focused % cols
	switch dir {
	case "left":
		col = (col - 1 + cols) % cols
	case "right":
		col = (col + 1) % cols
	case "up":
		row = (row - 1 + rows) % rows
	case "down":
		row = (row + 1) % rows
	}
	g.Focused = clampToCard(row, col, cols, dir, len(g.Cards))
	return g
}

// clampToCard maps the (row, col) into a valid card index. The last row
// may have empty cells when len(cards)%cols != 0; horizontal wrap into an
// empty cell collapses to a valid neighbour so the user never sees a
// "stuck" focus.
func clampToCard(row, col, cols int, dir string, total int) int {
	candidate := row*cols + col
	if candidate < total {
		return candidate
	}
	switch dir {
	case "right":
		// Wrapping past the last filled cell on the bottom row → jump to
		// the first card on that row.
		return row * cols
	case "left":
		// Wrapping left out of an empty cell → land on the last filled card.
		return total - 1
	case "down":
		// Vertical wrap landed in an empty bottom-row cell → drop to the
		// card directly above (same column).
		return (row-1)*cols + col
	default:
		return total - 1
	}
}

// Selected returns the focused card, or a zero-value HubCard when the grid
// is empty. Empty grids are not expected at runtime but a defensive zero
// keeps test setups simple.
func (g CardGrid) Selected() HubCard {
	if len(g.Cards) == 0 {
		return HubCard{}
	}
	return g.Cards[g.Focused]
}

// Render paints the grid at the supplied outer width. Cards are sized so
// the row fits the budget after accounting for the per-card border and a
// fixed inter-card gap.
func (g CardGrid) Render(width int) string {
	if len(g.Cards) == 0 {
		return ""
	}
	cols := g.Cols(width)
	cardWidth := cardWidthFor(width, cols)
	rendered := make([]string, len(g.Cards))
	for i, c := range g.Cards {
		rendered[i] = g.renderCard(c, i == g.Focused, cardWidth)
	}
	return joinRows(rendered, cols)
}

// cardWidthFor computes the per-card content width such that
// cols*cardWidth + (cols-1)*gap + cols*cardChrome ≤ outerWidth.
// cardChrome is the 4 cols a rounded-border + 1-col padding panel adds.
func cardWidthFor(outerWidth, cols int) int {
	const cardChrome = 4
	const gap = 2
	if cols <= 0 {
		cols = 1
	}
	available := outerWidth - (cols-1)*gap
	w := available/cols - cardChrome
	if w < 18 {
		w = 18
	}
	return w
}

// joinRows splits the rendered card strings into rows of `cols` items and
// joins each row horizontally (with a small inter-card gap). Final
// incomplete rows render left-aligned without padding.
func joinRows(rendered []string, cols int) string {
	const gap = 2
	gapStr := strings.Repeat(" ", gap)
	rows := make([]string, 0, (len(rendered)+cols-1)/cols)
	for i := 0; i < len(rendered); i += cols {
		end := i + cols
		if end > len(rendered) {
			end = len(rendered)
		}
		row := rendered[i:end]
		rows = append(rows, joinWithGap(row, gapStr))
	}
	out := make([]string, 0, len(rows)*2-1)
	for i, r := range rows {
		if i > 0 {
			out = append(out, "")
		}
		out = append(out, r)
	}
	return lipgloss.JoinVertical(lipgloss.Left, out...)
}

// joinWithGap interleaves the cards in a row with the supplied gap string.
func joinWithGap(cards []string, gap string) string {
	if len(cards) == 0 {
		return ""
	}
	parts := make([]string, 0, len(cards)*2-1)
	for i, c := range cards {
		if i > 0 {
			parts = append(parts, gap)
		}
		parts = append(parts, c)
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, parts...)
}

// renderCard renders a single tile. Focused cards use PanelFocused for the
// brighter ColBorderHi border and lift the description colour from
// ColMuted to ColInk so the focused tile reads at a glance.
func (g CardGrid) renderCard(c HubCard, focused bool, width int) string {
	panel := PanelStyle
	titleStyle := lipgloss.NewStyle().Foreground(ColPrimary).Bold(true)
	descStyle := lipgloss.NewStyle().Foreground(ColMuted)
	iconStyle := lipgloss.NewStyle().Foreground(ColPink).Bold(true)
	if focused {
		panel = PanelFocused
		descStyle = lipgloss.NewStyle().Foreground(ColInk)
		iconStyle = lipgloss.NewStyle().Foreground(ColPeach).Bold(true)
	}
	head := lipgloss.JoinHorizontal(lipgloss.Top,
		iconStyle.Render(c.Icon),
		"  ",
		titleStyle.Render(c.Title),
	)
	descWidth := width - 2
	if descWidth < 8 {
		descWidth = 8
	}
	desc := descStyle.Width(descWidth).Render(c.Description)
	chip := ""
	if focused {
		chip = ChipPrimary.Render("◆ focused")
	} else {
		chip = lipgloss.NewStyle().Foreground(ColFaint).Render("◇")
	}
	body := lipgloss.JoinVertical(lipgloss.Left, head, "", desc, "", chip)
	return panel.Width(width).Render(body)
}
