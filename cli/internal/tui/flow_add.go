package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/nikships/skills-registry/cli/internal/scan"
)

type AddFlowDeps struct {
	Resolve        func(context.Context, string) (dir string, cleanup func(), err error)
	Discover       func(dir, label string) ([]scan.Skill, error)
	Slugs          func(context.Context) (map[string]struct{}, error)
	Files          func(scan.Skill) (map[string][]byte, error)
	Publish        func(context.Context, string, map[string][]byte, string) (sha string, err error)
	InstallTargets InstallTargetLoader
	Install        func(ctx context.Context, slug string, targets []any) ([]string, error)
	// Gate classifies the typed source and reviews what was found inside it.
	// The hub shares the CLI's gate through this hook rather than reasoning
	// about URLs itself, so a public folder URL pasted into the hub is held to
	// the same rules as `skills-registry add <url>`. When nil, the flow treats
	// every source as trusted, which is the behavior unit tests that omit the
	// hook expect.
	//
	// `dir` is the directory the source resolved into, so the hook can also
	// annotate the fetched copies once it has reviewed them. It reviews before
	// it annotates, which is why the two are one hook rather than two.
	Gate func(ctx context.Context, source, dir string, skills []scan.Skill) (ImportGate, error)
}

// ImportGate is the hub's view of the import gate for one source: whether the
// origin is untrusted, the public index's grades, and what a local scan of the
// fetched SKILL.md files matched.
//
// It is a plain data struct so the presentation layer never re-derives a
// verdict. In particular ScoreLines is pre-rendered by the gate, which is what
// guarantees an absent grade reads as "unscored" here too.
type ImportGate struct {
	// Untrusted reports whether the gate applies. False leaves the flow's
	// original behavior untouched.
	Untrusted bool
	// Reason is a short justification, e.g. "a public GitHub repository owned
	// by openclaw".
	Reason string
	// ScoreLines are the index's grades, one per line, already labelled.
	ScoreLines []string
	// Indexed reports whether the index had a row at all.
	Indexed bool
	// Findings are the local scan's hits, pre-rendered one per line.
	Findings []string
	// BlockSummary is non-empty when something needs explicit consent (a Poor
	// safety grade, or a scan hit).
	BlockSummary string
	// Disclaimer states what the local scan is and is not worth.
	Disclaimer string
}

// Blocked reports whether this gate needs the extra consent step.
func (g ImportGate) Blocked() bool { return g.BlockSummary != "" }

type addFlowState int

const (
	addStateSource addFlowState = iota
	addStateLoading
	addStateSelect
	addStateGate
	addStateInstallOptIn
	addStateInstall
	addStateConfirm
	addStatePublishing
)

// addFlowExit is one add flow's outcome, handed to a parent flow that embeds
// it. `published` is authoritative: a parent must not infer "something was
// written" from the toast's wording.
type addFlowExit struct {
	toast     string
	ok        bool
	published int
}

type AddFlowModel struct {
	ctx  context.Context
	repo string
	deps AddFlowDeps

	// onExit, when set, replaces the hub's flow-exit message with the one it
	// returns, so a flow that embeds this one (the Discover picker) learns the
	// outcome instead of the hub closing both at once.
	onExit func(addFlowExit) tea.Msg
	// presetSource is a source chosen elsewhere, which skips the input step.
	presetSource string
	// published counts the skills actually pushed, reported through onExit.
	published int

	state        addFlowState
	source       InputModel
	selectModel  MultiSelectModel
	installModel InstallPickerModel
	confirmModel ChoiceModel
	gateModel    ChoiceModel
	optInModel   ChoiceModel
	spinner      spinner.Model

	width, height int
	sparkleIdx    int
	sourceText    string
	skills        []scan.Skill
	picked        []scan.Skill
	targets       []any
	skipped       []string
	gate          ImportGate
	cleanupFn     func()
}

type addLoadedMsg struct {
	skills  []scan.Skill
	skipped []string
	gate    ImportGate
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

// NewAddFlowFromSource builds an add flow for a source the user already chose
// elsewhere, so the flow opens on the resolve spinner rather than on an input
// box asking for a source they just picked.
func NewAddFlowFromSource(ctx context.Context, repo string, deps AddFlowDeps, source string) AddFlowModel {
	m := NewAddFlow(ctx, repo, deps)
	m.presetSource = source
	m.sourceText = redactSourceUserInfo(source)
	m.state = addStateLoading
	return m
}

// WithOnExit wires the embedded ending. Leave it unset for the hub, which
// consumes the default flow-exit message.
func (m AddFlowModel) WithOnExit(fn func(addFlowExit) tea.Msg) AddFlowModel {
	m.onExit = fn
	return m
}

func (m AddFlowModel) Init() tea.Cmd {
	if m.presetSource != "" {
		return tea.Batch(sparkleTick(), m.spinner.Tick, m.startLoad(m.presetSource))
	}
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
	case addStateGate:
		return m.handleGateKey(msg)
	case addStateInstallOptIn:
		return m.handleOptInKey(msg)
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
		return m.afterSelect()
	}
	next, cmd := m.selectModel.Update(msg)
	m.selectModel = next.(MultiSelectModel)
	return m, cmd
}

// afterSelect routes past the multi-select. An untrusted source gets the
// warning step first (when something is blocked) and then the install opt-in;
// a trusted one goes straight to the agent picker, as before.
func (m AddFlowModel) afterSelect() (tea.Model, tea.Cmd) {
	if !m.gate.Untrusted {
		return m.openInstallStep()
	}
	if m.gate.Blocked() {
		m.gateModel = NewChoice(
			"Import despite the warning?",
			m.gate.BlockSummary,
			[]Choice{
				{Value: "no", Label: "No, cancel the import", Hint: "Make no changes"},
				{Value: "yes", Label: "Yes, import anyway", Hint: "Publish to your registry"},
			})
		m.state = addStateGate
		return m, nil
	}
	return m.openInstallOptIn()
}

func (m AddFlowModel) handleGateKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "ctrl+c" || msg.String() == "esc" {
		return m.exit("add · cancelled", true)
	}
	next, cmd := m.gateModel.Update(msg)
	m.gateModel = next.(ChoiceModel)
	if msg.String() != "enter" {
		return m, cmd
	}
	if m.gateModel.Value() != "yes" {
		return m.exit("add · cancelled", true)
	}
	return m.openInstallOptIn()
}

// openInstallOptIn asks whether an untrusted import should also be installed
// into agent folders. The default answer is no: publishing is recoverable,
// while an install makes every agent load the file each session.
func (m AddFlowModel) openInstallOptIn() (tea.Model, tea.Cmd) {
	if m.deps.InstallTargets == nil || m.deps.Install == nil {
		return m.openConfirm()
	}
	m.optInModel = NewChoice(
		fmt.Sprintf("Also install %d untrusted skill(s) into agent folders?", len(m.picked)),
		"Every agent then loads this SKILL.md each session. The registry write happens either way.",
		[]Choice{
			{Value: "no", Label: "No, registry only (recommended)", Hint: "Nothing is written to disk"},
			{Value: "yes", Label: "Yes, choose agent folders", Hint: "Opens the agent picker"},
		})
	m.state = addStateInstallOptIn
	return m, nil
}

func (m AddFlowModel) handleOptInKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "ctrl+c" || msg.String() == "esc" {
		return m.exit("add · cancelled", true)
	}
	next, cmd := m.optInModel.Update(msg)
	m.optInModel = next.(ChoiceModel)
	if msg.String() != "enter" {
		return m, cmd
	}
	if m.optInModel.Value() != "yes" {
		m.targets = nil
		return m.openConfirm()
	}
	return m.openInstallStep()
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
	if m.gate.Untrusted {
		subtitle += " Nothing under scripts/ is ever run."
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
	out := addLoadedMsg{skills: publishable, skipped: skipped, cleanup: cleanup}
	if deps.Gate != nil {
		gate, gerr := deps.Gate(ctx, source, dir, publishable)
		if gerr != nil {
			cleanup()
			return addLoadedMsg{err: gerr}
		}
		out.gate = gate
	}
	return out
}

func (m AddFlowModel) handleLoaded(msg addLoadedMsg) (tea.Model, tea.Cmd) {
	m.cleanupFn = msg.cleanup
	if msg.err != nil {
		return m.exit("✗ add: "+flattenErr(msg.err), false)
	}
	m.skills = msg.skills
	m.skipped = msg.skipped
	m.gate = msg.gate
	if len(msg.skills) == 0 {
		return m.exit("add · nothing new to publish", true)
	}
	title := "Select skills to publish"
	if m.gate.Untrusted {
		title = "Untrusted source — select skills to publish to your registry"
	}
	m.selectModel = NewMultiSelect(title, skillsToItems(msg.skills), nil, true)
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
	m.published = len(msg.pushed)
	if len(msg.installed) > 0 {
		return m.exit(fmt.Sprintf("✓ added %d skill(s) from %s · installed locally", len(msg.pushed), m.sourceText), true)
	}
	return m.exit(fmt.Sprintf("✓ added %d skill(s) from %s", len(msg.pushed), m.sourceText), true)
}

func (m AddFlowModel) exit(toast string, ok bool) (tea.Model, tea.Cmd) {
	m.runCleanup()
	if m.onExit == nil {
		return m, flowExitCmd(toast, ok)
	}
	exit := addFlowExit{toast: toast, ok: ok, published: m.published}
	fn := m.onExit
	return m, func() tea.Msg { return fn(exit) }
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
		return m.renderGate() + m.selectModel.View()
	case addStateGate:
		return m.renderGate() + m.gateModel.View()
	case addStateInstallOptIn:
		return m.optInModel.View()
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

// renderGate renders the untrusted-source banner shown above the select and
// warning steps: the origin, the index's grades (an absent one already
// labelled), the scan's findings, and what the scan is worth. Empty for a
// trusted source.
func (m AddFlowModel) renderGate() string {
	if !m.gate.Untrusted {
		return ""
	}
	var b strings.Builder
	b.WriteString(WarnStyle.Render("!  Untrusted source"))
	if m.gate.Reason != "" {
		b.WriteString(HintStyle.Render(" — " + m.gate.Reason))
	}
	b.WriteString("\n")
	for _, line := range m.gate.ScoreLines {
		b.WriteString("   " + line + "\n")
	}
	if !m.gate.Indexed {
		b.WriteString(HintStyle.Render("   (not in the public index; unscored means unvetted, not safe)") + "\n")
	}
	for _, f := range m.gate.Findings {
		b.WriteString(WarnStyle.Render("   · "+f) + "\n")
	}
	if m.gate.Disclaimer != "" {
		b.WriteString(HintStyle.Render("   "+m.gate.Disclaimer) + "\n")
	}
	b.WriteString("\n")
	return b.String()
}

func (m AddFlowModel) renderFooter() string {
	switch m.state {
	case addStateSource:
		return flowFooter(m.width, m.sparkleIdx, []flowKey{{"type", "source"}, {"enter", "scan"}, {"esc", "cancel"}})
	case addStateSelect, addStateInstall:
		return flowFooter(m.width, m.sparkleIdx, []flowKey{{"space", "toggle"}, {"tab", "select all"}, {"enter", "continue"}, {"esc", "cancel"}})
	case addStateConfirm, addStateGate, addStateInstallOptIn:
		return flowFooter(m.width, m.sparkleIdx, []flowKey{{"↑/↓", "choose"}, {"enter", "confirm"}, {"esc", "cancel"}})
	default:
		return flowFooter(m.width, m.sparkleIdx, []flowKey{{"wait", "working"}})
	}
}
