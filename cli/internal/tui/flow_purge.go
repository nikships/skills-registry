package tui

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/anand-92/skills-registry/cli/internal/scan"
)

// PurgeFlowDeps wires the Purge Local Skills hub action to its scan and
// delete primitives. Both callbacks live in cli/cmd/skills-registry/ so
// this package stays decoupled from the registry client and filesystem.
//
// Discover enumerates the local skill folders that would be deleted —
// typically scan.Discover over the known dot-folders under $HOME/$CWD.
// Delete removes the supplied folders best-effort and returns per-entry
// success/failure rather than aborting on the first error, matching the
// wizard cleanup contract.
type PurgeFlowDeps struct {
	Discover func(context.Context) ([]scan.Skill, error)
	Delete   func(context.Context, []scan.Skill) (deleted int, failed int, err error)
}

type purgeFlowState int

const (
	purgeStateScanning purgeFlowState = iota
	purgeStateConfirm
	purgeStateDeleting
)

// PurgeFlowModel is the embedded Bubble Tea flow launched by the Purge
// Local hub card. It walks the user through:
//  1. Scan local dot-folders for SKILL.md folders.
//  2. Show a confirmation prompt with a per-source breakdown.
//  3. On confirm, delete each folder and toast the outcome.
//
// The model never mutates anything until the user explicitly confirms;
// Esc at any stage exits cleanly with a "cancelled" toast.
type PurgeFlowModel struct {
	ctx  context.Context
	deps PurgeFlowDeps

	state        purgeFlowState
	skills       []scan.Skill
	confirmModel ChoiceModel
	spinner      spinner.Model

	width, height int
	sparkleIdx    int
}

type purgeLoadedMsg struct {
	skills []scan.Skill
	err    error
}

type purgeDoneMsg struct {
	deleted int
	failed  int
	err     error
}

// NewPurgeFlow constructs the purge flow with the supplied deps. The
// embedded Bubble Tea program runs the scan immediately on Init() so
// the first frame already shows the spinner.
func NewPurgeFlow(ctx context.Context, deps PurgeFlowDeps) PurgeFlowModel {
	sp := spinner.New()
	sp.Spinner = spinner.Points
	sp.Style = lipgloss.NewStyle().Foreground(ColPink).Bold(true)
	return PurgeFlowModel{
		ctx:     ctx,
		deps:    deps,
		state:   purgeStateScanning,
		spinner: sp,
	}
}

func (m PurgeFlowModel) Init() tea.Cmd {
	return tea.Batch(sparkleTick(), m.spinner.Tick, m.startLoad())
}

func (m PurgeFlowModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil
	case sparkleTickMsg:
		m.sparkleIdx++
		return m, sparkleTick()
	case spinner.TickMsg:
		return m.handleSpinner(msg)
	case purgeLoadedMsg:
		return m.handleLoaded(msg)
	case purgeDoneMsg:
		return m.handleDone(msg)
	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m PurgeFlowModel) handleSpinner(msg spinner.TickMsg) (tea.Model, tea.Cmd) {
	if m.state != purgeStateScanning && m.state != purgeStateDeleting {
		return m, nil
	}
	var cmd tea.Cmd
	m.spinner, cmd = m.spinner.Update(msg)
	return m, cmd
}

func (m PurgeFlowModel) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// ctrl+c is honored in every state so the user can force-quit even
	// while a scan or delete is in flight. Without this, HubProgram
	// delegates every key to the active flow and the user has no way
	// out until the background operation completes.
	if msg.String() == "ctrl+c" {
		return m, tea.Quit
	}
	if m.state != purgeStateConfirm {
		return m, nil
	}
	return m.handleConfirmKey(msg)
}

func (m PurgeFlowModel) handleConfirmKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "ctrl+c" || msg.String() == "esc" {
		return m, flowExitCmd("purge · cancelled", true)
	}
	next, cmd := m.confirmModel.Update(msg)
	m.confirmModel = next.(ChoiceModel)
	if msg.String() != "enter" {
		return m, cmd
	}
	if m.confirmModel.Value() != "yes" {
		return m, flowExitCmd("purge · cancelled", true)
	}
	m.state = purgeStateDeleting
	return m, tea.Batch(m.spinner.Tick, m.startDelete())
}

func (m PurgeFlowModel) startLoad() tea.Cmd {
	return func() tea.Msg {
		if m.deps.Discover == nil {
			return purgeLoadedMsg{err: fmt.Errorf("purge flow is not configured")}
		}
		skills, err := m.deps.Discover(m.ctx)
		return purgeLoadedMsg{skills: skills, err: err}
	}
}

func (m PurgeFlowModel) startDelete() tea.Cmd {
	return func() tea.Msg {
		if m.deps.Delete == nil {
			return purgeDoneMsg{err: fmt.Errorf("purge flow is not configured")}
		}
		deleted, failed, err := m.deps.Delete(m.ctx, m.skills)
		return purgeDoneMsg{deleted: deleted, failed: failed, err: err}
	}
}

func (m PurgeFlowModel) handleLoaded(msg purgeLoadedMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		return m, flowExitCmd("✗ purge: "+flattenErr(msg.err), false)
	}
	if len(msg.skills) == 0 {
		return m, flowExitCmd("purge · nothing to delete", true)
	}
	m.skills = msg.skills
	m.confirmModel = newFlowConfirm(
		fmt.Sprintf("Delete %d local skill folder(s)?", len(msg.skills)),
		purgeConfirmPrompt(msg.skills),
		"Yes, delete them",
	)
	m.state = purgeStateConfirm
	return m, nil
}

func (m PurgeFlowModel) handleDone(msg purgeDoneMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		return m, flowExitCmd("✗ purge: "+flattenErr(msg.err), false)
	}
	if msg.failed > 0 {
		return m, flowExitCmd(
			fmt.Sprintf("✗ purge · removed %d, %d failed", msg.deleted, msg.failed),
			false,
		)
	}
	return m, flowExitCmd(
		fmt.Sprintf("✓ purged %d local skill folder(s)", msg.deleted),
		true,
	)
}

// purgeConfirmPrompt builds the descriptive prompt body, listing the
// per-source breakdown so the user sees concretely what they're about
// to remove.
func purgeConfirmPrompt(skills []scan.Skill) string {
	bySource := map[string]int{}
	for _, sk := range skills {
		label := sk.Source
		if label == "" {
			label = "(unknown)"
		}
		bySource[label]++
	}
	labels := make([]string, 0, len(bySource))
	for k := range bySource {
		labels = append(labels, k)
	}
	sort.Strings(labels)
	lines := []string{
		"Removes every local SKILL.md folder discovered under your known dot-folders.",
		"The registry repo is not touched.",
		"",
		"Breakdown:",
	}
	for _, src := range labels {
		lines = append(lines, fmt.Sprintf("  · %s (%d folder(s))", src, bySource[src]))
	}
	return strings.Join(lines, "\n")
}

func (m PurgeFlowModel) View() string {
	return flowFrame("Skills Registry · Purge Local", m.width, m.sparkleIdx, m.renderBody(), m.renderFooter())
}

func (m PurgeFlowModel) renderBody() string {
	switch m.state {
	case purgeStateScanning:
		return m.spinner.View() + " " + lipgloss.NewStyle().Foreground(ColInk).
			Render("Scanning local dot-folders for skill copies …")
	case purgeStateConfirm:
		return m.confirmModel.View()
	case purgeStateDeleting:
		return m.spinner.View() + " " + lipgloss.NewStyle().Foreground(ColInk).
			Render(fmt.Sprintf("Deleting %d local skill folder(s) …", len(m.skills)))
	}
	return ""
}

func (m PurgeFlowModel) renderFooter() string {
	switch m.state {
	case purgeStateConfirm:
		return flowFooter(m.width, m.sparkleIdx, []flowKey{{"↑/↓", "choose"}, {"enter", "confirm"}, {"esc", "cancel"}})
	default:
		return flowFooter(m.width, m.sparkleIdx, []flowKey{{"wait", "working"}})
	}
}
