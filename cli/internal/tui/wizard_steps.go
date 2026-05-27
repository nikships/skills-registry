package tui

import (
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ────────────────────────────────────────────────────────────────────────────
// Step 5 (Agent select), 6 (Cleanup), 7 (Connect MCP client), 8 (Done)
//
// Each step owns:
//   - A small handler set (key dispatch + async-message handlers).
//   - A renderer that fits inside the existing PanelFocused frame.
//
// Long-running work (deps.InstallAgents, deps.DeleteCleanup) runs in
// goroutines via tea.Cmd; the spinnerActive() set above keeps the
// spinner ticking while we wait. Step 7 is purely informational so it
// has no goroutine.
// ────────────────────────────────────────────────────────────────────────────

// ────────────────────────────────────────────────────────────────────────────
// Step 5: Agent select
// ────────────────────────────────────────────────────────────────────────────

// loadAgentChoices populates the multi-select rows + default-checked set
// from deps.AgentChoices. Idempotent: subsequent calls are no-ops so the
// user's filter / selection survive a step revisit.
func (m *WizardModel) loadAgentChoices() {
	if m.agentLoaded {
		return
	}
	m.agentLoaded = true
	if m.deps.AgentChoices == nil {
		return
	}
	items := m.deps.AgentChoices()
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Locked != items[j].Locked {
			return items[i].Locked
		}
		return items[i].Display < items[j].Display
	})
	m.agentItems = items
	if m.agentSelected == nil {
		m.agentSelected = map[int]struct{}{}
	}
	for i, it := range items {
		if it.Locked {
			continue
		}
		if it.Default {
			m.agentSelected[i] = struct{}{}
		}
	}
}

// agentFilteredIndices returns the indices of non-locked rows that match
// the current filter substring. Locked rows render in a fixed strip above
// the filterable list.
func (m WizardModel) agentFilteredIndices() []int {
	var out []int
	lower := strings.ToLower(m.agentFilter)
	for i, it := range m.agentItems {
		if it.Locked {
			continue
		}
		if lower == "" {
			out = append(out, i)
			continue
		}
		if strings.Contains(strings.ToLower(it.Display+" "+it.Hint), lower) {
			out = append(out, i)
		}
	}
	return out
}

// agentSelectedValues returns every locked item's value plus every
// user-toggled value, in the order they appear in agentItems.
func (m WizardModel) agentSelectedValues() []any {
	out := make([]any, 0, len(m.agentItems))
	for i, it := range m.agentItems {
		if it.Locked {
			out = append(out, it.Value)
			continue
		}
		if _, ok := m.agentSelected[i]; ok {
			out = append(out, it.Value)
		}
	}
	return out
}

// handleAgentSelectKey routes the multi-select key map. Once the install
// goroutine is in flight the only meaningful key is enter (advance once
// it's done) — every other key is swallowed.
func (m WizardModel) handleAgentSelectKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.agentInstalling || m.agentInstallDone {
		return m.handleAgentInstallStateKey(msg)
	}
	filtered := m.agentFilteredIndices()
	switch msg.String() {
	case "up":
		if m.agentCursor > 0 {
			m.agentCursor--
		}
		return m, nil
	case "down":
		if m.agentCursor < len(filtered)-1 {
			m.agentCursor++
		}
		return m, nil
	case " ":
		m = m.toggleAgentAtCursor(filtered)
		return m, nil
	case "tab":
		for _, idx := range filtered {
			m.agentSelected[idx] = struct{}{}
		}
		return m, nil
	case "enter":
		return m.startAgentInstall()
	case "backspace":
		if len(m.agentFilter) > 0 {
			runes := []rune(m.agentFilter)
			m.agentFilter = string(runes[:len(runes)-1])
			m.agentCursor = 0
		}
		return m, nil
	}
	if msg.Type == tea.KeyRunes && len(msg.String()) == 1 {
		m.agentFilter += msg.String()
		m.agentCursor = 0
	}
	return m, nil
}

// toggleAgentAtCursor flips the selected state of the row at the cursor.
// Returns the updated receiver so callers can fall back to the value
// receiver convention used elsewhere in this file.
func (m WizardModel) toggleAgentAtCursor(filtered []int) WizardModel {
	if len(filtered) == 0 || m.agentCursor >= len(filtered) {
		return m
	}
	idx := filtered[m.agentCursor]
	if _, ok := m.agentSelected[idx]; ok {
		delete(m.agentSelected, idx)
	} else {
		m.agentSelected[idx] = struct{}{}
	}
	return m
}

// handleAgentInstallStateKey runs once the install goroutine has been
// started. Enter advances once the install is done; everything else is a
// no-op so the user can't accidentally fire a second install.
func (m WizardModel) handleAgentInstallStateKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() != "enter" {
		return m, nil
	}
	if !m.agentInstallDone {
		return m, nil
	}
	return m.advanceStep()
}

// startAgentInstall kicks off the InstallAgents goroutine. Without a
// dep wired we fast-path through "installed 0 agents" so the test suite
// can drive the state machine.
func (m WizardModel) startAgentInstall() (tea.Model, tea.Cmd) {
	picked := m.agentSelectedValues()
	m.agentInstalling = true
	if m.deps.InstallAgents == nil {
		m.agentInstallDone = true
		return m, nil
	}
	ctx := m.ctx
	fn := m.deps.InstallAgents
	repo := m.pushRepo
	return m, tea.Batch(
		m.spinner.Tick,
		func() tea.Msg {
			paths, err := fn(ctx, repo, picked)
			return wizardAgentInstallDoneMsg{paths: paths, err: err}
		},
	)
}

// handleAgentInstallDone records the result of the install goroutine.
// The user still needs to press enter to advance — they should see the
// success summary before moving on.
func (m WizardModel) handleAgentInstallDone(msg wizardAgentInstallDoneMsg) (tea.Model, tea.Cmd) {
	m.agentInstalling = false
	m.agentInstallDone = true
	m.agentPaths = msg.paths
	m.agentInstallErr = msg.err
	return m, nil
}

// renderAgentSelectBody renders the multi-select rendered into the wizard
// panel. The locked-universal entries pin to the top; the filterable list
// follows. Once install is in flight the panel switches to a "installing…"
// view and finally to a "✓ installed into N folders" summary.
func (m WizardModel) renderAgentSelectBody() string {
	title := lipgloss.NewStyle().Foreground(ColPrimary).Bold(true).
		Render(m.step.Title())
	if m.agentInstalling && !m.agentInstallDone {
		return m.renderAgentInstallingBody(title)
	}
	if m.agentInstallDone {
		return m.renderAgentInstallSummary(title)
	}
	prompt := lipgloss.NewStyle().Foreground(ColInk).
		Render("Pick which AI agents should learn about your registry.")
	hint := lipgloss.NewStyle().Foreground(ColMuted).Italic(true).
		Render("· locked entries are always included.")
	locked := m.renderAgentLockedStrip()
	filterLine := m.renderAgentFilterLine()
	rows := m.renderAgentRows()
	cta := m.renderAgentSelectCTA()
	parts := []string{title, "", prompt, "", hint}
	if locked != "" {
		parts = append(parts, "", locked)
	}
	parts = append(parts, "", filterLine, "", rows, "", cta)
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

// renderAgentInstallingBody surfaces the spinner + status while the
// InstallAgents goroutine is in flight.
func (m WizardModel) renderAgentInstallingBody(title string) string {
	picked := len(m.agentSelectedValues())
	caption := lipgloss.NewStyle().Foreground(ColInk).
		Render(fmt.Sprintf("Installing skills-registry/SKILL.md into %d agent folder(s)…", picked))
	return lipgloss.JoinVertical(lipgloss.Left,
		title, "",
		m.spinner.View()+" "+caption,
	)
}

// renderAgentInstallSummary renders the post-install state: success badge,
// up to five written paths, and the enter-to-continue CTA.
func (m WizardModel) renderAgentInstallSummary(title string) string {
	if m.agentInstallErr != nil {
		return lipgloss.JoinVertical(lipgloss.Left,
			title, "",
			ErrorStyle.Render("✗ install failed: "+m.agentInstallErr.Error()),
			"",
			DownloadChip.Render("⏎ enter")+
				lipgloss.NewStyle().Foreground(ColMuted).Render("  continue anyway"),
		)
	}
	headline := lipgloss.NewStyle().Foreground(ColAccent).Bold(true).
		Render(fmt.Sprintf("✓ installed into %d folder(s).", len(m.agentPaths)))
	preview := m.renderAgentPathPreview()
	cta := DownloadChip.Render("⏎ enter") +
		lipgloss.NewStyle().Foreground(ColAccent).Bold(true).
			Render("  continue to cleanup")
	parts := []string{title, "", headline}
	if preview != "" {
		parts = append(parts, "", preview)
	}
	parts = append(parts, "", cta)
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

// renderAgentPathPreview lists up to five of the written paths so the
// user sees concrete evidence the install landed.
func (m WizardModel) renderAgentPathPreview() string {
	if len(m.agentPaths) == 0 {
		return ""
	}
	limit := min(5, len(m.agentPaths))
	lines := make([]string, 0, limit+1)
	for i := 0; i < limit; i++ {
		bullet := lipgloss.NewStyle().Foreground(ColPink).Bold(true).Render("· ✧")
		path := lipgloss.NewStyle().Foreground(ColPeach).Italic(true).Render(m.agentPaths[i])
		lines = append(lines, bullet+" "+path)
	}
	if len(m.agentPaths) > limit {
		lines = append(lines, lipgloss.NewStyle().Foreground(ColMuted).Italic(true).
			Render(fmt.Sprintf("  … +%d more", len(m.agentPaths)-limit)))
	}
	return strings.Join(lines, "\n")
}

// renderAgentLockedStrip renders the "always included" header + rows.
// Returns "" when no locked items exist so the panel doesn't reserve
// vertical space for an empty section.
func (m WizardModel) renderAgentLockedStrip() string {
	var rows []string
	for _, it := range m.agentItems {
		if !it.Locked {
			continue
		}
		mark := lipgloss.NewStyle().Foreground(ColAccent).Bold(true).Render("✓")
		label := lipgloss.NewStyle().Foreground(ColInk).Render(it.Display)
		hint := lipgloss.NewStyle().Foreground(ColMuted).Italic(true).Render("  " + it.Hint)
		rows = append(rows, "  "+mark+" "+label+hint)
	}
	if len(rows) == 0 {
		return ""
	}
	header := lipgloss.NewStyle().Foreground(ColMuted).Italic(true).
		Render("Always included:")
	return header + "\n" + strings.Join(rows, "\n")
}

// renderAgentFilterLine renders the "Filter:" prompt + current text. We
// use the same chrome as the bubbles textinput in earlier steps so the
// surface looks coherent.
func (m WizardModel) renderAgentFilterLine() string {
	prompt := lipgloss.NewStyle().Foreground(ColPink).Bold(true).Render("Filter ›")
	text := lipgloss.NewStyle().Foreground(ColInk).Render(m.agentFilter)
	if m.agentFilter == "" {
		text = lipgloss.NewStyle().Foreground(ColMuted).Italic(true).Render("type to narrow the list…")
	}
	return prompt + " " + text
}

// renderAgentRows lists up to maxVisible filterable rows around the cursor.
// Selected rows show a filled circle; the cursor row gets a bold caret.
func (m WizardModel) renderAgentRows() string {
	filtered := m.agentFilteredIndices()
	if len(filtered) == 0 {
		return lipgloss.NewStyle().Foreground(ColMuted).Italic(true).
			Render("  (no matches)")
	}
	const maxVisible = 8
	start, end := windowAround(m.agentCursor, len(filtered), maxVisible)
	var lines []string
	for i := start; i < end; i++ {
		idx := filtered[i]
		lines = append(lines, m.renderAgentRow(idx, i == m.agentCursor))
	}
	if start > 0 || end < len(filtered) {
		lines = append(lines, lipgloss.NewStyle().Foreground(ColMuted).Italic(true).Render("  …"))
	}
	return strings.Join(lines, "\n")
}

// renderAgentRow renders a single row in the filterable list. cursor=true
// applies the focused styling and prepends a ❯ caret.
func (m WizardModel) renderAgentRow(idx int, cursor bool) string {
	it := m.agentItems[idx]
	_, picked := m.agentSelected[idx]
	bullet := lipgloss.NewStyle().Foreground(ColFaint).Render("○")
	label := lipgloss.NewStyle().Foreground(ColInk).Render(it.Display)
	if picked {
		accent := lipgloss.NewStyle().Foreground(ColAccent).Bold(true)
		bullet = accent.Render("●")
		label = accent.Render(it.Display)
	}
	hint := ""
	if it.Hint != "" {
		hint = "  " + lipgloss.NewStyle().Foreground(ColMuted).Render(it.Hint)
	}
	prefix := "  "
	if cursor {
		prefix = lipgloss.NewStyle().Foreground(ColPink).Bold(true).Render("❯ ")
	}
	return prefix + bullet + " " + label + hint
}

// renderAgentSelectCTA is the keymap chip line.
func (m WizardModel) renderAgentSelectCTA() string {
	muted := lipgloss.NewStyle().Foreground(ColMuted)
	return KeyStyle.Render("space") + muted.Render(" toggle · ") +
		KeyStyle.Render("tab") + muted.Render(" select all · ") +
		DownloadChip.Render("⏎ enter") + muted.Render(" install")
}

// ────────────────────────────────────────────────────────────────────────────
// Step 6: Cleanup
// ────────────────────────────────────────────────────────────────────────────

// startCleanupLoad fires the LoadCleanup goroutine. The wizard treats a
// missing dep as "nothing to clean" so the state machine stays runnable
// in tests.
func (m WizardModel) startCleanupLoad() tea.Cmd {
	if m.deps.LoadCleanup == nil {
		return func() tea.Msg { return wizardCleanupLoadedMsg{} }
	}
	ctx := m.ctx
	fn := m.deps.LoadCleanup
	repo := m.pushRepo
	skills := m.skills
	return func() tea.Msg {
		entries := fn(ctx, repo, skills)
		return wizardCleanupLoadedMsg{entries: entries}
	}
}

// handleCleanupLoaded records the entries and auto-advances when the
// candidate set is empty (the user shouldn't be prompted with a yes/no
// for "delete 0 things").
func (m WizardModel) handleCleanupLoaded(msg wizardCleanupLoadedMsg) (tea.Model, tea.Cmd) {
	m.cleanupLoaded = true
	m.cleanupEntries = msg.entries
	if len(msg.entries) == 0 {
		// Nothing to delete — mark the step "chosen" so a stray enter
		// advances cleanly.
		m.cleanupChosen = true
		m.cleanupDone = true
	}
	return m, nil
}

// handleCleanupKey moves the cursor between Yes/No and confirms on enter.
// After the deletion goroutine resolves, enter advances to the MCP step.
func (m WizardModel) handleCleanupKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.cleanupChosen {
		return m.handleCleanupChosenKey(msg)
	}
	switch msg.String() {
	case "left", "h":
		m.cleanupCursor = 0
		return m, nil
	case "right", "l":
		m.cleanupCursor = 1
		return m, nil
	case "y", "Y":
		m.cleanupCursor = 0
		return m.confirmCleanup(true)
	case "n", "N":
		m.cleanupCursor = 1
		return m.confirmCleanup(false)
	case "enter":
		return m.confirmCleanup(m.cleanupCursor == 0)
	}
	return m, nil
}

// handleCleanupChosenKey runs after the user has answered. Enter advances
// once any deletion goroutine has finished; if the user chose "no",
// cleanupDone is true immediately.
func (m WizardModel) handleCleanupChosenKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() != "enter" {
		return m, nil
	}
	if !m.cleanupDone {
		return m, nil
	}
	return m.advanceStep()
}

// confirmCleanup locks in the choice and kicks off the delete goroutine
// when the user chose Yes. "No" is a no-op apart from setting cleanupDone.
func (m WizardModel) confirmCleanup(yes bool) (tea.Model, tea.Cmd) {
	m.cleanupChosen = true
	m.cleanupYes = yes
	if !yes {
		m.cleanupDone = true
		return m, nil
	}
	if m.deps.DeleteCleanup == nil || len(m.cleanupEntries) == 0 {
		m.cleanupDone = true
		return m, nil
	}
	m.cleanupRunning = true
	entries := append([]WizardCleanupEntry(nil), m.cleanupEntries...)
	fn := m.deps.DeleteCleanup
	return m, tea.Batch(
		m.spinner.Tick,
		func() tea.Msg {
			deleted, failed := fn(entries)
			return wizardCleanupDoneMsg{deleted: deleted, failed: failed}
		},
	)
}

// handleCleanupDone records the deletion summary.
func (m WizardModel) handleCleanupDone(msg wizardCleanupDoneMsg) (tea.Model, tea.Cmd) {
	m.cleanupRunning = false
	m.cleanupDone = true
	m.cleanupDeleted = msg.deleted
	m.cleanupFailed = msg.failed
	return m, nil
}

// renderCleanupBody renders the four phases of step 6: loading, prompt,
// running, and summary.
func (m WizardModel) renderCleanupBody() string {
	title := lipgloss.NewStyle().Foreground(ColPrimary).Bold(true).
		Render(m.step.Title())
	if !m.cleanupLoaded {
		return lipgloss.JoinVertical(lipgloss.Left, title, "",
			m.spinner.View()+" "+
				lipgloss.NewStyle().Foreground(ColInk).
					Render("Scanning your dot-folders for redundant copies…"),
		)
	}
	if !m.cleanupChosen {
		return m.renderCleanupPrompt(title)
	}
	if m.cleanupRunning {
		caption := lipgloss.NewStyle().Foreground(ColInk).
			Render(fmt.Sprintf("Removing %d local entry(ies)…", len(m.cleanupEntries)))
		return lipgloss.JoinVertical(lipgloss.Left, title, "", m.spinner.View()+" "+caption)
	}
	return m.renderCleanupSummary(title)
}

// renderCleanupPrompt is the yes/no prompt with a per-source breakdown.
func (m WizardModel) renderCleanupPrompt(title string) string {
	intro := lipgloss.NewStyle().Foreground(ColInk).
		Render("Your skills live in the registry now. The local copies are dead weight —")
	intro2 := lipgloss.NewStyle().Foreground(ColInk).
		Render("every coding agent re-reads them on each session, bloating context.")
	breakdown := m.renderCleanupBreakdown()
	count := lipgloss.NewStyle().Foreground(ColAccent).Bold(true).
		Render(fmt.Sprintf("%d", len(m.cleanupEntries)))
	stat := lipgloss.JoinHorizontal(lipgloss.Top,
		lipgloss.NewStyle().Foreground(ColInk).Render("This removes "),
		count,
		lipgloss.NewStyle().Foreground(ColInk).Render(" local entry(ies). Nothing in the registry is touched."),
	)
	buttons := m.renderCleanupButtons()
	hint := lipgloss.NewStyle().Foreground(ColMuted).Italic(true).
		Render("←/→ choose · enter confirm · y/n shortcuts")
	parts := []string{title, "", intro, intro2}
	if breakdown != "" {
		parts = append(parts, "", breakdown)
	}
	parts = append(parts, "", stat, "", buttons, "", hint)
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

// renderCleanupBreakdown groups the entries by source and shows a count.
func (m WizardModel) renderCleanupBreakdown() string {
	bySource := map[string]int{}
	symlinks := map[string]int{}
	for _, en := range m.cleanupEntries {
		bySource[en.Source]++
		if en.IsSymlink {
			symlinks[en.Source]++
		}
	}
	labels := make([]string, 0, len(bySource))
	for k := range bySource {
		labels = append(labels, k)
	}
	sort.Strings(labels)
	var lines []string
	for _, src := range labels {
		line := fmt.Sprintf("  · %s (%d entry(ies))", src, bySource[src])
		if n := symlinks[src]; n > 0 {
			line += fmt.Sprintf(" — %d symlink(s)", n)
		}
		lines = append(lines, lipgloss.NewStyle().Foreground(ColMuted).Render(line))
	}
	return strings.Join(lines, "\n")
}

// renderCleanupButtons draws the Yes / No pair with the focused button
// styled per the cancel-overlay button convention.
func (m WizardModel) renderCleanupButtons() string {
	yes := renderOverlayButton("Yes, delete", m.cleanupCursor == 0, false)
	no := renderOverlayButton("No, keep them", m.cleanupCursor == 1, false)
	return lipgloss.JoinHorizontal(lipgloss.Top, yes, "   ", no)
}

// renderCleanupSummary is the post-action body.
func (m WizardModel) renderCleanupSummary(title string) string {
	cta := DownloadChip.Render("⏎ enter") +
		lipgloss.NewStyle().Foreground(ColAccent).Bold(true).
			Render("  continue to MCP install")
	var headline string
	switch {
	case !m.cleanupYes:
		headline = lipgloss.NewStyle().Foreground(ColPeach).Italic(true).
			Render("· kept local skill copies in place.")
	case len(m.cleanupEntries) == 0:
		headline = lipgloss.NewStyle().Foreground(ColAccent).Bold(true).
			Render("✓ already tidy — nothing to remove.")
	default:
		headline = lipgloss.NewStyle().Foreground(ColAccent).Bold(true).
			Render(fmt.Sprintf("✓ removed %d local entry(ies).", m.cleanupDeleted))
		if m.cleanupFailed > 0 {
			headline += lipgloss.NewStyle().Foreground(ColDanger).
				Render(fmt.Sprintf("  (%d failed)", m.cleanupFailed))
		}
	}
	return lipgloss.JoinVertical(lipgloss.Left, title, "", headline, "", cta)
}

// ────────────────────────────────────────────────────────────────────────────
// Step 7: Connect MCP client
//
// The CLI never installs or boots an MCP server — the hosted server at
// mcp.skills-registry.dev is the only one users talk to. This step is
// purely informational: it fetches the snippet from deps.MCPSnippet,
// renders it inside a code panel, and waits for the user to press enter.
// ────────────────────────────────────────────────────────────────────────────

// startMCPConnect snapshots the snippet immediately (synchronous — no
// goroutine, no install) and flips mcpDone so the renderer shows the
// finished panel right away. Pointer receiver so mcpStarted survives
// back to onEnterStep's caller.
func (m *WizardModel) startMCPConnect() tea.Cmd {
	m.mcpStarted = true
	snippet := ""
	if m.deps.MCPSnippet != nil {
		snippet = m.deps.MCPSnippet()
	}
	return func() tea.Msg { return wizardMCPDoneMsg{snippet: snippet} }
}

// mcpQuickInstallRow is one entry in the quick-install panel shown below
// the snippet on step 7. Commands may evolve as each tool's MCP CLI
// matures — update this slice and the command strings stay in one place.
type mcpQuickInstallRow struct {
	label   string
	command string
}

// mcpQuickInstallRows lists the one-liner commands each major MCP client
// needs to register the hosted server. Verified against each tool's
// official docs as of May 2025 — re-check when tools publish new releases.
var mcpQuickInstallRows = []mcpQuickInstallRow{
	{
		label:   "Claude Code",
		command: "claude mcp add --transport http skills-registry https://mcp.skills-registry.dev/mcp",
	},
	{
		label:   "Codex CLI",
		command: "codex mcp add skills-registry --url https://mcp.skills-registry.dev/mcp",
	},
	{
		label:   "Factory Droid",
		command: "droid mcp add skills-registry https://mcp.skills-registry.dev/mcp --type http",
	},
}

// handleMCPDone stores the snippet so the renderer can show it and fires
// an async clipboard write if deps.CopyToClipboard is wired.
func (m WizardModel) handleMCPDone(msg wizardMCPDoneMsg) (tea.Model, tea.Cmd) {
	m.mcpDone = true
	m.mcpSnippet = msg.snippet
	m.mcpQuickCopied = -1
	// Reset clipboard state so a previous visit's success badge doesn't
	// flicker during re-entry.
	m.mcpClipboardDone = false
	m.mcpClipboardOK = false
	if msg.snippet == "" || m.deps.CopyToClipboard == nil {
		m.mcpClipboardDone = true
		return m, nil
	}
	snippet := msg.snippet
	fn := m.deps.CopyToClipboard
	return m, func() tea.Msg {
		if err := fn(snippet); err != nil {
			return wizardMCPClipboardMsg{ok: false, errMsg: err.Error(), idx: -1}
		}
		return wizardMCPClipboardMsg{ok: true, idx: -1}
	}
}

// handleMCPClipboard records whether the async clipboard write succeeded.
// idx==-1 means the main snippet; idx>=0 means a quick-install row.
func (m WizardModel) handleMCPClipboard(msg wizardMCPClipboardMsg) (tea.Model, tea.Cmd) {
	if !m.mcpDone {
		return m, nil
	}
	if msg.idx == -1 {
		m.mcpClipboardDone = true
		m.mcpClipboardOK = msg.ok
	} else if msg.ok {
		m.mcpQuickCopied = msg.idx
	}
	return m, nil
}

// handleMCPKey routes key events on step 7: arrow keys navigate the
// quick-install panel, 'c' copies the highlighted row's command, and
// enter advances to Done once the snippet has been captured.
func (m WizardModel) handleMCPKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if len(mcpQuickInstallRows) > 0 {
			m.mcpQuickCursor = (m.mcpQuickCursor - 1 + len(mcpQuickInstallRows)) % len(mcpQuickInstallRows)
		}
		return m, nil
	case "down", "j":
		if len(mcpQuickInstallRows) > 0 {
			m.mcpQuickCursor = (m.mcpQuickCursor + 1) % len(mcpQuickInstallRows)
		}
		return m, nil
	case "c":
		return m.handleMCPQuickInstallCopy()
	case "enter":
		if !m.mcpDone {
			return m, nil
		}
		return m.advanceStep()
	}
	return m, nil
}

// handleMCPQuickInstallCopy copies the highlighted quick-install row's
// command to the clipboard. mcpQuickCopied is only set upon a successful
// write so the "✓ copied" badge is not shown after a clipboard error.
func (m WizardModel) handleMCPQuickInstallCopy() (WizardModel, tea.Cmd) {
	if m.deps.CopyToClipboard == nil || len(mcpQuickInstallRows) == 0 {
		return m, nil
	}
	idx := m.mcpQuickCursor
	cmd := mcpQuickInstallRows[idx].command
	fn := m.deps.CopyToClipboard
	return m, func() tea.Msg {
		if err := fn(cmd); err != nil {
			return nil
		}
		return wizardMCPClipboardMsg{ok: true, idx: idx}
	}
}

// renderMCPBody shows the headline + snippet panel + quick-install panel
// + continue chip.
func (m WizardModel) renderMCPBody() string {
	title := lipgloss.NewStyle().Foreground(ColPrimary).Bold(true).
		Render(m.step.Title())
	var headlineText string
	switch {
	case !m.mcpClipboardDone:
		headlineText = "✦ Copying to clipboard…"
	case m.mcpClipboardOK:
		headlineText = "✓ Copied to clipboard!"
	default:
		headlineText = "✦ Paste this into your MCP client config."
	}
	headline := lipgloss.NewStyle().Foreground(ColAccent).Bold(true).Render(headlineText)
	intro := lipgloss.NewStyle().Foreground(ColInk).
		Render("The hosted server handles OAuth on first connect — no install needed.")
	snippet := m.renderMCPSnippetPanel()
	quickInstall := m.renderMCPQuickInstallPanel()
	cta := DownloadChip.Render("⏎ enter") +
		lipgloss.NewStyle().Foreground(ColAccent).Bold(true).
			Render("  continue")
	parts := []string{title, "", headline, "", intro}
	if snippet != "" {
		parts = append(parts, "", snippet)
	}
	if quickInstall != "" {
		parts = append(parts, "", quickInstall)
	}
	parts = append(parts, "", cta)
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

// renderMCPQuickInstallPanel renders the three quick-install rows as a
// compact scrollable list. The focused row gets a highlight chip; the
// last-copied row shows a ✓ badge.
func (m WizardModel) renderMCPQuickInstallPanel() string {
	if len(mcpQuickInstallRows) == 0 {
		return ""
	}
	panelWidth := 80
	if m.width > 16 {
		panelWidth = m.width - 8
	}

	mutedStyle := lipgloss.NewStyle().Foreground(ColMuted)
	titleBar := mutedStyle.Render("── Quick install " + strings.Repeat("─", max(0, panelWidth-17)))
	const labelWidth = 14
	rows := make([]string, 0, len(mcpQuickInstallRows))
	for i, row := range mcpQuickInstallRows {
		rows = append(rows, m.renderMCPQuickInstallRow(row, i, labelWidth))
	}
	hint := lipgloss.NewStyle().Foreground(ColMuted).Italic(true).
		Render("  ↑/↓ to move · c to copy highlighted command")
	bottomBar := mutedStyle.Render(strings.Repeat("─", panelWidth))
	return lipgloss.JoinVertical(lipgloss.Left,
		titleBar,
		lipgloss.JoinVertical(lipgloss.Left, rows...),
		"",
		hint,
		bottomBar,
	)
}

// renderMCPQuickInstallRow renders a single quick-install row. The
// focused row gets bold primary chrome and a ▶ caret; unfocused rows
// stay muted. A "✓ copied" badge is appended on the most recently copied
// row so the user gets immediate feedback after pressing `c`.
func (m WizardModel) renderMCPQuickInstallRow(row mcpQuickInstallRow, i, labelWidth int) string {
	label := fmt.Sprintf("%-*s", labelWidth, row.label)
	prefix := "  "
	labelStyle := lipgloss.NewStyle().Foreground(ColMuted)
	cmdStyle := lipgloss.NewStyle().Foreground(ColInk)
	if i == m.mcpQuickCursor {
		prefix = lipgloss.NewStyle().Foreground(ColPrimary).Bold(true).Render("▶ ")
		labelStyle = lipgloss.NewStyle().Foreground(ColPrimary).Bold(true)
		cmdStyle = lipgloss.NewStyle().Foreground(ColInk).Bold(true)
	}
	line := prefix + labelStyle.Render(label) + " " + cmdStyle.Render(row.command)
	if m.mcpDone && i == m.mcpQuickCopied {
		line += "  " + lipgloss.NewStyle().Foreground(ColAccent).Bold(true).Render("✓ copied")
	}
	return line
}

// renderMCPSnippetPanel renders the JSON snippet inside a rounded-border
// PanelStyle so it reads as a code block.
func (m WizardModel) renderMCPSnippetPanel() string {
	if m.mcpSnippet == "" {
		return ""
	}
	header := lipgloss.NewStyle().Foreground(ColMuted).Italic(true).
		Render("mcp.json (Claude Code / Claude Desktop / Cursor / VS Code):")
	body := lipgloss.NewStyle().Foreground(ColInk).Render(m.mcpSnippet)
	panel := PanelStyle
	if m.width > 8 {
		panel = panel.Width(m.width - 8)
	}
	return lipgloss.JoinVertical(lipgloss.Left, header, "", panel.Render(body))
}

// ────────────────────────────────────────────────────────────────────────────
// Step 8: Done
// ────────────────────────────────────────────────────────────────────────────

// handleDoneKey treats enter as the terminal "launch hub" trigger. The
// existing advanceStep() logic on the Done step sets Completed()=true and
// emits tea.Quit, which the launcher interprets as "launch hub".
func (m WizardModel) handleDoneKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() != "enter" {
		return m, nil
	}
	return m.advanceStep()
}

// renderDoneBody renders the celebration summary: repo slug, skill count,
// agent install count, and the launch-hub CTA.
func (m WizardModel) renderDoneBody() string {
	title := lipgloss.NewStyle().Foreground(ColPrimary).Bold(true).
		Render(m.step.Title())
	headline := lipgloss.NewStyle().Foreground(ColAccent).Bold(true).
		Render("Your registry is live.")
	stats := m.renderDoneStats()
	hint := lipgloss.NewStyle().Foreground(ColMuted).Italic(true).
		Render("· run `skills-registry` any time to open the hub.")
	cta := DownloadChip.Render("⏎ enter") +
		lipgloss.NewStyle().Foreground(ColAccent).Bold(true).
			Render("  continue to the hub")
	return lipgloss.JoinVertical(lipgloss.Left, title, "", headline, "", stats, "", hint, "", cta)
}

// renderDoneStats renders the three-line summary chip stack.
func (m WizardModel) renderDoneStats() string {
	muted := lipgloss.NewStyle().Foreground(ColMuted)
	repo := m.pushRepo
	if repo == "" {
		repo = strings.TrimSpace(m.repoInput.Value())
	}
	repoLine := lipgloss.JoinHorizontal(lipgloss.Top,
		muted.Render("  ◆ Registry "),
		lipgloss.NewStyle().Foreground(ColPrimary).Bold(true).Render(repo),
	)
	pushedLine := lipgloss.JoinHorizontal(lipgloss.Top,
		muted.Render("  ◆ "),
		lipgloss.NewStyle().Foreground(ColAccent).Bold(true).
			Render(fmt.Sprintf("%d", m.pushed)),
		muted.Render(" skill(s) pushed"),
	)
	agentLine := lipgloss.JoinHorizontal(lipgloss.Top,
		muted.Render("  ◆ Installed into "),
		lipgloss.NewStyle().Foreground(ColPink).Bold(true).
			Render(fmt.Sprintf("%d", len(m.agentPaths))),
		muted.Render(" agent folder(s)"),
	)
	return lipgloss.JoinVertical(lipgloss.Left, repoLine, pushedLine, agentLine)
}
