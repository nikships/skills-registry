package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ────────────────────────────────────────────────────────────────────────────
// WizardStep — the 8-step onboarding state machine
// ────────────────────────────────────────────────────────────────────────────

// WizardStep enumerates the eight onboarding stages the wizard walks the user
// through. The order matches the bootstrap CLI flow that F2.2 / F2.3 will
// inline into this model, so a user who runs `skill-registry` for the first
// time sees the same sequence — just inside an alt-screen Bubble Tea frame
// instead of a series of synchronous prompts.
type WizardStep int

const (
	// WizardStepScan discovers local skill folders. F2.2 wires the real
	// scan.Discover call; F2.1 renders only the placeholder panel.
	WizardStepScan WizardStep = iota
	// WizardStepRepoName collects the GitHub repository name.
	WizardStepRepoName
	// WizardStepVisibility picks between public and private visibility.
	WizardStepVisibility
	// WizardStepPush uploads every local skill in a batched git push.
	WizardStepPush
	// WizardStepAgentSelect multi-selects the agent dot-folders to seed
	// with the skill-registry SKILL.md.
	WizardStepAgentSelect
	// WizardStepCleanup offers to delete the now-redundant local copies.
	WizardStepCleanup
	// WizardStepMCPInstall provisions the MCP entry point for desktop
	// clients and prints the wire-up JSON snippet.
	WizardStepMCPInstall
	// WizardStepDone is the terminal summary panel; pressing enter here
	// hands off to the hub launcher.
	WizardStepDone
)

// wizardStepCount is the total number of steps; kept as a constant so the
// step indicator stays in sync with the enum.
const wizardStepCount = 8

// Title returns the user-facing label for the step. Used by the step
// indicator and the panel header.
func (s WizardStep) Title() string {
	switch s {
	case WizardStepScan:
		return "Scan local skills"
	case WizardStepRepoName:
		return "Name your registry"
	case WizardStepVisibility:
		return "Choose visibility"
	case WizardStepPush:
		return "Push to GitHub"
	case WizardStepAgentSelect:
		return "Install into agents"
	case WizardStepCleanup:
		return "Tidy local copies"
	case WizardStepMCPInstall:
		return "Wire up MCP"
	case WizardStepDone:
		return "All set!"
	}
	return "Unknown"
}

// description returns the placeholder body shown for the step. F2.2 / F2.3
// replace this with real per-step content (spinner over scan progress,
// textinput for repo name, etc.); F2.1 owns the framing copy.
func (s WizardStep) description() string {
	switch s {
	case WizardStepScan:
		return "Discovering local skill folders under your home and current directory."
	case WizardStepRepoName:
		return "Choose a name for the GitHub repo that will host your registry."
	case WizardStepVisibility:
		return "Private (recommended) keeps your skills to yourself; Public shares them."
	case WizardStepPush:
		return "Uploading every local skill in a single batched git push."
	case WizardStepAgentSelect:
		return "Select which AI agents should learn about your registry."
	case WizardStepCleanup:
		return "Optionally remove the now-redundant copies under each dot-folder."
	case WizardStepMCPInstall:
		return "Install the skill-registry MCP entry point for desktop clients."
	case WizardStepDone:
		return "Setup complete — your registry is live."
	}
	return ""
}

// ────────────────────────────────────────────────────────────────────────────
// Messages
// ────────────────────────────────────────────────────────────────────────────

// wizardTransitionMsg fires after the inter-step animation delay so the
// renderer can swap panels with a visible beat instead of a hard cut.
type wizardTransitionMsg struct{ to WizardStep }

// wizardTransitionDelay paces inter-step transitions. 180ms is short enough
// to feel snappy but long enough that the eye registers the spinner glyph in
// the panel — important for WIZARD-012 where the step indicator must
// visibly update on each transition.
const wizardTransitionDelay = 180 * time.Millisecond

// wizardTransition returns a tea.Cmd that announces a step change after the
// animation delay. F2.2 / F2.3 will replace direct calls with command chains
// that finish real work (scan / push / install) before announcing — the
// message contract stays the same.
func wizardTransition(to WizardStep) tea.Cmd {
	return tea.Tick(wizardTransitionDelay, func(time.Time) tea.Msg {
		return wizardTransitionMsg{to: to}
	})
}

// ────────────────────────────────────────────────────────────────────────────
// Model
// ────────────────────────────────────────────────────────────────────────────

// WizardModel is the alt-screen Bubble Tea model for the F2.1 onboarding
// frame. It owns:
//
//   - the step state machine and inter-step transition animation
//   - the hero / progress / panel / footer chrome (matching the list TUI)
//   - the Esc-triggered cancellation confirmation overlay
//   - exit signaling (Cancelled / Completed) consumed by the launcher
//
// F2.2 and F2.3 will plug per-step business logic into renderStepBody and
// handleStepKey; F2.1 only renders placeholder copy so the frame can be
// validated against WIZARD-001 / -012 / -013 / -014 and VISUAL-001 before
// the heavier features land.
type WizardModel struct {
	ctx context.Context

	step          WizardStep
	width, height int

	// Animation state. While transitioning the panel renders a spinner /
	// "Loading…" line and the indicator advances to the target step.
	transitioning    bool
	transitionTarget WizardStep
	spinner          spinner.Model
	sparkleIdx       int

	// Cancellation confirmation overlay. cancelCursor=0 keeps the wizard
	// running (safe default), cancelCursor=1 aborts.
	cancelOverlay bool
	cancelCursor  int

	// Final state inspected by the launcher after tea.Quit returns.
	cancelled bool
	completed bool
}

// NewWizard constructs the F2.1 wizard frame. ctx is held for the per-step
// goroutines F2.2 / F2.3 will issue (scan / push / MCP install).
func NewWizard(ctx context.Context) WizardModel {
	sp := spinner.New()
	sp.Spinner = spinner.Points
	sp.Style = lipgloss.NewStyle().Foreground(ColPink).Bold(true)
	return WizardModel{
		ctx:     ctx,
		step:    WizardStepScan,
		spinner: sp,
	}
}

// Init implements tea.Model. Kicks off the persistent sparkle animation that
// drives the gradient bar phase + footer dots — same cadence the list TUI
// uses, so the two views feel like one app.
func (m WizardModel) Init() tea.Cmd {
	return sparkleTick()
}

// Step returns the current step. Exposed for tests and for callers that want
// to inspect progress mid-flow.
func (m WizardModel) Step() WizardStep { return m.step }

// Cancelled reports whether the user confirmed the Esc-cancel overlay or
// pressed Ctrl+C. Launchers should suppress any follow-up work in this case.
func (m WizardModel) Cancelled() bool { return m.cancelled }

// Completed reports whether the wizard reached the Done step and the user
// pressed enter. Launchers can use this to decide whether to hand off to
// the hub.
func (m WizardModel) Completed() bool { return m.completed }

// Update implements tea.Model. Dispatches by message type; per-step key
// handling will land in handleStepKey once F2.2 / F2.3 wire it up.
func (m WizardModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil
	case sparkleTickMsg:
		m.sparkleIdx++
		return m, sparkleTick()
	case spinner.TickMsg:
		return m.handleSpinnerTick(msg)
	case wizardTransitionMsg:
		m.transitioning = false
		m.step = msg.to
		return m, nil
	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

// handleSpinnerTick only re-ticks the spinner while we're transitioning —
// idle ticks would burn CPU and chrome the static panels for no reason.
func (m WizardModel) handleSpinnerTick(msg spinner.TickMsg) (tea.Model, tea.Cmd) {
	if !m.transitioning {
		return m, nil
	}
	var cmd tea.Cmd
	m.spinner, cmd = m.spinner.Update(msg)
	return m, cmd
}

// handleKey routes keystrokes to the cancel overlay if it's up, otherwise to
// the wizard-frame defaults. Ctrl+C is always a hard exit (terminal escape
// hatch convention), regardless of overlay state.
func (m WizardModel) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "ctrl+c" {
		m.cancelled = true
		return m, tea.Quit
	}
	if m.cancelOverlay {
		return m.handleCancelKey(msg)
	}
	return m.handleStepKey(msg)
}

// handleStepKey processes keystrokes in the wizard frame (no overlay up).
// F2.2 / F2.3 will route to per-step handlers; F2.1 only supports "esc"
// (open overlay) and "enter" (advance) so the frame is exercisable.
func (m WizardModel) handleStepKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.cancelOverlay = true
		m.cancelCursor = 0
		return m, nil
	case "enter":
		return m.advanceStep()
	}
	return m, nil
}

// advanceStep moves to the next step (or completes the wizard when on
// Done). Starts a transition animation that the renderer surfaces via the
// spinner glyph; the actual step swap lands on wizardTransitionMsg.
func (m WizardModel) advanceStep() (tea.Model, tea.Cmd) {
	if m.transitioning {
		return m, nil
	}
	if m.step == WizardStepDone {
		m.completed = true
		return m, tea.Quit
	}
	next := m.step + 1
	m.transitioning = true
	m.transitionTarget = next
	return m, tea.Batch(wizardTransition(next), m.spinner.Tick)
}

// handleCancelKey runs the keymap while the cancel-confirmation overlay is
// up. Left/right (and h/l) move the cursor; enter confirms the selection;
// "n" or esc dismiss; "y" is a shortcut for "yes, cancel".
func (m WizardModel) handleCancelKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "left", "h":
		m.cancelCursor = 0
		return m, nil
	case "right", "l":
		m.cancelCursor = 1
		return m, nil
	case "y", "Y":
		m.cancelled = true
		return m, tea.Quit
	case "n", "N", "esc":
		m.cancelOverlay = false
		return m, nil
	case "enter":
		if m.cancelCursor == 1 {
			m.cancelled = true
			return m, tea.Quit
		}
		m.cancelOverlay = false
		return m, nil
	}
	return m, nil
}

// ────────────────────────────────────────────────────────────────────────────
// View
// ────────────────────────────────────────────────────────────────────────────

// View implements tea.Model. The base frame renders unconditionally; when
// the cancel overlay is active we centre it on top using lipgloss.Place —
// the same overlay pattern the list TUI uses for its help panel.
func (m WizardModel) View() string {
	base := m.renderFrame()
	if !m.cancelOverlay {
		return base
	}
	overlay := m.renderCancelOverlay()
	if m.width <= 0 || m.height <= 0 {
		return overlay
	}
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, overlay)
}

// renderFrame stacks the four chrome sections: hero banner, step indicator,
// step-specific panel, and the footer keybinding hints. Each section gets a
// blank line separator so the layout breathes the same way the list TUI's
// header/body/toast/footer does.
func (m WizardModel) renderFrame() string {
	hero := m.renderHero()
	progress := m.renderProgress()
	panel := m.renderStepPanel()
	footer := m.renderFooter()
	body := lipgloss.JoinVertical(lipgloss.Left,
		hero,
		"",
		progress,
		"",
		panel,
		"",
		footer,
	)
	return body
}

// renderHero matches the list TUI hero: SparkleStyle bracketing a HeroStyle
// title, with the animated gradient bar below. The bar's width tracks the
// terminal so it spans the full chrome — the gradient phase advances with
// the global sparkle tick, giving the wizard the same "alive" feel.
func (m WizardModel) renderHero() string {
	title := lipgloss.JoinHorizontal(lipgloss.Top,
		SparkleStyle.Render("✦"),
		" ",
		HeroStyle.Render("Skills Registry · Onboarding"),
		" ",
		SparkleStyle.Render("✧"),
	)
	barWidth := m.width - 2
	if barWidth <= 0 {
		barWidth = 40
	}
	gradient := miniGradientBar(barWidth, m.sparkleIdx)
	return lipgloss.JoinVertical(lipgloss.Left, title, gradient)
}

// renderProgress renders the WIZARD-012 step indicator: a row of dots (filled
// for completed steps, ringed for the current step, hollow for upcoming
// steps) plus the explicit "Step N / 8 · Title" caption underneath. During
// a transition the indicator advances to the target so the user sees motion
// the moment they press enter.
func (m WizardModel) renderProgress() string {
	current := int(m.step) + 1
	if m.transitioning {
		current = int(m.transitionTarget) + 1
	}

	dots := make([]string, wizardStepCount)
	for i := 0; i < wizardStepCount; i++ {
		switch {
		case i+1 < current:
			dots[i] = lipgloss.NewStyle().Foreground(ColAccent).Bold(true).Render("●")
		case i+1 == current:
			dots[i] = lipgloss.NewStyle().Foreground(ColPink).Bold(true).Render("◉")
		default:
			dots[i] = lipgloss.NewStyle().Foreground(ColFaint).Render("○")
		}
	}
	progressGlyphs := strings.Join(dots, " ")
	label := lipgloss.NewStyle().Foreground(ColPrimary).Bold(true).
		Render(fmt.Sprintf("Step %d / %d", current, wizardStepCount))
	stepTitle := lipgloss.NewStyle().Foreground(ColPeach).Italic(true).
		Render(WizardStep(current - 1).Title())
	sep := KeySepStyle.Render(" · ")
	caption := lipgloss.JoinHorizontal(lipgloss.Top, label, sep, stepTitle)
	return lipgloss.JoinVertical(lipgloss.Left, progressGlyphs, caption)
}

// renderStepPanel wraps the step body in a rounded-border PanelFocused. The
// panel tracks the terminal width so resizing reflows naturally.
func (m WizardModel) renderStepPanel() string {
	body := m.renderStepBody()
	panel := PanelFocused
	if m.width > 6 {
		panel = panel.Width(m.width - 4)
	}
	return panel.Render(body)
}

// renderStepBody picks between the transition placeholder and the actual
// step body. F2.2 / F2.3 will extend this with real per-step components;
// the F2.1 placeholder uses the step's title + description + CTA so each
// panel is visually distinct without owning real business logic.
func (m WizardModel) renderStepBody() string {
	if m.transitioning {
		loading := lipgloss.NewStyle().Foreground(ColMuted).
			Render(" Loading the next step…")
		return m.spinner.View() + loading
	}
	title := lipgloss.NewStyle().Foreground(ColPrimary).Bold(true).
		Render(m.step.Title())
	desc := lipgloss.NewStyle().Foreground(ColInk).
		Render(m.step.description())
	cta := DownloadChip.Render("⏎ enter") +
		lipgloss.NewStyle().Foreground(ColMuted).Render("  continue")
	if m.step == WizardStepDone {
		cta = DownloadChip.Render("⏎ enter") +
			lipgloss.NewStyle().Foreground(ColAccent).Bold(true).
				Render("  open the hub")
	}
	return lipgloss.JoinVertical(lipgloss.Left, title, "", desc, "", cta)
}

// renderFooter mirrors the list TUI footer: key+description pairs separated
// by dim dots, plus an animated breathing-dots glyph on the right edge.
func (m WizardModel) renderFooter() string {
	keys := []struct{ k, d string }{
		{"enter", "next"},
		{"esc", "cancel"},
	}
	parts := make([]string, 0, len(keys)*3)
	for i, kv := range keys {
		if i > 0 {
			parts = append(parts, KeySepStyle.Render(" · "))
		}
		parts = append(parts, KeyStyle.Render(kv.k), " ", KeyDescStyle.Render(kv.d))
	}
	left := strings.Join(parts, "")
	right := SubtitleStyle.Render(animationDots(m.sparkleIdx))
	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, left, strings.Repeat(" ", gap), right)
}

// renderCancelOverlay is the WIZARD-013 confirmation panel. The HelpOverlay
// style (double border + ColPrimary) makes it pop above the wizard chrome;
// the focused button is rendered with a contrasting chip so the current
// selection is unambiguous on both light and dark backgrounds.
func (m WizardModel) renderCancelOverlay() string {
	title := ErrorStyle.Render("Cancel onboarding?")
	body := lipgloss.NewStyle().Foreground(ColInk).
		Render("Nothing has been written to GitHub yet.\n" +
			"You can restart any time with `skill-registry`.")
	keepLabel := "No, keep going"
	cancelLabel := "Yes, cancel"
	keepBtn := renderOverlayButton(keepLabel, m.cancelCursor == 0, false)
	cancelBtn := renderOverlayButton(cancelLabel, m.cancelCursor == 1, true)
	buttons := lipgloss.JoinHorizontal(lipgloss.Top, keepBtn, "   ", cancelBtn)
	hint := SubtitleStyle.Render("←/→ choose · enter confirm · n / esc keep going")
	inner := lipgloss.JoinVertical(lipgloss.Left, title, "", body, "", buttons, "", hint)
	return HelpOverlay.Render(inner)
}

// renderOverlayButton renders one option in the cancel overlay. The
// destructive variant uses ColDanger; the safe variant uses ColAccent. An
// unfocused button is dimmed so the focus state is the dominant visual.
func renderOverlayButton(label string, focused, destructive bool) string {
	if !focused {
		return lipgloss.NewStyle().Foreground(ColMuted).Padding(0, 1).
			Render(label)
	}
	bg := ColAccent
	if destructive {
		bg = ColDanger
	}
	return lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#FFFDF5")).
		Background(bg).
		Padding(0, 1).
		Render(label)
}
