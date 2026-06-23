package tui

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

type HubDeps struct {
	Repo     string
	Count    HubCountLoader
	Manage   ManageFlowDeps
	Settings SettingsFlowDeps
	Add      AddFlowDeps
	Publish  PublishFlowDeps
	Sync     SyncFlowDeps
	Purge    PurgeFlowDeps

	// Reload rebuilds the full dependency set for a newly-saved
	// repo/branch so a Settings change takes effect without restarting
	// the program. Every flow's deps (and the count loader) capture the
	// repo at construction time, so swapping a single field isn't enough
	// — the whole set has to be rebuilt from the new config. The cmd
	// layer wires this to buildHubDeps; when nil (tests), exitFlow still
	// refreshes the header repo + Settings deps from the saved values so
	// the visible state matches what was written to disk.
	Reload func(repo, branch string) HubDeps
}

type ManageFlowDeps struct {
	Rows           RowLoader
	Install        Installer
	InstallTargets InstallTargetLoader
	Delete         Deleter
}

type SettingsFlowDeps struct {
	Repo      string
	Branch    string
	CacheRoot string
	HostedMCP string
	Save      SettingsSaver
}

type HubProgram struct {
	ctx           context.Context
	deps          HubDeps
	hub           HubModel
	flow          tea.Model
	width, height int
}

type flowExitMsg struct {
	toast string
	ok    bool

	// repo/branch carry a newly-persisted Settings change back to the
	// HubProgram so the dashboard chrome and every embedded flow pick up
	// the new registry live. An empty repo means "no config change" —
	// the case for every non-settings flow exit.
	repo   string
	branch string
}

func flowExitCmd(toast string, ok bool) tea.Cmd {
	return func() tea.Msg { return flowExitMsg{toast: toast, ok: ok} }
}

func NewHubProgram(ctx context.Context, deps HubDeps) HubProgram {
	repo := deps.Repo
	if repo == "" {
		repo = deps.Settings.Repo
	}
	deps.Repo = repo
	return HubProgram{
		ctx:  ctx,
		deps: deps,
		hub:  NewHub(ctx, repo, deps.Count),
	}
}

func (m HubProgram) Init() tea.Cmd {
	return m.hub.Init()
}

func (m HubProgram) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		return m.handleResize(msg)
	case hubLaunchMsg:
		return m.launchFlow(msg.action)
	case flowExitMsg:
		return m.exitFlow(msg)
	}
	if m.flow != nil {
		return m.updateFlow(msg)
	}
	return m.updateHub(msg)
}

func (m HubProgram) View() string {
	if m.flow != nil {
		return m.flow.View()
	}
	return m.hub.View()
}

func (m HubProgram) handleResize(msg tea.WindowSizeMsg) (tea.Model, tea.Cmd) {
	m.width, m.height = msg.Width, msg.Height
	hubModel, hubCmd := m.hub.Update(msg)
	m.hub = hubModel.(HubModel)
	if m.flow == nil {
		return m, hubCmd
	}
	flowModel, flowCmd := m.flow.Update(msg)
	m.flow = flowModel
	return m, tea.Batch(hubCmd, flowCmd)
}

func (m HubProgram) updateHub(msg tea.Msg) (tea.Model, tea.Cmd) {
	next, cmd := m.hub.Update(msg)
	m.hub = next.(HubModel)
	return m, cmd
}

func (m HubProgram) updateFlow(msg tea.Msg) (tea.Model, tea.Cmd) {
	next, cmd := m.flow.Update(msg)
	m.flow = next
	return m, cmd
}

func (m HubProgram) launchFlow(action string) (tea.Model, tea.Cmd) {
	flow, cmd := m.newFlow(action)
	if flow == nil {
		return m.exitFlow(flowExitMsg{
			toast: fmt.Sprintf("✗ unknown action: %s", action),
			ok:    false,
		})
	}
	m.flow = flow
	cmds := []tea.Cmd{cmd}
	if m.width > 0 || m.height > 0 {
		next, resizeCmd := m.flow.Update(tea.WindowSizeMsg{Width: m.width, Height: m.height})
		m.flow = next
		cmds = append(cmds, resizeCmd)
	}
	return m, tea.Batch(cmds...)
}

func (m HubProgram) newFlow(action string) (tea.Model, tea.Cmd) {
	switch action {
	case HubActionManage, HubActionBrowse, HubActionRemove:
		flow := NewList(m.ctx, m.deps.Repo, m.deps.Manage.Rows, m.deps.Manage.Install).
			WithDeleter(m.deps.Manage.Delete).
			WithInstallTargets(m.deps.Manage.InstallTargets).
			WithOnExit(listFlowExit)
		return flow, flow.Init()
	case HubActionSync:
		flow := NewSyncFlow(m.ctx, m.deps.Repo, m.deps.Sync)
		return flow, flow.Init()
	case HubActionAdd:
		flow := NewAddFlow(m.ctx, m.deps.Repo, m.deps.Add)
		return flow, flow.Init()
	case HubActionPublish:
		flow := NewPublishFlow(m.ctx, m.deps.Publish)
		return flow, flow.Init()
	case HubActionPurge:
		flow := NewPurgeFlow(m.ctx, m.deps.Purge)
		return flow, flow.Init()
	case HubActionSettings:
		flow := NewSettings(
			m.deps.Settings.Repo,
			m.deps.Settings.Branch,
			m.deps.Settings.CacheRoot,
			m.deps.Settings.HostedMCP,
			m.deps.Settings.Save,
		).WithOnExit(settingsFlowExit)
		return flow, flow.Init()
	}
	return nil, nil
}

func (m HubProgram) exitFlow(msg flowExitMsg) (tea.Model, tea.Cmd) {
	text := strings.TrimSpace(msg.toast)
	m.flow = nil
	if msg.repo != "" {
		m.applyConfigChange(msg.repo, msg.branch)
	}
	m.hub = m.hub.WithSelectionReset()
	if text != "" {
		m.hub = m.hub.WithToast(text, msg.ok)
	}
	if m.hub.loader == nil {
		return m, nil
	}
	m.hub.countLoaded = false
	m.hub.countErr = nil
	return m, tea.Batch(runHubCountLoader(m.ctx, m.hub.loader), m.hub.spinner.Tick)
}

// applyConfigChange swaps in the dependency set for a freshly-saved
// repo/branch so the dashboard chrome and every embedded flow target the
// new registry without a restart. With a Reload hook wired (production)
// it rebuilds the whole set — including the skill-count loader — from the
// new config; without one (tests) it still refreshes the header repo and
// Settings deps so the visible state matches what was written to disk.
// The hub is rebuilt afterwards so the header repo chip and count loader
// track the new registry; the terminal size and sparkle phase captured so
// far are carried over so the first post-save frame doesn't reflow or
// stutter.
func (m *HubProgram) applyConfigChange(repo, branch string) {
	if m.deps.Reload != nil {
		m.deps = m.deps.Reload(repo, branch)
	} else {
		m.deps.Repo = repo
		m.deps.Settings.Repo = repo
		m.deps.Settings.Branch = branch
	}
	hub := NewHub(m.ctx, m.deps.Repo, m.deps.Count)
	hub.width, hub.height = m.hub.width, m.hub.height
	hub.sparkleIdx = m.hub.sparkleIdx
	m.hub = hub
}

func listFlowExit(m ListModel) tea.Msg {
	if m.toast != "" {
		return flowExitMsg{toast: m.toast, ok: m.toastOK}
	}
	return flowExitMsg{toast: "manage skills · closed", ok: true}
}

func settingsFlowExit(m SettingsModel) tea.Msg {
	if err := m.SaveError(); err != nil {
		return flowExitMsg{toast: "✗ settings: " + flattenErr(err), ok: false}
	}
	if path := m.SavedPath(); path != "" {
		// origRepo/origBranch hold the values actually written to disk —
		// save() advances them only on a successful write, so an
		// edit-after-save the user never persisted (esc still commits the
		// input) doesn't leak into the live reload. Branch is already
		// defaulted to "main" by save(), matching config.Save on disk.
		return flowExitMsg{
			toast:  "✓ settings saved → " + path,
			ok:     true,
			repo:   m.origRepo,
			branch: m.origBranch,
		}
	}
	return flowExitMsg{toast: "settings · closed", ok: true}
}

func flattenErr(err error) string {
	if err == nil {
		return ""
	}
	return strings.ReplaceAll(err.Error(), "\n", " · ")
}
