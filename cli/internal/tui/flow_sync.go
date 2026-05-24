package tui

import (
	"context"
	"fmt"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/anand-92/skills-registry/cli/internal/scan"
)

type SyncFlowDeps struct {
	Discover func(context.Context) ([]scan.Skill, error)
	Slugs    func(context.Context) (map[string]struct{}, error)
	Files    func(scan.Skill) (map[string][]byte, error)
	Publish  func(context.Context, string, map[string][]byte, string) (sha string, err error)
}

type syncFlowState int

const (
	syncStateScanning syncFlowState = iota
	syncStateSelect
	syncStateConfirm
	syncStatePublishing
)

type SyncFlowModel struct {
	ctx  context.Context
	repo string
	deps SyncFlowDeps

	state        syncFlowState
	selectModel  MultiSelectModel
	confirmModel ChoiceModel
	spinner      spinner.Model

	width, height int
	sparkleIdx    int
	missing       []scan.Skill
	picked        []scan.Skill
}

type syncLoadedMsg struct {
	missing []scan.Skill
	err     error
}

type syncPublishedMsg struct {
	pushed []string
	err    error
}

func NewSyncFlow(ctx context.Context, repo string, deps SyncFlowDeps) SyncFlowModel {
	sp := spinner.New()
	sp.Spinner = spinner.Points
	sp.Style = lipgloss.NewStyle().Foreground(ColPink).Bold(true)
	return SyncFlowModel{
		ctx:     ctx,
		repo:    repo,
		deps:    deps,
		state:   syncStateScanning,
		spinner: sp,
	}
}

func (m SyncFlowModel) Init() tea.Cmd {
	return tea.Batch(sparkleTick(), m.spinner.Tick, m.startLoad())
}

func (m SyncFlowModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil
	case sparkleTickMsg:
		m.sparkleIdx++
		return m, sparkleTick()
	case spinner.TickMsg:
		return m.handleSpinner(msg)
	case syncLoadedMsg:
		return m.handleLoaded(msg)
	case syncPublishedMsg:
		return m.handlePublished(msg)
	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m SyncFlowModel) handleSpinner(msg spinner.TickMsg) (tea.Model, tea.Cmd) {
	if m.state != syncStateScanning && m.state != syncStatePublishing {
		return m, nil
	}
	var cmd tea.Cmd
	m.spinner, cmd = m.spinner.Update(msg)
	return m, cmd
}

func (m SyncFlowModel) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.state {
	case syncStateSelect:
		return m.handleSelectKey(msg)
	case syncStateConfirm:
		return m.handleConfirmKey(msg)
	}
	return m, nil
}

func (m SyncFlowModel) handleSelectKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "esc":
		return m, flowExitCmd("sync · cancelled", true)
	case "enter":
		values := m.selectModel.SelectedValues()
		if len(values) == 0 {
			return m, flowExitCmd("sync · nothing selected", true)
		}
		m.picked = valuesToSkills(values)
		m.confirmModel = newFlowConfirm(
			fmt.Sprintf("Push %d skill(s) to %s?", len(m.picked), m.repo),
			"Only the registry repo is updated.",
			"Yes, push",
		)
		m.state = syncStateConfirm
		return m, nil
	}
	next, cmd := m.selectModel.Update(msg)
	m.selectModel = next.(MultiSelectModel)
	return m, cmd
}

func (m SyncFlowModel) handleConfirmKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "ctrl+c" || msg.String() == "esc" {
		return m, flowExitCmd("sync · cancelled", true)
	}
	next, cmd := m.confirmModel.Update(msg)
	m.confirmModel = next.(ChoiceModel)
	if msg.String() != "enter" {
		return m, cmd
	}
	if m.confirmModel.Value() != "yes" {
		return m, flowExitCmd("sync · cancelled", true)
	}
	m.state = syncStatePublishing
	return m, tea.Batch(m.spinner.Tick, m.startPublish())
}

func (m SyncFlowModel) startLoad() tea.Cmd {
	return func() tea.Msg {
		if m.deps.Discover == nil || m.deps.Slugs == nil {
			return syncLoadedMsg{err: fmt.Errorf("sync flow is not configured")}
		}
		local, err := m.deps.Discover(m.ctx)
		if err != nil {
			return syncLoadedMsg{err: err}
		}
		remote, err := m.deps.Slugs(m.ctx)
		if err != nil {
			return syncLoadedMsg{err: err}
		}
		return syncLoadedMsg{missing: scan.DedupeAgainst(local, remote)}
	}
}

func (m SyncFlowModel) handleLoaded(msg syncLoadedMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		return m, flowExitCmd("✗ sync: "+flattenErr(msg.err), false)
	}
	if len(msg.missing) == 0 {
		return m, flowExitCmd("sync · already in sync", true)
	}
	m.missing = msg.missing
	m.selectModel = NewMultiSelect(
		fmt.Sprintf("Found %d local skill(s) missing from the registry — pick which to push", len(msg.missing)),
		skillsToItems(msg.missing),
		nil,
		true,
	)
	m.state = syncStateSelect
	return m, nil
}

func (m SyncFlowModel) startPublish() tea.Cmd {
	return func() tea.Msg {
		pushed, err := publishSkillSet(m.ctx, m.deps.Files, m.deps.Publish, m.picked, func(slug string) string {
			return fmt.Sprintf("sync: %s", slug)
		})
		return syncPublishedMsg{pushed: pushed, err: err}
	}
}

func (m SyncFlowModel) handlePublished(msg syncPublishedMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		return m, flowExitCmd("✗ sync: "+flattenErr(msg.err), false)
	}
	return m, flowExitCmd(fmt.Sprintf("✓ synced %d skill(s)", len(msg.pushed)), true)
}

func (m SyncFlowModel) View() string {
	return flowFrame("Skills Registry · Sync", m.width, m.sparkleIdx, m.renderBody(), m.renderFooter())
}

func (m SyncFlowModel) renderBody() string {
	switch m.state {
	case syncStateScanning:
		return m.spinner.View() + " " + lipgloss.NewStyle().Foreground(ColInk).
			Render("Scanning local dot-folders and registry slugs …")
	case syncStateSelect:
		return m.selectModel.View()
	case syncStateConfirm:
		return m.confirmModel.View()
	case syncStatePublishing:
		return m.spinner.View() + " " + lipgloss.NewStyle().Foreground(ColInk).
			Render(fmt.Sprintf("Pushing %d skill(s) to %s …", len(m.picked), m.repo))
	}
	return ""
}

func (m SyncFlowModel) renderFooter() string {
	switch m.state {
	case syncStateSelect:
		return flowFooter(m.width, m.sparkleIdx, []flowKey{{"space", "toggle"}, {"tab", "select all"}, {"enter", "continue"}, {"esc", "cancel"}})
	case syncStateConfirm:
		return flowFooter(m.width, m.sparkleIdx, []flowKey{{"↑/↓", "choose"}, {"enter", "confirm"}, {"esc", "cancel"}})
	default:
		return flowFooter(m.width, m.sparkleIdx, []flowKey{{"wait", "working"}})
	}
}
