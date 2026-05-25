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
}

type ManageFlowDeps struct {
	Rows     RowLoader
	Download Downloader
	Delete   Deleter
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
		flow := NewList(m.ctx, m.deps.Repo, m.deps.Manage.Rows, m.deps.Manage.Download).
			WithDeleter(m.deps.Manage.Delete).
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
	if m.SavedPath() != "" {
		return flowExitMsg{toast: "✓ settings saved → " + m.SavedPath(), ok: true}
	}
	return flowExitMsg{toast: "settings · closed", ok: true}
}

func flattenErr(err error) string {
	if err == nil {
		return ""
	}
	return strings.ReplaceAll(err.Error(), "\n", " · ")
}
