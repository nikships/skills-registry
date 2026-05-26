package tui

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ansiSeq matches the standard CSI escape sequences lipgloss emits when
// rendering with a color-capable profile (foreground/background colors,
// bold/italic toggles, and the `\x1b[0m` reset). Test assertions on the
// rendered output need to ignore these so they don't trip on terminal
// styling that wraps the visible glyphs.
var ansiSeq = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

// stripANSI removes ANSI CSI escape sequences from s. Used by delegate /
// preview tests so suffix / content assertions are deterministic across
// `go test` environments where lipgloss may or may not emit ANSI codes.
func stripANSI(s string) string { return ansiSeq.ReplaceAllString(s, "") }

// testDownloadDest is the canonical stub dest used by the download-flow
// tests. The literal value is opaque — tests only verify the model echoes
// it back unchanged — but anchoring it on a single const keeps the
// fixture path consistent across cases.
const testDownloadDest = "/tmp/cache/skills-mcp/skills/foo_skill"

// stubDownloader returns a Downloader that records every slug it sees and
// returns the configured dest/err.
type stubDownloader struct {
	calls []string
	dest  string
	err   error
}

func (s *stubDownloader) fn() Downloader {
	return func(_ context.Context, slug string) (string, string, error) {
		s.calls = append(s.calls, slug)
		return s.dest, "", s.err
	}
}

type stubDeleter struct {
	calls []string
	sha   string
	err   error
}

func (s *stubDeleter) fn() Deleter {
	return func(_ context.Context, slug string) (string, error) {
		s.calls = append(s.calls, slug)
		return s.sha, s.err
	}
}

// readyModel returns a ListModel in stateReady with two rows loaded.
func readyModel(t *testing.T, dl Downloader) ListModel {
	t.Helper()
	loader := func() ([]SkillRow, error) {
		return []SkillRow{
			{Slug: "foo_skill", Name: "Foo", Desc: "first"},
			{Slug: "bar_skill", Name: "Bar", Desc: "second"},
		}, nil
	}
	m := NewList(context.Background(), "owner/repo", loader, dl)
	// Skip the loader by injecting the rowsLoadedMsg directly.
	got, _ := m.Update(rowsLoadedMsg{rows: []SkillRow{
		{Slug: "foo_skill", Name: "Foo", Desc: "first"},
		{Slug: "bar_skill", Name: "Bar", Desc: "second"},
	}})
	return got.(ListModel)
}

func enterKey() tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyEnter} }

func deleteKey() tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")} }

func TestEnter_TriggersDownload(t *testing.T) {
	stub := &stubDownloader{dest: testDownloadDest}
	m := readyModel(t, stub.fn())

	got, cmd := m.Update(enterKey())
	mm := got.(ListModel)

	if mm.rowState["foo_skill"] != StatusDownloading {
		t.Fatalf("rowState[foo_skill] = %v, want StatusDownloading", mm.rowState["foo_skill"])
	}
	if mm.inflight != 1 {
		t.Fatalf("inflight = %d, want 1", mm.inflight)
	}
	if cmd == nil {
		t.Fatal("Update returned nil cmd; expected a download tea.Cmd")
	}

	// Executing the command should invoke the downloader and yield a
	// downloadDoneMsg{} with the resolved dest.
	msg := cmd()
	// The cmd is a tea.Batch — drain it until we find the downloadDoneMsg.
	done, ok := drainForDone(msg)
	if !ok {
		t.Fatal("did not get downloadDoneMsg from cmd output")
	}
	if done.slug != "foo_skill" {
		t.Fatalf("done.slug = %q, want foo_skill", done.slug)
	}
	if done.dest != stub.dest {
		t.Fatalf("done.dest = %q, want %q", done.dest, stub.dest)
	}
	if got := len(stub.calls); got != 1 || stub.calls[0] != "foo_skill" {
		t.Fatalf("downloader calls = %v, want [foo_skill]", stub.calls)
	}
}

func TestEnter_IgnoresDoublePressWhileDownloading(t *testing.T) {
	stub := &stubDownloader{dest: testDownloadDest}
	m := readyModel(t, stub.fn())

	// First enter starts the download.
	got, _ := m.Update(enterKey())
	mm := got.(ListModel)

	// Second enter on the same row must be a no-op.
	got2, cmd2 := mm.Update(enterKey())
	mm2 := got2.(ListModel)

	if mm2.inflight != 1 {
		t.Fatalf("inflight after double-press = %d, want 1", mm2.inflight)
	}
	if cmd2 != nil {
		t.Fatalf("expected nil cmd on double-press, got %T", cmd2)
	}
}

// Each row may only be downloaded once per session — pressing enter again
// after the download finished is a no-op, regardless of outcome.
func TestEnter_NoOpAfterTerminalStatus(t *testing.T) {
	for _, st := range []struct {
		name   string
		status RowStatus
	}{
		{"done", StatusDone},
		{"err", StatusErr},
	} {
		t.Run(st.name, func(t *testing.T) {
			stub := &stubDownloader{dest: testDownloadDest}
			m := readyModel(t, stub.fn())
			m.rowState["foo_skill"] = st.status

			got, cmd := m.Update(enterKey())
			mm := got.(ListModel)

			if mm.inflight != 0 {
				t.Fatalf("inflight = %d, want 0", mm.inflight)
			}
			if cmd != nil {
				t.Fatalf("expected nil cmd on enter after %s, got %T", st.name, cmd)
			}
			if mm.rowState["foo_skill"] != st.status {
				t.Fatalf("rowState mutated to %v, want %v", mm.rowState["foo_skill"], st.status)
			}
			if len(stub.calls) != 0 {
				t.Fatalf("downloader was called %d times, want 0", len(stub.calls))
			}
		})
	}
}

func TestDownloadDoneMsg_Success(t *testing.T) {
	stub := &stubDownloader{dest: testDownloadDest}
	m := readyModel(t, stub.fn())
	// Pretend the download is already in flight.
	m.rowState["foo_skill"] = StatusDownloading
	m.inflight = 1

	got, _ := m.Update(downloadDoneMsg{
		slug: "foo_skill",
		dest: testDownloadDest,
	})
	mm := got.(ListModel)

	if mm.rowState["foo_skill"] != StatusDone {
		t.Fatalf("rowState[foo_skill] = %v, want StatusDone", mm.rowState["foo_skill"])
	}
	if mm.inflight != 0 {
		t.Fatalf("inflight = %d, want 0", mm.inflight)
	}
	if !mm.toastOK {
		t.Fatal("toastOK = false, want true on success")
	}
	if !strings.Contains(mm.toast, "Foo") || !strings.Contains(mm.toast, testDownloadDest) {
		t.Fatalf("toast = %q, want it to mention Foo and dest path", mm.toast)
	}
	if mm.rowDest["foo_skill"] != testDownloadDest {
		t.Fatalf("rowDest[foo_skill] = %q, want dest path", mm.rowDest["foo_skill"])
	}
}

func TestDownloadDoneMsg_Error(t *testing.T) {
	stub := &stubDownloader{err: errors.New("boom")}
	m := readyModel(t, stub.fn())
	m.rowState["foo_skill"] = StatusDownloading
	m.inflight = 1

	got, _ := m.Update(downloadDoneMsg{
		slug: "foo_skill",
		err:  errors.New("boom"),
	})
	mm := got.(ListModel)

	if mm.rowState["foo_skill"] != StatusErr {
		t.Fatalf("rowState[foo_skill] = %v, want StatusErr", mm.rowState["foo_skill"])
	}
	if mm.inflight != 0 {
		t.Fatalf("inflight = %d, want 0", mm.inflight)
	}
	if mm.toastOK {
		t.Fatal("toastOK = true, want false on error")
	}
	if !strings.Contains(mm.toast, "Foo") || !strings.Contains(mm.toast, "boom") {
		t.Fatalf("toast = %q, want it to mention Foo and boom", mm.toast)
	}
}

// Multi-line errors (typical of `gh` subprocess failures) must be flattened
// before being placed in the toast — otherwise the body would push the
// footer off-screen.
func TestDownloadDoneMsg_FlattensMultilineError(t *testing.T) {
	stub := &stubDownloader{}
	m := readyModel(t, stub.fn())
	m.rowState["foo_skill"] = StatusDownloading
	m.inflight = 1

	got, _ := m.Update(downloadDoneMsg{
		slug: "foo_skill",
		err:  errors.New("HTTP 404\nNot Found\nhttps://api.github.com/…"),
	})
	mm := got.(ListModel)

	if strings.Contains(mm.toast, "\n") {
		t.Fatalf("toast contains a newline: %q", mm.toast)
	}
	if !strings.Contains(mm.toast, "HTTP 404") || !strings.Contains(mm.toast, "Not Found") {
		t.Fatalf("toast lost error content: %q", mm.toast)
	}
}

func TestEnter_NoOpWithoutDownloader(t *testing.T) {
	m := readyModel(t, nil)
	got, cmd := m.Update(enterKey())
	mm := got.(ListModel)
	if mm.inflight != 0 {
		t.Fatalf("inflight = %d, want 0 (no downloader)", mm.inflight)
	}
	if cmd != nil {
		t.Fatalf("expected nil cmd, got %T", cmd)
	}
	if _, ok := mm.rowState["foo_skill"]; ok {
		t.Fatal("rowState entry should not be created when downloader is nil")
	}
}

func TestDeleteKeyNoOpWithoutDeleter(t *testing.T) {
	m := readyModel(t, nil)
	got, cmd := m.Update(deleteKey())
	mm := got.(ListModel)
	if mm.confirmRemoval {
		t.Fatal("delete key opened confirmation without a deleter")
	}
	if cmd != nil {
		t.Fatalf("expected nil cmd without deleter, got %T", cmd)
	}
}

func TestDeleteKeyOpensConfirmWithCancelDefault(t *testing.T) {
	stub := &stubDeleter{sha: "abcdef123"}
	m := readyModel(t, nil).WithDeleter(stub.fn())
	got, cmd := m.Update(deleteKey())
	mm := got.(ListModel)
	if cmd != nil {
		t.Fatalf("opening confirmation should not start delete, got %T", cmd)
	}
	if !mm.confirmRemoval {
		t.Fatal("delete key did not open remove confirmation")
	}
	if mm.removeCursor != 0 {
		t.Fatalf("removeCursor = %d, want 0 (Cancel default)", mm.removeCursor)
	}
	if !strings.Contains(mm.View(), "Remove foo_skill?") {
		t.Fatalf("remove overlay missing selected slug:\n%s", mm.View())
	}
}

func TestDeleteConfirmCancelDoesNotDelete(t *testing.T) {
	stub := &stubDeleter{sha: "abcdef123"}
	m := readyModel(t, nil).WithDeleter(stub.fn())
	got, _ := m.Update(deleteKey())
	got, cmd := got.(ListModel).Update(enterKey())
	mm := got.(ListModel)
	if mm.confirmRemoval {
		t.Fatal("enter on default Cancel should close confirmation")
	}
	if cmd != nil {
		t.Fatalf("cancel returned cmd %T, want nil", cmd)
	}
	if len(stub.calls) != 0 {
		t.Fatalf("deleter calls = %v, want none", stub.calls)
	}
}

func TestDeleteConfirmSuccessRemovesRow(t *testing.T) {
	stub := &stubDeleter{sha: "abcdef123"}
	m := readyModel(t, nil).WithDeleter(stub.fn())
	got, _ := m.Update(deleteKey())
	got, _ = got.(ListModel).Update(tea.KeyMsg{Type: tea.KeyRight})
	got, cmd := got.(ListModel).Update(enterKey())
	mm := got.(ListModel)
	if mm.rowState["foo_skill"] != StatusRemoving {
		t.Fatalf("rowState[foo_skill] = %v, want StatusRemoving", mm.rowState["foo_skill"])
	}
	if mm.inflight != 1 {
		t.Fatalf("inflight = %d, want 1", mm.inflight)
	}
	if cmd == nil {
		t.Fatal("confirm yes returned nil cmd")
	}
	done, ok := drainForDelete(cmd())
	if !ok {
		t.Fatal("did not get deleteDoneMsg from cmd output")
	}
	if done.slug != "foo_skill" || done.sha != "abcdef123" {
		t.Fatalf("deleteDoneMsg = %+v", done)
	}
	if len(stub.calls) != 1 || stub.calls[0] != "foo_skill" {
		t.Fatalf("deleter calls = %v, want [foo_skill]", stub.calls)
	}

	got, _ = mm.Update(done)
	mm = got.(ListModel)
	if mm.inflight != 0 {
		t.Fatalf("inflight after delete done = %d, want 0", mm.inflight)
	}
	if mm.findRow("foo_skill") != nil {
		t.Fatal("foo_skill row still present after successful delete")
	}
	if !mm.toastOK || !strings.Contains(mm.toast, "removed Foo") {
		t.Fatalf("toast = %q ok=%v, want removed Foo success", mm.toast, mm.toastOK)
	}
}

func TestHelpShowsRemoveOnlyWhenDeleterEnabled(t *testing.T) {
	withoutDelete := readyModel(t, nil).renderHelp()
	if strings.Contains(withoutDelete, "remove selected skill") {
		t.Fatalf("help without deleter advertised remove:\n%s", withoutDelete)
	}

	stub := &stubDeleter{sha: "abcdef123"}
	withDelete := readyModel(t, nil).WithDeleter(stub.fn()).renderHelp()
	if !strings.Contains(withDelete, "remove selected skill") {
		t.Fatalf("help with deleter missing remove:\n%s", withDelete)
	}
}

// TestSlugMatchesName pins the suppression rule used by both the preview
// "slug · …" line and the list-row right column. Anything that's just the
// canonical Slugify of the title is treated as redundant and hidden.
func TestSlugMatchesName(t *testing.T) {
	for _, tc := range []struct {
		name string
		slug string
		want bool
	}{
		{"adaptive", "adaptive", true},
		{"camera1-to-camerax", "camera1_to_camerax", true},
		{"My Skill", "my_skill", true},
		{"agent-platform-skills-registry", "agent_platform_skills_registry", true},
		// Genuinely different — slug was overridden or stored differently.
		{"camera1-to-camerax", "cam1", false},
		// Empty cases.
		{"", "", true},
		{"foo", "", false},
		{"", "foo", false},
	} {
		t.Run(tc.name+"_"+tc.slug, func(t *testing.T) {
			if got := slugMatchesName(tc.slug, tc.name); got != tc.want {
				t.Fatalf("slugMatchesName(%q, %q) = %v, want %v", tc.slug, tc.name, got, tc.want)
			}
		})
	}
}

// drainForDone walks a tea.Msg that may be a Batch / sequence wrapper and
// returns the first downloadDoneMsg found.
func drainForDone(msg tea.Msg) (downloadDoneMsg, bool) {
	switch v := msg.(type) {
	case downloadDoneMsg:
		return v, true
	case tea.BatchMsg:
		for _, c := range v {
			if c == nil {
				continue
			}
			if d, ok := drainForDone(c()); ok {
				return d, true
			}
		}
	}
	return downloadDoneMsg{}, false
}

func drainForDelete(msg tea.Msg) (deleteDoneMsg, bool) {
	switch v := msg.(type) {
	case deleteDoneMsg:
		return v, true
	case tea.BatchMsg:
		for _, c := range v {
			if c == nil {
				continue
			}
			if d, ok := drainForDelete(c()); ok {
				return d, true
			}
		}
	}
	return deleteDoneMsg{}, false
}

// TestTruncatePinsDisplayCellBudget pins the F3.5/F3.3 contract: the
// truncate helper measures the result in lipgloss-reported display cells
// (not raw rune counts) so wide-char input never overflows the budget.
func TestTruncatePinsDisplayCellBudget(t *testing.T) {
	for _, tc := range []struct {
		name  string
		in    string
		n     int
		wantW int
	}{
		{"ascii_short_unchanged", "hello", 10, 5},
		{"ascii_long_truncated", strings.Repeat("ab", 100), 20, 20},
		{"emoji_wide_chars", "🌈 the future is bright", 10, 10},
		{"cjk_double_width", "こんにちは世界の皆様へ", 8, 8},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := truncate(tc.in, tc.n)
			if w := lipgloss.Width(got); w > tc.n {
				t.Errorf("truncate(%q, %d) = %q (width %d), want ≤ %d",
					tc.in, tc.n, got, w, tc.n)
			}
			// When truncation actually happened, the result must end
			// with the ellipsis sentinel.
			if lipgloss.Width(tc.in) > tc.n && !strings.HasSuffix(got, "…") {
				t.Errorf("truncate(%q, %d) = %q, expected … suffix",
					tc.in, tc.n, got)
			}
		})
	}
}

// TestSkillDelegateRenderLongDescription drives skillDelegate.Render
// with a 250-char ASCII description and verifies neither line of the
// rendered row exceeds the supplied list width. Catches regressions
// where the delegate forgot to clamp the description column.
func TestSkillDelegateRenderLongDescription(t *testing.T) {
	desc := strings.Repeat("This skill does many useful things. ", 7) // 252 chars
	row := SkillRow{Slug: "long_desc", Name: "Long Skill", Desc: desc}
	rendered := renderSingleDelegate(t, row, 80)
	for _, line := range strings.Split(rendered, "\n") {
		if w := lipgloss.Width(line); w > 80 {
			t.Errorf("delegate line exceeds width 80: %d cells: %q", w, line)
		}
	}
}

// TestSkillDelegateRenderMultiByteName covers the multi-byte UTF-8
// path: a name composed of emoji + CJK runes must (a) not crash, (b)
// not produce lines wider than the list budget, and (c) preserve the
// rune boundary in any truncation.
func TestSkillDelegateRenderMultiByteName(t *testing.T) {
	row := SkillRow{
		Slug: "wide_chars",
		Name: "🌈 ようこそ to my skill — let's build 世界",
		Desc: "🚀 builds futures · 🪐 spaces · 🌟 stars · 💎 jewels · " +
			strings.Repeat("✨", 50),
	}
	rendered := renderSingleDelegate(t, row, 60)
	for _, line := range strings.Split(rendered, "\n") {
		if w := lipgloss.Width(line); w > 60 {
			t.Errorf("delegate line exceeds width 60: %d cells: %q", w, line)
		}
	}
}

// TestPreviewPanelClampsLongTitle exercises the preview pane with a
// pathologically long title and asserts every rendered row stays within
// the panel width. Mirrors the F3.5 fix that added truncate() calls on
// the title and slug rows.
func TestPreviewPanelClampsLongTitle(t *testing.T) {
	m := buildListWithRow(SkillRow{
		Slug: "long_title_slug",
		Name: strings.Repeat("Looooong-title-segment ", 20),
		Desc: strings.Repeat("payload ", 40),
	})
	rendered := m.renderPreviewPanel()
	for _, line := range strings.Split(rendered, "\n") {
		// Preview panel = m.preview.Width + 4 (border + padding). The
		// inner clamp should keep every text row ≤ m.preview.Width + 4.
		max := m.preview.Width + 4
		if w := lipgloss.Width(line); w > max {
			t.Errorf("preview line exceeds panel width %d: %d cells: %q",
				max, w, line)
		}
	}
}

// TestPreviewPanelMultiByteDescription drives the preview with emoji +
// CJK in the description. Because PreviewBody uses lipgloss soft-wrap,
// the description spans several rows but no individual row may exceed
// the preview width.
func TestPreviewPanelMultiByteDescription(t *testing.T) {
	m := buildListWithRow(SkillRow{
		Slug: "multibyte",
		Name: "Multi-byte 🌍",
		Desc: "Mixes 🌈 and 世界 and " + strings.Repeat("é", 220),
	})
	rendered := m.renderPreviewPanel()
	for _, line := range strings.Split(rendered, "\n") {
		max := m.preview.Width + 4
		if w := lipgloss.Width(line); w > max {
			t.Errorf("preview line exceeds panel width %d: %d cells: %q",
				max, w, line)
		}
	}
}

// renderSingleDelegate constructs a list with one item and renders the
// skillDelegate at index 0, returning the captured bytes. The list is
// wired with the same delegate hooks as the production NewList path so
// the test exercises the real skillDelegate.Render.
func renderSingleDelegate(t *testing.T, row SkillRow, width int) string {
	t.Helper()
	l := newDelegateRenderHarness(width)
	l.SetItems([]list.Item{row})
	d := newSkillDelegate(func(string) RowStatus { return StatusIdle })
	var buf strings.Builder
	d.Render(&buf, l, 0, row)
	return buf.String()
}

// newDelegateRenderHarness builds a bubbles list.Model with a non-zero
// width and a no-op delegate so the skillDelegate.Render call has a
// real m.Width() to query.
func newDelegateRenderHarness(width int) list.Model {
	d := newSkillDelegate(func(string) RowStatus { return StatusIdle })
	l := list.New(nil, d, width, 10)
	l.SetSize(width, 10)
	return l
}

// buildListWithRow returns a ListModel sized to a wide-enough terminal
// to exercise the dual-pane preview rendering path, pre-loaded with a
// single row and stateReady so renderPreviewPanel sees a selected row.
func buildListWithRow(row SkillRow) ListModel {
	m := NewList(context.Background(), "owner/repo",
		func() ([]SkillRow, error) { return []SkillRow{row}, nil }, nil)
	gm, _ := m.Update(tea.WindowSizeMsg{Width: 140, Height: 30})
	m = gm.(ListModel)
	gm, _ = m.Update(rowsLoadedMsg{rows: []SkillRow{row}})
	return gm.(ListModel)
}

// TestWrapToLinesShortFits pins the no-wrap path: when the source fits
// the width budget, wrapToLines returns it unchanged with no ellipsis
// suffix and no embedded newlines.
func TestWrapToLinesShortFits(t *testing.T) {
	got := wrapToLines("short skill description.", 80, 2)
	if got != "short skill description." {
		t.Fatalf("short input mutated: %q", got)
	}
	if strings.Contains(got, "\n") {
		t.Fatalf("short input gained a newline: %q", got)
	}
	if strings.HasSuffix(got, "…") {
		t.Fatalf("short input gained an ellipsis: %q", got)
	}
}

// TestWrapToLinesFlattensInternalNewlines confirms that embedded "\n"s
// in the source become spaces — SKILL.md descriptions are by
// convention a single paragraph, and preserving newlines would push
// the rendered row out of its fixed Height() budget.
func TestWrapToLinesFlattensInternalNewlines(t *testing.T) {
	got := wrapToLines("first line\nsecond line", 80, 2)
	if strings.Contains(got, "\n") {
		t.Fatalf("internal newline not flattened: %q", got)
	}
	if got != "first line second line" {
		t.Fatalf("unexpected flatten output: %q", got)
	}
}

// TestWrapToLinesUsesTwoLinesBeforeEllipsizing exercises the wrap path
// for content that fits inside `maxLines` after soft-wrap: the output
// has exactly one newline and no ellipsis.
func TestWrapToLinesUsesTwoLinesBeforeEllipsizing(t *testing.T) {
	// "abc abc abc abc abc abc" is 23 cells; with width 12 it wraps to
	// two ~12-cell lines.
	in := strings.TrimSpace(strings.Repeat("abc ", 6))
	got := wrapToLines(in, 12, 2)
	if got == in {
		t.Fatalf("expected wrap, got identity: %q", got)
	}
	if strings.Count(got, "\n") != 1 {
		t.Fatalf("expected exactly one newline, got %d: %q",
			strings.Count(got, "\n"), got)
	}
	if strings.HasSuffix(got, "…") {
		t.Fatalf("unexpected ellipsis on in-budget wrap: %q", got)
	}
	for _, line := range strings.Split(got, "\n") {
		if w := lipgloss.Width(line); w > 12 {
			t.Errorf("wrapped line exceeds width 12: %d cells: %q", w, line)
		}
	}
}

// TestWrapToLinesOverflowEllipsizesLastLine drives wrapToLines past
// `maxLines`: the result has exactly maxLines lines (no more, no
// fewer), each ≤ width, and the last line ends with "…" to flag the
// dropped content.
func TestWrapToLinesOverflowEllipsizesLastLine(t *testing.T) {
	in := strings.Repeat("alpha bravo ", 30)
	got := wrapToLines(in, 20, 2)
	lines := strings.Split(got, "\n")
	if len(lines) != 2 {
		t.Fatalf("expected exactly 2 lines, got %d: %q", len(lines), got)
	}
	for _, l := range lines {
		if w := lipgloss.Width(l); w > 20 {
			t.Errorf("line exceeds budget: %d cells: %q", w, l)
		}
	}
	if !strings.HasSuffix(lines[1], "…") {
		t.Errorf("overflow line missing ellipsis: %q", lines[1])
	}
}

// TestWrapToLinesEdgeCases pins the defensive zero/negative returns so
// callers can pass user-supplied widths without crashing the TUI.
func TestWrapToLinesEdgeCases(t *testing.T) {
	if got := wrapToLines("anything", 0, 2); got != "" {
		t.Errorf("width=0 returned %q, want \"\"", got)
	}
	if got := wrapToLines("anything", 80, 0); got != "" {
		t.Errorf("maxLines=0 returned %q, want \"\"", got)
	}
}

// TestSkillDelegateHeightIsThree pins the contract for the bubbles
// list delegate: one title row + two description rows. Going back to
// Height(): 2 would silently reintroduce the issue #28 truncation
// because the second description line would simply not be drawn.
func TestSkillDelegateHeightIsThree(t *testing.T) {
	d := newSkillDelegate(func(string) RowStatus { return StatusIdle })
	if got := d.Height(); got != 3 {
		t.Fatalf("skillDelegate.Height() = %d, want 3", got)
	}
}

// TestSkillDelegateEmitsExactlyHeightRows asserts the delegate writes
// Height() lines for every row, regardless of how long the
// description is. The bubbles list assumes a constant per-item row
// count; a short description must pad, not collapse.
func TestSkillDelegateEmitsExactlyHeightRows(t *testing.T) {
	for _, tc := range []struct {
		name string
		row  SkillRow
	}{
		{"empty_desc", SkillRow{Slug: "s", Name: "Short", Desc: ""}},
		{"short_desc", SkillRow{Slug: "s", Name: "Short", Desc: "tiny"}},
		{"long_desc", SkillRow{Slug: "s", Name: "Long", Desc: strings.Repeat("alpha bravo ", 40)}},
		{"multibyte_desc", SkillRow{Slug: "s", Name: "Multi", Desc: strings.Repeat("世界 ", 80)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rendered := renderSingleDelegate(t, tc.row, 80)
			lines := strings.Split(rendered, "\n")
			if len(lines) != 3 {
				t.Fatalf("expected 3 lines, got %d:\n%s", len(lines), rendered)
			}
		})
	}
}

// TestSkillDelegateRendersTwoDescLines is the issue #28 regression
// test: a description long enough to wrap (but short enough to fit in
// the 2-line budget) must show *both* lines, not just an ellipsized
// first line. We assert by counting how many of the rendered rows
// carry visible description text (i.e. non-whitespace after the
// cursor bar).
func TestSkillDelegateRendersTwoDescLines(t *testing.T) {
	// 140 cells of ASCII description; at delegate budget width-6 = 74
	// cells this wraps to roughly two ~70-cell lines, fitting in the
	// 2-line budget without overflow.
	desc := strings.TrimSpace(strings.Repeat("alpha bravo charlie ", 7))
	row := SkillRow{Slug: "wrap_me", Name: "Wrap Me", Desc: desc}
	rendered := renderSingleDelegate(t, row, 80)
	// Strip ANSI before measuring "visible content" so the per-row check
	// counts real glyphs rather than styling residue.
	lines := strings.Split(stripANSI(rendered), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d:\n%s", len(lines), rendered)
	}
	// Both description rows (index 1 and 2) must carry visible content
	// from the description, beyond the cursor-bar glyph.
	for i := 1; i <= 2; i++ {
		stripped := strings.TrimSpace(lines[i])
		if len(stripped) <= 3 { // "· " bar is 3 cells; require real text past it
			t.Errorf("description row %d is empty/whitespace: %q", i, lines[i])
		}
	}
}

// TestSkillDelegateLongDescriptionEllipsizes asserts that when the
// description exceeds the 2-line budget the final visible line ends
// with "…" — the user gets a clear "see the preview pane for the
// rest" signal instead of silent loss.
func TestSkillDelegateLongDescriptionEllipsizes(t *testing.T) {
	desc := strings.Repeat("alpha bravo charlie delta echo foxtrot ", 8)
	row := SkillRow{Slug: "ellipsize", Name: "Ellipsize", Desc: desc}
	rendered := renderSingleDelegate(t, row, 80)
	// Strip ANSI before splitting so the suffix / width assertions are
	// deterministic regardless of the test environment's color profile.
	plain := stripANSI(rendered)
	lines := strings.Split(plain, "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d:\n%s", len(lines), rendered)
	}
	// Width budget is enforced on every line (lipgloss.Width is
	// ANSI-aware, so it can operate on the original rendered output).
	for _, line := range strings.Split(rendered, "\n") {
		if w := lipgloss.Width(line); w > 80 {
			t.Errorf("delegate line exceeds width 80: %d cells: %q", w, line)
		}
	}
	if !strings.HasSuffix(strings.TrimRight(lines[2], " "), "…") {
		t.Errorf("expected ellipsis on the last description line: %q", lines[2])
	}
}

// TestPreviewPanelKeepsHintWithLongDescription is the issue #28
// preview-pane regression test: when the description would wrap past
// the available panel height, the gradient / meta / hint rows must
// still be drawn. Previously, the trailing
// `.Height(m.preview.Height).Render(body)` clamp silently chopped
// the bottom of `body`, so the hint chip disappeared.
func TestPreviewPanelKeepsHintWithLongDescription(t *testing.T) {
	m := buildListWithRow(SkillRow{
		Slug: "very_long_skill",
		Name: "Very Long",
		Desc: strings.Repeat("alpha bravo charlie delta ", 80),
	})
	rendered := m.renderPreviewPanel()
	// The download CTA chip is the canonical "fixed footer" element of
	// the preview pane. If it's still in the output, the clamp didn't
	// chop the bottom.
	if !strings.Contains(rendered, "download") {
		t.Errorf("preview pane lost download hint with a long description:\n%s",
			rendered)
	}
	if !strings.Contains(rendered, "registry · owner/repo") {
		t.Errorf("preview pane lost registry meta with a long description:\n%s",
			rendered)
	}
	// Width invariant still holds across every line.
	for _, line := range strings.Split(rendered, "\n") {
		max := m.preview.Width + 4
		if w := lipgloss.Width(line); w > max {
			t.Errorf("preview line exceeds panel width %d: %d cells: %q",
				max, w, line)
		}
	}
}

// TestClampPreviewDescPassthrough exercises clampPreviewDesc's no-op
// path: a raw description that already fits the budget must come back
// styled with PreviewBody. clampPreviewDesc now owns the rendering, so
// the expected output is the same `PreviewBody.Width(...).Render(in)`
// the caller would have produced for the in-budget case.
func TestClampPreviewDescPassthrough(t *testing.T) {
	in := "a short description."
	got := clampPreviewDesc(in, 60, 30, false)
	want := PreviewBody.Width(60).Render(in)
	if got != want {
		t.Errorf("clampPreviewDesc mutated an in-budget block:\nwant: %q\ngot:  %q", want, got)
	}
}
