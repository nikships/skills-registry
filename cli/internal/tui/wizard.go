package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/anand-92/skills-registry/cli/internal/scan"
)

// ────────────────────────────────────────────────────────────────────────────
// WizardStep — the 8-step onboarding state machine
// ────────────────────────────────────────────────────────────────────────────

// WizardStep enumerates the eight onboarding stages the wizard walks the user
// through. The order matches the legacy bootstrap CLI flow, so a user who
// runs `skills-registry` for the first time sees the same sequence — just
// inside an alt-screen Bubble Tea frame instead of a series of synchronous
// prompts.
type WizardStep int

const (
	// WizardStepScan discovers local skill folders via deps.Scan.
	WizardStepScan WizardStep = iota
	// WizardStepRepoName collects the GitHub repository name.
	WizardStepRepoName
	// WizardStepVisibility picks between public and private visibility.
	WizardStepVisibility
	// WizardStepPush uploads every local skill in a batched git push.
	WizardStepPush
	// WizardStepAgentSelect multi-selects the agent dot-folders to seed
	// with the skills-registry SKILL.md.
	WizardStepAgentSelect
	// WizardStepCleanup offers to delete the now-redundant local copies.
	WizardStepCleanup
	// WizardStepMCPConnect prints the hosted-MCP JSON snippet for the
	// user to paste into their client config. The CLI never installs or
	// boots an MCP server — this step is purely informational.
	WizardStepMCPConnect
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
	case WizardStepMCPConnect:
		return "Connect MCP client"
	case WizardStepDone:
		return "All set!"
	}
	return "Unknown"
}

// ────────────────────────────────────────────────────────────────────────────
// WizardDeps — side effects the wizard delegates to its launcher
// ────────────────────────────────────────────────────────────────────────────

// WizardDeps wires the wizard model to its launcher's IO. Each callback runs
// inside a tea.Cmd / goroutine; the wizard never touches the network, the
// filesystem, or `gh` directly. Any nil callback is treated as a no-op so
// unit tests can exercise the state machine without stubbing every dep.
type WizardDeps struct {
	// Scan discovers local skill folders (typically via scan.Discover).
	Scan func(ctx context.Context) ([]scan.Skill, error)
	// CreateRepo provisions the GitHub repo and returns "owner/name".
	CreateRepo func(ctx context.Context, name, visibility string) (string, error)
	// SaveConfig persists the resolved repo into ~/.config/skills-mcp/registry.toml.
	// Wired immediately after CreateRepo so a later push failure doesn't
	// orphan the registry.
	SaveConfig func(repo string) error
	// Push uploads every skill to repo via a single git push. onProgress
	// fires once per file as the working tree is materialized; onStatus
	// fires once per phase (e.g. "pushing to github…"). Returns the count
	// of pushed skills.
	Push func(ctx context.Context, repo string, skills []scan.Skill,
		onProgress func(done, total int), onStatus func(msg string)) (int, error)

	// AgentChoices returns the agent multi-select rows. Locked entries
	// render at the top of the panel and are always installed; the rest
	// are filterable. Values are opaque to the wizard — InstallAgents
	// receives them back verbatim.
	AgentChoices func() []WizardAgent
	// InstallAgents writes SKILL.md into the selected dot-folders and
	// returns the list of paths written.
	InstallAgents func(ctx context.Context, repo string, picked []any) ([]string, error)
	// LoadCleanup returns the local-copy entries that are safe to delete
	// now that the registry has the canonical versions. Empty slice means
	// nothing to clean — the wizard auto-advances past the cleanup step.
	LoadCleanup func(ctx context.Context, repo string, skills []scan.Skill) []WizardCleanupEntry
	// DeleteCleanup removes the supplied entries and returns the deleted /
	// failed counts. Best-effort — partial failures are surfaced via the
	// returned `failed` count, never as an error.
	DeleteCleanup func(entries []WizardCleanupEntry) (deleted, failed int)
	// MCPSnippet returns the JSON snippet to paste into mcp.json. The CLI
	// never installs an MCP server; this step is purely informational.
	MCPSnippet func() string
}

// WizardAgent is one row in the embedded agent multi-select on step 5.
type WizardAgent struct {
	Display string
	Hint    string
	Locked  bool
	Default bool
	Value   any
}

// WizardCleanupEntry is one local skill folder (or symlink) that the
// cleanup step offers to remove.
type WizardCleanupEntry struct {
	Path      string
	Source    string
	IsSymlink bool
}

// ────────────────────────────────────────────────────────────────────────────
// Messages
// ────────────────────────────────────────────────────────────────────────────

// wizardTransitionMsg fires after the inter-step animation delay so the
// renderer can swap panels with a visible beat instead of a hard cut.
type wizardTransitionMsg struct{ to WizardStep }

// wizardScanDoneMsg lands when the deps.Scan goroutine finishes.
type wizardScanDoneMsg struct {
	skills []scan.Skill
	err    error
}

// wizardScanRevealMsg paces the "found N skills" animated counter.
type wizardScanRevealMsg struct{}

// wizardPushProgressMsg surfaces a single progress / status tick from
// the push goroutine.
type wizardPushProgressMsg struct {
	done, total int
	status      string
}

// wizardPushDoneMsg is the terminal signal from the push goroutine.
type wizardPushDoneMsg struct {
	repo   string
	pushed int
	err    error
}

// wizardAgentInstallDoneMsg lands when deps.InstallAgents resolves.
type wizardAgentInstallDoneMsg struct {
	paths []string
	err   error
}

// wizardCleanupLoadedMsg lands when deps.LoadCleanup resolves with the
// set of removable local entries. Empty `entries` means "nothing to do"
// and triggers an auto-advance.
type wizardCleanupLoadedMsg struct {
	entries []WizardCleanupEntry
}

// wizardCleanupDoneMsg lands when deps.DeleteCleanup finishes.
type wizardCleanupDoneMsg struct {
	deleted int
	failed  int
}

// wizardMCPDoneMsg carries the snippet to render in step 7. The CLI
// doesn't install an MCP server, so just the JSON body is needed.
type wizardMCPDoneMsg struct {
	snippet string
}

const (
	// wizardTransitionDelay paces inter-step transitions. 180ms is short
	// enough to feel snappy but long enough that the eye registers the
	// spinner glyph in the panel — important for WIZARD-012.
	wizardTransitionDelay = 180 * time.Millisecond
	// wizardScanRevealDelay paces the animated counter when scan completes.
	// 60ms per tick gives a satisfying count-up at 30-50 skill registries
	// without dragging on much longer at 200+.
	wizardScanRevealDelay = 60 * time.Millisecond
)

// wizardTransition returns a tea.Cmd that announces a step change after the
// animation delay.
func wizardTransition(to WizardStep) tea.Cmd {
	return tea.Tick(wizardTransitionDelay, func(time.Time) tea.Msg {
		return wizardTransitionMsg{to: to}
	})
}

// wizardScanReveal returns a tea.Cmd that ticks the animated counter.
func wizardScanReveal() tea.Cmd {
	return tea.Tick(wizardScanRevealDelay, func(time.Time) tea.Msg {
		return wizardScanRevealMsg{}
	})
}

// ────────────────────────────────────────────────────────────────────────────
// Model
// ────────────────────────────────────────────────────────────────────────────

// WizardModel is the alt-screen Bubble Tea model for the onboarding wizard.
// All eight steps are wired to real business logic via WizardDeps; tests
// drive the state machine with stub deps.
type WizardModel struct {
	ctx  context.Context
	deps WizardDeps

	step          WizardStep
	width, height int

	// Animation state.
	transitioning    bool
	transitionTarget WizardStep
	spinner          spinner.Model
	sparkleIdx       int

	// Cancellation confirmation overlay.
	cancelOverlay bool
	cancelCursor  int

	// Final state inspected by the launcher after tea.Quit returns.
	cancelled bool
	completed bool

	// Step 1: Scan — populated as the scan goroutine resolves and the
	// reveal animation ticks the counter up.
	scanDone   bool
	scanErr    error
	skills     []scan.Skill
	scanReveal int

	// Step 2: RepoName — the embedded textinput owns its own cursor /
	// blink behavior; the wizard frames it with a focused panel.
	repoInput textinput.Model
	repoErr   string

	// Step 3: Visibility — visCursor is 0 (private, the safe default) or
	// 1 (public). Recorded as a stringly-typed value matching the gh API
	// flag set so the deps.CreateRepo callback doesn't need translation.
	visCursor  int
	visibility string

	// Step 4: Push — pushCh streams wizardPush* messages from the push
	// goroutine to the model. Done is set by the terminal wizardPushDoneMsg.
	pushStarted    bool
	pushDone       bool
	pushErr        error
	pushRepo       string
	pushed         int
	pushDoneFiles  int
	pushTotalFiles int
	pushStatus     string
	pushCh         chan tea.Msg

	// Step 5: Agent select — the embedded multi-select (locked-at-top +
	// filterable list). agentInstalling flips when the user hits enter
	// and the InstallAgents goroutine is in flight; agentInstallDone
	// gates advance to step 6.
	agentLoaded      bool
	agentItems       []WizardAgent
	agentFilter      string
	agentCursor      int
	agentSelected    map[int]struct{}
	agentInstalling  bool
	agentInstallDone bool
	agentInstallErr  error
	agentPaths       []string

	// Step 6: Cleanup — loaded once on entry. cleanupCursor: 0=yes (delete),
	// 1=no (keep). cleanupChosen flips when the user confirms; cleanupDone
	// when the DeleteCleanup goroutine returns.
	cleanupLoaded  bool
	cleanupEntries []WizardCleanupEntry
	cleanupCursor  int
	cleanupChosen  bool
	cleanupYes     bool
	cleanupRunning bool
	cleanupDone    bool
	cleanupDeleted int
	cleanupFailed  int

	// Step 7: Connect MCP client. Purely informational — the CLI never
	// installs or boots an MCP server. mcpStarted flips on step entry;
	// mcpDone flips immediately after rendering the snippet so the enter
	// key advances to Done.
	mcpStarted bool
	mcpDone    bool
	mcpSnippet string
}

// NewWizard constructs the wizard frame with empty WizardDeps. Callers that
// actually want the wizard to do work chain `.WithDeps(...)` to attach the
// real callbacks. Tests use the bare constructor to exercise the state
// machine without setting up the world.
func NewWizard(ctx context.Context) WizardModel {
	sp := spinner.New()
	sp.Spinner = spinner.Points
	sp.Style = lipgloss.NewStyle().Foreground(ColPink).Bold(true)
	ti := textinput.New()
	ti.Placeholder = "skills-registry"
	ti.Prompt = "› "
	ti.PromptStyle = lipgloss.NewStyle().Foreground(ColPink).Bold(true)
	ti.TextStyle = lipgloss.NewStyle().Foreground(ColInk)
	ti.Cursor.Style = lipgloss.NewStyle().Foreground(ColPrimary)
	ti.Focus()
	return WizardModel{
		ctx:           ctx,
		step:          WizardStepScan,
		spinner:       sp,
		repoInput:     ti,
		visibility:    "",
		agentSelected: map[int]struct{}{},
	}
}

// WithDeps attaches the live callbacks the wizard uses for its side
// effects. Returned as a value because Bubble Tea models are passed by
// value through Update.
func (m WizardModel) WithDeps(deps WizardDeps) WizardModel {
	m.deps = deps
	return m
}

// Init implements tea.Model. Kicks off the persistent sparkle animation
// that drives the gradient bar and footer dots, the spinner used by the
// scan/push steps, and (when deps.Scan is wired) the initial scan command.
// When no Scan dep is wired we post an empty wizardScanDoneMsg so the
// state machine still gets out of the "scanning…" state — useful for
// non-interactive smoke tests and any caller that opts out of scanning.
func (m WizardModel) Init() tea.Cmd {
	cmds := []tea.Cmd{sparkleTick(), m.spinner.Tick}
	if m.deps.Scan != nil {
		cmds = append(cmds, m.startScan())
	} else {
		cmds = append(cmds, func() tea.Msg { return wizardScanDoneMsg{} })
	}
	return tea.Batch(cmds...)
}

// Step returns the current step. Exposed for tests and for callers that want
// to inspect progress mid-flow.
func (m WizardModel) Step() WizardStep { return m.step }

// Cancelled reports whether the user confirmed the Esc-cancel overlay or
// pressed Ctrl+C.
func (m WizardModel) Cancelled() bool { return m.cancelled }

// Completed reports whether the wizard reached the Done step and the user
// pressed enter.
func (m WizardModel) Completed() bool { return m.completed }

// Repo returns the resolved owner/name slug once CreateRepo has succeeded.
// The launcher uses this for follow-up actions (config persistence,
// SKILL.md install) when the wizard exits.
func (m WizardModel) Repo() string { return m.pushRepo }

// Skills returns the locally-discovered skills. Empty before the scan
// completes; populated once for the lifetime of the wizard.
func (m WizardModel) Skills() []scan.Skill { return m.skills }

// Visibility returns the chosen visibility ("private", "public", or "" if
// the user never landed on the visibility step).
func (m WizardModel) Visibility() string { return m.visibility }

// Pushed returns the count of skills that were uploaded by the push step.
func (m WizardModel) Pushed() int { return m.pushed }

// AgentsInstalled returns the number of agent dot-folders that received
// the skills-registry SKILL.md. Zero before step 5 completes.
func (m WizardModel) AgentsInstalled() int { return len(m.agentPaths) }

// CleanupDeleted returns the number of local entries removed by step 6.
// Zero when the cleanup step was skipped (no entries to remove or the
// user chose "no").
func (m WizardModel) CleanupDeleted() int { return m.cleanupDeleted }

// ────────────────────────────────────────────────────────────────────────────
// Update — message dispatch
// ────────────────────────────────────────────────────────────────────────────

// Update implements tea.Model. Long handlers are extracted into helpers so
// every case stays well under the gocyclo ceiling.
func (m WizardModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if mm, cmd, handled := m.dispatchStandardMsg(msg); handled {
		return mm, cmd
	}
	if mm, cmd, handled := m.dispatchAsyncMsg(msg); handled {
		return mm, cmd
	}
	if key, ok := msg.(tea.KeyMsg); ok {
		return m.handleKey(key)
	}
	// The textinput on the RepoName step ignores anything it didn't
	// expect, but we still forward unknown messages so its cursor-blink
	// command resolves cleanly.
	if m.step == WizardStepRepoName && !m.cancelOverlay && !m.transitioning {
		var cmd tea.Cmd
		m.repoInput, cmd = m.repoInput.Update(msg)
		return m, cmd
	}
	return m, nil
}

// dispatchStandardMsg routes infrastructure-level messages (resize,
// sparkle/spinner ticks, transition completion). Returns handled=true
// when the message was consumed.
func (m WizardModel) dispatchStandardMsg(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil, true
	case sparkleTickMsg:
		m.sparkleIdx++
		return m, sparkleTick(), true
	case spinner.TickMsg:
		mm, cmd := m.handleSpinnerTick(msg)
		return mm, cmd, true
	case wizardTransitionMsg:
		mm, cmd := m.handleTransitionDone(msg)
		return mm, cmd, true
	}
	return m, nil, false
}

// dispatchAsyncMsg routes long-operation messages (scan / push / agent
// install / cleanup / MCP snippet).
func (m WizardModel) dispatchAsyncMsg(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
	switch msg := msg.(type) {
	case wizardScanDoneMsg:
		mm, cmd := m.handleScanDone(msg)
		return mm, cmd, true
	case wizardScanRevealMsg:
		mm, cmd := m.handleScanReveal()
		return mm, cmd, true
	case wizardPushProgressMsg:
		mm, cmd := m.handlePushProgress(msg)
		return mm, cmd, true
	case wizardPushDoneMsg:
		mm, cmd := m.handlePushDone(msg)
		return mm, cmd, true
	case wizardAgentInstallDoneMsg:
		mm, cmd := m.handleAgentInstallDone(msg)
		return mm, cmd, true
	case wizardCleanupLoadedMsg:
		mm, cmd := m.handleCleanupLoaded(msg)
		return mm, cmd, true
	case wizardCleanupDoneMsg:
		mm, cmd := m.handleCleanupDone(msg)
		return mm, cmd, true
	case wizardMCPDoneMsg:
		mm, cmd := m.handleMCPDone(msg)
		return mm, cmd, true
	}
	return m, nil, false
}

// handleSpinnerTick keeps the spinner ticking whenever the model is doing
// async work the user is waiting on. Idle ticks would burn CPU for no gain.
func (m WizardModel) handleSpinnerTick(msg spinner.TickMsg) (tea.Model, tea.Cmd) {
	if !m.spinnerActive() {
		return m, nil
	}
	var cmd tea.Cmd
	m.spinner, cmd = m.spinner.Update(msg)
	return m, cmd
}

// spinnerActive returns true while the spinner glyph should keep animating.
// Inter-step transitions, an in-flight scan, push, agent install, and
// cleanup all qualify. Step 7 (MCP snippet) renders synchronously, so
// it has no spinner state.
func (m WizardModel) spinnerActive() bool {
	switch {
	case m.transitioning:
		return true
	case m.step == WizardStepScan && !m.scanDone:
		return true
	case m.step == WizardStepPush && !m.pushDone:
		return true
	case m.step == WizardStepAgentSelect && m.agentInstalling && !m.agentInstallDone:
		return true
	case m.step == WizardStepCleanup && m.cleanupRunning:
		return true
	}
	return false
}

// handleTransitionDone clears the transition flag, swaps the step, and
// triggers any auto-start work for the newly active step.
func (m WizardModel) handleTransitionDone(msg wizardTransitionMsg) (tea.Model, tea.Cmd) {
	m.transitioning = false
	m.step = msg.to
	return m, m.onEnterStep()
}

// onEnterStep schedules side effects bound to entering a particular step.
// Used to auto-start the push goroutine on landing in WizardStepPush, to
// load the agent multi-select rows on step 5, to scan cleanup candidates
// on step 6, and to snapshot the hosted-MCP snippet on step 7.
func (m *WizardModel) onEnterStep() tea.Cmd {
	switch m.step {
	case WizardStepRepoName:
		// Re-focus the textinput in case its cursor blinked off during the
		// inter-step transition.
		m.repoInput.Focus()
		return textinput.Blink
	case WizardStepPush:
		if !m.pushStarted {
			return m.startPush()
		}
	case WizardStepAgentSelect:
		if !m.agentLoaded {
			m.loadAgentChoices()
		}
	case WizardStepCleanup:
		if !m.cleanupLoaded {
			return m.startCleanupLoad()
		}
	case WizardStepMCPConnect:
		if !m.mcpStarted {
			return m.startMCPConnect()
		}
	}
	return nil
}

// handleScanDone records the scan result and starts the animated counter.
func (m WizardModel) handleScanDone(msg wizardScanDoneMsg) (tea.Model, tea.Cmd) {
	m.scanDone = true
	m.skills = msg.skills
	m.scanErr = msg.err
	if len(msg.skills) == 0 {
		// Nothing to animate — go straight to "found 0".
		return m, nil
	}
	m.scanReveal = 0
	return m, wizardScanReveal()
}

// handleScanReveal bumps the animated counter and re-fires the tick until
// it catches up to len(m.skills).
func (m WizardModel) handleScanReveal() (tea.Model, tea.Cmd) {
	if m.scanReveal >= len(m.skills) {
		return m, nil
	}
	m.scanReveal++
	if m.scanReveal >= len(m.skills) {
		return m, nil
	}
	return m, wizardScanReveal()
}

// handlePushProgress records a single (done, total, status) tick and
// re-arms the channel listener.
func (m WizardModel) handlePushProgress(msg wizardPushProgressMsg) (tea.Model, tea.Cmd) {
	if msg.total > 0 {
		m.pushTotalFiles = msg.total
		m.pushDoneFiles = msg.done
	}
	if msg.status != "" {
		m.pushStatus = msg.status
	}
	return m, m.waitForPush()
}

// handlePushDone records the terminal push result. No re-arm — the goroutine
// is gone after this message.
func (m WizardModel) handlePushDone(msg wizardPushDoneMsg) (tea.Model, tea.Cmd) {
	m.pushDone = true
	m.pushErr = msg.err
	m.pushed = msg.pushed
	if msg.repo != "" {
		m.pushRepo = msg.repo
	}
	if msg.err == nil && m.pushTotalFiles > 0 {
		m.pushDoneFiles = m.pushTotalFiles
	}
	return m, nil
}

// handleKey routes keystrokes between the cancel overlay and the per-step
// handlers. Ctrl+C is always a hard exit (terminal escape hatch).
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

// handleStepKey routes by step. Esc opens the cancel overlay everywhere
// except during a transition, when it's swallowed (we don't want the user
// to abandon mid-animation).
func (m WizardModel) handleStepKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "esc" && !m.transitioning {
		m.cancelOverlay = true
		m.cancelCursor = 0
		return m, nil
	}
	switch m.step {
	case WizardStepScan:
		return m.handleScanKey(msg)
	case WizardStepRepoName:
		return m.handleRepoNameKey(msg)
	case WizardStepVisibility:
		return m.handleVisibilityKey(msg)
	case WizardStepPush:
		return m.handlePushKey(msg)
	case WizardStepAgentSelect:
		return m.handleAgentSelectKey(msg)
	case WizardStepCleanup:
		return m.handleCleanupKey(msg)
	case WizardStepMCPConnect:
		return m.handleMCPKey(msg)
	case WizardStepDone:
		return m.handleDoneKey(msg)
	}
	return m.handleDefaultKey(msg)
}

// handleScanKey advances to RepoName once the scan has finished. Enter
// before scanDone is a no-op — the user shouldn't be racing the spinner.
// Without a Scan dep wired (test mode), enter advances immediately so the
// state machine stays exercisable.
func (m WizardModel) handleScanKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() != "enter" {
		return m, nil
	}
	if !m.scanDone && m.deps.Scan != nil {
		return m, nil
	}
	return m.advanceStep()
}

// handleRepoNameKey forwards typing to the textinput and treats enter as
// "validate and advance". An empty value blocks advance and surfaces an
// inline error.
func (m WizardModel) handleRepoNameKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "enter" {
		if strings.TrimSpace(m.repoInput.Value()) == "" {
			m.repoErr = "Repository name can't be empty."
			return m, nil
		}
		m.repoErr = ""
		return m.advanceStep()
	}
	var cmd tea.Cmd
	m.repoInput, cmd = m.repoInput.Update(msg)
	m.repoErr = ""
	return m, cmd
}

// handleVisibilityKey moves the card cursor and locks in the choice on
// enter. We accept both arrow keys and h/l for vim users — same convention
// the list TUI uses.
func (m WizardModel) handleVisibilityKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "left", "h":
		m.visCursor = 0
		return m, nil
	case "right", "l":
		m.visCursor = 1
		return m, nil
	case "enter":
		if m.visCursor == 1 {
			m.visibility = "public"
		} else {
			m.visibility = "private"
		}
		return m.advanceStep()
	}
	return m, nil
}

// handlePushKey advances once the push has resolved. Enter before pushDone
// is swallowed; the panel makes it clear the user is waiting.
func (m WizardModel) handlePushKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() != "enter" {
		return m, nil
	}
	if !m.pushDone {
		return m, nil
	}
	if m.pushErr != nil {
		// Push failed — let enter cancel cleanly rather than steamrolling
		// into agent install with a broken registry.
		m.cancelled = true
		return m, tea.Quit
	}
	return m.advanceStep()
}

// handleDefaultKey is the fallback "enter advances" path for any step
// without its own handler. Every wired step has one, so this is a safety
// net rather than a hot path.
func (m WizardModel) handleDefaultKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() != "enter" {
		return m, nil
	}
	return m.advanceStep()
}

// advanceStep moves to the next step (or completes the wizard when on Done)
// after a short transition animation.
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

// handleCancelKey runs the keymap while the cancel-confirmation overlay
// is up. Left/right (and h/l) move the cursor; enter confirms; "n" or esc
// dismiss; "y" is a shortcut for "yes, cancel".
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
// Commands — side-effecting tea.Cmd factories
// ────────────────────────────────────────────────────────────────────────────

// startScan calls deps.Scan in a goroutine and returns a tea.Cmd that
// posts the result back to the model. Note: Init() doesn't return a
// model, so any state mutations here would be lost; we close over ctx
// and the callback instead.
func (m WizardModel) startScan() tea.Cmd {
	if m.deps.Scan == nil {
		return nil
	}
	ctx := m.ctx
	scanFn := m.deps.Scan
	return func() tea.Msg {
		skills, err := scanFn(ctx)
		return wizardScanDoneMsg{skills: skills, err: err}
	}
}

// startPush spawns the push goroutine and returns a tea.Cmd that listens
// for the first message it emits. Subsequent messages are awaited by
// re-arming `waitForPush` from each progress handler.
func (m *WizardModel) startPush() tea.Cmd {
	m.pushStarted = true
	ch := make(chan tea.Msg, 128)
	m.pushCh = ch
	repoName := strings.TrimSpace(m.repoInput.Value())
	visibility := m.visibility
	skills := append([]scan.Skill(nil), m.skills...)
	deps := m.deps
	ctx := m.ctx
	go runPushJob(ctx, ch, deps, repoName, visibility, skills)
	return m.waitForPush()
}

// waitForPush returns a tea.Cmd that blocks on the next message in the
// push channel. Each progress handler re-arms it so we drain the channel
// without busy-looping.
func (m WizardModel) waitForPush() tea.Cmd {
	ch := m.pushCh
	if ch == nil {
		return nil
	}
	return func() tea.Msg {
		msg, ok := <-ch
		if !ok {
			return nil
		}
		return msg
	}
}

// runPushJob is the long-running worker invoked by startPush. It owns the
// channel send-side and is responsible for the terminal wizardPushDoneMsg
// regardless of which sub-step fails.
func runPushJob(ctx context.Context, ch chan<- tea.Msg, deps WizardDeps,
	repoName, visibility string, skills []scan.Skill) {
	defer close(ch)
	repo, err := pushJobCreateRepo(ctx, ch, deps, repoName, visibility)
	if err != nil {
		ch <- wizardPushDoneMsg{err: err}
		return
	}
	if deps.SaveConfig != nil {
		if err := deps.SaveConfig(repo); err != nil {
			ch <- wizardPushDoneMsg{repo: repo, err: fmt.Errorf("save config: %w", err)}
			return
		}
	}
	if deps.Push == nil || len(skills) == 0 {
		ch <- wizardPushDoneMsg{repo: repo, pushed: 0}
		return
	}
	pushed := pushJobInvokePush(ctx, ch, deps, repo, skills)
	ch <- pushed
}

// pushJobCreateRepo runs the CreateRepo callback (when wired) and reports
// status + the resolved repo slug. Returns the slug on success or "" + err.
func pushJobCreateRepo(ctx context.Context, ch chan<- tea.Msg, deps WizardDeps,
	repoName, visibility string) (string, error) {
	if deps.CreateRepo == nil {
		// No callback wired — nothing to create. Use the entered name
		// verbatim so downstream steps still have a label.
		return repoName, nil
	}
	ch <- wizardPushProgressMsg{status: "creating repo on github…"}
	repo, err := deps.CreateRepo(ctx, repoName, visibility)
	if err != nil {
		return "", fmt.Errorf("create repo: %w", err)
	}
	if repo == "" {
		repo = repoName
	}
	ch <- wizardPushProgressMsg{status: fmt.Sprintf("✓ created %s", repo)}
	return repo, nil
}

// pushJobInvokePush invokes deps.Push with progress + status callbacks
// that pump messages into ch. Returns the terminal wizardPushDoneMsg
// (sent by the caller so close-channel sequencing is deterministic).
func pushJobInvokePush(ctx context.Context, ch chan<- tea.Msg, deps WizardDeps,
	repo string, skills []scan.Skill) wizardPushDoneMsg {
	ch <- wizardPushProgressMsg{status: "uploading skills…"}
	onProgress := func(done, total int) {
		// Non-blocking send: a slow consumer doesn't hold up the push.
		select {
		case ch <- wizardPushProgressMsg{done: done, total: total}:
		default:
		}
	}
	onStatus := func(msg string) {
		select {
		case ch <- wizardPushProgressMsg{status: msg}:
		default:
		}
	}
	pushed, err := deps.Push(ctx, repo, skills, onProgress, onStatus)
	if err != nil {
		return wizardPushDoneMsg{repo: repo, err: fmt.Errorf("push skills: %w", err)}
	}
	return wizardPushDoneMsg{repo: repo, pushed: pushed}
}

// ────────────────────────────────────────────────────────────────────────────
// View — chrome and per-step rendering
// ────────────────────────────────────────────────────────────────────────────

// View implements tea.Model. The cancel overlay is centred via lipgloss.Place
// on top of the base frame, matching the help overlay in the list TUI.
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

// renderFrame stacks the four chrome sections: hero, step indicator,
// step-specific panel, and footer.
func (m WizardModel) renderFrame() string {
	return lipgloss.JoinVertical(lipgloss.Left,
		m.renderHero(),
		"",
		m.renderProgress(),
		"",
		m.renderStepPanel(),
		"",
		m.renderFooter(),
	)
}

// renderHero matches the list TUI hero: SparkleStyle bracketing a HeroStyle
// title, with the animated gradient bar below.
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

// renderProgress renders the WIZARD-012 step indicator: a row of dots
// (filled / ringed / hollow) and the "Step N / 8 · Title" caption.
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

// renderStepPanel wraps the per-step body in a rounded-border PanelFocused.
func (m WizardModel) renderStepPanel() string {
	body := m.renderStepBody()
	panel := PanelFocused
	if m.width > 6 {
		panel = panel.Width(m.width - 4)
	}
	return panel.Render(body)
}

// renderStepBody dispatches by step. The transition state shows a generic
// spinner+caption so the user sees motion between panels.
func (m WizardModel) renderStepBody() string {
	if m.transitioning {
		loading := lipgloss.NewStyle().Foreground(ColMuted).
			Render(" Loading the next step…")
		return m.spinner.View() + loading
	}
	switch m.step {
	case WizardStepScan:
		return m.renderScanBody()
	case WizardStepRepoName:
		return m.renderRepoNameBody()
	case WizardStepVisibility:
		return m.renderVisibilityBody()
	case WizardStepPush:
		return m.renderPushBody()
	case WizardStepAgentSelect:
		return m.renderAgentSelectBody()
	case WizardStepCleanup:
		return m.renderCleanupBody()
	case WizardStepMCPConnect:
		return m.renderMCPBody()
	case WizardStepDone:
		return m.renderDoneBody()
	}
	return ""
}

// renderScanBody is the WIZARD-002 step body: a spinner while scan is in
// flight, an animated counter once it resolves, and an enter CTA when the
// reveal finishes.
func (m WizardModel) renderScanBody() string {
	title := lipgloss.NewStyle().Foreground(ColPrimary).Bold(true).
		Render(m.step.Title())
	if !m.scanDone {
		line := m.spinner.View() + " " +
			lipgloss.NewStyle().Foreground(ColInk).
				Render("Looking for SKILL.md files under ~/ and the current project…")
		return lipgloss.JoinVertical(lipgloss.Left, title, "", line)
	}
	if m.scanErr != nil {
		return lipgloss.JoinVertical(lipgloss.Left, title, "",
			ErrorStyle.Render("✗ scan failed: "+m.scanErr.Error()),
			"",
			DownloadChip.Render("⏎ enter")+
				lipgloss.NewStyle().Foreground(ColMuted).Render("  continue anyway"))
	}
	revealed := m.scanReveal
	if revealed > len(m.skills) {
		revealed = len(m.skills)
	}
	headline := m.renderScanHeadline(revealed)
	preview := m.renderScanPreview(revealed)
	cta := DownloadChip.Render("⏎ enter") +
		lipgloss.NewStyle().Foreground(ColMuted).Render("  use these skills")
	if revealed < len(m.skills) {
		cta = lipgloss.NewStyle().Foreground(ColMuted).
			Render("…animating found skills")
	}
	parts := []string{title, "", headline}
	if preview != "" {
		parts = append(parts, "", preview)
	}
	parts = append(parts, "", cta)
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

// renderScanHeadline returns the "Found N skill(s)" copy, with N taking
// the animated reveal value so the eye sees the counter climb.
func (m WizardModel) renderScanHeadline(revealed int) string {
	count := lipgloss.NewStyle().
		Bold(true).
		Foreground(ColAccent).
		Render(fmt.Sprintf("%d", revealed))
	noun := "skills"
	if revealed == 1 {
		noun = "skill"
	}
	tail := lipgloss.NewStyle().Foreground(ColInk).
		Render(fmt.Sprintf(" %s discovered locally.", noun))
	if revealed == 0 {
		tail = lipgloss.NewStyle().Foreground(ColInk).
			Render(" local skills discovered — we'll create an empty registry.")
	}
	return SparkleStyle.Render("✦ ") + count + tail
}

// renderScanPreview lists up to five revealed skills, dim slugs to the
// right of the names. Keeps the panel from blowing up on big registries
// where len(skills) >> what fits.
func (m WizardModel) renderScanPreview(revealed int) string {
	if revealed == 0 {
		return ""
	}
	max := 5
	if revealed < max {
		max = revealed
	}
	var lines []string
	for i := 0; i < max; i++ {
		sk := m.skills[i]
		bullet := lipgloss.NewStyle().Foreground(ColPink).Bold(true).Render("· ✧")
		name := lipgloss.NewStyle().Foreground(ColInk).Render(sk.Name)
		slug := lipgloss.NewStyle().Foreground(ColPeach).Italic(true).
			Render("  " + sk.Slug)
		lines = append(lines, bullet+" "+name+slug)
	}
	if revealed > max {
		lines = append(lines, lipgloss.NewStyle().Foreground(ColMuted).Italic(true).
			Render(fmt.Sprintf("  … +%d more", revealed-max)))
	}
	return strings.Join(lines, "\n")
}

// renderRepoNameBody is the WIZARD-003 step body: a styled textinput inside
// the focused panel with a helper line below and an inline error when the
// user tries to submit an empty value.
func (m WizardModel) renderRepoNameBody() string {
	title := lipgloss.NewStyle().Foreground(ColPrimary).Bold(true).
		Render(m.step.Title())
	prompt := lipgloss.NewStyle().Foreground(ColInk).
		Render("Name the GitHub repo that will host your registry.")
	input := m.repoInput.View()
	hint := lipgloss.NewStyle().Foreground(ColMuted).Italic(true).
		Render("· just the repo name (no `owner/` prefix) — created on your authenticated user account.")
	cta := DownloadChip.Render("⏎ enter") +
		lipgloss.NewStyle().Foreground(ColMuted).Render("  continue · ") +
		KeyStyle.Render("esc") +
		lipgloss.NewStyle().Foreground(ColMuted).Render(" cancel")
	parts := []string{title, "", prompt, "", input, "", hint}
	if m.repoErr != "" {
		parts = append(parts, "", ErrorStyle.Render("✗ "+m.repoErr))
	}
	parts = append(parts, "", cta)
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

// renderVisibilityBody is the WIZARD-004 step body: two side-by-side
// "cards" rendered as PanelStyle (unfocused) and PanelFocused (focused),
// each carrying an icon, title, and description.
func (m WizardModel) renderVisibilityBody() string {
	title := lipgloss.NewStyle().Foreground(ColPrimary).Bold(true).
		Render(m.step.Title())
	prompt := lipgloss.NewStyle().Foreground(ColInk).
		Render("Who can see this registry?")
	cards := m.renderVisibilityCards()
	cta := lipgloss.NewStyle().Foreground(ColMuted).Render("  ") +
		KeyStyle.Render("←/→") +
		lipgloss.NewStyle().Foreground(ColMuted).Render(" switch · ") +
		DownloadChip.Render("⏎ enter") +
		lipgloss.NewStyle().Foreground(ColMuted).Render(" confirm")
	return lipgloss.JoinVertical(lipgloss.Left, title, "", prompt, "", cards, "", cta)
}

// renderVisibilityCards renders the two visibility options side-by-side.
// Cards are width-budgeted from the outer wizard panel's inner content
// width (`m.width - 8` = m.width minus the outer panel's border+padding);
// in very narrow terminals they stack vertically so the body stays
// readable.
func (m WizardModel) renderVisibilityCards() string {
	cards := []visibilityCard{
		{
			icon:    "🔒",
			title:   "Private",
			tagline: "Only you can see and clone the registry.",
			detail:  "Recommended — keeps in-progress and personal skills out of view.",
		},
		{
			icon:    "🌐",
			title:   "Public",
			tagline: "Visible to anyone with the URL.",
			detail:  "Share your library with the community or other accounts.",
		},
	}
	// Inner content width of the outer wizard panel.
	innerWidth := m.width - 8
	if innerWidth < 40 {
		innerWidth = 40
	}
	// Each card's rendered width adds 4 cols of border+padding, and we
	// reserve 3 cols between the two cards. Solve for the per-card
	// content width.
	const gap = 3
	const cardChrome = 4
	cardWidth := (innerWidth-gap)/2 - cardChrome
	if cardWidth < 22 {
		// Narrow terminal — stack the cards instead of squeezing them.
		left := m.renderVisibilityCard(cards[0], m.visCursor == 0, innerWidth-cardChrome)
		right := m.renderVisibilityCard(cards[1], m.visCursor == 1, innerWidth-cardChrome)
		return lipgloss.JoinVertical(lipgloss.Left, left, "", right)
	}
	left := m.renderVisibilityCard(cards[0], m.visCursor == 0, cardWidth)
	right := m.renderVisibilityCard(cards[1], m.visCursor == 1, cardWidth)
	return lipgloss.JoinHorizontal(lipgloss.Top, left, strings.Repeat(" ", gap), right)
}

// visibilityCard is the data for a single visibility tile.
type visibilityCard struct {
	icon, title, tagline, detail string
}

// renderVisibilityCard renders one of the two visibility tiles. Focused
// cards use PanelFocused + brighter text, unfocused use PanelStyle and
// dim text so the focus state is unambiguous on light and dark terminals.
func (m WizardModel) renderVisibilityCard(c visibilityCard, focused bool, width int) string {
	panel := PanelStyle
	titleColor := ColInk
	detailColor := ColMuted
	if focused {
		panel = PanelFocused
		titleColor = ColPrimary
		detailColor = ColInk
	}
	head := lipgloss.JoinHorizontal(lipgloss.Top,
		lipgloss.NewStyle().Render(c.icon),
		"  ",
		lipgloss.NewStyle().Foreground(titleColor).Bold(true).Render(c.title),
	)
	tagline := lipgloss.NewStyle().Foreground(detailColor).
		Render(c.tagline)
	detail := lipgloss.NewStyle().Foreground(ColMuted).Italic(true).
		Render(c.detail)
	chip := ""
	if focused {
		chip = ChipPrimary.Render("◆ selected")
	} else {
		chip = lipgloss.NewStyle().Foreground(ColFaint).Render("◇ available")
	}
	body := lipgloss.JoinVertical(lipgloss.Left, head, "", tagline, "", detail, "", chip)
	return panel.Width(width).Render(body)
}

// renderPushBody is the WIZARD-005 step body: a status caption, an animated
// progress bar, and the post-push CTA (or error and cancel hint).
func (m WizardModel) renderPushBody() string {
	title := lipgloss.NewStyle().Foreground(ColPrimary).Bold(true).
		Render(m.step.Title())
	repo := strings.TrimSpace(m.repoInput.Value())
	if m.pushRepo != "" {
		repo = m.pushRepo
	}
	intro := m.renderPushIntro(repo)
	status := m.renderPushStatus()
	bar := m.renderPushProgress()
	cta := m.renderPushCTA()
	parts := []string{title, "", intro}
	if status != "" {
		parts = append(parts, "", status)
	}
	parts = append(parts, "", bar, "", cta)
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

// renderPushIntro frames the operation: how many skills, going where.
func (m WizardModel) renderPushIntro(repo string) string {
	repoChip := lipgloss.NewStyle().Foreground(ColPrimary).Bold(true).Render(repo)
	visChip := ChipAccent.Render(m.visibility)
	count := lipgloss.NewStyle().Foreground(ColAccent).Bold(true).
		Render(fmt.Sprintf("%d", len(m.skills)))
	noun := "skills"
	if len(m.skills) == 1 {
		noun = "skill"
	}
	return lipgloss.JoinHorizontal(lipgloss.Top,
		repoChip,
		KeySepStyle.Render("  ·  "),
		visChip,
		KeySepStyle.Render("  ·  "),
		count,
		lipgloss.NewStyle().Foreground(ColInk).Render(" "+noun+" → github"),
	)
}

// renderPushStatus surfaces the latest status string from the push job
// (e.g. "creating repo on github…", "pushing to github…").
func (m WizardModel) renderPushStatus() string {
	if m.pushStatus == "" {
		if !m.pushDone {
			return lipgloss.NewStyle().Foreground(ColMuted).Italic(true).
				Render("· waiting for the repo to come online…")
		}
		return ""
	}
	style := lipgloss.NewStyle().Foreground(ColPeach).Italic(true)
	if m.pushDone && m.pushErr == nil {
		style = lipgloss.NewStyle().Foreground(ColAccent).Bold(true)
	}
	return m.spinner.View() + " " + style.Render(m.pushStatus)
}

// renderPushProgress renders the animated progress bar. Width tracks the
// wizard frame; gradient colors cycle with the global sparkle phase so the
// bar reads as "alive" even when the upstream callback isn't firing yet.
func (m WizardModel) renderPushProgress() string {
	width := m.pushProgressWidth()
	done, total := m.pushDoneFiles, m.pushTotalFiles
	if m.pushDone && m.pushErr == nil {
		// Force a fully-filled bar so the user sees a clean "done".
		if total <= 0 {
			total = 1
		}
		done = total
	}
	bar := renderProgressBar(done, total, width, m.sparkleIdx)
	caption := m.renderPushCaption(done, total)
	return lipgloss.JoinVertical(lipgloss.Left, bar, caption)
}

// renderPushCaption renders the "done/total files" line under the bar.
func (m WizardModel) renderPushCaption(done, total int) string {
	if m.pushDone && m.pushErr == nil {
		return lipgloss.NewStyle().Foreground(ColAccent).Bold(true).
			Render(fmt.Sprintf("✓ pushed %d skill(s).", m.pushed))
	}
	if m.pushDone && m.pushErr != nil {
		return ErrorStyle.Render("✗ " + m.pushErr.Error())
	}
	if total == 0 {
		return lipgloss.NewStyle().Foreground(ColMuted).Italic(true).
			Render("preparing files…")
	}
	pct := done * 100 / total
	return lipgloss.NewStyle().Foreground(ColMuted).
		Render(fmt.Sprintf("uploaded %d / %d files · %d%%", done, total, pct))
}

// renderPushCTA picks the right post-state hint for the bottom of the
// push panel.
func (m WizardModel) renderPushCTA() string {
	if !m.pushDone {
		return lipgloss.NewStyle().Foreground(ColMuted).Italic(true).
			Render("· pushing to github — please wait.")
	}
	if m.pushErr != nil {
		return DownloadChip.Render("⏎ enter") +
			lipgloss.NewStyle().Foreground(ColDanger).
				Render("  exit the wizard")
	}
	return DownloadChip.Render("⏎ enter") +
		lipgloss.NewStyle().Foreground(ColAccent).Bold(true).
			Render("  continue to agent install")
}

// pushProgressWidth picks a reasonable width for the progress bar from the
// wizard frame. Clamped between 20 and 60 so the bar reads cleanly without
// dominating the panel on wide terminals.
func (m WizardModel) pushProgressWidth() int {
	w := m.width - 14
	if w > 60 {
		w = 60
	}
	if w < 20 {
		w = 20
	}
	return w
}

// renderFooter mirrors the list TUI footer.
func (m WizardModel) renderFooter() string {
	keys := m.footerKeys()
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

// footerKeys returns the keybindings to surface at the current step. The
// keymap follows the existing TUI conventions: enter advances, esc cancels,
// arrow keys navigate where they're meaningful.
func (m WizardModel) footerKeys() []struct{ k, d string } {
	switch m.step {
	case WizardStepVisibility:
		return []struct{ k, d string }{
			{"←/→", "switch"},
			{"enter", "confirm"},
			{"esc", "cancel"},
		}
	case WizardStepRepoName:
		return []struct{ k, d string }{
			{"type", "name"},
			{"enter", "continue"},
			{"esc", "cancel"},
		}
	case WizardStepAgentSelect:
		if m.agentInstalling || m.agentInstallDone {
			return []struct{ k, d string }{
				{"enter", "continue"},
				{"esc", "cancel"},
			}
		}
		return []struct{ k, d string }{
			{"space", "toggle"},
			{"tab", "select all"},
			{"enter", "install"},
			{"esc", "cancel"},
		}
	case WizardStepCleanup:
		if m.cleanupChosen {
			return []struct{ k, d string }{
				{"enter", "continue"},
				{"esc", "cancel"},
			}
		}
		return []struct{ k, d string }{
			{"←/→", "choose"},
			{"enter", "confirm"},
			{"esc", "cancel"},
		}
	case WizardStepDone:
		return []struct{ k, d string }{
			{"enter", "open the hub"},
		}
	}
	return []struct{ k, d string }{
		{"enter", "next"},
		{"esc", "cancel"},
	}
}

// renderCancelOverlay is the WIZARD-013 confirmation panel.
func (m WizardModel) renderCancelOverlay() string {
	title := ErrorStyle.Render("Cancel onboarding?")
	bodyText := "Nothing has been written to GitHub yet.\n" +
		"You can restart any time with `skills-registry`."
	if m.pushDone {
		bodyText = "Your registry has already been created on GitHub.\n" +
			"You can restart any time with `skills-registry`."
	}
	body := lipgloss.NewStyle().Foreground(ColInk).Render(bodyText)
	keepBtn := renderOverlayButton("No, keep going", m.cancelCursor == 0, false)
	cancelBtn := renderOverlayButton("Yes, cancel", m.cancelCursor == 1, true)
	buttons := lipgloss.JoinHorizontal(lipgloss.Top, keepBtn, "   ", cancelBtn)
	hint := SubtitleStyle.Render("←/→ choose · enter confirm · n / esc keep going")
	inner := lipgloss.JoinVertical(lipgloss.Left, title, "", body, "", buttons, "", hint)
	return HelpOverlay.Render(inner)
}

// renderOverlayButton renders one option in the cancel overlay.
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

// renderProgressBar paints a `width`-cell bar with `done/total` cells filled
// using a sparkle-phase-shifted palette. Unfilled cells use the faint
// adaptive color so the bar reads on both light and dark terminals.
func renderProgressBar(done, total, width, phase int) string {
	if width <= 0 {
		return ""
	}
	if total <= 0 {
		// Render an "indeterminate" bar that uses the phase to slide a
		// small lit block along the track. Better than rendering an empty
		// bar that suggests the work isn't progressing.
		var b strings.Builder
		litStart := phase % width
		for i := 0; i < width; i++ {
			if i >= litStart && i < litStart+4 {
				c := indeterminateColor(i - litStart)
				b.WriteString(lipgloss.NewStyle().Foreground(c).Render("▓"))
			} else {
				b.WriteString(lipgloss.NewStyle().Foreground(ColFaint).Render("░"))
			}
		}
		return b.String()
	}
	if done > total {
		done = total
	}
	filled := done * width / total
	palette := []lipgloss.AdaptiveColor{ColPrimary, ColPink, ColPeach, ColAccent, ColCyan}
	var b strings.Builder
	for i := 0; i < width; i++ {
		if i < filled {
			c := palette[(i+phase)%len(palette)]
			b.WriteString(lipgloss.NewStyle().Foreground(c).Render("█"))
		} else {
			b.WriteString(lipgloss.NewStyle().Foreground(ColFaint).Render("░"))
		}
	}
	return b.String()
}

// indeterminateColor cycles through a small palette so the moving block
// in an indeterminate bar visibly shimmers.
func indeterminateColor(i int) lipgloss.AdaptiveColor {
	palette := []lipgloss.AdaptiveColor{ColPrimary, ColPink, ColPeach, ColAccent}
	if i < 0 {
		i = -i
	}
	return palette[i%len(palette)]
}
