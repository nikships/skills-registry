package tui

import (
	"strings"
	"testing"
)

// gridFixture returns a CardGrid with six cards that mirrors the
// production DefaultHubCards layout (the F3.1 dashboard ships six
// tiles).
func gridFixture() CardGrid {
	return NewCardGrid([]HubCard{
		{ID: "a", Icon: "1", Title: "Alpha", Description: "first"},
		{ID: "b", Icon: "2", Title: "Bravo", Description: "second"},
		{ID: "c", Icon: "3", Title: "Charlie", Description: "third"},
		{ID: "d", Icon: "4", Title: "Delta", Description: "fourth"},
		{ID: "e", Icon: "5", Title: "Echo", Description: "fifth"},
		{ID: "f", Icon: "6", Title: "Foxtrot", Description: "sixth"},
	})
}

// TestCardGridCols pins down the responsive thresholds called out in the
// F3.1 spec: 3 columns at ≥120, 2 at ≥80, 1 below.
func TestCardGridCols(t *testing.T) {
	g := gridFixture()
	cases := []struct {
		width int
		want  int
	}{
		{40, 1},
		{79, 1},
		{80, 2},
		{119, 2},
		{120, 3},
		{200, 3},
	}
	for _, tc := range cases {
		if got := g.Cols(tc.width); got != tc.want {
			t.Errorf("Cols(%d) = %d, want %d", tc.width, got, tc.want)
		}
	}
}

// TestCardGridMoveRightWraps verifies a horizontal walk across the row
// cycles back to the first column at the right edge so a held arrow
// key keeps producing motion.
func TestCardGridMoveRightWraps(t *testing.T) {
	g := gridFixture()
	g.Focused = 0
	cols := 3
	for i := 0; i < 3; i++ {
		g = g.Move("right", cols)
	}
	if g.Focused != 0 {
		t.Errorf("Focused after 3 rights at cols=3 = %d, want 0 (wrapped)", g.Focused)
	}
}

// TestCardGridMoveLeftWraps mirrors the right-wrap test for the left edge.
func TestCardGridMoveLeftWraps(t *testing.T) {
	g := gridFixture()
	g.Focused = 0
	g = g.Move("left", 3)
	if g.Focused != 2 {
		t.Errorf("Focused after left from 0 at cols=3 = %d, want 2 (wrapped)", g.Focused)
	}
}

// TestCardGridMoveDownWraps verifies vertical wrap. With 6 cards at 3
// cols there are 2 rows; pressing down twice from the top returns to
// the top.
func TestCardGridMoveDownWraps(t *testing.T) {
	g := gridFixture()
	g.Focused = 0
	cols := 3
	g = g.Move("down", cols)
	if g.Focused != 3 {
		t.Errorf("after first down: Focused = %d, want 3", g.Focused)
	}
	g = g.Move("down", cols)
	if g.Focused != 0 {
		t.Errorf("after second down: Focused = %d, want 0 (wrapped)", g.Focused)
	}
}

// TestCardGridMoveUpWraps mirrors the down-wrap test for the top edge.
func TestCardGridMoveUpWraps(t *testing.T) {
	g := gridFixture()
	g.Focused = 0
	g = g.Move("up", 3)
	if g.Focused != 3 {
		t.Errorf("up from row 0 at cols=3 = %d, want 3 (wrapped to bottom row)", g.Focused)
	}
}

// TestCardGridMoveCollapsesEmptyCell guards the irregular-row case: 5
// cards at 2 cols make a 3-row grid where the last row only has one
// cell. Right-wrapping out of the (2,0) → (2,1) slot must land back on
// (2,0) instead of an undefined index.
func TestCardGridMoveCollapsesEmptyCell(t *testing.T) {
	g := NewCardGrid([]HubCard{
		{ID: "1"}, {ID: "2"}, {ID: "3"}, {ID: "4"}, {ID: "5"},
	})
	g.Focused = 4 // bottom-left, last filled cell
	g = g.Move("right", 2)
	if g.Focused != 4 {
		t.Errorf("right from last filled cell on partial row = %d, want 4 (collapsed)", g.Focused)
	}
}

// TestCardGridMoveDownLandsInPartialRow verifies a down-wrap into an
// empty cell drops to the card above so the user never sees focus stuck
// in a non-existent slot.
func TestCardGridMoveDownLandsInPartialRow(t *testing.T) {
	g := NewCardGrid([]HubCard{
		{ID: "1"}, {ID: "2"}, {ID: "3"}, {ID: "4"}, {ID: "5"},
	})
	g.Focused = 1 // (0,1)
	g = g.Move("down", 2)
	if g.Focused != 3 { // (1,1)
		t.Errorf("first down from (0,1) = %d, want 3 (1,1)", g.Focused)
	}
	g = g.Move("down", 2)
	// (2,1) is empty so clampToCard drops to (1,1) = card 3.
	if g.Focused != 3 {
		t.Errorf("second down from (1,1) = %d, want 3 (clamped to (1,1))", g.Focused)
	}
}

// TestCardGridSelectedTracksFocus is the primary read-side contract:
// Selected() must return the card at the current Focused index so the
// hub can resolve which action the user picked.
func TestCardGridSelectedTracksFocus(t *testing.T) {
	g := gridFixture()
	g.Focused = 2
	if got := g.Selected().ID; got != "c" {
		t.Errorf("Selected().ID at Focused=2 = %q, want \"c\"", got)
	}
}

// TestCardGridSelectedEmptyGrid checks the defensive zero-value path so
// a misconfigured launcher doesn't panic.
func TestCardGridSelectedEmptyGrid(t *testing.T) {
	g := NewCardGrid(nil)
	if got := g.Selected(); got.ID != "" || got.Title != "" {
		t.Errorf("empty grid Selected() = %+v, want zero value", got)
	}
}

// TestCardGridRenderEmpty returns an empty string so the surrounding hub
// frame can compose without special-casing.
func TestCardGridRenderEmpty(t *testing.T) {
	if got := NewCardGrid(nil).Render(100); got != "" {
		t.Errorf("Render of empty grid = %q, want \"\"", got)
	}
}

// TestCardGridRenderIncludesAllTitles is a coarse smoke test: every card
// in the grid must surface its title in the rendered output. Hard to
// assert on the exact bytes (lipgloss adds ANSI), so we look for the
// raw substrings inside a width that comfortably holds the full grid.
func TestCardGridRenderIncludesAllTitles(t *testing.T) {
	g := gridFixture()
	out := g.Render(200)
	wants := []string{"Alpha", "Bravo", "Charlie", "Delta", "Echo", "Foxtrot"}
	for _, want := range wants {
		if !strings.Contains(out, want) {
			t.Errorf("Render output missing %q card title:\n%s", want, out)
		}
	}
}

// TestCardGridMoveNoopOnEmpty guards against panics when Move is called
// on an empty grid (e.g. tests that build a fresh CardGrid{} value).
func TestCardGridMoveNoopOnEmpty(t *testing.T) {
	g := NewCardGrid(nil)
	g = g.Move("right", 3)
	if g.Focused != 0 {
		t.Errorf("Move on empty grid changed Focused to %d", g.Focused)
	}
}

// TestCardWidthForRespectsMinimum verifies the per-card width can't shrink
// below a sensible minimum at narrow widths.
func TestCardWidthForRespectsMinimum(t *testing.T) {
	if got := cardWidthFor(40, 3); got < 18 {
		t.Errorf("cardWidthFor(40, 3) = %d, want >= 18 (minimum)", got)
	}
	if got := cardWidthFor(200, 3); got < 30 {
		t.Errorf("cardWidthFor(200, 3) = %d, want >= 30 (room to render)", got)
	}
}
