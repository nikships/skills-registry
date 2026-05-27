package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/anand-92/skills-registry/cli/internal/scan"
)

type AddFlowDeps struct {
	Resolve        func(context.Context, string) (dir string, cleanup func(), err error)
	Discover       func(dir, label string) ([]scan.Skill, error)
	Slugs          func(context.Context) (map[string]struct{}, error)
	Files          func(scan.Skill) (map[string][]byte, error)
	Publish        func(context.Context, string, map[string][]byte, string) (sha string, err error)
	InstallTargets InstallTargetLoader
	Install        func(ctx context.Context, slug string, targets []any) ([]string, error)
}

type addFlowState int

const (
	addStateSource addFlowState = iota
	addStateLoading
	addStateSelect
	addStateInstall
	addStateConfirm
	addStatePublishing
)

type AddFlowModel struct {
	ctx  context.Context
	repo string
	deps AddFlowDeps

	state        addFlowState
	source       InputModel
	selectModel  MultiSelectModel
	installModel InstallPickerModel
	confirmModel ChoiceModel
	spinner      spinner.Model

	width, height int
	sparkleIdx    int
	sourceText    string
	skills        []scan.Skill
	picked        []scan.Skill
	targets       []any
	skipped       []string
	cleanupFn     func()
}

type addLoadedMsg struct {
	skills  []scan.Skill
	skipped []string
	cleanup func()
	err     error
}

type addPublishedMsg struct {
	pushed    []string
	installed map[string][]string
	err       error
}

func NewAddFlow(ctx context.Context, repo string, deps AddFlowDeps) AddFlowModel {
	sp := spinner.New()
	sp.Spinner = spinner.Points
	sp.Style = lipgloss.NewStyle().Foreground(ColPink).Bold(true)
	input := NewInput("Add skills", "", "owner/repo, git URL, or local path", "")
	input.Help = "enter to scan · esc to cancel"
	return AddFlowModel{
		ctx:     ctx,
		repo:    repo,
		deps:    deps,
		state:   addStateSource,
		source:  input,
		spinner: sp,
	}
}

func (m AddFlowModel) Init() tea.Cmd {
	return tea.Batch(sparkleTick(), m.source.Init())
}

func (m AddFlowModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil
	case sparkleTickMsg:
		m.sparkleIdx++
		return m, sparkleTick()
	case spinner.TickMsg:
		return m.handleSpinner(msg)
	case addLoadedMsg:
		return m.handleLoaded(msg)
	case addPublishedMsg:
		return m.handlePublished(msg)
	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m.forward(msg)
}

func (m AddFlowModel) handleSpinner(msg spinner.TickMsg) (tea.Model, tea.Cmd) {
	if m.state != addStateLoading && m.state != addStatePublishing {
		return m, nil
	}
	var cmd tea.Cmd
	m.spinner, cmd = m.spinner.Update(msg)
	return m, cmd
}

func (m AddFlowModel) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.state {
	case addStateSource:
		return m.handleSourceKey(msg)
	case addStateSelect:
		return m.handleSelectKey(msg)
	case addStateInstall:
		return m.handleInstallKey(msg)
	case addStateConfirm:
		return m.handleConfirmKey(msg)
	}
	return m, nil
}

func (m AddFlowModel) handleSourceKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "esc":
		return m.exit("add · cancelled", true)
	case "enter":
		source := strings.TrimSpace(m.source.Value())
		if source == "" {
			m.source.err = fmt.Errorf("source is required")
			return m, nil
		}
		if err := validateFlowSourceInput(source); err != nil {
			m.source.err = err
			return m, nil
		}
		m.sourceText = redactSourceUserInfo(source)
		m.state = addStateLoading
		return m, tea.Batch(m.spinner.Tick, m.startLoad(source))
	}
	var cmd tea.Cmd
	next, cmd := m.source.Update(msg)
	m.source = next.(InputModel)
	return m, cmd
}

func (m AddFlowModel) handleSelectKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "esc":
		return m.exit("add · cancelled", true)
	case "enter":
		values := m.selectModel.SelectedValues()
		if len(values) == 0 {
			return m.exit("add · nothing selected", true)
		}
		m.picked = valuesToSkills(values)
		return m.openInstallStep()
	}
	next, cmd := m.selectModel.Update(msg)
	m.selectModel = next.(MultiSelectModel)
	return m, cmd
}

// openInstallStep advances the wizard into the agent picker. The
// picker is built from deps.InstallTargets() if available; otherwise
// we skip the step (e.g. unit tests that omit the loader) and fall
// straight through to the confirmation panel.
func (m AddFlowModel) openInstallStep() (tea.Model, tea.Cmd) {
	if m.deps.InstallTargets == nil || m.deps.Install == nil {
		return m.openConfirm()
	}
	targets := m.deps.InstallTargets()
	if len(targets) == 0 {
		return m.openConfirm()
	}
	subtitle := fmt.Sprintf("%d skill(s) staged", len(m.picked))
	m.installModel = NewInstallPicker("Install locally into which agents?", subtitle, targets)
	m.state = addStateInstall
	return m, nil
}

func (m AddFlowModel) handleInstallKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	next, _ := m.installModel.Update(msg)
	m.installModel = next.(InstallPickerModel)
	if m.installModel.Cancelled() {
		return m.exit("add · cancelled", true)
	}
	if !m.installModel.Done() {
		return m, nil
	}
	m.targets = m.installModel.SelectedValues()
	return m.openConfirm()
}

func (m AddFlowModel) openConfirm() (tea.Model, tea.Cmd) {
	subtitle := "Only the registry repo is updated; selected agents get a local install."
	if len(m.targets) == 0 {
		subtitle = "Only the registry repo is updated. No local install (no agents selected)."
	}
	m.confirmModel = newFlowConfirm(
		fmt.Sprintf("Publish %d skill(s) from %s to %s?", len(m.picked), m.sourceText, m.repo),
		subtitle,
		"Yes, publish",
		"Continue with the registry write",
	)
	m.state = addStateConfirm
	return m, nil
}

func (m AddFlowModel) handleConfirmKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "ctrl+c" || msg.String() == "esc" {
		return m.exit("add · cancelled", true)
	}
	next, cmd := m.confirmModel.Update(msg)
	m.confirmModel = next.(ChoiceModel)
	if msg.String() != "enter" {
		return m, cmd
	}
	if m.confirmModel.Value() != "yes" {
		return m.exit("add · cancelled", true)
	}
	m.state = addStatePublishing
	return m, tea.Batch(m.spinner.Tick, m.startPublish())
}

func (m AddFlowModel) forward(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.state == addStateSource {
		var cmd tea.Cmd
		next, cmd := m.source.Update(msg)
		m.source = next.(InputModel)
		return m, cmd
	}
	return m, nil
}

func (m AddFlowModel) startLoad(source string) tea.Cmd {
	return func() tea.Msg {
		return runAddLoad(m.ctx, m.deps, source)
	}
}

func runAddLoad(ctx context.Context, deps AddFlowDeps, source string) addLoadedMsg {
	if deps.Resolve == nil || deps.Discover == nil || deps.Slugs == nil {
		return addLoadedMsg{err: fmt.Errorf("add flow is not configured")}
	}
	dir, cleanup, err := deps.Resolve(ctx, source)
	if cleanup == nil {
		cleanup = func() {}
	}
	if err != nil {
		cleanup()
		return addLoadedMsg{err: err}
	}
	skills, err := deps.Discover(dir, source)
	if err != nil {
		cleanup()
		return addLoadedMsg{err: err}
	}
	if len(skills) == 0 {
		cleanup()
		return addLoadedMsg{err: fmt.Errorf("no SKILL.md files found under %s", source)}
	}
	existing, err := deps.Slugs(ctx)
	if err != nil {
		cleanup()
		return addLoadedMsg{err: err}
	}
	publishable, skipped := filterExisting(skills, existing)
	return addLoadedMsg{skills: publishable, skipped: skipped, cleanup: cleanup}
}

func (m AddFlowModel) handleLoaded(msg addLoadedMsg) (tea.Model, tea.Cmd) {
	m.cleanupFn = msg.cleanup
	if msg.err != nil {
		return m.exit("✗ add: "+flattenErr(msg.err), false)
	}
	m.skills = msg.skills
	m.skipped = msg.skipped
	if len(msg.skills) == 0 {
		return m.exit("add · nothing new to publish", true)
	}
	m.selectModel = NewMultiSelect("Select skills to publish", skillsToItems(msg.skills), nil, true)
	m.state = addStateSelect
	return m, nil
}

func (m AddFlowModel) startPublish() tea.Cmd {
	picked := m.picked
	targets := m.targets
	ctx := m.ctx
	deps := m.deps
	source := m.sourceText
	return func() tea.Msg {
		pushed, err := publishSkillSet(ctx, deps.Files, deps.Publish, picked, func(slug string) string {
			return fmt.Sprintf("add: %s (from %s)", slug, source)
		})
		if err != nil {
			return addPublishedMsg{pushed: pushed, err: err}
		}
		installed := map[string][]string{}
		if deps.Install != nil && len(targets) > 0 {
			for _, slug := range pushed {
				paths, ierr := deps.Install(ctx, slug, targets)
				if ierr != nil {
					return addPublishedMsg{
						pushed:    pushed,
						installed: installed,
						err:       fmt.Errorf("install %s locally: %w", slug, ierr),
					}
				}
				installed[slug] = paths
			}
		}
		return addPublishedMsg{pushed: pushed, installed: installed}
	}
}

func (m AddFlowModel) handlePublished(msg addPublishedMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		return m.exit("✗ add: "+flattenErr(msg.err), false)
	}
	if len(msg.installed) > 0 {
		return m.exit(fmt.Sprintf("✓ added %d skill(s) from %s · installed locally", len(msg.pushed), m.sourceText), true)
	}
	return m.exit(fmt.Sprintf("✓ added %d skill(s) from %s", len(msg.pushed), m.sourceText), true)
}

func (m AddFlowModel) exit(toast string, ok bool) (tea.Model, tea.Cmd) {
	m.runCleanup()
	return m, flowExitCmd(toast, ok)
}

func (m *AddFlowModel) runCleanup() {
	if m.cleanupFn == nil {
		return
	}
	m.cleanupFn()
	m.cleanupFn = nil
}

func (m AddFlowModel) View() string {
	return flowFrame("Skills Registry · Add", m.width, m.sparkleIdx, m.renderBody(), m.renderFooter())
}

func (m AddFlowModel) renderBody() string {
	switch m.state {
	case addStateSource:
		return m.source.View()
	case addStateLoading:
		return m.spinner.View() + " " + lipgloss.NewStyle().Foreground(ColInk).
			Render("Resolving and scanning "+m.sourceText+" …")
	case addStateSelect:
		return m.selectModel.View()
	case addStateInstall:
		return m.installModel.View()
	case addStateConfirm:
		return m.confirmModel.View()
	case addStatePublishing:
		return m.spinner.View() + " " + lipgloss.NewStyle().Foreground(ColInk).
			Render(fmt.Sprintf("Publishing %d skill(s) to %s …", len(m.picked), m.repo))
	}
	return ""
}

func (m AddFlowModel) renderFooter() string {
	switch m.state {
	case addStateSource:
		return flowFooter(m.width, m.sparkleIdx, []flowKey{{"type", "source"}, {"enter", "scan"}, {"esc", "cancel"}})
	case addStateSelect:
		return flowFooter(m.width, m.sparkleIdx, []flowKey{{"space", "toggle"}, {"tab", "select all"}, {"enter", "continue"}, {"esc", "cancel"}})
	case addStateInstall:
		return flowFooter(m.width, m.sparkleIdx, []flowKey{{"space", "toggle"}, {"tab", "select all"}, {"enter", "continue"}, {"esc", "cancel"}})
	case addStateConfirm:
		return flowFooter(m.width, m.sparkleIdx, []flowKey{{"↑/↓", "choose"}, {"enter", "confirm"}, {"esc", "cancel"}})
	default:
		return flowFooter(m.width, m.sparkleIdx, []flowKey{{"wait", "working"}})
	}
}
